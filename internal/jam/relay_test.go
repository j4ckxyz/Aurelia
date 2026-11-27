package jam

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

// fakeRelay is a relay as relay/ is, in little: one room, which the
// host's connection opens, and guests' tunnels to it, whose bytes it
// passes on. evil has it answer tunnels itself, with a key of its own.
type fakeRelay struct {
	mu   sync.Mutex
	room string
	pin  string
	sess *yamux.Session
	key  string
	evil bool
}

func (f *fakeRelay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v2/host":
		if r.Header.Get("Authorization") != "Bearer "+f.key {
			http.Error(w, "the relay's key is wrong", http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		cfg := yamux.DefaultConfig()
		cfg.LogOutput = io.Discard
		sess, _ := yamux.Client(websocket.NetConn(context.Background(), ws, websocket.MessageBinary), cfg)
		f.mu.Lock()
		f.room, f.pin, f.sess = r.URL.Query().Get("room"), r.URL.Query().Get("pin"), sess
		f.mu.Unlock()
	case r.URL.Path == "/v2/lookup":
		f.mu.Lock()
		room, pin := f.room, f.pin
		f.mu.Unlock()
		io.WriteString(w, `{"room":"`+room+`","pin":"`+pin+`"}`)
	case strings.HasPrefix(r.URL.Path, "/v2/r/"):
		f.mu.Lock()
		sess, room := f.sess, f.room
		f.mu.Unlock()
		if sess == nil || r.URL.Path != "/v2/r/"+room {
			http.Error(w, "no Jam is here", http.StatusNotFound)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
		defer conn.Close()
		if f.evil {
			// The relay, as the host: what the guest says is its own.
			cert, _ := newCert()
			tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{*cert}})
			tc.Handshake()
			tc.Close()
			return
		}
		stream, err := sess.Open()
		if err != nil {
			return
		}
		defer stream.Close()
		go io.Copy(stream, conn)
		io.Copy(conn, stream)
	default:
		http.NotFound(w, r)
	}
}

// A host reached through a relay: a guest joins it there, with its code,
// and plays its songs through it; the relay's own address vouches for
// nobody, as a relay on the tailnet would for whoever came through it.
func TestRelayJoin(t *testing.T) {
	relay := &fakeRelay{key: "the-relay-key-here"}
	rs := httptest.NewServer(relay)
	defer rs.Close()
	var asked sync.Mutex
	var requests []Request
	h := newHost(t, func(h *Host) {
		h.Identify = func(_ context.Context, remote string) *Peer {
			if remote == "relay" {
				return nil
			}
			return &Peer{User: "Ada", Device: "Studio", Mine: true}
		}
		h.OnRequest = func(r Request) {
			asked.Lock()
			requests = append(requests, r)
			asked.Unlock()
			h.Deny(r.ID)
		}
		h.Media = func(ctx context.Context, key string) (io.ReadCloser, int64, error) {
			return io.NopCloser(strings.NewReader("the song " + key)), -1, nil
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrs := make(chan string, 4)
	go h.ServeRelay(ctx, rs.URL+"/", "wrong-key-wrong-key", func(addr string, err error) {
		if addr != "" {
			t.Error("a wrong key is let in")
		}
		var re *RelayError
		if err != nil && (!asErr(err, &re) || re.Status != http.StatusUnauthorized) {
			t.Errorf("the refusal: %v", err)
		}
		addrs <- ""
	})
	if <-addrs != "" {
		t.Fatal("a wrong key")
	}
	cancel()
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go h.ServeRelay(ctx, rs.URL, relay.key, func(addr string, err error) { addrs <- addr })
	via := <-addrs
	if !strings.HasPrefix(via, rs.URL+"/v2/r/") || !IsRelayAddr(via) || !strings.Contains(via, "#") {
		t.Fatalf("the Jam is at %q", via)
	}
	// Without the code, though the relay's address is one Identify knows.
	if _, err := Join(context.Background(), via, Hello{Name: "Bo"}, Handlers{}); err == nil {
		t.Fatal("in through the relay without the code")
	}
	asked.Lock()
	if len(requests) != 1 || requests[0].Peer != nil {
		t.Errorf("asked: %+v", requests)
	}
	asked.Unlock()
	// With it.
	found, err := LookupRelay(context.Background(), rs.URL, "k7q x4r")
	if err != nil || found != via {
		t.Fatalf("looked up: %q %v", found, err)
	}
	g, err := Join(context.Background(), found, Hello{Name: "Bo", Code: h.Code()}, Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Leave()
	waitFor(t, "Bo in", func() bool { return len(h.Members()) == 1 })
	resp, err := client.Get(g.MediaURL("srvA/s1"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "the song srvA/s1" {
		t.Errorf("the song through the relay: %q", b)
	}
	// An address without the fingerprint has it from the relay.
	bare, _, _ := strings.Cut(via, "#")
	g2, err := Join(context.Background(), bare, Hello{Name: "Cy", Code: h.Code()}, Handlers{})
	if err != nil {
		t.Fatalf("without the fingerprint: %v", err)
	}
	g2.Leave()
	// With another's fingerprint, the host is not who answers: nothing is
	// said to it, not even the code.
	other := bare + "#" + strings.Repeat("A", 43)
	if _, err := Join(context.Background(), other, Hello{Name: "Di", Code: h.Code()}, Handlers{}); err == nil || !strings.Contains(err.Error(), ErrNotHost.Error()) {
		t.Errorf("another's fingerprint: %v", err)
	}
	// A relay that answers as the host, which it is not.
	relay.mu.Lock()
	relay.evil = true
	relay.mu.Unlock()
	if _, err := Join(context.Background(), via, Hello{Name: "Ed", Code: h.Code()}, Handlers{}); err == nil || !strings.Contains(err.Error(), ErrNotHost.Error()) {
		t.Errorf("a relay as the host: %v", err)
	}
}

func asErr(err error, target **RelayError) bool {
	re, ok := err.(*RelayError)
	if ok {
		*target = re
	}
	return ok
}

// An invite names the relay too; what was typed, pasted or followed is
// read back.
func TestParseTarget(t *testing.T) {
	via := "https://relay.example.com/v2/r/0123456789abcdef0123456789abcdef#" + strings.Repeat("A", 43)
	inv := InviteVia("192.168.1.20:47475", "K7QX4R", via)
	cases := []struct {
		in   string
		want Target
		ok   bool
	}{
		{inv, Target{Addr: "192.168.1.20:47475", Via: via, Code: "K7QX4R"}, true},
		{Invite("192.168.1.20:47475", "K7QX4R"), Target{Addr: "192.168.1.20:47475", Code: "K7QX4R"}, true},
		{via + " k7q x4r", Target{Via: via, Code: "K7QX4R"}, true},
		{"K7Q X4R", Target{Code: "K7QX4R"}, true},
		{inv[:strings.Index(inv, "?via=")] + "?via=javascript%3Aalert", Target{Addr: "192.168.1.20:47475", Code: "K7QX4R"}, true},
		{inv[:strings.Index(inv, "?via=")] + "?via=" + url.QueryEscape("https://relay.example.com/v2/r/0123#x"), Target{Addr: "192.168.1.20:47475", Code: "K7QX4R"}, true},
		{"https://example.com/not-a-jam K7QX4R", Target{}, false},
	}
	for _, c := range cases {
		got, ok := ParseTarget(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%q: %+v %v; want %+v %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

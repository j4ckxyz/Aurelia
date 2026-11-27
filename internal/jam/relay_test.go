package jam

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/yamux"
)

// fakeRelay is a relay as relay/ is, in little: one room, which the
// host's connection opens, and guests reach at /r/<room>/.
type fakeRelay struct {
	mu   sync.Mutex
	room string
	sess *yamux.Session
	key  string
}

func (f *fakeRelay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v1/host":
		if r.Header.Get("Authorization") != "Bearer "+f.key {
			http.Error(w, "the relay's key is wrong", http.StatusUnauthorized)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: aurelia-relay\r\n\r\n")
		cfg := yamux.DefaultConfig()
		cfg.LogOutput = io.Discard
		sess, _ := yamux.Client(conn, cfg)
		f.mu.Lock()
		f.room, f.sess = r.URL.Query().Get("room"), sess
		f.mu.Unlock()
	case r.URL.Path == "/v1/lookup":
		f.mu.Lock()
		room := f.room
		f.mu.Unlock()
		io.WriteString(w, `{"room":"`+room+`"}`)
	case strings.HasPrefix(r.URL.Path, "/r/"):
		f.mu.Lock()
		sess, room := f.sess, f.room
		f.mu.Unlock()
		if sess == nil || !strings.HasPrefix(r.URL.Path, "/r/"+room+"/") {
			http.Error(w, "no Jam is here", http.StatusNotFound)
			return
		}
		(&httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.Out.URL.Scheme, pr.Out.URL.Host = "http", "jam"
				pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, "/r/"+room)
			},
			Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
				return sess.Open()
			}},
			FlushInterval: -1,
		}).ServeHTTP(w, r)
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
	if !strings.HasPrefix(via, rs.URL+"/r/") || !IsRelayAddr(via) {
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
	via := "https://relay.example.com/r/0123456789abcdef0123456789abcdef"
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
		{"https://example.com/not-a-jam K7QX4R", Target{}, false},
	}
	for _, c := range cases {
		got, ok := ParseTarget(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%q: %+v %v; want %+v %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

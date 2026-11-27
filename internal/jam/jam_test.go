package jam

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"aurelia/internal/library"
)

// waitFor waits for cond, a few seconds at most.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited in vain for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// recorder keeps what a guest is told.
type recorder struct {
	mu     sync.Mutex
	states []*State
	ended  string
	away   []bool
}

func (r *recorder) handlers() Handlers {
	return Handlers{
		OnState: func(st *State) { r.mu.Lock(); r.states = append(r.states, st); r.mu.Unlock() },
		OnEnd:   func(why string) { r.mu.Lock(); r.ended = why; r.mu.Unlock() },
		OnAway:  func(a bool) { r.mu.Lock(); r.away = append(r.away, a); r.mu.Unlock() },
	}
}

func (r *recorder) last() *State {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.states) == 0 {
		return nil
	}
	return r.states[len(r.states)-1]
}

func (r *recorder) end() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ended
}

// newHost makes a host, and set, before it starts, sets its callbacks.
func newHost(t *testing.T, set ...func(h *Host)) *Host {
	t.Helper()
	h, err := NewHost("Ada", "srvA", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range set {
		fn(h)
	}
	h.Start()
	t.Cleanup(h.Close)
	return h
}

func join(t *testing.T, h *Host, name, server string) (*Guest, *recorder) {
	return joinWith(t, h, name, server, nil)
}

// joinWith joins with fetch to share the guest's songs.
func joinWith(t *testing.T, h *Host, name, server string, fetch func(ctx context.Context, f Fetch) (io.ReadCloser, int64, error)) (*Guest, *recorder) {
	t.Helper()
	r := &recorder{}
	hs := r.handlers()
	hs.OnFetch = fetch
	g, err := Join(context.Background(), h.Addr(), Hello{Name: name, Server: server, Code: h.Code()}, hs)
	if err != nil {
		t.Fatalf("joining: %v", err)
	}
	t.Cleanup(g.Leave)
	return g, r
}

func song(server, id, name string) *Song {
	return &Song{Server: server, Song: library.Song{ID: id, Name: name, Seconds: 200}}
}

// A guest with the code comes in, and is sent what plays: the details of
// each song once, and then only its key.
func TestJoinAndState(t *testing.T) {
	var members []Member
	var mu sync.Mutex
	h := newHost(t, func(h *Host) { h.OnMembers = func(m []Member) { mu.Lock(); members = m; mu.Unlock() } })
	_, r := join(t, h, "Bo", "srvB")
	waitFor(t, "the guest among the members", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(members) == 1 && members[0].Name == "Bo" && !members[0].Away
	})
	s1, s2 := song("srvA", "1", "One"), song("srvA", "2", "Two")
	h.Publish(State{Queue: []Item{{Key: Key("srvA", "1"), Song: s1}, {Key: Key("srvA", "2"), Song: s2, By: "Bo"}}, Index: 0, Playing: true, Position: 1000, At: time.Now().UnixMilli()})
	waitFor(t, "the state", func() bool { st := r.last(); return st != nil && len(st.Queue) == 2 })
	st := r.last()
	if st.Host != "Ada" || st.Queue[0].Song == nil || st.Queue[0].Song.Name != "One" || st.Queue[1].By != "Bo" || !st.Playing {
		t.Fatalf("the state: %+v", st)
	}
	if len(st.Members) != 2 || !st.Members[0].Host || st.Members[1].Name != "Bo" {
		t.Errorf("the members told: %+v", st.Members)
	}
	h.Publish(State{Queue: []Item{{Key: Key("srvA", "2"), Song: s2}, {Key: Key("srvA", "3"), Song: song("srvA", "3", "Three")}}, Index: 0})
	waitFor(t, "the second state", func() bool { st := r.last(); return st != nil && st.Queue[0].Key == Key("srvA", "2") })
	st = r.last()
	if st.Queue[0].Song != nil || st.Queue[1].Song == nil {
		t.Errorf("a song is sent once: %+v", st.Queue)
	}
}

func TestWrongCode(t *testing.T) {
	h := newHost(t)
	_, err := Join(context.Background(), h.Addr(), Hello{Name: "Eve", Code: "AAAAAA"}, Handlers{})
	var denied *ErrDenied
	if !errors.As(err, &denied) || !strings.Contains(denied.Why, "not the code") {
		t.Fatalf("a wrong code: %v", err)
	}
}

// Without the code, the host is asked.
func TestJoinByAsking(t *testing.T) {
	reqs := make(chan Request, 2)
	h := newHost(t, func(h *Host) { h.OnRequest = func(r Request) { reqs <- r } })
	waited := make(chan bool, 1)
	done := make(chan error, 1)
	go func() {
		g, err := Join(context.Background(), h.Addr(), Hello{Name: "Cy"}, Handlers{Waiting: func() { waited <- true }})
		if g != nil {
			t.Cleanup(g.Leave)
		}
		done <- err
	}()
	r := <-reqs
	if r.Name != "Cy" {
		t.Fatalf("the request: %+v", r)
	}
	<-waited
	h.Allow(r.ID)
	if err := <-done; err != nil {
		t.Fatalf("let in: %v", err)
	}

	go func() {
		_, err := Join(context.Background(), h.Addr(), Hello{Name: "Di"}, Handlers{})
		done <- err
	}()
	r = <-reqs
	h.Deny(r.ID)
	var denied *ErrDenied
	if err := <-done; !errors.As(err, &denied) {
		t.Fatalf("turned away: %v", err)
	}
}

func TestAsk(t *testing.T) {
	asks := make(chan Ask, 1)
	var who Member
	h := newHost(t, func(h *Host) { h.OnAsk = func(m Member, a Ask) { who = m; asks <- a } })
	g, _ := join(t, h, "Bo", "srvB")
	g.Ask(Ask{Op: OpNext, Songs: []Song{*song("srvB", "9", "Nine")}})
	select {
	case a := <-asks:
		if a.Op != OpNext || len(a.Songs) != 1 || a.Songs[0].Server != "srvB" || who.Name != "Bo" {
			t.Errorf("asked %+v by %+v", a, who)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the ask did not come")
	}
}

// A guest gets a song of the host's from it, and nobody else does.
func TestMedia(t *testing.T) {
	h := newHost(t, func(h *Host) {
		h.Media = func(ctx context.Context, key string) (io.ReadCloser, int64, error) {
			if key != Key("srvA", "1") {
				return nil, 0, errors.New("not in the queue")
			}
			return io.NopCloser(strings.NewReader("the song")), 8, nil
		}
	})
	g, _ := join(t, h, "Bo", "srvB")
	resp, err := client.Get(g.MediaURL(Key("srvA", "1")))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "the song" || resp.ContentLength != 8 {
		t.Errorf("the song: %d %q (%d)", resp.StatusCode, body, resp.ContentLength)
	}
	resp, err = client.Get(strings.Replace(g.MediaURL(Key("srvA", "1")), "&t=", "&t=x", 1))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("without the token: %d", resp.StatusCode)
	}
}

// A song of a guest's is fetched from them: by the host itself, by its
// pull address, and for other guests through the host.
func TestRelay(t *testing.T) {
	h := newHost(t, func(h *Host) {
		h.Media = func(ctx context.Context, key string) (io.ReadCloser, int64, error) {
			server, _ := SplitKey(key)
			return h.Fetch(ctx, server, Fetch{Kind: KindMedia, Key: key})
		}
		h.Image = func(ctx context.Context, server, item, tag string, px int) (io.ReadCloser, int64, error) {
			return h.Fetch(ctx, server, Fetch{Kind: KindImage, Item: item, Tag: tag, Px: px})
		}
	})
	joinWith(t, h, "Bo", "srvB", func(ctx context.Context, f Fetch) (io.ReadCloser, int64, error) {
		switch {
		case f.Kind == KindMedia && f.Key == Key("srvB", "9"):
			return io.NopCloser(strings.NewReader(strings.Repeat("b", 300_000))), 300_000, nil
		case f.Kind == KindImage && f.Item == "al9":
			return io.NopCloser(strings.NewReader("png")), -1, nil
		}
		return nil, 0, errors.New("not mine")
	})

	resp, err := client.Get(h.PullURL(Key("srvB", "9")))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(body) != 300_000 {
		t.Fatalf("pulled: %d, %d bytes", resp.StatusCode, len(body))
	}

	other, _ := join(t, h, "Cy", "srvC")
	resp, err = client.Get(other.MediaURL(Key("srvB", "9")))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(body) != 300_000 {
		t.Errorf("through the host: %d, %d bytes", resp.StatusCode, len(body))
	}
	resp, err = client.Get(other.ImageURL("srvB", "al9", "t", 96))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "png" {
		t.Errorf("the picture: %q", body)
	}
	resp, err = client.Get(h.ImageURL("srvB", "al9", "t", 96))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "png" {
		t.Errorf("the picture, for the host itself: %q", body)
	}

	// What the guest does not have is an error, and a server nobody
	// has a song of is not found.
	resp, _ = client.Get(h.PullURL(Key("srvB", "404")))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("a song the guest lacks: %d", resp.StatusCode)
	}
	resp, _ = client.Get(h.PullURL(Key("srvZ", "1")))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a server of nobody's: %d", resp.StatusCode)
	}
}

// A guest whose connection is lost comes back as itself.
func TestReconnect(t *testing.T) {
	h := newHost(t)
	g, r := join(t, h, "Bo", "srvB")
	h.Publish(State{Index: -1})
	waitFor(t, "the state", func() bool { return r.last() != nil })
	h.mu.Lock()
	stop := h.members[0].stop
	h.mu.Unlock()
	stop() // the network fails
	waitFor(t, "away and back", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.away) == 2 && r.away[0] && !r.away[1]
	})
	if ms := h.Members(); len(ms) != 1 || ms[0].ID != g.ID() {
		t.Errorf("after coming back: %+v, the guest is %s", ms, g.ID())
	}
	if r.end() != "" {
		t.Errorf("ended: %q", r.end())
	}
}

func TestLeaveRemoveAndEnd(t *testing.T) {
	h := newHost(t)
	g, _ := join(t, h, "Bo", "srvB")
	g2, r2 := join(t, h, "Cy", "srvC")
	_, r3 := join(t, h, "Di", "srvD")
	waitFor(t, "three guests", func() bool { return len(h.Members()) == 3 })
	g.Leave()
	waitFor(t, "Bo gone", func() bool { return len(h.Members()) == 2 })
	h.Remove(g2.ID())
	waitFor(t, "Cy told", func() bool { return strings.Contains(r2.end(), "took you out") })
	if len(h.Members()) != 1 {
		t.Errorf("after the removal: %+v", h.Members())
	}
	h.Close()
	waitFor(t, "Di told", func() bool { return strings.Contains(r3.end(), "ended") })
}

// Hosts are found on the network: every one that shows itself, or the
// one of a code.
func TestDiscover(t *testing.T) {
	h := newHost(t)
	if h.announce == nil {
		t.Skip("the port of the broadcasts is taken")
	}
	find := func(code string) []Found {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		var mu sync.Mutex
		var all []Found
		Discover(ctx, code, func(f Found) { mu.Lock(); all = append(all, f); mu.Unlock() })
		return all
	}
	if got := find(""); len(got) != 1 || got[0].Name != "Ada" || !strings.HasSuffix(got[0].Addr, ":"+strconv.Itoa(h.Port())) {
		t.Fatalf("found %+v", got)
	}
	if got := find(h.Code()); len(got) != 1 {
		t.Errorf("by its code: %+v", got)
	}
	if got := find("ZZZZZZ"); len(got) != 0 {
		t.Errorf("by another code: %+v", got)
	}
	h.SetDiscoverable(false)
	if got := find(""); len(got) != 0 {
		t.Errorf("hidden, found %+v", got)
	}
	if got := find(h.Code()); len(got) != 1 {
		t.Errorf("hidden, by its code: %+v", got)
	}
}

func TestParseJoin(t *testing.T) {
	for in, want := range map[string][3]string{
		"k7q x4r":                                {"", "K7QX4R", "true"},
		"K7Q-X4R":                                {"", "K7QX4R", "true"},
		"abc":                                    {"", "ABC", "false"},
		"aurelia://jam/192.168.1.5:47475/K7QX4R": {"192.168.1.5:47475", "K7QX4R", "true"},
		"192.168.1.5:5000 k7qx4r":                {"192.168.1.5:5000", "K7QX4R", "true"},
		"192.168.1.5/K7QX4R":                     {"192.168.1.5:47475", "K7QX4R", "true"},
		// On a tailnet: a 100.x address, its IPv6 one, a MagicDNS name.
		"100.101.102.103 K7Q X4R":                        {"100.101.102.103:47475", "K7QX4R", "true"},
		"100.101.102.103:47476/k7qx4r":                   {"100.101.102.103:47476", "K7QX4R", "true"},
		"fd7a:115c:a1e0::ca01:f51e K7QX4R":               {"[fd7a:115c:a1e0::ca01:f51e]:47475", "K7QX4R", "true"},
		"aurelia://jam/[fd7a:115c:a1e0::1]:47475/K7QX4R": {"[fd7a:115c:a1e0::1]:47475", "K7QX4R", "true"},
		"studio.tail1234.ts.net K7Q X4R":                 {"studio.tail1234.ts.net:47475", "K7QX4R", "true"},
		"studio.tail1234.ts.net.:47475/K7QX4R":           {"studio.tail1234.ts.net:47475", "K7QX4R", "true"},
		"100.101.102.103":                                {"", "100101102103", "false"},
	} {
		addr, code, ok := ParseJoin(in)
		if addr != want[0] || code != want[1] || (ok && want[2] != "true") || (!ok && want[2] == "true") {
			t.Errorf("%q: %q %q %v", in, addr, code, ok)
		}
	}
	if c := NewCode(); len(c) != CodeLen || NormalizeCode(c) != c || strings.ContainsAny(c, "01OIL") {
		t.Errorf("a code: %q", c)
	}
}

// The clock of a guest on the same computer is the host's.
func TestClock(t *testing.T) {
	h := newHost(t)
	g, _ := join(t, h, "Bo", "srvB")
	time.Sleep(600 * time.Millisecond)
	if d := g.HostNow().Sub(time.Now()); d > 20*time.Millisecond || d < -20*time.Millisecond {
		t.Errorf("the clocks differ by %v", d)
	}
}

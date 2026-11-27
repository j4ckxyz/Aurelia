package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

const (
	testKey = "a-key-of-sixteen-or-more"
	room1   = "0123456789abcdef0123456789abcdef"
	room2   = "ffffffffffffffffffffffffffffffff"
	pin1    = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	pin2    = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	secret1 = "secret-secret-secret"
)

func wsURL(base string) string { return "ws" + strings.TrimPrefix(base, "http") }

// host connects to the relay as a Jam's host does, and serves each
// tunnel with serve, until the test ends or stop is called.
func host(t *testing.T, relay, key, room, code, pin, secret string, serve func(net.Conn)) (status int, stop func()) {
	t.Helper()
	q := url.Values{"room": {room}, "code": {code}, "pin": {pin}}
	h := http.Header{"X-Room-Secret": {secret}}
	if key != "" {
		h.Set("Authorization", "Bearer "+key)
	}
	ws, resp, err := websocket.Dial(context.Background(), wsURL(relay)+"/v2/host?"+q.Encode(), &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		if resp == nil {
			t.Fatal(err)
		}
		return resp.StatusCode, func() {}
	}
	conn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	sess, err := yamux.Server(conn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := sess.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				serve(c)
			}()
		}
	}()
	stop = func() { sess.Close() }
	t.Cleanup(stop)
	return resp.StatusCode, stop
}

// echo answers each line with what is given.
func echo(what string) func(net.Conn) {
	return func(c net.Conn) {
		br := bufio.NewReader(c)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			io.WriteString(c, what+line)
		}
	}
}

// tunnel connects as a guest to a room.
func tunnel(t *testing.T, relay, room string) (net.Conn, int) {
	t.Helper()
	ws, resp, err := websocket.Dial(context.Background(), wsURL(relay)+"/v2/r/"+room, nil)
	if err != nil {
		if resp == nil {
			t.Fatal(err)
		}
		return nil, resp.StatusCode
	}
	c := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
	t.Cleanup(func() { c.Close() })
	return c, resp.StatusCode
}

func say(t *testing.T, c net.Conn, line string) string {
	t.Helper()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	io.WriteString(c, line+"\n")
	got, _ := bufio.NewReader(c).ReadString('\n')
	return strings.TrimSuffix(got, "\n")
}

func newRelay(t *testing.T, open bool, change func(*limits)) (*server, *httptest.Server) {
	lim := defaultLimits()
	if change != nil {
		change(&lim)
	}
	key := testKey
	if open {
		key = ""
	}
	s := newServer(key, open, lim)
	ts := httptest.NewServer(s.handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func waitRooms(t *testing.T, s *server, n int) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		s.mu.Lock()
		got := len(s.rooms)
		s.mu.Unlock()
		if got == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d rooms, not %d", got, n)
		}
	}
}

func get(t *testing.T, u string, h http.Header) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", u, nil)
	for k, v := range h {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// A host with the key opens a room; guests reach it there, by tunnels
// whose bytes go through as they are; one without the key, or whose room
// is another's, does not.
func TestRelay(t *testing.T) {
	s, ts := newRelay(t, false, nil)
	if st, _ := host(t, ts.URL, "wrong-key-wrong-key", room1, "K7QX4R", pin1, secret1, echo("")); st != http.StatusUnauthorized {
		t.Errorf("a wrong key: %d", st)
	}
	if st, _ := host(t, ts.URL, "", room1, "K7QX4R", pin1, secret1, echo("")); st != http.StatusUnauthorized {
		t.Errorf("no key: %d", st)
	}
	if st, _ := host(t, ts.URL, testKey, "short", "K7QX4R", pin1, secret1, echo("")); st != http.StatusBadRequest {
		t.Errorf("a bad room: %d", st)
	}
	if st, _ := host(t, ts.URL, testKey, room1, "K7QX4R", "not-a-pin", secret1, echo("")); st != http.StatusBadRequest {
		t.Errorf("a bad fingerprint: %d", st)
	}
	if st, _ := host(t, ts.URL, testKey, room1, "K7QX4R", pin1, secret1, echo("host: ")); st != http.StatusSwitchingProtocols {
		t.Fatalf("the host: %d", st)
	}
	waitRooms(t, s, 1)
	c, st := tunnel(t, ts.URL, room1)
	if c == nil {
		t.Fatalf("the tunnel: %d", st)
	}
	if got := say(t, c, "hello"); got != "host: hello" {
		t.Errorf("through the tunnel: %q", got)
	}
	// Another's room, or code, is not taken; nor is the room with another
	// fingerprint.
	if st, _ := host(t, ts.URL, testKey, room1, "K7QX4R", pin1, "another-secret-here", echo("")); st != http.StatusConflict {
		t.Errorf("another's room: %d", st)
	}
	if st, _ := host(t, ts.URL, testKey, room1, "K7QX4R", pin2, secret1, echo("")); st != http.StatusConflict {
		t.Errorf("the room with another fingerprint: %d", st)
	}
	if st, _ := host(t, ts.URL, testKey, room2, "K7QX4R", pin2, "another-secret-here", echo("")); st != http.StatusConflict {
		t.Errorf("another's code: %d", st)
	}
	// A room nobody has.
	if _, st := tunnel(t, ts.URL, room2); st != http.StatusNotFound {
		t.Errorf("no room: %d", st)
	}
	// A page in a browser, which would use the relay as its own.
	if st, _ := get(t, ts.URL+"/v2/r/"+room1, http.Header{"Origin": {"https://evil.example"}, "Connection": {"Upgrade"}, "Upgrade": {"websocket"}, "Sec-Websocket-Version": {"13"}, "Sec-Websocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="}}); st != http.StatusForbidden {
		t.Errorf("from a browser: %d", st)
	}
	// Aurelia before the tunnels.
	if st, body := get(t, ts.URL+"/v1/host?room="+room1, nil); st != http.StatusGone || !strings.Contains(body, "update Aurelia") {
		t.Errorf("an old host: %d %q", st, body)
	}
	if st, _ := get(t, ts.URL+"/r/"+room1+"/ws", nil); st != http.StatusGone {
		t.Errorf("an old guest: %d", st)
	}
}

// The host back, with its secret, has its room again; gone, the room is
// gone, and its tunnels with it.
func TestRelayReconnect(t *testing.T) {
	s, ts := newRelay(t, false, nil)
	_, stop := host(t, ts.URL, testKey, room1, "K7QX4R", pin1, secret1, echo("first "))
	waitRooms(t, s, 1)
	c, _ := tunnel(t, ts.URL, room1)
	if got := say(t, c, "x"); got != "first x" {
		t.Errorf("got %q", got)
	}
	if st, _ := host(t, ts.URL, testKey, room1, "K7QX4R", pin1, secret1, echo("second ")); st != http.StatusSwitchingProtocols {
		t.Fatalf("back: %d", st)
	}
	time.Sleep(50 * time.Millisecond)
	c2, _ := tunnel(t, ts.URL, room1)
	if got := say(t, c2, "x"); got != "second x" {
		t.Errorf("after the host came back, got %q", got)
	}
	// The first host's tunnel went with its connection.
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Error("a tunnel outlived its host's connection")
	}
	stop()
	s.mu.Lock()
	for _, rm := range s.rooms {
		rm.sess.Close()
	}
	s.mu.Unlock()
	waitRooms(t, s, 0)
	if _, st := tunnel(t, ts.URL, room1); st != http.StatusNotFound {
		t.Errorf("a room without a host: %d", st)
	}
}

// A guest with only the code finds the room, and the host's fingerprint;
// trying codes is slowed.
func TestRelayLookup(t *testing.T) {
	s, ts := newRelay(t, false, nil)
	host(t, ts.URL, testKey, room1, "K7QX4R", pin1, secret1, echo(""))
	waitRooms(t, s, 1)
	for _, q := range []string{"code=k7q+x4r", "room=" + room1} {
		st, body := get(t, ts.URL+"/v2/lookup?"+q, nil)
		var got struct{ Room, Pin string }
		json.Unmarshal([]byte(body), &got)
		if st != http.StatusOK || got.Room != room1 || got.Pin != pin1 {
			t.Errorf("%s: %d %q", q, st, body)
		}
	}
	limited := false
	for range 40 {
		st, _ := get(t, ts.URL+"/v2/lookup?code=AAAAAA", nil)
		if st == http.StatusTooManyRequests {
			limited = true
			break
		}
		if st != http.StatusNotFound {
			t.Fatalf("an unknown code: %d", st)
		}
	}
	if !limited {
		t.Error("codes are tried without end")
	}
}

// A public relay lets anyone host, but no address more than a few Jams,
// nor a Jam more than so many connections, or bandwidth.
func TestRelayOpen(t *testing.T) {
	s, ts := newRelay(t, true, func(l *limits) {
		l.tunnelsPerRoom = 2
		l.roomRate = 200 << 10
	})
	codes := []string{"AAAAAA", "BBBBBB", "CCCCCC", "DDDDDD"}
	for i, code := range codes {
		room := strings.Repeat(string("0123"[i]), 32)
		st, _ := host(t, ts.URL, "", room, code, pin1, secret1, func(c net.Conn) {
			// What the guest asks for, as a song.
			var n int
			if _, err := io.ReadFull(c, make([]byte, 1)); err == nil {
				n = 600 << 10
			}
			c.Write(make([]byte, n))
		})
		want := http.StatusSwitchingProtocols
		if i == 3 {
			want = http.StatusTooManyRequests
		}
		if st != want {
			t.Errorf("Jam %d of one address: %d", i+1, st)
		}
	}
	waitRooms(t, s, 3)
	room := strings.Repeat("0", 32)
	a, _ := tunnel(t, ts.URL, room)
	b, _ := tunnel(t, ts.URL, room)
	if a == nil || b == nil {
		t.Fatal("tunnels")
	}
	if _, st := tunnel(t, ts.URL, room); st != http.StatusServiceUnavailable {
		t.Errorf("a third connection to a Jam of two at most: %d", st)
	}
	began := time.Now()
	a.SetDeadline(time.Now().Add(10 * time.Second))
	a.Write([]byte{1})
	n, _ := io.ReadFull(a, make([]byte, 600<<10))
	// 600 KB at 200 KB a second, less the first 50 KB at once.
	if took := time.Since(began); n != 600<<10 || took < 2*time.Second {
		t.Errorf("%d bytes in %s", n, took)
	}
}

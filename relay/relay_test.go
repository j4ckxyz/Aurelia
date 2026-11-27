package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
)

const testKey = "a-key-of-sixteen-or-more"

// host connects to the relay as a Jam's host does, and serves h through
// it, until the test ends or the returned func is called.
func host(t *testing.T, relay, key, room, code, secret string, h http.Handler) (status int, stop func()) {
	t.Helper()
	u, _ := url.Parse(relay)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{"room": {room}, "code": {code}}
	fmt.Fprintf(conn, "GET /v1/host?%s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nX-Room-Secret: %s\r\nConnection: Upgrade\r\nUpgrade: aurelia-relay\r\n\r\n", q.Encode(), u.Host, key, secret)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return resp.StatusCode, func() {}
	}
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	sess, err := yamux.Server(&prefixedReader{Conn: conn, r: br}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(sess)
	stop = func() { srv.Close(); sess.Close() }
	t.Cleanup(stop)
	return resp.StatusCode, stop
}

type prefixedReader struct {
	net.Conn
	r *bufio.Reader
}

func (p *prefixedReader) Read(b []byte) (int, error) { return p.r.Read(b) }

func newRelay(t *testing.T) (*server, *httptest.Server) {
	s := newServer(testKey, 4)
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

const room1 = "0123456789abcdef0123456789abcdef"

// A host with the key opens a room; guests reach it there, at its own
// paths; one without the key, or whose room is another's, does not.
func TestRelay(t *testing.T) {
	s, ts := newRelay(t)
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s %s?%s from %s", r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("X-Forwarded-For"))
	})
	if st, _ := host(t, ts.URL, "wrong-key-wrong-key", room1, "K7QX4R", "secret-secret-secret", echo); st != http.StatusUnauthorized {
		t.Errorf("a wrong key: %d", st)
	}
	if st, _ := host(t, ts.URL, testKey, "short", "K7QX4R", "secret-secret-secret", echo); st != http.StatusBadRequest {
		t.Errorf("a bad room: %d", st)
	}
	if st, _ := host(t, ts.URL, testKey, room1, "K7QX4R", "secret-secret-secret", echo); st != http.StatusSwitchingProtocols {
		t.Fatalf("the host: %d", st)
	}
	waitRooms(t, s, 1)
	resp, err := http.Get(ts.URL + "/r/" + room1 + "/media?k=a%2Fb&t=x")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if got := string(b); !strings.HasPrefix(got, "GET /media?k=a%2Fb&t=x from 127.0.0.1") {
		t.Errorf("the host was asked: %q", got)
	}
	// Another's room, or code, is not taken.
	if st, _ := host(t, ts.URL, testKey, room1, "K7QX4R", "another-secret-here", echo); st != http.StatusConflict {
		t.Errorf("another's room: %d", st)
	}
	if st, _ := host(t, ts.URL, testKey, "ffffffffffffffffffffffffffffffff", "K7QX4R", "another-secret-here", echo); st != http.StatusConflict {
		t.Errorf("another's code: %d", st)
	}
	// A room nobody has.
	if resp, _ := http.Get(ts.URL + "/r/ffffffffffffffffffffffffffffffff/ws"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("no room: %d", resp.StatusCode)
	}
}

// The host back, with its secret, has its room again; gone, the room is
// gone.
func TestRelayReconnect(t *testing.T) {
	s, ts := newRelay(t)
	say := func(what string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, what) })
	}
	get := func() (int, string) {
		resp, err := http.Get(ts.URL + "/r/" + room1 + "/x")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	_, stop := host(t, ts.URL, testKey, room1, "K7QX4R", "secret-secret-secret", say("first"))
	waitRooms(t, s, 1)
	if _, got := get(); got != "first" {
		t.Errorf("got %q", got)
	}
	if st, _ := host(t, ts.URL, testKey, room1, "K7QX4R", "secret-secret-secret", say("second")); st != http.StatusSwitchingProtocols {
		t.Fatalf("back: %d", st)
	}
	time.Sleep(50 * time.Millisecond)
	if _, got := get(); got != "second" {
		t.Errorf("after the host came back, got %q", got)
	}
	stop()
	s.mu.Lock()
	for _, rm := range s.rooms {
		rm.sess.Close()
	}
	s.mu.Unlock()
	waitRooms(t, s, 0)
	if st, _ := get(); st != http.StatusNotFound {
		t.Errorf("a room without a host: %d", st)
	}
}

// A guest with only the code finds the room; trying codes is slowed.
func TestRelayLookup(t *testing.T) {
	s, ts := newRelay(t)
	host(t, ts.URL, testKey, room1, "K7QX4R", "secret-secret-secret", http.NotFoundHandler())
	waitRooms(t, s, 1)
	resp, err := http.Get(ts.URL + "/v1/lookup?code=k7q+x4r")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Room string }
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.Room != room1 {
		t.Errorf("the room of the code: %q", got.Room)
	}
	limited := false
	for range 40 {
		resp, _ := http.Get(ts.URL + "/v1/lookup?code=AAAAAA")
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = true
			break
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("an unknown code: %d", resp.StatusCode)
		}
	}
	if !limited {
		t.Error("codes are tried without end")
	}
}

// A connection a guest upgrades, as the Jam's WebSocket, goes through
// both ways.
func TestRelayUpgrade(t *testing.T) {
	s, ts := newRelay(t)
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "upgrade", http.StatusBadRequest)
			return
		}
		conn, rw, _ := w.(http.Hijacker).Hijack()
		defer conn.Close()
		io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		line, _ := rw.ReadString('\n')
		io.WriteString(conn, "echo "+line)
	})
	host(t, ts.URL, testKey, room1, "K7QX4R", "secret-secret-secret", up)
	waitRooms(t, s, 1)
	u, _ := url.Parse(ts.URL)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "GET /r/%s/ws HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", room1, u.Host)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("the upgrade: %v %v", resp, err)
	}
	io.WriteString(conn, "hello\n")
	if line, _ := br.ReadString('\n'); line != "echo hello\n" {
		t.Errorf("through the upgrade: %q", line)
	}
}

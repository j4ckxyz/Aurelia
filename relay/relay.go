package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// The relay's protocol, as Aurelia speaks it:
//
//   - A Jam's host connects with GET /v1/host?room=R&code=C, with the
//     relay's key as a bearer token, a secret of its room in X-Room-Secret,
//     and "Upgrade: aurelia-relay". The relay answers 101, and the
//     connection carries yamux: the relay opens a stream for each
//     connection of a guest, which the host serves as it serves its own.
//     R is the host's to choose, 32 hex digits; it keeps it across
//     reconnections, with the same secret, so that its invites stay good.
//   - A guest reaches the host at /r/R/...: /r/R/ws is the host's /ws.
//   - GET /v1/lookup?code=C finds the room of the Jam of a code, for a
//     guest that has only the code.
//
// Guests need no key: what lets them in is the host's, as on the local
// network: the Jam's code, or the host's yes.

const upgradeProto = "aurelia-relay"

var (
	roomRE = regexp.MustCompile(`^[0-9a-f]{32}$`)
	codeRE = regexp.MustCompile(`^[0-9A-Z]{6}$`)
)

type server struct {
	key        string // a host's key; "" lets none in
	maxRooms   int
	trustProxy bool // the address of a client is the proxy's X-Forwarded-For
	lookups    *limiter
	hosts      *limiter

	mu    sync.Mutex
	rooms map[string]*room
	codes map[string]*room
}

type room struct {
	id, code string
	secret   [32]byte // sha256 of the host's secret
	sess     *yamux.Session
	proxy    *httputil.ReverseProxy
	since    time.Time
}

func newServer(key string, maxRooms int) *server {
	return &server{
		key: key, maxRooms: maxRooms,
		lookups: newLimiter(30, time.Minute),
		hosts:   newLimiter(30, time.Minute),
		rooms:   map[string]*room{}, codes: map[string]*room{},
	}
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.serveHealth)
	mux.HandleFunc("GET /v1/host", s.serveHost)
	mux.HandleFunc("GET /v1/lookup", s.serveLookup)
	mux.HandleFunc("/r/{room}/", s.serveGuest)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "Aurelia's Jam relay. See https://github.com/j4ckxyz/Aurelia/tree/main/relay\n")
	})
	return mux
}

func (s *server) serveHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	n := len(s.rooms)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "rooms": n})
}

// client is who a request is from, for the limits.
func (s *server) client(r *http.Request) string {
	if s.trustProxy {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			parts := strings.Split(f, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *server) serveHost(w http.ResponseWriter, r *http.Request) {
	if !s.hosts.allow(s.client(r)) {
		http.Error(w, "too many tries; wait a minute", http.StatusTooManyRequests)
		return
	}
	auth, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if s.key == "" || subtle.ConstantTimeCompare([]byte(auth), []byte(s.key)) != 1 {
		http.Error(w, "the relay's key is wrong", http.StatusUnauthorized)
		return
	}
	id, code := r.URL.Query().Get("room"), r.URL.Query().Get("code")
	secret := r.Header.Get("X-Room-Secret")
	if !roomRE.MatchString(id) || !codeRE.MatchString(code) || len(secret) < 16 {
		http.Error(w, "a room needs its id, its Jam's code and a secret", http.StatusBadRequest)
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), upgradeProto) {
		http.Error(w, "upgrade to "+upgradeProto, http.StatusUpgradeRequired)
		return
	}
	sum := sha256.Sum256([]byte(secret))
	s.mu.Lock()
	old := s.rooms[id]
	switch {
	case old != nil && subtle.ConstantTimeCompare(old.secret[:], sum[:]) != 1:
		s.mu.Unlock()
		http.Error(w, "the room is another host's", http.StatusConflict)
		return
	case old == nil && len(s.rooms) >= s.maxRooms:
		s.mu.Unlock()
		http.Error(w, "the relay is full", http.StatusServiceUnavailable)
		return
	case old == nil && s.codes[code] != nil:
		s.mu.Unlock()
		http.Error(w, "another Jam has that code", http.StatusConflict)
		return
	}
	s.mu.Unlock()

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "the connection cannot be taken over (HTTP/2?)", http.StatusInternalServerError)
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	conn.SetDeadline(time.Time{})
	if _, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+upgradeProto+"\r\n\r\n"); err != nil {
		conn.Close()
		return
	}
	// Anything the host sent after its request, already read, comes first.
	var c net.Conn = conn
	if n := rw.Reader.Buffered(); n > 0 {
		head, _ := rw.Reader.Peek(n)
		c = &prefixed{Conn: conn, head: append([]byte(nil), head...)}
	}
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.ConnectionWriteTimeout = 30 * time.Second
	cfg.StreamOpenTimeout = 15 * time.Second
	cfg.LogOutput = io.Discard
	sess, err := yamux.Client(c, cfg)
	if err != nil {
		conn.Close()
		return
	}
	rm := &room{id: id, code: code, secret: sum, sess: sess, since: time.Now()}
	rm.proxy = s.proxyTo(rm)
	s.mu.Lock()
	if old := s.rooms[id]; old != nil {
		// The host again, its last connection not yet known to be gone.
		old.sess.Close()
		delete(s.codes, old.code)
	}
	s.rooms[id], s.codes[code] = rm, rm
	n := len(s.rooms)
	s.mu.Unlock()
	log.Printf("room %s… open (%d open)", id[:6], n)

	<-sess.CloseChan()
	s.mu.Lock()
	if s.rooms[id] == rm {
		delete(s.rooms, id)
		delete(s.codes, code)
	}
	n = len(s.rooms)
	s.mu.Unlock()
	log.Printf("room %s… closed after %s (%d open)", id[:6], time.Since(rm.since).Round(time.Second), n)
}

// proxyTo hands a guest's requests to the room's host, each connection
// on a stream of its own: the WebSocket of the Jam, and its songs.
func (s *server) proxyTo(rm *room) *httputil.ReverseProxy {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return rm.sess.Open()
		},
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: 2 * time.Minute, // a host asked to let a guest in answers when it does
	}
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host = "http", "jam"
			pr.Out.URL.Path = "/" + strings.TrimPrefix(strings.TrimPrefix(pr.In.URL.Path, "/r/"+rm.id), "/")
			pr.Out.URL.RawPath = ""
			pr.Out.Host = "jam"
			pr.SetXForwarded()
		},
		Transport:     tr,
		FlushInterval: -1, // songs and the Jam's messages go on as they come
		ErrorLog:      log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, context.Canceled) {
				return
			}
			http.Error(w, "the Jam's host did not answer", http.StatusBadGateway)
		},
	}
}

func (s *server) serveGuest(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	rm := s.rooms[r.PathValue("room")]
	s.mu.Unlock()
	if rm == nil {
		http.Error(w, "no Jam is here: it ended, or its host lost the connection", http.StatusNotFound)
		return
	}
	rm.proxy.ServeHTTP(w, r)
}

func (s *server) serveLookup(w http.ResponseWriter, r *http.Request) {
	if !s.lookups.allow(s.client(r)) {
		http.Error(w, "too many tries; wait a minute", http.StatusTooManyRequests)
		return
	}
	code := strings.ToUpper(strings.ReplaceAll(r.URL.Query().Get("code"), " ", ""))
	s.mu.Lock()
	rm := s.codes[code]
	s.mu.Unlock()
	if rm == nil || !codeRE.MatchString(code) {
		http.Error(w, "no Jam has that code here", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"room": rm.id})
}

// prefixed is a connection whose first bytes were read already.
type prefixed struct {
	net.Conn
	head []byte
}

func (p *prefixed) Read(b []byte) (int, error) {
	if len(p.head) > 0 {
		n := copy(b, p.head)
		p.head = p.head[n:]
		return n, nil
	}
	return p.Conn.Read(b)
}

// limiter lets each client so many tries in a window.
type limiter struct {
	n      int
	window time.Duration
	mu     sync.Mutex
	seen   map[string][]time.Time
}

func newLimiter(n int, window time.Duration) *limiter {
	return &limiter{n: n, window: window, seen: map[string][]time.Time{}}
}

func (l *limiter) allow(who string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.seen) > 10000 {
		// Forget those not seen within the window.
		for k, ts := range l.seen {
			if now.Sub(ts[len(ts)-1]) > l.window {
				delete(l.seen, k)
			}
		}
	}
	ts := l.seen[who]
	for len(ts) > 0 && now.Sub(ts[0]) > l.window {
		ts = ts[1:]
	}
	if len(ts) >= l.n {
		l.seen[who] = ts
		return false
	}
	l.seen[who] = append(ts, now)
	return true
}

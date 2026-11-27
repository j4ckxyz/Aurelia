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
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"golang.org/x/time/rate"
)

// The relay's protocol, as Aurelia speaks it (v2):
//
//   - A Jam's host connects a WebSocket to GET /v2/host?room=R&code=C&pin=P,
//     with a secret of its room in X-Room-Secret, and, unless the relay is
//     open to all, the relay's key as a bearer token. Its binary messages
//     carry yamux: the relay opens a stream for each tunnel of a guest.
//     R is the host's to choose, 32 hex digits; it keeps it across
//     reconnections, with the same secret, so that its invites stay good.
//     P is the fingerprint of the host's key (SHA-256 of its public key,
//     base64url), which a guest that has only the code learns here.
//   - A guest connects a WebSocket to GET /v2/r/R: a tunnel to the host,
//     whose bytes the relay passes on as they are. Inside it, the guest
//     speaks TLS with the host, and checks the host's key against P: what
//     goes through the relay is for the host and its guests alone.
//   - GET /v2/lookup?code=C, or ?room=R, finds the room of a Jam and its
//     fingerprint, for a guest that has only the code.
//
// Guests need no key: what lets them in is the host's, as on the local
// network: the Jam's code, or the host's yes. Since all the relay carries
// is TLS, it serves nothing of its own a browser would show, and it
// cannot read, or change, what the Jam says.

var (
	roomRE = regexp.MustCompile(`^[0-9a-f]{32}$`)
	codeRE = regexp.MustCompile(`^[0-9A-Z]{6}$`)
	pinRE  = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

// limits are what the relay carries at most.
type limits struct {
	rooms, roomsPerClient     int
	tunnels, tunnelsPerRoom   int
	rate, roomRate            float64 // bytes a second, all told and of one room; 0 for no cap
	hostTries, tunnelTries    int     // a minute, per client
	lookups, lookupsPerClient int     // a minute, all told and per client
}

func defaultLimits() limits {
	return limits{
		rooms: 64, roomsPerClient: 3,
		tunnels: 1024, tunnelsPerRoom: 48,
		rate: 25e6, roomRate: 3e6,
		hostTries: 10, tunnelTries: 240,
		lookups: 1200, lookupsPerClient: 30,
	}
}

type server struct {
	key      string // a host's key; "" with open lets anyone host
	open     bool
	lim      limits
	ipHeader string // the header a client's address is in, from the proxy in front
	xff      bool   // the address of a client is the last of X-Forwarded-For

	hosts, tunnelTries, lookups *limiter
	allLookups                  *rate.Limiter
	bw                          *rate.Limiter // nil for no cap
	tunnels                     atomic.Int64

	mu    sync.Mutex
	rooms map[string]*room
	codes map[string]*room
}

type room struct {
	id, code, pin string
	client        string
	secret        [32]byte // sha256 of the host's secret
	sess          *yamux.Session
	since         time.Time
	bw            *rate.Limiter // nil for no cap
	tunnels       atomic.Int64
}

func newServer(key string, open bool, lim limits) *server {
	s := &server{
		key: key, open: open, lim: lim,
		hosts:       newLimiter(lim.hostTries, time.Minute),
		tunnelTries: newLimiter(lim.tunnelTries, time.Minute),
		lookups:     newLimiter(lim.lookupsPerClient, time.Minute),
		allLookups:  rate.NewLimiter(rate.Limit(float64(lim.lookups)/60), max(lim.lookups/10, 1)),
		rooms:       map[string]*room{}, codes: map[string]*room{},
	}
	if lim.rate > 0 {
		s.bw = rate.NewLimiter(rate.Limit(lim.rate), burst(lim.rate))
	}
	return s
}

// burst is a token bucket's: a quarter of a second, at least a chunk.
func burst(r float64) int { return max(int(r/4), chunk) }

const chunk = 32 << 10

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.serveHealth)
	mux.HandleFunc("GET /v2/host", s.serveHost)
	mux.HandleFunc("GET /v2/r/{room}", s.serveTunnel)
	mux.HandleFunc("GET /v2/lookup", s.serveLookup)
	old := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "update Aurelia: this relay needs version 0.9.1 or later", http.StatusGone)
	}
	mux.HandleFunc("/v1/", old)
	mux.HandleFunc("/r/", old)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "Aurelia's Jam relay. See https://github.com/j4ckxyz/Aurelia/tree/main/relay\n")
	})
	return headers(mux)
}

// headers are on every answer: none of them is a page to show.
func headers(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; sandbox")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

func (s *server) serveHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	n := len(s.rooms)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "rooms": n, "tunnels": s.tunnels.Load(), "open": s.open})
}

// client is who a request is from, for the limits: an address, and for
// IPv6 its /64, which one computer may have all of.
func (s *server) client(r *http.Request) string {
	addr := ""
	switch {
	case s.ipHeader != "":
		addr = strings.TrimSpace(r.Header.Get(s.ipHeader))
	case s.xff:
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			parts := strings.Split(f, ",")
			addr = strings.TrimSpace(parts[len(parts)-1])
		}
	}
	if addr == "" {
		addr = r.RemoteAddr
		if host, _, err := net.SplitHostPort(addr); err == nil {
			addr = host
		}
	}
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return addr
	}
	ip = ip.Unmap()
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.String()
	}
	return ip.String()
}

// accept takes a WebSocket: from Aurelia, which sends no Origin; a page
// in a browser, which does, is turned away.
func accept(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	if r.Header.Get("Origin") != "" {
		http.Error(w, "not for browsers", http.StatusForbidden)
		return nil, errors.New("from a browser")
	}
	return websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
}

func (s *server) serveHost(w http.ResponseWriter, r *http.Request) {
	who := s.client(r)
	if !s.hosts.allow(who) {
		http.Error(w, "too many tries; wait a minute", http.StatusTooManyRequests)
		return
	}
	if !s.open {
		auth, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.key == "" || subtle.ConstantTimeCompare([]byte(auth), []byte(s.key)) != 1 {
			http.Error(w, "the relay's key is wrong", http.StatusUnauthorized)
			return
		}
	}
	q := r.URL.Query()
	id, code, pin := q.Get("room"), q.Get("code"), q.Get("pin")
	secret := r.Header.Get("X-Room-Secret")
	if !roomRE.MatchString(id) || !codeRE.MatchString(code) || !pinRE.MatchString(pin) || len(secret) < 16 || len(secret) > 256 {
		http.Error(w, "a room needs its id, its Jam's code, its host's fingerprint and a secret", http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256([]byte(secret))
	if why, status := s.mayOpen(id, code, pin, who, sum); why != "" {
		http.Error(w, why, status)
		return
	}
	ws, err := accept(w, r)
	if err != nil {
		return
	}
	conn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.ConnectionWriteTimeout = 30 * time.Second
	cfg.StreamOpenTimeout = 15 * time.Second
	cfg.StreamCloseTimeout = 30 * time.Second
	cfg.AcceptBacklog = 1 // the relay opens streams; the host opens none
	cfg.LogOutput = io.Discard
	sess, err := yamux.Client(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	rm := &room{id: id, code: code, pin: pin, client: who, secret: sum, sess: sess, since: time.Now()}
	if s.lim.roomRate > 0 {
		rm.bw = rate.NewLimiter(rate.Limit(s.lim.roomRate), burst(s.lim.roomRate))
	}
	s.mu.Lock()
	// Checked again: another may have come meanwhile.
	if why, _ := s.mayOpenLocked(id, code, pin, who, sum); why != "" {
		s.mu.Unlock()
		sess.Close()
		return
	}
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

func (s *server) mayOpen(id, code, pin, who string, sum [32]byte) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mayOpenLocked(id, code, pin, who, sum)
}

// mayOpenLocked says why a host may not have its room, if it may not.
func (s *server) mayOpenLocked(id, code, pin, who string, sum [32]byte) (string, int) {
	old := s.rooms[id]
	if old != nil {
		if subtle.ConstantTimeCompare(old.secret[:], sum[:]) != 1 || old.pin != pin {
			return "the room is another host's", http.StatusConflict
		}
		if other := s.codes[code]; other != nil && other != old {
			return "another Jam has that code", http.StatusConflict
		}
		return "", 0
	}
	if len(s.rooms) >= s.lim.rooms {
		return "the relay is full", http.StatusServiceUnavailable
	}
	if s.codes[code] != nil {
		return "another Jam has that code", http.StatusConflict
	}
	mine := 0
	for _, rm := range s.rooms {
		if rm.client == who {
			mine++
		}
	}
	if mine >= s.lim.roomsPerClient {
		return "too many Jams from one address", http.StatusTooManyRequests
	}
	return "", 0
}

// serveTunnel is a guest's tunnel to a room's host.
func (s *server) serveTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.tunnelTries.allow(s.client(r)) {
		http.Error(w, "too many connections; wait a minute", http.StatusTooManyRequests)
		return
	}
	s.mu.Lock()
	rm := s.rooms[r.PathValue("room")]
	s.mu.Unlock()
	if rm == nil {
		http.Error(w, "no Jam is here: it ended, or its host lost the connection", http.StatusNotFound)
		return
	}
	if s.tunnels.Add(1) > int64(s.lim.tunnels) {
		s.tunnels.Add(-1)
		http.Error(w, "the relay is full", http.StatusServiceUnavailable)
		return
	}
	defer s.tunnels.Add(-1)
	if rm.tunnels.Add(1) > int64(s.lim.tunnelsPerRoom) {
		rm.tunnels.Add(-1)
		http.Error(w, "too many connections to this Jam", http.StatusServiceUnavailable)
		return
	}
	defer rm.tunnels.Add(-1)
	stream, err := rm.sess.OpenStream()
	if err != nil {
		http.Error(w, "the Jam's host did not answer", http.StatusBadGateway)
		return
	}
	defer stream.Close()
	ws, err := accept(w, r)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := websocket.NetConn(ctx, ws, websocket.MessageBinary)
	defer conn.Close()
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	done := make(chan struct{}, 2)
	pipe := func(dst io.Writer, src io.Reader) {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, chunk)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				last.Store(time.Now().UnixNano())
				if s.wait(ctx, rm, n) != nil {
					return
				}
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	go pipe(stream, conn)
	go pipe(conn, stream)
	// A tunnel that carries nothing for long is let go: the guest makes
	// another when it wants one.
	idle := time.NewTicker(time.Minute)
	defer idle.Stop()
	for {
		select {
		case <-done:
			return
		case <-rm.sess.CloseChan():
			return
		case <-idle.C:
			if time.Since(time.Unix(0, last.Load())) > 5*time.Minute {
				return
			}
		}
	}
}

// wait holds n bytes back for as long as the caps say.
func (s *server) wait(ctx context.Context, rm *room, n int) error {
	if rm.bw != nil {
		if err := rm.bw.WaitN(ctx, n); err != nil {
			return err
		}
	}
	if s.bw != nil {
		return s.bw.WaitN(ctx, n)
	}
	return nil
}

func (s *server) serveLookup(w http.ResponseWriter, r *http.Request) {
	if !s.lookups.allow(s.client(r)) || !s.allLookups.Allow() {
		http.Error(w, "too many tries; wait a minute", http.StatusTooManyRequests)
		return
	}
	q := r.URL.Query()
	code := strings.ToUpper(strings.ReplaceAll(q.Get("code"), " ", ""))
	id := q.Get("room")
	s.mu.Lock()
	var rm *room
	switch {
	case codeRE.MatchString(code):
		rm = s.codes[code]
	case roomRE.MatchString(id):
		rm = s.rooms[id]
	}
	s.mu.Unlock()
	if rm == nil {
		http.Error(w, "no Jam has that code here", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"room": rm.id, "pin": rm.pin})
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
		if len(l.seen) > 50000 {
			// Too many to keep: everyone starts again.
			clear(l.seen)
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

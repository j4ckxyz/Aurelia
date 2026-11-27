package jam

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

// A relay (relay/ in Aurelia's source), the public one or the user's own,
// lets a Jam be joined from any network: the host connects out to it, and
// guests reach the host through it, by tunnels of their own. Inside a
// tunnel, guest and host speak TLS, the guest checking the host's key
// against its fingerprint, which the invite carries: the relay passes on
// what it cannot read, nor change. The host serves guests there as it
// serves those of its network; nothing of the Jam's own changes.
//
// A Jam's address at a relay is https://relay.example/v2/r/<room>#<pin>:
// the fingerprint is after the #, which is never sent to the relay.

// relayClient connects to relays: never through a proxy, and over
// HTTP/1.1, whose connection becomes a WebSocket.
var relayClient = &http.Client{Transport: &http.Transport{
	Proxy:                 nil,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 15 * time.Second,
	IdleConnTimeout:       30 * time.Second,
}}

// RelayError is a relay's refusal: its status, and why.
type RelayError struct {
	Status int
	Text   string
}

func (e *RelayError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "the relay turned the key away"
	case http.StatusServiceUnavailable:
		return "the relay is full"
	case http.StatusNotFound:
		return "this is not a Jam relay, or not its address"
	}
	if e.Text != "" {
		return "the relay: " + e.Text
	}
	return fmt.Sprintf("the relay answered %d", e.Status)
}

func relayError(resp *http.Response, err error) error {
	if resp == nil || resp.StatusCode == http.StatusSwitchingProtocols {
		return err
	}
	var b []byte
	if resp.Body != nil {
		b, _ = io.ReadAll(io.LimitReader(resp.Body, 300))
	}
	return &RelayError{Status: resp.StatusCode, Text: strings.TrimSpace(string(b))}
}

// relayRoom is the host's room at relays, its secret and its key: the
// same across reconnections, so its invites stay good.
func (h *Host) relayRoom() (room, secret string, cert *tls.Certificate, pin string) {
	h.relayOnce.Do(func() {
		h.room, h.roomSecret = token(16), token(16)
		h.cert, h.pin = newCert()
	})
	return h.room, h.roomSecret, h.cert, h.pin
}

// newCert makes the host's key, for its tunnels: its own, for this Jam
// alone, with a certificate of no one's but its own.
func newCert() (*tls.Certificate, string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Aurelia Jam"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"jam.aurelia"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pinOf(leaf)
}

// pinOf is a certificate's fingerprint: of its public key.
func pinOf(c *x509.Certificate) string {
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ServeRelay keeps the Jam reachable through the relay at base, as
// https://relay.example, with its key ("" for a relay open to all), until
// ctx ends or the host closes. It connects again as the connection drops:
// soon, then less often, and rarely after a refusal. report is told the
// Jam's address through the relay as it is reached, and why not as it is
// lost ("" and the error).
func (h *Host) ServeRelay(ctx context.Context, base, key string, report func(addr string, err error)) {
	base = strings.TrimSuffix(strings.TrimSpace(base), "/")
	wait := time.Second
	for ctx.Err() == nil {
		h.mu.Lock()
		closed := h.closed
		h.mu.Unlock()
		if closed {
			return
		}
		began := time.Now()
		err := h.serveRelayOnce(ctx, base, key, report)
		if ctx.Err() != nil {
			return
		}
		report("", err)
		var re *RelayError
		switch {
		case errors.As(err, &re) && re.Status == http.StatusTooManyRequests:
			wait = time.Minute
		case errors.As(err, &re) && re.Status < 500:
			wait = 2 * time.Minute // the key, or the address, is wrong: it stays so a while
		case time.Since(began) > time.Minute:
			wait = time.Second // it was up: back at once
		default:
			wait = min(2*wait, 30*time.Second)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (h *Host) serveRelayOnce(ctx context.Context, base, key string, report func(addr string, err error)) error {
	room, secret, cert, pin := h.relayRoom()
	hd := http.Header{"X-Room-Secret": {secret}}
	if key != "" {
		hd.Set("Authorization", "Bearer "+key)
	}
	u := relayWS(base) + "/v2/host?" + url.Values{"room": {room}, "code": {h.code}, "pin": {pin}}.Encode()
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	ws, resp, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPClient: relayClient, HTTPHeader: hd, CompressionMode: websocket.CompressionDisabled})
	cancel()
	if err != nil {
		return relayError(resp, err)
	}
	conn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.ConnectionWriteTimeout = 30 * time.Second
	cfg.AcceptBacklog = 64
	cfg.LogOutput = io.Discard
	sess, err := yamux.Server(conn, cfg)
	if err != nil {
		conn.Close()
		return err
	}
	defer sess.Close()
	stop := context.AfterFunc(ctx, func() { sess.Close() })
	defer stop()
	srv := &http.Server{
		Handler:           relayed(h.srv.Handler),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	h.mu.Lock()
	h.relays = append(h.relays, srv)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		for i, s := range h.relays {
			if s == srv {
				h.relays = append(h.relays[:i], h.relays[i+1:]...)
				break
			}
		}
		h.mu.Unlock()
		srv.Close()
	}()
	report(RelayAddr(base, room, pin), nil)
	ln := tls.NewListener(limitListener(sess, 64), &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"http/1.1"},
	})
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, yamux.ErrSessionShutdown) || sess.IsClosed() {
		err = errors.New("the connection to the relay was lost")
	}
	return err
}

// relayed serves what comes through a relay: from nobody the network
// vouches for, whatever the relay's own address is (a relay on the
// tailnet is a device of the host's own, which Identify would let in).
func relayed(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "relay"
		h.ServeHTTP(w, r)
	})
}

// limitListener lets n connections at once be served, of those the relay
// opens: more wait.
func limitListener(l net.Listener, n int) net.Listener {
	return &limited{Listener: l, sem: make(chan struct{}, n)}
}

type limited struct {
	net.Listener
	sem chan struct{}
}

func (l *limited) Accept() (net.Conn, error) {
	l.sem <- struct{}{}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.sem
		return nil, err
	}
	return &release{Conn: c, done: func() { <-l.sem }}, nil
}

type release struct {
	net.Conn
	once sync.Once
	done func()
}

func (r *release) Close() error {
	err := r.Conn.Close()
	r.once.Do(r.done)
	return err
}

// Read tells of a deadline as one that passes: TLS takes any other error
// as the end of the connection, and the HTTP server sets a deadline in
// the past to stop a read when a WebSocket takes the connection over.
func (r *release) Read(b []byte) (int, error) {
	n, err := r.Conn.Read(b)
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		err = timeoutErr{err}
	}
	return n, err
}

type timeoutErr struct{ error }

func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var (
	relayRoomRE = regexp.MustCompile(`^[0-9a-f]{32}$`)
	relayPinRE  = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

// RelayAddr is a Jam's address at the relay at base.
func RelayAddr(base, room, pin string) string {
	return strings.TrimSuffix(base, "/") + "/v2/r/" + room + "#" + pin
}

// parseRelayAddr reads a Jam's address at a relay: the relay's, the
// Jam's room there, and the host's fingerprint, "" when it has none.
func parseRelayAddr(addr string) (base, room, pin string, ok bool) {
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return "", "", "", false
	}
	prefix, room, found := strings.Cut(u.Path, "/v2/r/")
	room = strings.TrimSuffix(room, "/")
	if !found || !relayRoomRE.MatchString(room) {
		return "", "", "", false
	}
	if u.Fragment != "" && !relayPinRE.MatchString(u.Fragment) {
		return "", "", "", false
	}
	return u.Scheme + "://" + u.Host + prefix, room, u.Fragment, true
}

// LookupRelay finds the Jam of a code at the relay at base: its address
// there. Since it is the relay that says which key is the host's, a Jam
// joined so is as private as the relay is trusted; an invite says itself.
func LookupRelay(ctx context.Context, base, code string) (string, error) {
	return lookupRelay(ctx, base, url.Values{"code": {NormalizeCode(code)}})
}

func lookupRelay(ctx context.Context, base string, q url.Values) (string, error) {
	base = strings.TrimSuffix(strings.TrimSpace(base), "/")
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/v2/lookup?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := relayClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		if resp.StatusCode == http.StatusNotFound && strings.Contains(string(b), "no Jam") {
			return "", errors.New("no Jam with that code is on the relay")
		}
		return "", &RelayError{Status: resp.StatusCode, Text: strings.TrimSpace(string(b))}
	}
	var got struct{ Room, Pin string }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&got); err != nil || !relayRoomRE.MatchString(got.Room) || !relayPinRE.MatchString(got.Pin) {
		return "", errors.New("the relay's answer is not one")
	}
	return RelayAddr(base, got.Room, got.Pin), nil
}

// pinned is a relay's address of a Jam with its host's fingerprint: as
// given, or as the relay says when it has none.
func pinned(ctx context.Context, addr string) (string, error) {
	base, room, pin, ok := parseRelayAddr(addr)
	if !ok {
		return "", errors.New("not a Jam's address at a relay")
	}
	if pin != "" {
		return addr, nil
	}
	return lookupRelay(ctx, base, url.Values{"room": {room}})
}

// IsRelayAddr reports whether a host's address is a relay's: a URL, not
// an address of a network.
func IsRelayAddr(addr string) bool {
	return strings.HasPrefix(addr, "https://") || strings.HasPrefix(addr, "http://")
}

// Tunnels: a guest's requests to a host at a relay go to a name of its
// own, which the Jam's client dials through the relay.
const tunnelSuffix = ".tunnel.aurelia.invalid"

var tunnels sync.Map // name → relay address

// tunnelName is the name a relay's address of a Jam is reached at.
func tunnelName(addr string) string {
	sum := sha256.Sum256([]byte(addr))
	name := fmt.Sprintf("%x", sum[:10]) + tunnelSuffix
	tunnels.LoadOrStore(name, addr)
	return name
}

// dialTunnel connects to a host through a relay: a WebSocket to the
// relay, and TLS inside it with the host, whose key must be the one of
// the address.
func dialTunnel(ctx context.Context, name string) (net.Conn, error) {
	v, ok := tunnels.Load(strings.TrimSuffix(name, ":80"))
	if !ok {
		return nil, errors.New("jam: no such tunnel")
	}
	base, room, pin, ok := parseRelayAddr(v.(string))
	if !ok || pin == "" {
		return nil, errors.New("jam: the address has no fingerprint of the host")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ws, resp, err := websocket.Dial(ctx, relayWS(base)+"/v2/r/"+room, &websocket.DialOptions{HTTPClient: relayClient, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return nil, relayError(resp, err)
	}
	conn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
	want, _ := base64.RawURLEncoding.DecodeString(pin)
	tc := tls.Client(conn, &tls.Config{
		// The host's certificate is its own; what vouches for it is its
		// key's fingerprint, checked here.
		InsecureSkipVerify: true,
		ServerName:         "jam.aurelia",
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"http/1.1"},
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("jam: the host gave no key")
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].RawSubjectPublicKeyInfo)
			if subtle.ConstantTimeCompare(sum[:], want) != 1 {
				return ErrNotHost
			}
			return nil
		},
	})
	if err := tc.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return tc, nil
}

// ErrNotHost is the error of a tunnel to someone other than the host the
// address names: a relay that is not to be trusted.
var ErrNotHost = errors.New("jam: what answered through the relay is not the Jam's host")

// dialJam dials a host: on the network, or through a relay.
func dialJam(ctx context.Context, network, addr string) (net.Conn, error) {
	if strings.HasSuffix(strings.TrimSuffix(addr, ":80"), tunnelSuffix) {
		return dialTunnel(ctx, addr)
	}
	return netDialer.DialContext(ctx, network, addr)
}

var netDialer = &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}

// httpBase is the URL a host's address is reached at.
func httpBase(addr string) string {
	if IsRelayAddr(addr) {
		return "http://" + tunnelName(addr)
	}
	return "http://" + addr
}

// wsBase is the URL a host's address is reached at, for a WebSocket.
func wsBase(addr string) string { return "ws://" + strings.TrimPrefix(httpBase(addr), "http://") }

// relayWS is a relay's own URL, for a WebSocket.
func relayWS(base string) string {
	if s, ok := strings.CutPrefix(base, "https://"); ok {
		return "wss://" + strings.TrimSuffix(s, "/")
	}
	return "ws://" + strings.TrimSuffix(strings.TrimPrefix(base, "http://"), "/")
}

// Aurelia-proxy is a small proxy to run on a machine of your own, for
// Aurelia to reach a server that the network you are on blocks.
//
// It is not an open proxy. It speaks TLS, so that the network sees
// neither its password nor where it is asked to go; it asks for a name
// and a password; and it connects only to the hosts it is told to allow,
// at public addresses, on the ports allowed. Whoever learned its
// password could reach those hosts through it, and nothing else: not the
// machine it runs on, nor that machine's network. On Linux it also gives
// up, as it starts, every file and every connection it does not need, so
// that a fault in it would not open the machine either.
//
//	aurelia-proxy -allow music.example.com -name 203.0.113.5
//	aurelia-proxy -name 203.0.113.5 url     the address to give Aurelia
//
// The first run makes a certificate and a password in -dir. Aurelia is
// given the certificate's fingerprint with the address, and takes no
// other: no certificate authority is involved.
package main

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
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type config struct {
	listen, dir, name, user string
	allow                   []string // hosts, or *.domain
	ports                   map[string]bool
	idle                    time.Duration
	tunnels                 int
}

func main() {
	var c config
	home, _ := os.UserConfigDir()
	allow := flag.String("allow", "", "the hosts to connect to, separated by commas; *.example.com allows every host of a domain")
	ports := flag.String("ports", "443", "the ports to connect to, separated by commas")
	flag.StringVar(&c.listen, "listen", ":8443", "the address to listen on, or several separated by commas: the first is the one given to Aurelia, and one that cannot be listened on is left out")
	flag.StringVar(&c.dir, "dir", filepath.Join(home, "aurelia-proxy"), "where the certificate and the password are kept")
	flag.StringVar(&c.name, "name", "", "the address clients reach this machine at, a name or an IP address: for the certificate, and for the address given to Aurelia")
	flag.StringVar(&c.user, "user", "aurelia", "the name clients sign in with")
	flag.DurationVar(&c.idle, "idle", 10*time.Minute, "how long a connection may pass nothing before it is closed")
	flag.IntVar(&c.tunnels, "tunnels", 64, "how many connections may be open at once")
	requireSandbox := flag.Bool("sandbox", false, "do not run without the sandbox, which Linux gives")
	flag.Parse()
	for _, h := range strings.Split(*allow, ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			c.allow = append(c.allow, h)
		}
	}
	c.ports = map[string]bool{}
	for _, p := range strings.Split(*ports, ",") {
		if p = strings.TrimSpace(p); p != "" {
			c.ports[p] = true
		}
	}
	log.SetFlags(log.LstdFlags)
	if c.name == "" {
		log.Fatal("-name is needed: the address clients reach this machine at")
	}
	cert, password, err := secrets(c.dir, c.name)
	if err != nil {
		log.Fatal(err)
	}
	if flag.Arg(0) == "url" {
		fmt.Println(proxyURL(c, cert, password))
		return
	}
	if len(c.allow) == 0 {
		log.Fatal("-allow is needed: the hosts this proxy may connect to")
	}
	p := newProxy(c, password)
	srv := &http.Server{
		Addr:              c.listen,
		Handler:           p,
		ReadHeaderTimeout: 15 * time.Second,
		MaxHeaderBytes:    8 << 10,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		// Tunnels take over their connection, which HTTP/2 has none of.
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){},
		ErrorLog:     log.New(io.Discard, "", 0), // scanners' noise
	}
	// Listening first, then giving up what is not needed any more: the
	// certificate and the password are in memory.
	var lns []net.Listener
	for _, addr := range listenAddrs(c.listen) {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			// As a port under 1024 where the system keeps those to root.
			log.Printf("not listening on %s: %v", addr, err)
			continue
		}
		lns = append(lns, ln)
	}
	if len(lns) == 0 {
		log.Fatal("nowhere to listen")
	}
	connect := []string{"53"} // the resolver's, when an answer is long
	for port := range c.ports {
		connect = append(connect, port)
	}
	sort.Strings(connect)
	what, err := sandbox([]string{"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf", "/run/systemd/resolve"}, connect)
	switch {
	case err == nil:
		log.Printf("sandboxed: %s", what)
	case *requireSandbox:
		log.Fatalf("no sandbox, and -sandbox asks for one: %v", err)
	default:
		log.Printf("not sandboxed: %v", err)
	}
	failed := make(chan error, len(lns))
	for _, ln := range lns {
		log.Printf("listening on %s; allowing %s on ports %s", ln.Addr(), strings.Join(c.allow, ", "), *ports)
		go func() { failed <- srv.ServeTLS(ln, "", "") }()
	}
	log.Fatal(<-failed)
}

// listenAddrs splits what -listen was given.
func listenAddrs(s string) []string {
	var out []string
	for _, a := range strings.Split(s, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// secrets returns the certificate and the password kept in dir, making
// them on the first run.
func secrets(dir, name string) (tls.Certificate, string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, "", err
	}
	certFile, keyFile, passFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"), filepath.Join(dir, "password")
	if _, err := os.Stat(certFile); errors.Is(err, os.ErrNotExist) {
		if err := makeCertificate(certFile, keyFile, name); err != nil {
			return tls.Certificate{}, "", err
		}
	}
	if _, err := os.Stat(passFile); errors.Is(err, os.ErrNotExist) {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return tls.Certificate{}, "", err
		}
		if err := os.WriteFile(passFile, []byte(base64.RawURLEncoding.EncodeToString(b)+"\n"), 0o600); err != nil {
			return tls.Certificate{}, "", err
		}
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	b, err := os.ReadFile(passFile)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	password := strings.TrimSpace(string(b))
	if len(password) < 16 {
		return tls.Certificate{}, "", fmt.Errorf("the password in %s is too short to be safe", passFile)
	}
	return cert, password, nil
}

// makeCertificate makes a certificate for name that lasts ten years. It
// is signed by nobody: Aurelia takes it by its fingerprint.
func makeCertificate(certFile, keyFile, name string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "aurelia-proxy"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(name); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{name}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

// pin is the fingerprint of a certificate's public key, as Aurelia takes
// it.
func pin(cert tls.Certificate) string {
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// proxyURL is the address to give Aurelia.
func proxyURL(c config, cert tls.Certificate, password string) string {
	port := "8443"
	if addrs := listenAddrs(c.listen); len(addrs) > 0 {
		_, port, _ = net.SplitHostPort(addrs[0])
	}
	u := url.URL{Scheme: "https", User: url.UserPassword(c.user, password), Host: net.JoinHostPort(c.name, port), Fragment: "pin-sha256=" + pin(cert)}
	return u.String()
}

type proxy struct {
	c     config
	want  [sha256.Size]byte // of "user:password"
	slots chan struct{}
	// lookup finds a name's addresses; tests put their own.
	lookup func(ctx context.Context, host string) ([]net.IPAddr, error)
	// dial connects to a vetted address; tests put their own.
	dial func(ctx context.Context, addr string) (net.Conn, error)
	// public reports whether an address is one of the internet; tests
	// allow their own.
	public func(net.IP) bool

	mu     sync.Mutex
	failed map[string]*failures
}

// failures are the wrong passwords an address gave lately.
type failures struct {
	n     int
	since time.Time
}

func newProxy(c config, password string) *proxy {
	return &proxy{
		c: c, want: sha256.Sum256([]byte(c.user + ":" + password)), slots: make(chan struct{}, c.tunnels),
		lookup: net.DefaultResolver.LookupIPAddr, failed: map[string]*failures{}, public: publicAddress,
		dial: func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
		},
	}
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	client, _, _ := net.SplitHostPort(r.RemoteAddr)
	if p.blocked(client) {
		http.Error(w, "too many wrong passwords", http.StatusTooManyRequests)
		return
	}
	if !p.authorized(r) {
		p.wrong(client)
		time.Sleep(time.Second) // guessing is slow
		w.Header().Set("Proxy-Authenticate", `Basic realm="aurelia-proxy"`)
		http.Error(w, "sign in", http.StatusProxyAuthRequired)
		return
	}
	if r.Method != http.MethodConnect {
		http.Error(w, "this proxy makes tunnels only", http.StatusMethodNotAllowed)
		return
	}
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || !p.allowed(host, port) {
		log.Printf("%s: refused %s", client, r.Host)
		http.Error(w, "this proxy does not connect there", http.StatusForbidden)
		return
	}
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	up, err := p.connect(r.Context(), host, port)
	if err != nil {
		log.Printf("%s: %s: %v", client, r.Host, err)
		http.Error(w, "could not connect", http.StatusBadGateway)
		return
	}
	defer up.Close()
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no tunnel", http.StatusInternalServerError)
		return
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
		return
	}
	start := time.Now()
	sent, received := p.pipe(conn, buf.Reader, up)
	log.Printf("%s: %s: %d bytes there, %d back, in %s", client, r.Host, sent, received, time.Since(start).Round(time.Second))
}

// authorized reports whether a request carries the name and the
// password, compared so that the time taken tells nothing of them.
func (p *proxy) authorized(r *http.Request) bool {
	const prefix = "Basic "
	h := r.Header.Get("Proxy-Authorization")
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(h[len(prefix):]))
	if err != nil {
		return false
	}
	got := sha256.Sum256(raw)
	return subtle.ConstantTimeCompare(got[:], p.want[:]) == 1
}

// An address that gives ten wrong passwords in ten minutes is turned
// away for the rest of them.
const (
	maxFailures   = 10
	failureWindow = 10 * time.Minute
)

func (p *proxy) blocked(client string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	f := p.failed[client]
	if f == nil {
		return false
	}
	if time.Since(f.since) > failureWindow {
		delete(p.failed, client)
		return false
	}
	return f.n >= maxFailures
}

func (p *proxy) wrong(client string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.failed) > 10000 { // a flood of addresses: forget the oldest part
		for k, f := range p.failed {
			if time.Since(f.since) > failureWindow/2 {
				delete(p.failed, k)
			}
		}
		if len(p.failed) > 10000 {
			clear(p.failed)
		}
	}
	f := p.failed[client]
	if f == nil || time.Since(f.since) > failureWindow {
		f = &failures{since: time.Now()}
		p.failed[client] = f
	}
	f.n++
}

// allowed reports whether the proxy may connect to a host and a port.
func (p *proxy) allowed(host, port string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if !p.c.ports[port] || net.ParseIP(host) != nil {
		return false // by name only: an address could be anyone's
	}
	for _, a := range p.c.allow {
		if a == host || strings.HasPrefix(a, "*.") && strings.HasSuffix(host, a[1:]) && len(host) > len(a)-1 {
			return true
		}
	}
	return false
}

// connect connects to a host at one of its public addresses. A name that
// leads to this machine, or to a private network, is refused: the proxy
// is a way out, not a way in.
func (p *proxy) connect(ctx context.Context, host, port string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	addrs, err := p.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	var last error = errors.New("the name leads to no public address")
	for _, a := range addrs {
		if !p.public(a.IP) {
			continue
		}
		// To the address that was checked, not to the name again.
		conn, err := p.dial(ctx, net.JoinHostPort(a.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

var notPublic = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
		"240.0.0.0/4", "64:ff9b::/96", "2001:db8::/32", "fc00::/7",
	} {
		_, n, _ := net.ParseCIDR(cidr)
		nets = append(nets, n)
	}
	return nets
}()

// publicAddress reports whether an address is one of the internet: not
// this machine, a private network, a link, or a range kept for other
// uses, as that of carriers' networks, which VPNs use.
func publicAddress(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, n := range notPublic {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// pipe passes bytes both ways until either side ends, or nothing passes
// for the idle time. It returns the bytes passed each way.
func (p *proxy) pipe(client net.Conn, fromClient io.Reader, up net.Conn) (sent, received int64) {
	relay := func(dst net.Conn, src io.Reader, srcConn net.Conn, n *int64, done chan<- struct{}) {
		buf := make([]byte, 32<<10)
		for {
			srcConn.SetReadDeadline(time.Now().Add(p.c.idle))
			m, err := src.Read(buf)
			if m > 0 {
				dst.SetWriteDeadline(time.Now().Add(time.Minute))
				if _, werr := dst.Write(buf[:m]); werr != nil {
					break
				}
				*n += int64(m)
			}
			if err != nil {
				break
			}
		}
		// One way ended: so does the other.
		client.Close()
		up.Close()
		done <- struct{}{}
	}
	done := make(chan struct{}, 2)
	// What the client sent along with its request comes first: it is in
	// the reader the request was read from.
	go relay(up, fromClient, client, &sent, done)
	go relay(client, up, up, &received, done)
	<-done
	<-done
	return sent, received
}

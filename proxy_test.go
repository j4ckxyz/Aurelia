package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
)

func TestParseProxy(t *testing.T) {
	for in, want := range map[string]string{
		"":                                 "",
		"  ":                               "",
		"socks5://10.0.0.5:1080":           "socks5://10.0.0.5:1080",
		"SOCKS5://Proxy.Example:1080/":     "socks5://Proxy.Example:1080",
		"socks5h://proxy.example:1080":     "socks5h://proxy.example:1080",
		"proxy.example:8080":               "http://proxy.example:8080",
		"http://user:secret@proxy.ex:3128": "http://user:secret@proxy.ex:3128",
		"https://proxy.example:443/path?x": "https://proxy.example:443",
	} {
		u, err := parseProxy(in)
		got := ""
		if u != nil {
			got = u.String()
		}
		if err != nil || got != want {
			t.Errorf("parseProxy(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"socks4://host:1080", "ftp://host:21", "http://host", "host", "http://:8080", "socks5://"} {
		if u, err := parseProxy(bad); err == nil {
			t.Errorf("parseProxy(%q) = %v, and no error", bad, u)
		}
	}
	for host, want := range map[string]bool{"localhost": true, "127.0.0.1": true, "192.168.1.20": true, "10.1.2.3": true, "::1": true,
		"music.example.com": false, "8.8.8.8": false, "jellyfin.local": false} {
		if got := local(host); got != want {
			t.Errorf("local(%q) = %v", host, got)
		}
	}
	if proxyRisk("http://music.example") == "" || proxyRisk("https://music.example") != "" || proxyRisk("music.example") != "" {
		t.Error("the risk of a proxy is told for the wrong addresses")
	}
}

// blocked is a server's name that this network does not resolve, as on a
// network that blocks it.
const blocked = "music.blocked.test"

// fakeJellyfin answers what signing in asks.
func fakeJellyfin(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/System/Info/Public":
			json.NewEncoder(w).Encode(map[string]string{"ServerName": "Home", "Id": "srv", "Version": "10.11"})
		case "/Users/AuthenticateByName":
			var in struct{ Username, Pw string }
			json.NewDecoder(r.Body).Decode(&in)
			if in.Pw != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"AccessToken": "token", "ServerId": "srv", "User": map[string]string{"Name": in.Username, "Id": "u"}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// httpProxy is a proxy that knows where the blocked server is, and notes
// what it is asked.
func httpProxy(t *testing.T, target string) (address string, asked func() []string) {
	t.Helper()
	var mu sync.Mutex
	var log []string
	direct := &http.Transport{Proxy: nil}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		log = append(log, r.Method+" "+r.RequestURI)
		mu.Unlock()
		if r.Method == http.MethodConnect {
			// A tunnel: the proxy passes bytes it cannot read.
			up, err := net.Dial("tcp", target)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				up.Close()
				return
			}
			conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
			go func() { io.Copy(up, conn); up.Close() }()
			go func() { io.Copy(conn, up); conn.Close() }()
			return
		}
		if r.URL.Hostname() != blocked {
			http.Error(w, "unknown host", http.StatusBadGateway)
			return
		}
		out := r.Clone(r.Context())
		out.RequestURI = ""
		out.URL.Host = target
		resp, err := direct.RoundTrip(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://"), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), log...)
	}
}

// socksProxy is a SOCKS5 proxy that knows where the blocked server is,
// and notes the names it is asked for.
func socksProxy(t *testing.T, target string) (address string, asked func() []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var names []string
	serve := func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 262)
		// The greeting: version, and the ways to sign in it offers.
		if _, err := io.ReadFull(conn, buf[:2]); err != nil || buf[0] != 5 {
			return
		}
		io.ReadFull(conn, buf[:buf[1]])
		conn.Write([]byte{5, 0}) // no sign-in
		// The request: connect to a name.
		if _, err := io.ReadFull(conn, buf[:4]); err != nil || buf[1] != 1 {
			return
		}
		if buf[3] != 3 { // not a name: the client resolved it itself
			conn.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		io.ReadFull(conn, buf[:1])
		n := int(buf[0])
		io.ReadFull(conn, buf[:n+2])
		name, port := string(buf[:n]), binary.BigEndian.Uint16(buf[n:])
		mu.Lock()
		names = append(names, name)
		mu.Unlock()
		_ = port
		if name != blocked {
			conn.Write([]byte{5, 4, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		up, err := net.Dial("tcp", target)
		if err != nil {
			conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		defer up.Close()
		conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		go io.Copy(up, conn)
		io.Copy(conn, up)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(conn)
		}
	}()
	return ln.Addr().String(), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), names...)
	}
}

// A server whose name this network does not resolve is signed in to
// through a proxy, HTTP or SOCKS5, from the sign-in page.
func TestSignInThroughAProxy(t *testing.T) {
	server := fakeJellyfin(t)
	target := strings.TrimPrefix(server.URL, "http://")
	httpAddr, httpAsked := httpProxy(t, target)
	socksAddr, socksAsked := socksProxy(t, target)

	a := testApp(t)
	t.Cleanup(func() { a.setProxy("") })
	a.signOut()
	a.drain()
	tt := ui.NewTester(a.view, 1240, 900)
	wantTexts(t, tt, "Sign in to your Jellyfin server", "Connect through a proxy")

	// Without a proxy, the server is not found, and the page says what
	// may help.
	f := &a.login
	f.server, f.user, f.password = "http://"+blocked, "ada", "secret"
	a.signIn()
	wait(t, a, "the sign-in to fail", func() bool { return !f.busy })
	tt.Frame()
	if a.signedIn() || !strings.Contains(f.err, "proxy may reach it") || !f.proxyShown {
		t.Fatalf("without a proxy: signed in %v, %q", a.signedIn(), f.err)
	}
	wantTexts(t, tt, "Proxy")
	// A proxy's address that is none is told.
	f.proxy = "socks4://nope:1"
	a.signIn()
	if f.busy || !strings.Contains(f.err, "SOCKS4") {
		t.Errorf("a proxy that is none: busy %v, %q", f.busy, f.err)
	}
	// A proxy that is not there.
	f.proxy = "http://127.0.0.1:9"
	a.signIn()
	wait(t, a, "the sign-in to fail", func() bool { return !f.busy })
	if a.signedIn() || !strings.Contains(f.err, "The proxy did not let the connection through") {
		t.Errorf("a proxy that is not there: %q", f.err)
	}

	// Through the HTTP proxy.
	f.proxy = httpAddr
	tt.Frame()
	// The server's address is http://: the page says what the proxy sees.
	if !tt.HasText(proxyRisk(f.server)) {
		t.Errorf("no warning for a server without https; texts %q", tt.Texts())
	}
	a.signIn()
	wait(t, a, "the sign-in through the HTTP proxy", func() bool { return !f.busy })
	if !a.signedIn() || a.settings.Session.UserName != "ada" || a.settings.Proxy != httpAddr {
		t.Fatalf("through the HTTP proxy: signed in %v, %q; the proxy kept is %q", a.signedIn(), f.err, a.settings.Proxy)
	}
	if asked := httpAsked(); len(asked) < 2 || !strings.Contains(asked[0], blocked) {
		t.Errorf("the HTTP proxy was asked %q", asked)
	}
	// The next run uses it too.
	if got := loadSettings(a.dirs).Proxy; got != httpAddr {
		t.Errorf("the settings on disk keep the proxy %q", got)
	}

	// Through the SOCKS5 proxy, from Settings: the proxy is given the
	// server's name, which it resolves, not this network.
	cn := &a.connection
	cn.proxy = "socks5://" + socksAddr
	a.applyProxy()
	wait(t, a, "the test of the connection", func() bool { return !cn.testing })
	if !cn.reached || !strings.Contains(cn.status, "Reached Home through the proxy") {
		t.Errorf("through the SOCKS5 proxy: %q %q", cn.status, cn.err)
	}
	if asked := socksAsked(); len(asked) == 0 || asked[0] != blocked {
		t.Errorf("the SOCKS5 proxy was asked for %q", asked)
	}
	a.router.Push("/settings")
	tt.SetSize(1240, 2400)
	tt.Frame()
	wantTexts(t, tt, "Connection", "Proxy", cn.status)

	// No proxy again: the server is out of reach, and Settings say so.
	cn.proxy = ""
	a.applyProxy()
	wait(t, a, "the test of the connection", func() bool { return !cn.testing })
	if cn.reached || !strings.Contains(cn.status, "Could not reach the server directly") || a.settings.Proxy != "" {
		t.Errorf("without the proxy: %q", cn.status)
	}
}

// For a server with https, a proxy is asked for a tunnel to the server
// and nothing else: it sees neither the addresses asked for nor what is
// sent, the password included.
func TestAProxyCannotReadHTTPS(t *testing.T) {
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"ServerName": "Home", "Id": "srv"})
	}))
	defer secure.Close()
	addr, asked := httpProxy(t, strings.TrimPrefix(secure.URL, "https://"))
	a := testApp(t)
	t.Cleanup(func() { a.setProxy("") })
	if err := a.setProxy(addr); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The test's server has a certificate nobody vouches for: the
	// connection is refused by Aurelia, past the proxy, which is the
	// point: the proxy cannot stand in for the server.
	_, err := jellyfin.Login(ctx, "https://"+blocked, "ada", "secret", "dev", "test")
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("a server with a certificate nobody vouches for: %v", err)
	}
	got := asked()
	if len(got) == 0 {
		t.Fatal("the proxy was not asked")
	}
	for _, line := range got {
		if line != "CONNECT "+blocked+":443" {
			t.Errorf("the proxy saw %q", line)
		}
	}
}

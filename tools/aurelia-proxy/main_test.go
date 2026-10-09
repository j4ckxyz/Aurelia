package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// testProxy runs a proxy with a certificate and a password of its own,
// allowing the hosts given. Names resolve to target, the test's server,
// which counts as public.
func testProxy(t *testing.T, target string, allow ...string) (c config, password string, cert tls.Certificate, p *proxy, addr string) {
	t.Helper()
	c = config{listen: "127.0.0.1:0", dir: t.TempDir(), name: "127.0.0.1", user: "aurelia", allow: allow,
		ports: map[string]bool{"443": true}, idle: 2 * time.Second, tunnels: 4}
	cert, password, err := secrets(c.dir, c.name)
	if err != nil {
		t.Fatal(err)
	}
	p = newProxy(c, password)
	p.public = func(ip net.IP) bool { return ip.IsLoopback() }
	p.dial = func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", target)
	}
	p.lookup = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		if host == "localhost" {
			return []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}, nil
		}
		return nil, fmt.Errorf("no DNS in tests")
	}
	srv := httptest.NewUnstartedServer(p)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	srv.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return c, password, cert, p, strings.TrimPrefix(srv.URL, "https://")
}

// ask sends a request to the proxy as a client would, and returns the
// status and the connection, which is a tunnel after a 200.
func ask(t *testing.T, addr, method, target, user, password string) (int, net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	auth := ""
	if user != "" {
		auth = "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password)) + "\r\n"
	}
	host := target
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		host = u.Host
	}
	fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", method, target, host, auth)
	br := bufio.NewReader(conn)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	resp, err := http.ReadResponse(br, &http.Request{Method: method})
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	return resp.StatusCode, conn, br
}

// The proxy passes what it is allowed to, to those who know its
// password, and nothing else.
func TestProxy(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	// localhost stands for the allowed server.
	c, password, cert, p, addr := testProxy(t, echo.Addr().String(), "localhost", "*.allowed.example")

	// The tunnel carries bytes both ways.
	status, conn, br := ask(t, addr, "CONNECT", "localhost:443", "aurelia", password)
	if status != 200 {
		t.Fatalf("an allowed host: %d", status)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(conn, "hello through the tunnel\n")
	if line, err := br.ReadString('\n'); err != nil || line != "hello through the tunnel\n" {
		t.Errorf("through the tunnel: %q, %v", line, err)
	}
	conn.Close()

	for _, tc := range []struct {
		what, method, target, user, password string
		want                                 int
	}{
		{"no password", "CONNECT", "localhost:443", "", "", 407},
		{"a wrong password", "CONNECT", "localhost:443", "aurelia", "guess", 407},
		{"a wrong name", "CONNECT", "localhost:443", "admin", password, 407},
		{"a host not allowed", "CONNECT", "example.com:443", "aurelia", password, 403},
		{"a port not allowed", "CONNECT", "localhost:22", "aurelia", password, 403},
		{"this machine by its address", "CONNECT", "127.0.0.1:443", "aurelia", password, 403},
		{"a host that only ends like an allowed one", "CONNECT", "evilallowed.example:443", "aurelia", password, 403},
		{"the bare domain of a wildcard", "CONNECT", "allowed.example:443", "aurelia", password, 403},
		{"a page, not a tunnel", "GET", "http://localhost/", "aurelia", password, 405},
	} {
		if got, _, _ := ask(t, addr, tc.method, tc.target, tc.user, tc.password); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.what, got, tc.want)
		}
	}
	if !p.allowed("cdn.allowed.example", "443") || !p.allowed("LOCALHOST.", "443") {
		t.Error("a host of an allowed domain is refused")
	}

	// A name that leads inside is refused, whatever the list says.
	p.public = publicAddress
	if got, _, _ := ask(t, addr, "CONNECT", "localhost:443", "aurelia", password); got != 502 {
		t.Errorf("an allowed name that leads to this machine: %d, want 502", got)
	}
	for ip, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.0.0.8": false, "192.168.1.1": false, "172.16.5.4": false, "169.254.169.254": false,
		"100.64.0.1": false, "100.100.100.100": false, "::1": false, "fd7a:115c:a1e0::1": false, "fe80::1": false, "0.0.0.0": false,
	} {
		if got := publicAddress(net.ParseIP(ip)); got != want {
			t.Errorf("publicAddress(%s) = %v", ip, got)
		}
	}

	// The address for Aurelia: the name, the password, and the
	// fingerprint of this very certificate.
	u, err := url.Parse(proxyURL(c, cert, password))
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := u.User.Password()
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	if u.Scheme != "https" || u.User.Username() != "aurelia" || pw != password || len(password) < 30 ||
		u.Fragment != "pin-sha256="+base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Errorf("the address for Aurelia is %s", u.Redacted())
	}
	if len(leaf.IPAddresses) != 1 || time.Until(leaf.NotAfter) < 9*365*24*time.Hour {
		t.Errorf("the certificate is for %v until %v", leaf.IPAddresses, leaf.NotAfter)
	}
	// The same ones the next time.
	cert2, password2, err := secrets(c.dir, c.name)
	if err != nil || password2 != password || pin(cert2) != pin(cert) {
		t.Errorf("the second run has other secrets: %v", err)
	}
}

// Wrong passwords are slow, and an address that keeps giving them is
// turned away.
func TestProxyTurnsGuessersAway(t *testing.T) {
	_, password, _, p, addr := testProxy(t, "127.0.0.1:9", "localhost")
	for i := 0; i < maxFailures; i++ {
		p.wrong("127.0.0.1")
	}
	if got, _, _ := ask(t, addr, "CONNECT", "localhost:443", "aurelia", password); got != 429 {
		t.Errorf("after %d wrong passwords, the right one gets %d", maxFailures, got)
	}
	// And in again once the time has passed.
	p.mu.Lock()
	p.failed["127.0.0.1"].since = time.Now().Add(-failureWindow - time.Second)
	p.mu.Unlock()
	if got, _, _ := ask(t, addr, "CONNECT", "localhost:443", "aurelia", password); got == 429 {
		t.Error("still turned away after the time passed")
	}
}

// A tunnel that passes nothing is closed.
func TestProxyClosesIdleTunnels(t *testing.T) {
	quiet, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer quiet.Close()
	go func() {
		for {
			c, err := quiet.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	_, password, _, _, addr := testProxy(t, quiet.Addr().String(), "localhost")
	status, conn, br := ask(t, addr, "CONNECT", "localhost:443", "aurelia", password)
	if status != 200 {
		t.Fatal(status)
	}
	conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	start := time.Now()
	if _, err := br.ReadByte(); err == nil {
		t.Error("the quiet tunnel sent something")
	}
	if took := time.Since(start); took < time.Second || took > 6*time.Second {
		t.Errorf("the quiet tunnel closed after %v; its idle time is 2 s", took)
	}
}

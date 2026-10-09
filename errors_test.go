package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"aurelia/internal/jellyfin"
)

// An error tells which step of the way failed, and how: these are the
// errors as the network makes them, not as a test would write them.
func TestErrorsSayWhy(t *testing.T) {
	a := testApp(t)
	t.Cleanup(func() { a.setProxy("") })
	probe := func(server string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := jellyfin.Probe(ctx, server)
		return err
	}
	check := func(what string, err error, want ...string) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: no error", what)
			return
		}
		got := friendly(err)
		t.Logf("%s:\n    %s", what, got)
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q does not say %q\n    (the error: %v)", what, got, w, err)
			}
		}
		// Nothing of how Go words it.
		for _, noise := range []string{"dial tcp", "Get \"", "proxyconnect", "jellyfin:", "->"} {
			if strings.Contains(got, noise) {
				t.Errorf("%s: %q still says %q", what, got, noise)
			}
		}
	}
	// A server that answers every request the same way.
	answering := func(code int, header map[string]string, body string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range header {
				w.Header().Set(k, v)
			}
			w.WriteHeader(code)
			w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	page := map[string]string{"Content-Type": "text/html; charset=UTF-8"}

	// Straight to the server.
	gone, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := gone.Addr().String()
	gone.Close()
	check("nothing listening", probe("http://"+closed), closed+" refused the connection", "nothing is listening")
	check("a name the network does not know", probe("http://"+blocked), "does not know the name "+blocked, "blocking")

	plainServer := fakeJellyfin(t)
	check("https asked of a server at http", probe("https://"+strings.TrimPrefix(plainServer.URL, "http://")), "does not answer at https", "http://")
	secure := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(secure.Close)
	check("a certificate nobody vouches for", probe(secure.URL), "a certificate this computer does not trust", "inspects connections")

	check("another site", probe(answering(200, page, "<html>Welcome</html>").URL), "answered with a web page", "not as a Jellyfin server")
	check("nothing at the path", probe(answering(404, nil, "").URL), "Nothing of Jellyfin was found", "404")
	check("Cloudflare's check", probe(answering(403, map[string]string{"Cf-Mitigated": "challenge", "Content-Type": "text/html", "Server": "cloudflare"}, "<html>Just a moment...</html>").URL),
		"Cloudflare", "only a browser passes", "403")
	check("a block page", probe(answering(403, page, "<html>Blocked by policy</html>").URL), "a page that refuses", "network that blocks")
	check("a front with no server behind", probe(answering(502, page, "<html>Bad gateway</html>").URL), "Jellyfin behind it did not", "502")
	check("a server's own error", probe(answering(500, nil, "").URL), "an error of its own", "500")

	_, err = jellyfin.Login(context.Background(), plainServer.URL, "ada", "wrong", "dev", "test")
	check("a wrong password", err, "refused the sign-in", "password")

	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	t.Cleanup(hung.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	_, err = jellyfin.Probe(ctx, hung.URL)
	cancel()
	check("a server that does not answer", err, "was reached, but its answer did not come in time")

	hangsUp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hangsUp.Close() })
	go func() {
		for {
			c, err := hangsUp.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	check("a connection cut", probe("http://"+hangsUp.Addr().String()), "was cut as it began")

	// A connection nothing answers takes the time it is given: this is
	// the error it ends with.
	silent := func(op string, inner error) error {
		return &url.Error{Op: "Get", URL: "https://music.example/System/Info/Public", Err: &net.OpError{Op: op, Net: "tcp", Err: inner}}
	}
	noAnswer := &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 443}, Err: os.ErrDeadlineExceeded}
	check("a server nothing answers at", &url.Error{Op: "Get", URL: "https://music.example/System/Info/Public", Err: noAnswer},
		"Nothing answered at music.example:443 within 8 seconds", "blocks it looks the same")

	// Through a proxy.
	if err := a.setProxy("https://aurelia:hunter2@203.0.113.7:8443"); err != nil {
		t.Fatal(err)
	}
	check("a proxy nothing answers at", silent("proxyconnect", noAnswer),
		"The proxy at 203.0.113.7:8443 did not answer within 8 seconds", "block that port", "443 and 80")
	if err := a.setProxy("https://aurelia:hunter2@203.0.113.7:443"); err != nil {
		t.Fatal(err)
	}
	check("a proxy at 443 nothing answers at", silent("proxyconnect", noAnswer), "The proxy at 203.0.113.7:443 did not answer")
	if got := friendly(silent("proxyconnect", noAnswer)); strings.Contains(got, "443 and 80") {
		t.Errorf("a proxy at 443 is told of the ports that pass: %q", got)
	}

	if err := a.setProxy("http://" + closed); err != nil {
		t.Fatal(err)
	}
	check("a proxy that is not there", probe("https://"+blocked), "The proxy's address, "+closed+", refused the connection")

	// A proxy that answers the request for a tunnel with a refusal.
	for code, want := range map[int]string{
		407: "refused its user name or password",
		403: "it does not connect to " + blocked + ":443",
		429: "after too many wrong passwords",
		502: "it could not reach " + blocked + ":443",
	} {
		refusing := answering(code, nil, "")
		if err := a.setProxy(refusing.URL); err != nil {
			t.Fatal(err)
		}
		check("a proxy answering "+http.StatusText(code), probe("https://"+blocked), "The proxy at "+strings.TrimPrefix(refusing.URL, "http://"), want)
	}
	// The same proxy asked for a server at http:// answers in its place.
	refusing := answering(407, nil, "")
	if err := a.setProxy(refusing.URL); err != nil {
		t.Fatal(err)
	}
	check("a proxy refusing, for a server at http", probe("http://"+blocked), "The proxy refused its user name or password")

	// A proxy over TLS: one that vouches for itself, without and with
	// another's fingerprint.
	addr, _ := anyProxy(t, strings.TrimPrefix(plainServer.URL, "http://"), true)
	if err := a.setProxy("https://aurelia:hunter2@" + addr); err != nil {
		t.Fatal(err)
	}
	check("a proxy nobody vouches for", probe("https://"+blocked), "The proxy at "+addr, "needs its fingerprint")
	other := sha256.Sum256([]byte("another key"))
	if err := a.setProxy("https://aurelia:hunter2@" + addr + "#pin-sha256=" + base64.RawURLEncoding.EncodeToString(other[:])); err != nil {
		t.Fatal(err)
	}
	check("a proxy with another certificate", probe("https://"+blocked), "another certificate than the one its address names", "reading connections")

	// What is not of the network is told as it is.
	if got := friendly(os.ErrNotExist); got != os.ErrNotExist.Error() {
		t.Errorf("an error of a file: %q", got)
	}
}

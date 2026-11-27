package tailnet

import (
	"context"
	"net"
	"net/http"
	"strconv"

	"tailscale.com/safesocket"

	tslocal "tailscale.com/client/local"
)

// newClient finds Tailscale on macOS, which comes three ways. The App
// Store's and the standalone app's daemons listen on a port of this
// computer, with a token, which the library finds (the App Store's by
// asking lsof): found once here, they are not looked for at every
// request. The open-source tailscaled is at its socket.
func newClient() *tslocal.Client {
	port, token, err := safesocket.LocalTCPPortAndToken()
	if err != nil {
		return &tslocal.Client{UseSocketOnly: true, OmitAuth: true}
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
	return &tslocal.Client{OmitAuth: true, Transport: &withToken{token: token, base: &http.Transport{DialContext: dial}}}
}

// withToken signs the requests to the daemon with its token, as the
// library itself does.
type withToken struct {
	token string
	base  http.RoundTripper
}

func (t *withToken) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.SetBasicAuth("", t.token)
	return t.base.RoundTrip(r)
}

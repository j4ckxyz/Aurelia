//go:build !darwin

package tailnet

import tslocal "tailscale.com/client/local"

// newClient finds Tailscale where it always is: on Linux at its socket,
// on Windows at its named pipe.
func newClient() *tslocal.Client { return &tslocal.Client{} }

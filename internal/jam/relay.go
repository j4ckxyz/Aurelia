package jam

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/yamux"
)

// A relay, of the user's own (relay/ in Aurelia's source), lets a Jam be
// joined from any network: the host connects out to it, and guests reach
// the host through it at https://relay.example/r/<room>. The host serves
// them there as it serves those of its network; nothing of the Jam's own
// changes. Only the host needs the relay's key: guests get in with the
// Jam's code, or the host's yes.

const relayProto = "aurelia-relay"

// relayClient connects to relays: never through a proxy, and over
// HTTP/1.1, whose connection the relay takes over.
var relayClient = &http.Client{Transport: &http.Transport{
	Proxy:                 nil,
	TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
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

// relayRoom is the host's room at relays, and its secret: the same across
// reconnections, so its invites stay good.
func (h *Host) relayRoom() (room, secret string) {
	h.relayOnce.Do(func() { h.room, h.roomSecret = token(16), token(16) })
	return h.room, h.roomSecret
}

// ServeRelay keeps the Jam reachable through the relay at base, as
// https://relay.example, with its key, until ctx ends or the host closes.
// It connects again as the connection drops: soon, then less often, and
// rarely after a refusal. report is told the Jam's address through the
// relay as it is reached, and why not as it is lost ("" and the error).
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
	room, secret := h.relayRoom()
	u := base + "/v1/host?" + url.Values{"room": {room}, "code": {h.code}}.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Room-Secret", secret)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", relayProto)
	resp, err := relayClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		resp.Body.Close()
		return &RelayError{Status: resp.StatusCode, Text: strings.TrimSpace(string(b))}
	}
	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		resp.Body.Close()
		return errors.New("the relay's connection cannot be written to")
	}
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.ConnectionWriteTimeout = 30 * time.Second
	cfg.LogOutput = io.Discard
	sess, err := yamux.Server(rwc, cfg)
	if err != nil {
		rwc.Close()
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
	report(base+"/r/"+room, nil)
	err = srv.Serve(sess)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, yamux.ErrSessionShutdown) {
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

// LookupRelay finds the Jam of a code at the relay at base: its address
// there.
func LookupRelay(ctx context.Context, base, code string) (string, error) {
	base = strings.TrimSuffix(strings.TrimSpace(base), "/")
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/v1/lookup?code="+url.QueryEscape(NormalizeCode(code)), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
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
	var got struct{ Room string }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&got); err != nil || got.Room == "" {
		return "", errors.New("the relay's answer is not one")
	}
	return base + "/r/" + got.Room, nil
}

// IsRelayAddr reports whether a host's address is a relay's: a URL, not
// an address of a network.
func IsRelayAddr(addr string) bool {
	return strings.HasPrefix(addr, "https://") || strings.HasPrefix(addr, "http://")
}

// httpBase is the URL a host's address is reached at.
func httpBase(addr string) string {
	if IsRelayAddr(addr) {
		return strings.TrimSuffix(addr, "/")
	}
	return "http://" + addr
}

// wsBase is httpBase for a WebSocket.
func wsBase(addr string) string {
	b := httpBase(addr)
	if s, ok := strings.CutPrefix(b, "https://"); ok {
		return "wss://" + s
	}
	return "ws://" + strings.TrimPrefix(b, "http://")
}

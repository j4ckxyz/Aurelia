package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
)

// A proxy, when the settings name one, carries everything Aurelia asks of
// the network: signing in, the library, pictures, songs and updates. It
// is for networks that block the server: the proxy is asked for the
// server by its name, so the name need not resolve here, and the
// network sees a connection to the proxy only.
//
// What a proxy can see depends on the server's address. With https, the
// proxy passes on a connection it cannot read: it learns which server is
// used, and how much. With http, it reads everything, the password
// included.
var proxy atomic.Pointer[url.URL]

// network is the transport of everything the app asks of the network:
// its own clients, and the updater's, which use the default one. It
// stands in front of the transport that holds the connections, so that
// a new proxy can take a new one: connections made the old way would
// otherwise go on being used, whatever the settings say.
var network = &switchTransport{}

type switchTransport struct {
	base *http.Transport // the default transport, as Go makes it
	cur  atomic.Pointer[http.Transport]
}

func (t *switchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.cur.Load().RoundTrip(req)
}

func (t *switchTransport) CloseIdleConnections() { t.cur.Load().CloseIdleConnections() }

// renew makes the requests to come use connections of their own, made
// the way the settings now say. Those under way end the way they began.
func (t *switchTransport) renew() {
	fresh := t.base.Clone()
	fresh.Proxy = proxyFor
	// The loaders of pictures, a song or two, and the library, each with
	// a connection that stays open on servers without HTTP/2.
	fresh.MaxIdleConnsPerHost = 8
	if old := t.cur.Swap(fresh); old != nil {
		old.CloseIdleConnections()
	}
}

func init() {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{ForceAttemptHTTP2: true}
	}
	network.base = base
	network.renew()
	http.DefaultTransport = network
}

// proxyFor is the Proxy of the app's transports: the proxy of the
// settings, else the one the environment names, as HTTPS_PROXY does.
func proxyFor(req *http.Request) (*url.URL, error) {
	u := proxy.Load()
	if u == nil {
		return http.ProxyFromEnvironment(req)
	}
	// A server on this computer or on the network it is on is not what a
	// proxy elsewhere could reach.
	if local(req.URL.Hostname()) {
		return nil, nil
	}
	return u, nil
}

// local reports whether a host is this computer, or an address of a
// private network.
func local(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// parseProxy reads a proxy's address as a user types it:
//
//	socks5://host:1080
//	http://user:password@host:8080
//	host:8080                        an HTTP proxy
//
// "" is no proxy.
func parseProxy(s string) (*url.URL, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, errors.New("this is not a proxy's address")
	}
	switch u.Scheme = strings.ToLower(u.Scheme); u.Scheme {
	case "http", "https", "socks5", "socks5h":
	case "socks", "socks4", "socks4a":
		return nil, errors.New("SOCKS4 proxies are not supported: use a SOCKS5 or an HTTP proxy")
	default:
		return nil, fmt.Errorf("a proxy is http://, https:// or socks5://, not %s://", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, errors.New("the proxy's address has no host")
	}
	if u.Port() == "" {
		return nil, errors.New("the proxy's address needs a port, as host:8080")
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	return u, nil
}

// setProxy makes the app use a proxy, or none for "". What is asked of
// the network from then on goes the new way.
func (a *App) setProxy(s string) error {
	u, err := parseProxy(s)
	if err != nil {
		return err
	}
	old := proxy.Swap(u)
	if (old == nil) != (u == nil) || old != nil && old.String() != u.String() {
		network.renew()
	}
	return nil
}

// proxyRisk says what the proxy could read of a server's traffic, "" when
// nothing: for a server without https.
func proxyRisk(server string) string {
	s := strings.ToLower(strings.TrimSpace(server))
	if strings.HasPrefix(s, "http://") {
		return "This server's address is http://, not https://: the proxy can read everything sent through it, your password included."
	}
	return ""
}

// connection is the part of Settings about reaching the server.
type connection struct {
	proxy   string // the field
	err     string // why the field's proxy is none
	testing bool
	status  string // what the last test found
	reached bool
}

// applyProxy makes the proxy of the field the app's, and tries it.
func (a *App) applyProxy() {
	cn := &a.connection
	cn.err, cn.status = "", ""
	if err := a.setProxy(cn.proxy); err != nil {
		cn.err = err.Error()
		return
	}
	cn.proxy = strings.TrimSpace(cn.proxy)
	a.settings.Proxy, a.login.proxy = cn.proxy, cn.proxy
	a.login.proxyShown = cn.proxy != ""
	a.saveSettings()
	a.testConnection()
}

// testConnection asks the server who it is, the way the app now reaches
// it, and tells how that went.
func (a *App) testConnection() {
	cn := &a.connection
	sess := a.settings.Session
	if cn.testing || sess == nil {
		return
	}
	cn.testing, cn.status = true, ""
	server := sess.Server
	how := "directly"
	if proxy.Load() != nil && !local(hostOf(server)) {
		how = "through the proxy"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		start := time.Now()
		info, err := jellyfin.Probe(ctx, server)
		took := time.Since(start)
		a.update(func() {
			cn.testing = false
			if err != nil {
				cn.reached, cn.status = false, "Could not reach the server "+how+": "+friendly(err)
				return
			}
			cn.reached = true
			cn.status = fmt.Sprintf("Reached %s %s in %d ms.", info.ServerName, how, took.Milliseconds())
			if a.offline || a.syncErr != "" {
				a.sync() // it answers again
			}
		})
	}()
}

func hostOf(server string) string {
	if u, err := url.Parse(server); err == nil {
		return u.Hostname()
	}
	return ""
}

// connectionCard is the part of Settings about reaching the server.
func (a *App) connectionCard(c *ui.Context) {
	p := a.pal
	cn := &a.connection
	if cn.testing {
		c.After(200 * time.Millisecond)
	}
	a.card(c, func() {
		a.setting(c, "Proxy", "Reach your server through an HTTP or SOCKS5 proxy, for networks that block it. Empty is none.", func() {
			in := ui.TextInput(c.Key("proxy"), &cn.proxy).Width(250).Placeholder("socks5://host:1080").Label("Proxy")
			changed := strings.TrimSpace(cn.proxy) != a.settings.Proxy
			label := "Test"
			if changed {
				label = "Apply"
			}
			if (a.pillButton(c, "", label, changed).Disabled(cn.testing).Clicked() || in.Submitted()) && !cn.testing {
				a.applyProxy()
			}
		})
		note := func(glyph string, color ui.Color, text string) {
			ui.Row(c).Gap(10).AlignItems(ui.Start).Padding(10, 16).Children(func() {
				ui.Icon(c, icon(glyph)).Size(15, 15).TextColor(color).Margin(1, 0, 0)
				ui.Text(c, text).FontSize(12).TextColor(p.muted).Grow(1)
			})
		}
		switch {
		case cn.err != "":
			note("circle-alert", p.danger, cn.err)
		case cn.testing:
			note("loader-circle", p.muted, "Asking the server…")
		case cn.status != "" && cn.reached:
			note("circle-check", p.success, cn.status)
		case cn.status != "":
			note("circle-alert", p.warning, cn.status)
		}
		if s := a.settings.Session; s != nil && a.settings.Proxy != "" {
			if risk := proxyRisk(s.Server); risk != "" {
				note("circle-alert", p.warning, risk)
			} else {
				note("shield-check", p.muted, "Your server's address is https://, so the proxy cannot read what passes: it learns only which server you use.")
			}
		}
	})
}

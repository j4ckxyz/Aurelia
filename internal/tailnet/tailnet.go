// Package tailnet reads Tailscale, as its own app on this computer runs
// it: which devices are on the tailnet, whose each is, and who a
// connection comes from. It asks Tailscale's daemon, through the client
// library Tailscale's own command line uses, so that nothing need be on
// the PATH; the daemon does the rest. A computer without Tailscale, or
// with it stopped or signed out, simply has no tailnet.
package tailnet

import (
	"cmp"
	"context"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"

	tslocal "tailscale.com/client/local"
)

// API is what is asked of Tailscale. Local is Tailscale's daemon; tests
// put a fake in its place.
type API interface {
	// Status is the tailnet as this computer sees it: with every device
	// on it, or only this one.
	Status(ctx context.Context, peers bool) (*ipnstate.Status, error)
	// WhoIs is the device and the user an address of the tailnet is of:
	// a connection's remote address, as "100.101.102.103:41234".
	WhoIs(ctx context.Context, addr string) (*apitype.WhoIsResponse, error)
	// Watch calls changed whenever Tailscale's state may have changed: it
	// connects or disconnects, signs in or out, or this computer's
	// addresses change. It returns as ctx ends or Tailscale goes.
	Watch(ctx context.Context, changed func()) error
}

// Local is the Tailscale of this computer.
func Local() API { return &localAPI{} }

type localAPI struct {
	mu   sync.Mutex
	lc   *tslocal.Client
	made time.Time
}

// client is the client of the daemon, made once it is found: how it is
// reached is looked for once, not for every request, which on macOS
// means running lsof.
func (l *localAPI) client() *tslocal.Client {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lc == nil {
		l.lc, l.made = newClient(), time.Now()
	}
	return l.lc
}

// forget has the daemon looked for anew, as after it restarted: on
// macOS, on another port, with another token. It is looked for once a
// minute at most: on a Mac without Tailscale, looking runs lsof.
func (l *localAPI) forget(lc *tslocal.Client) {
	l.mu.Lock()
	if l.lc == lc && time.Since(l.made) > time.Minute {
		l.lc = nil
	}
	l.mu.Unlock()
}

func (l *localAPI) Status(ctx context.Context, peers bool) (*ipnstate.Status, error) {
	lc := l.client()
	var st *ipnstate.Status
	var err error
	if peers {
		st, err = lc.Status(ctx)
	} else {
		st, err = lc.StatusWithoutPeers(ctx)
	}
	if err != nil {
		l.forget(lc)
	}
	return st, err
}

func (l *localAPI) WhoIs(ctx context.Context, addr string) (*apitype.WhoIsResponse, error) {
	return l.client().WhoIs(ctx, addr)
}

func (l *localAPI) Watch(ctx context.Context, changed func()) error {
	lc := l.client()
	// Without the network map, which a big tailnet makes big: what is
	// told is enough to know to ask for the status again. A daemon that
	// does not know that option turns the watch away, as one of 1.104
	// does NotifyRateLimit: it is then watched as it can be.
	w, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState|ipn.NotifyNoNetMap)
	if err != nil && ctx.Err() == nil {
		w, err = lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
	}
	if err != nil {
		l.forget(lc)
		return err
	}
	defer w.Close()
	for {
		n, err := w.Next()
		if err != nil {
			l.forget(lc)
			return err
		}
		if n.State != nil || n.SelfChange != nil || n.LoginFinished != nil || n.Prefs != nil || n.NetMap != nil {
			changed()
		}
	}
}

// IsTailscaleIP reports whether an address is of a tailnet: in
// 100.64.0.0/10, the shared address space Tailscale gives its devices
// addresses of, or in fd7a:115c:a1e0::/48, its IPv6 range.
func IsTailscaleIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return cgnat.Contains(ip) || ula.Contains(ip)
}

var (
	cgnat = netip.MustParsePrefix("100.64.0.0/10")
	ula   = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// Owner is whose a device is.
type Owner struct {
	// Mine is set for a device of the user signed in to Tailscale here.
	Mine bool
	// Name and Login are the owner's: "Sam", "sam@github". A tagged
	// device has no owner, but Tags.
	Name, Login string
	Tags        []string
}

// Label says whose a device is: "Yours", "Sam (sam@github)", or for a
// device without an owner, its tags.
func (o Owner) Label() string {
	switch {
	case len(o.Tags) > 0:
		return strings.Join(o.Tags, ", ")
	case o.Mine:
		return "Yours"
	case o.Name != "" && o.Login != "" && o.Name != o.Login:
		return o.Name + " (" + o.Login + ")"
	}
	return cmp.Or(o.Name, o.Login, "Someone else")
}

// Device is a device of the tailnet.
type Device struct {
	ID      string // Tailscale's stable ID of it
	Name    string // as Tailscale shows it: "Sam's ThinkPad"
	DNSName string // its MagicDNS name, without the final dot
	OS      string
	Online  bool
	IPs     []netip.Addr
	Owner   Owner
	// Sharee is set for a device of someone this computer is shared
	// with: it may reach this one, but not this one it.
	Sharee bool
}

// Addr is the address to reach a device at: its IPv4 one, which every
// network takes, else its first.
func (d Device) Addr() netip.Addr {
	for _, ip := range d.IPs {
		if ip.Is4() {
			return ip
		}
	}
	if len(d.IPs) > 0 {
		return d.IPs[0]
	}
	return netip.Addr{}
}

// Net is the tailnet, as this computer sees it.
type Net struct {
	// Running is set while Tailscale is connected and signed in: the
	// tailnet is there.
	Running bool
	// State is Tailscale's: "Running", "Stopped", "NeedsLogin"…
	State string
	Self  Device
	// UserID is the user signed in here, which owns the devices that
	// are this user's.
	UserID int64
	// Devices are the others: this user's first, then by name.
	Devices []Device
}

// Read reads a status.
func Read(st *ipnstate.Status) Net {
	var n Net
	if st == nil {
		return n
	}
	n.State = st.BackendState
	if st.Self == nil {
		return n
	}
	me := st.Self.UserID
	n.UserID = int64(me)
	n.Self = device(st, st.Self, me)
	n.Self.Owner.Mine = true
	n.Running = st.BackendState == ipn.Running.String() && len(n.Self.IPs) > 0
	for _, p := range st.Peer {
		if p == nil {
			continue
		}
		n.Devices = append(n.Devices, device(st, p, me))
	}
	slices.SortFunc(n.Devices, func(a, b Device) int {
		if a.Owner.Mine != b.Owner.Mine {
			if a.Owner.Mine {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})
	return n
}

func device(st *ipnstate.Status, p *ipnstate.PeerStatus, me tailcfg.UserID) Device {
	d := Device{
		ID: string(p.ID), Name: name(p.HostName, p.DNSName), DNSName: strings.TrimSuffix(p.DNSName, "."),
		OS: p.OS, Online: p.Online, IPs: slices.Clone(p.TailscaleIPs), Sharee: p.ShareeNode,
	}
	if p.Tags != nil && p.Tags.Len() > 0 {
		d.Owner.Tags = p.Tags.AsSlice()
	} else {
		d.Owner.Mine = p.UserID == me
		if u, ok := st.User[p.UserID]; ok {
			d.Owner.Name, d.Owner.Login = u.DisplayName, u.LoginName
		}
	}
	return d
}

// name is what a device is called: the name its computer gives itself,
// but for the "localhost" of phones, whose MagicDNS name says more.
func name(host, dns string) string {
	if host != "" && host != "localhost" {
		return host
	}
	if label, _, _ := strings.Cut(dns, "."); label != "" {
		return label
	}
	return cmp.Or(host, "A device")
}

// Who is whom a WhoIs found: the device and its owner. self is the user
// signed in here, whose devices are Mine.
func Who(w *apitype.WhoIsResponse, self int64) (Device, bool) {
	if w == nil || w.Node == nil {
		return Device{}, false
	}
	n := w.Node
	host := ""
	if n.Hostinfo.Valid() {
		host = n.Hostinfo.Hostname()
	}
	d := Device{
		ID: string(n.StableID), Name: name(host, cmp.Or(n.ComputedName, n.Name)), DNSName: strings.TrimSuffix(n.Name, "."),
		Online: true,
	}
	for _, p := range n.Addresses {
		d.IPs = append(d.IPs, p.Addr())
	}
	if len(n.Tags) > 0 {
		d.Owner.Tags = slices.Clone(n.Tags)
		return d, true
	}
	d.Owner.Mine = int64(n.User) == self
	if u := w.UserProfile; u != nil {
		d.Owner.Name, d.Owner.Login = u.DisplayName, u.LoginName
	}
	return d, true
}

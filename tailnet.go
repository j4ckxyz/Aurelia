package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"aurelia/internal/jam"
	"aurelia/internal/tailnet"
)

// Tailscale, for Jams. A computer on a tailnet reaches the others on it
// wherever they are, as if on one network: a Jam's guests may be across
// town. The app reads Tailscale's daemon (internal/tailnet) for the
// devices of the tailnet and whose each is, and keeps a door open on
// this computer's Tailscale addresses (jam.Door), which the Aurelias of
// the tailnet ask whether Aurelia is here and send invites to. The door
// is open while Aurelia is, the Jams' beta on or not, unless the
// settings close it; the rest of what Tailscale gives is the beta's.
//
// Who a connection is from is Tailscale's word, by WhoIs, not the
// connection's own: a device of the same user joins the user's Jam
// without the code, anyone else's asks, as who they are, and an invite
// shows who really sent it.

// tailState is the app's part of Tailscale.
type tailState struct {
	// api is Tailscale, nil when it is not asked, as in tests that do not
	// put a fake of it there.
	api tailnet.API
	// port is where doors are, reach the address of a device's, and allow
	// which of this computer's addresses its own may open on: those of
	// Tailscale, but in tests.
	port  int
	reach func(ip netip.Addr) string
	allow func(ip netip.Addr) bool

	// net is the tailnet as last read; known is set once Tailscale
	// answered, as it does once it is installed.
	net   tailnet.Net
	known bool
	reads int // how many times it was read

	door    *jam.Door
	doorOn  []netip.Addr
	doorErr string
	// What the door's goroutines read: the user signed in to Tailscale,
	// and what the door says of this device.
	userID atomic.Int64
	about  atomic.Pointer[jam.About]

	// found is what the doors of the devices said, by device ID; probing
	// is set while they are asked.
	found    map[string]tailFound
	probing  bool
	probedAt time.Time
	// sent is what became of the invites sent, by device ID.
	sent map[string]string
	// invites are those come here, waiting for an answer.
	invites []tailInvite

	kick  chan struct{}
	panel atomic.Bool // the Jam's dialog shows: the tailnet is read often
}

// tailFound is what a device's door said, when it answered.
type tailFound struct {
	about *jam.About
	at    time.Time
}

// tailInvite is an invite come to this computer's door.
type tailInvite struct {
	id   string
	from jam.Peer
	addr string // where the Jam is
	code string
	at   time.Time
	// oneWay is set for an invite from a device of someone this computer
	// is shared with: Tailscale lets it reach this one, not this one it,
	// so its Jam cannot be joined. It may join one held here.
	oneWay bool
}

// What became of an invite sent.
const (
	inviteSending = "sending"
	inviteSent    = "sent"
	inviteFailed  = "failed"
)

// tailProbeWait is how long a device has to answer whether it runs
// Aurelia: Tailscale's connections are quick to make, and a device
// that runs it answers at once.
var tailProbeWait = 1500 * time.Millisecond

func (a *App) initTail() {
	t := &a.tail
	t.port = jam.TCPPort
	t.reach = func(ip netip.Addr) string { return net.JoinHostPort(ip.String(), strconv.Itoa(jam.TCPPort)) }
	t.allow = tailnet.IsTailscaleIP
	t.found, t.sent = map[string]tailFound{}, map[string]string{}
	t.kick = make(chan struct{}, 1)
}

// startTail has Tailscale read, now and as it changes.
func (a *App) startTail() {
	t := &a.tail
	t.api = tailnet.Local()
	a.tailAbout()
	go a.tailWatch(t.api)
	go a.tailLoop(t.api)
}

// tailKick has the tailnet read again, soon.
func (a *App) tailKick() {
	select {
	case a.tail.kick <- struct{}{}:
	default:
	}
}

// tailWatch has the tailnet read whenever Tailscale tells of a change.
// Without Tailscale, it asks now and then whether it came.
func (a *App) tailWatch(api tailnet.API) {
	wait := time.Second
	for {
		began := time.Now()
		api.Watch(context.Background(), a.tailKick)
		if time.Since(began) > time.Minute {
			wait = time.Second
		}
		a.tailKick()
		time.Sleep(wait)
		wait = min(2*wait, time.Minute)
	}
}

// tailLoop reads the tailnet when kicked, and every so often: often
// while the Jam's dialog shows its devices.
func (a *App) tailLoop(api tailnet.API) {
	var last time.Time
	for {
		every := time.Minute
		if a.tail.panel.Load() {
			every = 8 * time.Second
		}
		select {
		case <-a.tail.kick:
		case <-time.After(every):
		}
		if d := 1500*time.Millisecond - time.Since(last); d > 0 {
			time.Sleep(d)
		}
		last = time.Now()
		a.tailRead(api, a.tail.panel.Load())
	}
}

// tailRead reads the tailnet, off the main goroutine: with every device
// on it when peers is set. What it read is applied on the main one.
func (a *App) tailRead(api tailnet.API, peers bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := api.Status(ctx, peers)
	var n tailnet.Net
	if err == nil {
		n = tailnet.Read(st)
	}
	a.update(func() { a.tailApply(n, err == nil, peers) })
}

// tailApply takes the tailnet as read.
func (a *App) tailApply(n tailnet.Net, answered, peers bool) {
	t := &a.tail
	t.reads++
	t.known = answered && n.State != ""
	if !peers && n.Running {
		// Read without the devices, which were not asked of it.
		n.Devices = t.net.Devices
	}
	t.net = n
	t.userID.Store(n.UserID)
	a.tailDoor()
	a.tailAbout()
	if peers && n.Running && t.panel.Load() {
		a.tailProbe()
	}
	if a.win != nil {
		a.win.Invalidate()
	}
}

// tailUp reports whether the tailnet is there.
func (a *App) tailUp() bool { return a.tail.api != nil && a.tail.net.Running }

// tailDoor opens the door on this computer's Tailscale addresses, or
// closes it, as the tailnet and the settings now say.
func (a *App) tailDoor() {
	t := &a.tail
	addrs := a.tailDoorAddrs()
	if t.door != nil && slices.Equal(addrs, t.doorOn) {
		return
	}
	if old := t.door; old != nil {
		t.door, t.doorOn = nil, nil
		go old.Close()
	}
	if len(addrs) == 0 {
		return
	}
	d := &jam.Door{
		Identify: a.tailIdentify,
		About: func() jam.About {
			if p := t.about.Load(); p != nil {
				return *p
			}
			return jam.About{}
		},
		OnInvite: func(from jam.Peer, addr string, inv jam.Invitation) {
			oneWay := a.tailOneWay(from.Node)
			a.update(func() { a.tailInvited(from, addr, inv, oneWay) })
		},
	}
	if err := jam.OpenDoor(d, addrs, t.port); err != nil {
		// As where the Jam held here took the port on every address
		// first: tried again as the tailnet is read again.
		t.doorErr = err.Error()
		return
	}
	t.door, t.doorOn, t.doorErr = d, addrs, ""
	if h := a.jam.host; h != nil {
		d.SetJam(h.Handler())
	}
}

// tailDoorAddrs are where the door is to be open: this computer's
// Tailscale addresses, and never another, not every address nor the
// network's. There are none while the tailnet is not there, or the
// settings keep the door closed.
func (a *App) tailDoorAddrs() []netip.Addr {
	t := &a.tail
	if !a.tailUp() || a.settings.TailnetInvitesOff {
		return nil
	}
	var addrs []netip.Addr
	for _, ip := range t.net.Self.IPs {
		if ip.IsValid() && !ip.IsUnspecified() && t.allow(ip) {
			addrs = append(addrs, ip)
		}
	}
	return addrs
}

// closeTail closes the door, as the app quits.
func (a *App) closeTail() {
	if d := a.tail.door; d != nil {
		a.tail.door, a.tail.doorOn = nil, nil
		d.Close()
	}
}

// tailIdentify says who a connection is from, by Tailscale: nil for an
// address not of the tailnet, or a device it does not know.
func (a *App) tailIdentify(ctx context.Context, remote string) *jam.Peer {
	t := &a.tail
	if t.api == nil {
		return nil
	}
	ap, err := netip.ParseAddrPort(remote)
	if err != nil || !t.allow(ap.Addr()) {
		return nil
	}
	w, err := t.api.WhoIs(ctx, remote)
	if err != nil {
		return nil
	}
	d, ok := tailnet.Who(w, t.userID.Load())
	if !ok {
		return nil
	}
	return &jam.Peer{User: d.Owner.Name, Login: d.Owner.Login, Device: d.Name, Node: d.ID, Mine: d.Owner.Mine, Tags: d.Owner.Tags}
}

// tailAbout is what the door says of this device, as it changes.
func (a *App) tailAbout() {
	t := &a.tail
	name := t.net.Self.Name
	if name == "" {
		name = deviceName()
	}
	about := &jam.About{App: jam.AppName, Proto: jam.Proto, Version: appVersion(), Name: name, Jam: a.settings.Jam}
	if h := a.jam.host; h != nil {
		about.Live = &jam.Live{Host: a.jamName(), People: 1 + len(a.jam.members), Port: a.tailJamPort()}
	}
	t.about.Store(about)
}

// tailJamPort is where the Jam held here is on the tailnet: at the
// door, which hands it on, else at the Jam's own port, of every address.
func (a *App) tailJamPort() int {
	if d := a.tail.door; d != nil {
		return d.Port()
	}
	if h := a.jam.host; h != nil {
		return h.Port()
	}
	return a.tail.port
}

// tailInviteLink is the invite to the Jam held here, at this computer's
// address on the tailnet, "" without one.
func (a *App) tailInviteLink() string {
	ip := a.tail.net.Self.Addr()
	if a.jam.host == nil || !ip.IsValid() {
		return ""
	}
	return jam.Invite(net.JoinHostPort(ip.String(), strconv.Itoa(a.tailJamPort())), a.jam.host.Code())
}

// tailHosted has the door hand the Jam this computer now holds, or no
// longer does, on.
func (a *App) tailHosted() {
	if d := a.tail.door; d != nil {
		if h := a.jam.host; h != nil {
			d.SetJam(h.Handler())
		} else {
			d.SetJam(nil)
		}
	}
	a.tailAbout()
}

// tailProbe asks the devices of the tailnet that may be reached whether
// they run Aurelia, all at once.
func (a *App) tailProbe() {
	t := &a.tail
	if t.probing {
		return
	}
	var ask []tailnet.Device
	for _, d := range t.net.Devices {
		if d.Online && !d.Sharee && d.Addr().IsValid() {
			ask = append(ask, d)
		}
	}
	// The user's own come first: a big tailnet is asked of in part.
	ask = ask[:min(len(ask), 96)]
	if len(ask) == 0 {
		t.found = map[string]tailFound{}
		return
	}
	t.probing = true
	reach := t.reach
	go func() {
		var mu sync.Mutex
		found := map[string]tailFound{}
		var wg sync.WaitGroup
		limit := make(chan struct{}, 16)
		for _, d := range ask {
			wg.Go(func() {
				limit <- struct{}{}
				defer func() { <-limit }()
				ctx, cancel := context.WithTimeout(context.Background(), tailProbeWait)
				defer cancel()
				about, err := jam.Probe(ctx, reach(d.Addr()))
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					found[d.ID] = tailFound{about: about, at: time.Now()}
				}
			})
		}
		wg.Wait()
		a.update(func() {
			t.probing, t.probedAt = false, time.Now()
			t.found = found
			// Those found are listed when Aurelia is closed there, as the
			// ones to send an invite link.
			seen := false
			for id := range found {
				if !slices.Contains(a.settings.TailnetSeen, id) {
					a.settings.TailnetSeen = append(a.settings.TailnetSeen, id)
					seen = true
				}
			}
			if seen {
				if n := len(a.settings.TailnetSeen); n > 64 {
					a.settings.TailnetSeen = a.settings.TailnetSeen[n-64:]
				}
				a.saveSettings()
			}
			if a.win != nil {
				a.win.Invalidate()
			}
		})
	}()
}

// tailDevice is a device as the Jam's dialog lists it.
type tailDevice struct {
	tailnet.Device
	about  *jam.About // what it said, nil when it did not answer
	seen   bool       // it ran Aurelia before
	member bool       // it is in the Jam held here
}

// tailDevices are the devices the Jam's dialog lists: those that run
// Aurelia, those that did and may be sent an invite link, and one of
// each person who may join this computer though it may not reach them.
func (a *App) tailDevices() []tailDevice {
	t := &a.tail
	in := map[string]bool{}
	for _, m := range a.jam.members {
		if m.Peer != nil && m.Peer.Node != "" {
			in[m.Peer.Node] = true
		}
	}
	sharees := map[string]bool{}
	var out, shared []tailDevice
	for _, d := range t.net.Devices {
		td := tailDevice{Device: d, about: t.found[d.ID].about, seen: slices.Contains(a.settings.TailnetSeen, d.ID), member: in[d.ID]}
		switch {
		case d.Sharee:
			// Shared with: one row a person, while a device of theirs is on.
			who := d.Owner.Login + "|" + d.Owner.Name
			if !d.Online || sharees[who] {
				continue
			}
			sharees[who] = true
			shared = append(shared, td)
			continue
		case td.about == nil && !td.seen:
			continue // no Aurelia there: a phone, a server
		}
		out = append(out, td)
	}
	return append(out, shared...)
}

// tailInvite invites a device to the Jam held here, which it starts if
// there is none.
func (a *App) tailInvite(d tailnet.Device) {
	t := &a.tail
	if a.jam.host == nil {
		a.startJam()
		if a.jam.host == nil {
			return
		}
	}
	if t.sent[d.ID] == inviteSending {
		return
	}
	t.sent[d.ID] = inviteSending
	addr, code, port := t.reach(d.Addr()), a.jam.host.Code(), a.tailJamPort()
	name := d.Name
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := jam.SendInvite(ctx, addr, code, port)
		a.update(func() {
			if err == nil {
				t.sent[d.ID] = inviteSent
				a.toast("Invited " + name + " to your Jam")
				return
			}
			t.sent[d.ID] = inviteFailed
			var refused *jam.InviteError
			if errors.As(err, &refused) && refused.Text != "" {
				a.toast(name + ": " + refused.Text)
			} else {
				a.toast(name + " did not answer. Copy the invite link and send it instead.")
			}
		})
	}()
}

// tailCopyInvite copies the invite link of the Jam held here, at this
// computer's address on the tailnet; it starts the Jam if there is none.
func (a *App) tailCopyInvite() {
	if a.jam.host == nil {
		a.startJam()
	}
	link := a.tailInviteLink()
	if link == "" {
		return
	}
	if a.native {
		mygo.Clipboard.WriteText(link)
	}
	a.toast("Invite link copied: it works on your tailnet")
}

// tailInvited is an invite come to the door.
func (a *App) tailInvited(from jam.Peer, addr string, inv jam.Invitation, oneWay bool) {
	t := &a.tail
	t.invites = slices.DeleteFunc(t.invites, func(i tailInvite) bool {
		return i.from.Node == from.Node || time.Since(i.at) > 10*time.Minute
	})
	if len(t.invites) >= 3 {
		return
	}
	t.invites = append(t.invites, tailInvite{id: strconv.FormatInt(time.Now().UnixNano(), 36), from: from, addr: addr, code: jam.NormalizeCode(inv.Code), at: time.Now(), oneWay: oneWay})
	if a.win != nil {
		a.win.Invalidate()
	}
	// Told of by the system too, when the window is not in front.
	if a.native && (a.win == nil || !a.win.IsFocused()) {
		n := mygo.NewNotification(mygo.NotificationOptions{
			ID: "jam-invite", Title: inviter(from) + " invited you to a Jam", Body: "Open Aurelia to join. Jams are in beta.", Group: "Aurelia",
		})
		n.OnClick(func() { a.update(a.show) })
		if err := n.Show(); err != nil {
			log.Print("the notification: ", err)
		}
	}
}

// tailOneWay reports whether a device of the tailnet is of someone this
// computer is shared with, which Tailscale lets reach this computer but
// not this computer it. It asks Tailscale, off the main goroutine.
func (a *App) tailOneWay(node string) bool {
	api := a.tail.api
	if api == nil || node == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	st, err := api.Status(ctx, true)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(tailnet.Read(st).Devices, func(d tailnet.Device) bool { return d.Sharee && d.ID == node })
}

// inviter is how an invite names who sent it: Tailscale's name of them.
func inviter(p jam.Peer) string {
	if p.Mine || p.User == "" {
		return cmpOr(p.Device, "A device of yours")
	}
	return p.User
}

func cmpOr(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// answerInvite takes an invite, or lets it be. Taking it turns the Jams'
// beta on, leaves the Jam this app is in, and joins.
func (a *App) answerInvite(id string, yes bool) {
	t := &a.tail
	i := slices.IndexFunc(t.invites, func(x tailInvite) bool { return x.id == id })
	if i < 0 {
		return
	}
	inv := t.invites[i]
	t.invites = slices.Delete(t.invites, i, i+1)
	if !yes {
		return
	}
	if !a.settings.Jam {
		a.settings.Jam = true
		a.saveSettings()
		a.tailAbout()
	}
	if inv.oneWay {
		// Their Jam cannot be reached from here: one held here can be
		// from there, which their list of the tailnet shows to join.
		if a.jam.guest != nil {
			a.stopJam()
		}
		if a.jam.host == nil {
			a.startJam()
		}
		a.openJam()
		return
	}
	a.stopJam()
	a.jam.open = true
	a.jam.joinErr = ""
	a.joinJam(inv.addr, inv.code)
}

// tailJoin joins the Jam a device of the tailnet holds: a device of the
// user's own goes in at once, anyone else's host is asked.
func (a *App) tailJoin(d tailDevice) {
	if d.about == nil || d.about.Live == nil || a.jam.in() {
		return
	}
	port := d.about.Live.Port
	addr := a.tail.reach(d.Addr())
	if host, _, err := net.SplitHostPort(addr); err == nil && port > 0 {
		addr = net.JoinHostPort(host, strconv.Itoa(port))
	}
	a.joinJam(addr, "")
}

// tailAddr reports whether an address, as "100.101.102.103:47475", is on
// a tailnet.
func tailAddr(addr string) bool {
	ap, err := netip.ParseAddrPort(addr)
	return err == nil && tailnet.IsTailscaleIP(ap.Addr())
}

package main

import (
	"context"
	"errors"
	"image"
	"image/png"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/views"

	"aurelia/internal/jam"
	"aurelia/internal/tailnet"
)

// fakeTail is Tailscale, as a test has it.
type fakeTail struct {
	mu   sync.Mutex
	st   *ipnstate.Status
	who  func(addr string) *apitype.WhoIsResponse
	asks int // WhoIs asked
}

func (f *fakeTail) Status(ctx context.Context, peers bool) (*ipnstate.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.st == nil {
		return nil, errors.New("Tailscale is not running")
	}
	st := *f.st
	if !peers {
		st.Peer = nil
	}
	return &st, nil
}

func (f *fakeTail) WhoIs(ctx context.Context, addr string) (*apitype.WhoIsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asks++
	if f.who == nil {
		return nil, errors.New("no such peer")
	}
	if w := f.who(addr); w != nil {
		return w, nil
	}
	return nil, errors.New("no such peer")
}

func (f *fakeTail) Watch(ctx context.Context, changed func()) error {
	<-ctx.Done()
	return ctx.Err()
}

// The people of the tailnets of the tests.
var tailUsers = map[tailcfg.UserID]tailcfg.UserProfile{
	10: {ID: 10, LoginName: "ada@github", DisplayName: "Ada"},
	20: {ID: 20, LoginName: "sam@github", DisplayName: "Sam"},
	30: {ID: 30, LoginName: "robin@example.com", DisplayName: "Robin"},
}

func tailPeer(id, host string, user tailcfg.UserID, ip string) *ipnstate.PeerStatus {
	return &ipnstate.PeerStatus{
		ID: tailcfg.StableNodeID(id), HostName: host, DNSName: strings.ToLower(host) + ".tail.ts.net.", OS: "linux",
		UserID: user, Online: true, TailscaleIPs: []netip.Addr{netip.MustParseAddr(ip)},
	}
}

func tailStatus(self *ipnstate.PeerStatus, peers ...*ipnstate.PeerStatus) *ipnstate.Status {
	st := &ipnstate.Status{BackendState: "Running", Self: self, TailscaleIPs: self.TailscaleIPs, User: tailUsers,
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{}}
	for _, p := range peers {
		st.Peer[key.NewNode().Public()] = p
	}
	return st
}

// whoIs is a WhoIs answer of a device.
func whoIs(id, host string, user tailcfg.UserID) *apitype.WhoIsResponse {
	u := tailUsers[user]
	return &apitype.WhoIsResponse{
		Node:        &tailcfg.Node{StableID: tailcfg.StableNodeID(id), Name: strings.ToLower(host) + ".tail.ts.net.", User: user, Hostinfo: (&tailcfg.Hostinfo{Hostname: host}).View()},
		UserProfile: &u,
	}
}

// onTailnet puts app a on a fake tailnet, on which it is self, and has
// its door at 127.0.0.1 as if it were its Tailscale address. reach maps
// the other devices' addresses to where their doors are.
func onTailnet(a *App, f *fakeTail, reach map[netip.Addr]string) {
	t := &a.tail
	t.api = f
	t.port = 0
	t.allow = func(ip netip.Addr) bool { return ip.IsLoopback() || tailnet.IsTailscaleIP(ip) }
	t.reach = func(ip netip.Addr) string {
		if r, ok := reach[ip]; ok {
			return r
		}
		return "127.0.0.1:1" // nobody there
	}
}

// readTail reads the tailnet as the app does, with its devices, and
// waits for their doors to be asked.
func readTail(t *testing.T, apps []*App, a *App) {
	t.Helper()
	n := a.tail.reads
	a.tailRead(a.tail.api, true)
	both(t, apps, "the tailnet read", func() bool { return a.tail.reads > n && !a.tail.probing })
}

// fakeDoor is a device of the tailnet running Aurelia, as its door
// answers.
func fakeDoor(t *testing.T, about jam.About) string {
	t.Helper()
	d := &jam.Door{
		Identify: func(context.Context, string) *jam.Peer { return &jam.Peer{Device: "x"} },
		About:    func() jam.About { return about },
	}
	if err := jam.OpenDoor(d, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d.Addrs()[0].String()
}

// The devices of the tailnet that run Aurelia show, each with whose it
// is: the user's own, someone else's, a tagged one's tags. Those of the
// people the computer is shared with can join it, and have no Invite
// button; a phone without Aurelia does not show.
func TestTailnetDevices(t *testing.T) {
	a := testApp(t)
	a.settings.Jam = true
	tags := views.SliceOf([]string{"tag:media"})
	nas := tailPeer("n4", "Basement", 999, "100.64.0.4")
	nas.Tags = &tags
	sharee := tailPeer("n5", "device-of-shared-to-user", 30, "100.64.0.5")
	sharee.ShareeNode = true
	sharee2 := tailPeer("n6", "device-of-shared-to-user", 30, "100.64.0.6")
	sharee2.ShareeNode = true
	offSharee := tailPeer("n7", "device-of-shared-to-user", 20, "100.64.0.7")
	offSharee.ShareeNode, offSharee.Online = true, false
	f := &fakeTail{st: tailStatus(tailPeer("me", "Desk", 10, "100.64.0.1"),
		tailPeer("n2", "Studio", 10, "100.64.0.2"),
		tailPeer("n3", "ThinkPad", 20, "100.64.0.3"),
		nas, sharee, sharee2, offSharee,
		tailPeer("n8", "Pixel", 20, "100.64.0.8"), // no Aurelia
	)}
	onTailnet(a, f, map[netip.Addr]string{
		netip.MustParseAddr("100.64.0.2"): fakeDoor(t, jam.About{Name: "Studio", Jam: true}),
		netip.MustParseAddr("100.64.0.3"): fakeDoor(t, jam.About{Name: "ThinkPad"}),
		netip.MustParseAddr("100.64.0.4"): fakeDoor(t, jam.About{Name: "Basement", Jam: true, Live: &jam.Live{Host: "Kit", People: 3, Port: 47475}}),
	})
	a.tail.panel.Store(true)
	readTail(t, []*App{a}, a)
	tt := ui.NewTester(a.view, 1240, 900)
	a.openJam()
	tt.Frame()
	wantTexts(t, tt, "TAILSCALE",
		"Studio", "Yours · Aurelia is open",
		"ThinkPad", "Sam (sam@github) · Aurelia is open, Jams off",
		"Basement", "tag:media · Hosting Kit's Jam · 3 people",
		"Robin", "robin@example.com · Can join you")
	if tt.HasText("Pixel") {
		t.Error("a device without Aurelia shows")
	}
	n := 0
	for _, s := range tt.Texts() {
		if s == "robin@example.com · Can join you" {
			n++
		}
		if strings.Contains(s, "sam@github · Can join you") {
			t.Error("a sharee whose devices are off shows")
		}
	}
	if n != 1 {
		t.Errorf("Robin shows %d times", n)
	}
	// Invite for the two that run Aurelia without a Jam, Join for the one
	// with: none for Robin.
	if got := countOf(tt.Texts(), "Invite"); got != 2 {
		t.Errorf("%d Invite buttons; texts: %q", got, tt.Texts())
	}
	if got := countOf(tt.Texts(), "Join"); got < 2 { // the code's, and Basement's
		t.Errorf("no Join for the Jam on the tailnet; texts: %q", tt.Texts())
	}
	// Those found are remembered: closed there, they are sent a link.
	if !slices.Contains(a.settings.TailnetSeen, "n2") || slices.Contains(a.settings.TailnetSeen, "n8") {
		t.Errorf("remembered: %q", a.settings.TailnetSeen)
	}
	a.tail.reach = func(netip.Addr) string { return "127.0.0.1:1" }
	readTail(t, []*App{a}, a)
	tt.Frame()
	wantTexts(t, tt, "Yours · Aurelia isn't open", "Copy invite link")
	// Without Tailscale, nothing of it shows.
	f.mu.Lock()
	f.st = nil
	f.mu.Unlock()
	readTail(t, []*App{a}, a)
	tt.Frame()
	if tt.HasText("TAILSCALE") || a.tail.known {
		t.Error("the tailnet shows without Tailscale")
	}
}

func countOf(texts []string, s string) int {
	n := 0
	for _, x := range texts {
		if x == s {
			n++
		}
	}
	return n
}

// A device that takes the connection but does not answer is not listed,
// and does not hold up the others.
func TestTailnetProbeTimeout(t *testing.T) {
	defer func(w time.Duration) { tailProbeWait = w }(tailProbeWait)
	tailProbeWait = 300 * time.Millisecond
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var held []net.Conn
	var mu sync.Mutex
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	defer func() {
		mu.Lock()
		for _, c := range held {
			c.Close()
		}
		mu.Unlock()
	}()
	a := testApp(t)
	a.settings.Jam = true
	f := &fakeTail{st: tailStatus(tailPeer("me", "Desk", 10, "100.64.0.1"),
		tailPeer("n2", "Studio", 10, "100.64.0.2"), tailPeer("n3", "Silent", 10, "100.64.0.3"))}
	onTailnet(a, f, map[netip.Addr]string{
		netip.MustParseAddr("100.64.0.2"): fakeDoor(t, jam.About{Name: "Studio"}),
		netip.MustParseAddr("100.64.0.3"): ln.Addr().String(),
	})
	a.tail.panel.Store(true)
	began := time.Now()
	readTail(t, []*App{a}, a)
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("asking took %v", took)
	}
	var names []string
	for _, d := range a.tailDevices() {
		names = append(names, d.Name)
	}
	if !slices.Equal(names, []string{"Studio"}) {
		t.Errorf("listed: %q", names)
	}
}

// Two apps on a tailnet: Ada's, and Bo's, whose Jams are off.
func tailPair(t *testing.T) (host, guest *App, hf, gf *fakeTail) {
	host = testAppWith(t, streams)
	host.settings.Jam = true
	host.settings.Session.UserName = "Ada"
	guest = testAppWith(t, streams)
	guest.settings.Session.ServerID, guest.settings.Session.UserName = "srv2", "Bo"
	hostIP, guestIP := netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("100.64.0.2")
	gf = &fakeTail{st: tailStatus(tailPeer("bo", "Laptop", 20, "100.64.0.2"), tailPeer("ada", "Studio", 10, "100.64.0.1"))}
	// Everything that comes to Bo's door is from Ada's Studio.
	gf.who = func(string) *apitype.WhoIsResponse { return whoIs("ada", "Studio", 10) }
	gf.st.Self.TailscaleIPs = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	onTailnet(guest, gf, nil)
	guest.tailApply(tailnet.Read(gf.st), true, false)
	if guest.tail.door == nil {
		t.Fatalf("Bo's door is not open: %s", guest.tail.doorErr)
	}
	t.Cleanup(guest.closeTail)
	hf = &fakeTail{st: tailStatus(tailPeer("ada", "Studio", 10, "100.64.0.1"), tailPeer("bo", "Laptop", 20, "100.64.0.2"))}
	hf.who = func(string) *apitype.WhoIsResponse { return whoIs("bo", "Laptop", 20) }
	hf.st.Self.TailscaleIPs = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	onTailnet(host, hf, map[netip.Addr]string{guestIP: guest.tail.door.Addrs()[0].String()})
	host.tail.panel.Store(true)
	host.tailApply(tailnet.Read(hf.st), true, false)
	if host.tail.door == nil {
		t.Fatalf("Ada's door is not open: %s", host.tail.doorErr)
	}
	t.Cleanup(host.closeTail)
	_ = hostIP
	t.Cleanup(func() { guest.stopJam(); host.stopJam() })
	return host, guest, hf, gf
}

// Ada invites Bo's laptop, whose Jams are off: it asks Bo, as from who
// Tailscale says sent it, and Bo's answer turns them on and joins.
func TestTailnetInviteWithJamsOff(t *testing.T) {
	host, guest, _, _ := tailPair(t)
	apps := []*App{host, guest}
	readTail(t, apps, host)
	ds := host.tailDevices()
	if len(ds) != 1 || ds[0].about == nil || ds[0].about.Jam {
		t.Fatalf("Ada sees %+v", ds)
	}
	tt := ui.NewTester(host.view, 1240, 900)
	host.openJam()
	tt.Frame()
	wantTexts(t, tt, "Laptop", "Sam (sam@github) · Aurelia is open, Jams off")
	click(t, tt, "Invite") // starts the Jam, which there was none of
	if host.jam.host == nil {
		t.Fatal("inviting started no Jam")
	}
	both(t, apps, "the invite", func() bool { return len(guest.tail.invites) == 1 })
	if s := host.tail.sent["bo"]; s != inviteSent {
		t.Errorf("the invite is %q", s)
	}
	inv := guest.tail.invites[0]
	if inv.from.User != "Ada" || inv.from.Device != "Studio" || inv.code != host.jam.host.Code() {
		t.Errorf("the invite: %+v", inv)
	}
	gt := ui.NewTester(guest.view, 1240, 900)
	gt.Frame()
	wantTexts(t, gt, "Ada invited you to a Jam (beta)", "Turn on and join", "Not now")
	click(t, gt, "Turn on and join")
	if !guest.settings.Jam {
		t.Error("the beta is still off")
	}
	both(t, apps, "Bo in Ada's Jam", func() bool { return guest.jam.guest != nil && len(host.jam.members) == 1 })
	// Who Tailscale says Bo is, not what Bo says.
	if m := host.jam.members[0]; m.Peer == nil || m.Peer.Device != "Laptop" || m.Peer.User != "Sam" || m.Peer.Mine {
		t.Errorf("Bo is %+v", m.Peer)
	}
	// Through Ada's door, at her Tailscale address.
	if !strings.HasPrefix(guest.jam.guest.Addr(), "127.0.0.1:") || guest.jam.guest.Addr() != host.tail.door.Addrs()[0].String() {
		t.Errorf("joined at %s, the door is at %v", guest.jam.guest.Addr(), host.tail.door.Addrs())
	}
	tt.Frame()
	wantTexts(t, tt, "Sam (sam@github) · In your Jam")
}

// An invite from a device of someone Bo's computer is shared with: Bo
// cannot reach it, so cannot join its Jam. Bo is offered to host one,
// which they can reach, instead.
func TestTailnetInviteOneWay(t *testing.T) {
	host, guest, _, gf := tailPair(t)
	apps := []*App{host, guest}
	gf.mu.Lock()
	for _, p := range gf.st.Peer {
		p.ShareeNode = true // Ada's Studio, to Bo's computer
	}
	gf.mu.Unlock()
	readTail(t, apps, host)
	host.tailInvite(host.tailDevices()[0].Device)
	both(t, apps, "the invite", func() bool { return len(guest.tail.invites) == 1 })
	if !guest.tail.invites[0].oneWay {
		t.Fatal("the invite is not known for one way")
	}
	gt := ui.NewTester(guest.view, 1240, 900)
	gt.Frame()
	wantTexts(t, gt, "Ada invited you to a Jam (beta)", "yours can't reach theirs", "Host a Jam")
	if gt.HasText("Turn on and join") {
		t.Error("offered to join a Jam that cannot be reached")
	}
	click(t, gt, "Host a Jam")
	if guest.jam.host == nil || guest.jam.guest != nil || !guest.jam.open || !guest.settings.Jam {
		t.Errorf("Bo hosts no Jam: host=%v guest=%v open=%v", guest.jam.host != nil, guest.jam.guest != nil, guest.jam.open)
	}
}

// "Not now" lets the invite be, and the beta stays off.
func TestTailnetInviteNotNow(t *testing.T) {
	host, guest, _, _ := tailPair(t)
	apps := []*App{host, guest}
	readTail(t, apps, host)
	host.tailInvite(host.tailDevices()[0].Device)
	both(t, apps, "the invite", func() bool { return len(guest.tail.invites) == 1 })
	gt := ui.NewTester(guest.view, 1240, 900)
	gt.Frame()
	click(t, gt, "Not now")
	gt.Frame()
	if len(guest.tail.invites) != 0 || guest.settings.Jam || guest.jam.in() || gt.HasText("Ada invited you to a Jam (beta)") {
		t.Error("the invite was taken")
	}
}

// A device of Ada's own joins her Jam over the tailnet without the code
// and without asking; someone else's asks, as who they are.
func TestTailnetOwnDeviceJoins(t *testing.T) {
	host, guest, hf, _ := tailPair(t)
	apps := []*App{host, guest}
	hf.mu.Lock()
	hf.who = func(string) *apitype.WhoIsResponse { return whoIs("ada2", "Ada's Laptop", 10) }
	hf.mu.Unlock()
	host.startJam()
	guest.settings.Jam = true
	guest.joinJam(host.tail.door.Addrs()[0].String(), "")
	both(t, apps, "Ada's laptop in", func() bool { return guest.jam.guest != nil && len(host.jam.members) == 1 })
	if len(host.jam.requests) != 0 {
		t.Errorf("her own device asked: %+v", host.jam.requests)
	}
	if m := host.jam.members[0]; m.Peer == nil || !m.Peer.Mine || m.Peer.Device != "Ada's Laptop" {
		t.Errorf("the member: %+v", m.Peer)
	}
	guest.leaveJam(true)
	both(t, apps, "gone", func() bool { return len(host.jam.members) == 0 })

	// Sam's: asks, as Tailscale names him, whatever name he gives.
	hf.mu.Lock()
	hf.who = func(string) *apitype.WhoIsResponse { return whoIs("n3", "Sam's ThinkPad", 20) }
	hf.mu.Unlock()
	guest.settings.Session.UserName = "Ada"
	guest.joinJam(host.tail.door.Addrs()[0].String(), "")
	both(t, apps, "Sam asking", func() bool { return len(host.jam.requests) == 1 })
	tt := ui.NewTester(host.view, 1240, 900)
	host.openJam()
	tt.Frame()
	wantTexts(t, tt, "Sam's ThinkPad (Sam) wants to join")
	click(t, tt, "Let in")
	both(t, apps, "Sam in", func() bool { return guest.jam.guest != nil && len(host.jam.members) == 1 })
	// A LAN join, of someone Tailscale does not know, keeps asking for
	// nothing but the code.
	if host.tailIdentify(context.Background(), "192.168.1.20:5000") != nil {
		t.Error("an address of the network is someone on the tailnet")
	}
}

// The door is open on this computer's Tailscale addresses alone: never
// every address, nor the network's, nor this computer's own, even where
// none of the Tailscale ones can be had. The setting closes it.
func TestTailnetDoorAddresses(t *testing.T) {
	a := testApp(t)
	f := &fakeTail{st: tailStatus(tailPeer("me", "Desk", 10, "100.127.255.254"))}
	f.st.Self.TailscaleIPs = []netip.Addr{
		netip.MustParseAddr("100.127.255.254"), netip.MustParseAddr("fd7a:115c:a1e0::fffe"),
		netip.MustParseAddr("192.168.1.9"), netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("0.0.0.0"),
	}
	a.tail.api = f
	a.tailApply(tailnet.Read(f.st), true, false)
	want := []netip.Addr{netip.MustParseAddr("100.127.255.254"), netip.MustParseAddr("fd7a:115c:a1e0::fffe")}
	if got := a.tailDoorAddrs(); !slices.Equal(got, want) {
		t.Errorf("the door would open on %v", got)
	}
	// This computer has neither: the door stays shut, on every address.
	if a.tail.door != nil {
		t.Errorf("a door open on %v", a.tail.door.Addrs())
		a.closeTail()
	}
	// The beta off, the door is open all the same; the setting shuts it.
	onTailnet(a, f, nil)
	f.st.Self.TailscaleIPs = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	a.tailApply(tailnet.Read(f.st), true, false)
	if a.tail.door == nil || a.settings.Jam {
		t.Fatalf("no door with the beta off: %s", a.tail.doorErr)
	}
	defer a.closeTail()
	a.settings.TailnetInvitesOff = true
	a.tailDoor()
	if a.tail.door != nil {
		t.Error("the setting does not shut the door")
	}
	tt := ui.NewTester(a.view, 1240, 900)
	a.goTo("/settings")
	tt.Frame()
	wantTexts(t, tt, "Let devices on my tailnet invite me to Jams")
}

// In a window too low for the Jam's dialog, as with many people or
// devices, what is under its title scrolls: nothing falls out of it.
func TestJamDialogScrolls(t *testing.T) {
	host, guest, _, _ := tailPair(t)
	readTail(t, []*App{host, guest}, host)
	host.startJam()
	host.openJam()
	// In a window high enough, all of it shows, as it did.
	tall := ui.NewTester(host.view, 1240, 900)
	tall.Frame()
	if end, ok := tall.Find("End Jam"); !ok || end.Y+end.H > 900 || end.H == 0 {
		t.Errorf("in a high window, End Jam is at %+v", end)
	}
	if dir := os.Getenv("AURELIA_SHOTS"); dir != "" {
		savePNG(t, filepath.Join(dir, "jam-tall.png"), tall.Image())
	}
	const h = 480
	tt := ui.NewTester(host.view, 1240, h)
	tt.Frame()
	title, ok := tt.Find("Your Jam")
	if !ok || title.Y < 0 {
		t.Fatalf("the title is at %+v", title)
	}
	if dir := os.Getenv("AURELIA_SHOTS"); dir != "" {
		savePNG(t, filepath.Join(dir, "jam-low-top.png"), tt.Image())
	}
	for range 10 {
		tt.Scroll(620, h/2, 0, 400)
		tt.Frame()
	}
	end, ok := tt.Find("End Jam")
	if !ok || end.Y+end.H > h || end.Y < 0 {
		t.Errorf("End Jam is at %+v, in a window %d high", end, h)
	}
	if title2, _ := tt.Find("Your Jam"); title2 != title {
		t.Errorf("the title moved from %+v to %+v", title, title2)
	}
	if dir := os.Getenv("AURELIA_SHOTS"); dir != "" {
		savePNG(t, filepath.Join(dir, "jam-low-end.png"), tt.Image())
	}
}

func savePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

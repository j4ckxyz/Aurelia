package tailnet

import (
	"net/netip"
	"testing"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/views"
)

func peer(id, host, dns string, user tailcfg.UserID, ip string) *ipnstate.PeerStatus {
	return &ipnstate.PeerStatus{
		ID: tailcfg.StableNodeID(id), HostName: host, DNSName: dns, UserID: user, Online: true,
		TailscaleIPs: []netip.Addr{netip.MustParseAddr(ip)},
	}
}

// Whose each device is: the user's own, someone else's, a tagged one's
// tags; one of someone the computer is shared with is a sharee's.
func TestRead(t *testing.T) {
	tags := views.SliceOf([]string{"tag:server"})
	server := peer("n3", "nas", "nas.tail.ts.net.", 999, "100.64.0.3")
	server.Tags = &tags
	sharee := peer("n4", "device-of-shared-to-user", "", 30, "100.64.0.4")
	sharee.ShareeNode = true
	phone := peer("n5", "localhost", "iphone-16.tail.ts.net.", 10, "100.64.0.5")
	st := &ipnstate.Status{
		BackendState: "Running",
		Self:         peer("me", "Studio", "studio.tail.ts.net.", 10, "100.64.0.1"),
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			key.NewNode().Public(): peer("n2", "ThinkPad", "thinkpad.tail.ts.net.", 20, "100.64.0.2"),
			key.NewNode().Public(): server,
			key.NewNode().Public(): sharee,
			key.NewNode().Public(): phone,
		},
		User: map[tailcfg.UserID]tailcfg.UserProfile{
			10: {ID: 10, LoginName: "ada@github", DisplayName: "Ada"},
			20: {ID: 20, LoginName: "sam@github", DisplayName: "Sam"},
			30: {ID: 30, LoginName: "robin@example.com", DisplayName: "Robin"},
		},
	}
	st.Self.TailscaleIPs = append(st.Self.TailscaleIPs, netip.MustParseAddr("fd7a:115c:a1e0::1"))
	n := Read(st)
	if !n.Running || n.UserID != 10 || n.Self.Name != "Studio" || n.Self.Addr() != netip.MustParseAddr("100.64.0.1") {
		t.Fatalf("self: %+v", n)
	}
	by := map[string]Device{}
	for _, d := range n.Devices {
		by[d.ID] = d
	}
	if n.Devices[0].ID != "n5" {
		t.Errorf("the user's own devices are not first: %s", n.Devices[0].Name)
	}
	cases := []struct{ id, name, label string }{
		{"n2", "ThinkPad", "Sam (sam@github)"},
		{"n3", "nas", "tag:server"},
		{"n5", "iphone-16", "Yours"},
		{"n4", "device-of-shared-to-user", "Robin (robin@example.com)"},
	}
	for _, c := range cases {
		d := by[c.id]
		if d.Name != c.name || d.Owner.Label() != c.label {
			t.Errorf("%s: %q, %q; want %q, %q", c.id, d.Name, d.Owner.Label(), c.name, c.label)
		}
	}
	if !by["n4"].Sharee || by["n2"].Sharee {
		t.Error("the sharee is not told apart")
	}
	if by["n3"].Owner.Mine || by["n2"].Owner.Mine || !by["n5"].Owner.Mine {
		t.Error("whose is wrong")
	}
	if Read(&ipnstate.Status{BackendState: "Stopped", Self: st.Self}).Running {
		t.Error("a stopped Tailscale is running")
	}
	if Read(nil).Running {
		t.Error("no Tailscale is running")
	}
}

// WhoIs: the device and its owner, the user's own or not.
func TestWho(t *testing.T) {
	hi := (&tailcfg.Hostinfo{Hostname: "Sam's ThinkPad"}).View()
	w := &apitype.WhoIsResponse{
		Node:        &tailcfg.Node{StableID: "n2", Name: "thinkpad.tail.ts.net.", User: 20, Hostinfo: hi},
		UserProfile: &tailcfg.UserProfile{ID: 20, LoginName: "sam@github", DisplayName: "Sam"},
	}
	d, ok := Who(w, 10)
	if !ok || d.Name != "Sam's ThinkPad" || d.Owner.Mine || d.Owner.Label() != "Sam (sam@github)" || d.ID != "n2" {
		t.Errorf("someone else's: %+v", d)
	}
	if d, _ := Who(w, 20); !d.Owner.Mine {
		t.Error("the user's own is not")
	}
	w.Node.Tags = []string{"tag:ci"}
	if d, _ := Who(w, 20); d.Owner.Mine || d.Owner.Label() != "tag:ci" {
		t.Errorf("a tagged device is someone's: %+v", d.Owner)
	}
	if _, ok := Who(&apitype.WhoIsResponse{}, 10); ok {
		t.Error("nobody is someone")
	}
}

func TestIsTailscaleIP(t *testing.T) {
	for s, want := range map[string]bool{
		"100.64.0.1": true, "100.127.255.254": true, "100.128.0.1": false, "100.63.255.255": false,
		"192.168.1.2": false, "fd7a:115c:a1e0::ca01:f51e": true, "fd7a:115c:a1e1::1": false,
		"::ffff:100.100.1.1": true, "127.0.0.1": false,
	} {
		if got := IsTailscaleIP(netip.MustParseAddr(s)); got != want {
			t.Errorf("%s: %v", s, got)
		}
	}
}

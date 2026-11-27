package jam

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"
)

var loopback = netip.MustParseAddr("127.0.0.1")

func openDoor(t *testing.T, d *Door) {
	t.Helper()
	if err := OpenDoor(d, []netip.Addr{loopback}, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
}

func doorAddr(d *Door) string { return d.Addrs()[0].String() }

// A door is open on the addresses it is given and no others: never on
// every address, and not on the computer's other ones.
func TestDoorBindsItsAddressesOnly(t *testing.T) {
	for _, a := range []string{"0.0.0.0", "::"} {
		if err := OpenDoor(&Door{}, []netip.Addr{netip.MustParseAddr(a)}, 0); !errors.Is(err, ErrUnspecified) {
			t.Errorf("a door on %s: %v", a, err)
		}
	}
	if err := OpenDoor(&Door{}, nil, 0); err == nil {
		t.Error("a door on no address")
	}
	// An address this computer does not have: nothing is open.
	away := &Door{}
	if err := OpenDoor(away, []netip.Addr{netip.MustParseAddr("100.127.255.254")}, 0); err == nil || len(away.Addrs()) != 0 {
		t.Errorf("a door on an address not had: %v, %v", err, away.Addrs())
		away.Close()
	}
	d := &Door{Identify: func(context.Context, string) *Peer { return &Peer{Device: "x"} }}
	openDoor(t, d)
	if got := d.Addrs(); len(got) != 1 || got[0].Addr() != loopback || d.Port() == 0 {
		t.Fatalf("open on %v", got)
	}
	// Not at the computer's address on its network.
	if ip := LocalIP(); ip != "127.0.0.1" {
		if c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(d.Port())), time.Second); err == nil {
			c.Close()
			t.Errorf("the door answers at %s too", ip)
		}
	}
	if c, err := net.DialTimeout("tcp", net.JoinHostPort("::1", strconv.Itoa(d.Port())), time.Second); err == nil {
		c.Close()
		t.Error("the door answers at ::1 too")
	}
}

// The door answers only those Identify knows, and tells them of the
// device; invites come with who sent them, by Identify alone.
func TestDoor(t *testing.T) {
	var known bool
	var mu sync.Mutex
	var got []string
	d := &Door{
		Identify: func(_ context.Context, remote string) *Peer {
			if !known {
				return nil
			}
			return &Peer{User: "Sam", Device: "ThinkPad", Node: "n2"}
		},
		About: func() About {
			return About{Name: "Studio", Jam: true, Live: &Live{Host: "Ada", People: 2, Port: 47475}}
		},
		OnInvite: func(from Peer, addr string, inv Invitation) {
			mu.Lock()
			got = append(got, from.Who()+" "+addr+" "+inv.Code)
			mu.Unlock()
		},
	}
	openDoor(t, d)
	ctx := context.Background()
	if _, err := Probe(ctx, doorAddr(d)); err == nil {
		t.Fatal("a stranger is answered")
	}
	if err := SendInvite(ctx, doorAddr(d), "K7QX4R", 4000); err == nil {
		t.Fatal("a stranger's invite is taken")
	}
	known = true
	about, err := Probe(ctx, doorAddr(d))
	if err != nil {
		t.Fatal(err)
	}
	if about.App != AppName || about.Proto != Proto || about.Name != "Studio" || !about.Jam || about.Live == nil || about.Live.People != 2 {
		t.Errorf("about: %+v", about)
	}
	if err := SendInvite(ctx, doorAddr(d), "K7QX4R", 4000); err != nil {
		t.Fatal(err)
	}
	// Again at once: a nuisance.
	if err := SendInvite(ctx, doorAddr(d), "K7QX4R", 4000); err == nil {
		t.Error("invites without end")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "ThinkPad (Sam) 127.0.0.1:4000 K7QX4R" {
		t.Errorf("invites: %q", got)
	}
}

// A device that does not answer is given up on as soon as the probe's
// time is up.
func TestProbeTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	// It takes the connection, and says nothing.
	var mu sync.Mutex
	var conns []net.Conn
	defer func() {
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	began := time.Now()
	if _, err := Probe(ctx, ln.Addr().String()); err == nil {
		t.Fatal("silence is an answer")
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("the probe took %v", took)
	}
	// And something that is not Aurelia is not.
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"app":"other"}`)) })}
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	go srv.Serve(ln2)
	defer srv.Close()
	if _, err := Probe(context.Background(), ln2.Addr().String()); err == nil {
		t.Error("another app is Aurelia")
	}
}

// The door hands the Jam held here on: it is joined through it.
func TestDoorHandsOnTheJam(t *testing.T) {
	h := newHost(t)
	d := &Door{Identify: func(context.Context, string) *Peer { return nil }}
	openDoor(t, d)
	ctx := context.Background()
	if _, err := Join(ctx, doorAddr(d), Hello{Name: "Bo", Code: h.Code()}, Handlers{}); err == nil {
		t.Fatal("joined a door without a Jam")
	}
	d.SetJam(h.Handler())
	g, err := Join(ctx, doorAddr(d), Hello{Name: "Bo", Code: h.Code()}, Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Leave()
	waitFor(t, "Bo in", func() bool { return len(h.Members()) == 1 })
}

// Over a tailnet, a device of the host's own owner joins without the
// code; anyone else's asks, as who Tailscale says they are.
func TestJoinAsIdentified(t *testing.T) {
	var mu sync.Mutex
	var asked []Request
	who := &Peer{User: "Ada", Device: "Laptop", Node: "n1", Mine: true}
	h := newHost(t, func(h *Host) {
		h.Identify = func(context.Context, string) *Peer {
			mu.Lock()
			defer mu.Unlock()
			return who
		}
		h.OnRequest = func(r Request) {
			mu.Lock()
			asked = append(asked, r)
			mu.Unlock()
			h.Allow(r.ID)
		}
	})
	addr := "127.0.0.1:" + strconv.Itoa(h.Port())
	g, err := Join(context.Background(), addr, Hello{Name: "Ada"}, Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Leave()
	waitFor(t, "Ada's laptop in", func() bool { return len(h.Members()) == 1 })
	mu.Lock()
	if len(asked) != 0 {
		t.Errorf("the host's own device asked: %+v", asked)
	}
	who = &Peer{User: "Sam", Login: "sam@github", Device: "Sam's ThinkPad", Node: "n2"}
	mu.Unlock()
	g2, err := Join(context.Background(), addr, Hello{Name: "I am Ada"}, Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer g2.Leave()
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0].Peer == nil || asked[0].Peer.Who() != "Sam's ThinkPad (Sam)" {
		t.Fatalf("asked: %+v", asked)
	}
	// The guests are told the device and the name, not the login.
	for _, m := range h.Members() {
		if m.Peer != nil && (m.Peer.Login != "" || m.Peer.Node != "") {
			t.Errorf("the guests are told %+v", m.Peer)
		}
	}
}

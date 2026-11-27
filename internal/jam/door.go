package jam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// A door is how the Aurelias of a tailnet know of one another: a small
// web server on this computer's Tailscale addresses alone, at TCPPort,
// which answers whether Aurelia is here, and takes invites to Jams. It
// is open while Aurelia is, the Jams' beta on or not, so that a device
// whose user never turned it on can be invited all the same.
//
// Every request is from someone Identify knows, which on a tailnet is
// Tailscale's word for who the device and its owner are: a request
// from anyone else is turned away. What else comes to the door is the
// Jam's, when this computer holds one: a Jam's own listener, on every
// address, may not have the port, as the door has it on the tailnet.
//
// Plain HTTP and WebSockets are enough: Tailscale encrypts what goes
// between its devices.

// About is what a device says of itself, asked whether it is Aurelia.
type About struct {
	App     string `json:"app"` // AppName
	Proto   int    `json:"proto"`
	Version string `json:"version,omitempty"`
	Name    string `json:"name"` // the device's, as the app shows it
	// Jam is set while the Jams' beta is on there.
	Jam bool `json:"jam,omitempty"`
	// Live is the Jam the device holds, nil for none.
	Live *Live `json:"live,omitempty"`
}

// AppName is what About.App says.
const AppName = "aurelia"

// Live is a Jam held on a device, as its door tells it.
type Live struct {
	Host   string `json:"host"`   // the host's name
	People int    `json:"people"` // in it, the host with them
	// Port is where it is joined, at the device's address on the tailnet.
	Port int `json:"port"`
}

// Invitation asks a device to join a Jam. Who sent it is the door's to
// know, by the connection, not the invite's to say.
type Invitation struct {
	Proto int    `json:"proto"`
	Code  string `json:"code"`
	// Port is where the Jam is: at the address the invite came from.
	Port int `json:"port"`
}

// Door is the door of this computer on a tailnet.
type Door struct {
	// Identify says who a request is from, by its remote address: nil
	// for nobody it knows, who is turned away.
	Identify func(ctx context.Context, remoteAddr string) *Peer
	// About is what the door answers about this device.
	About func() About
	// OnInvite is an invite to the Jam at addr, from peer.
	OnInvite func(from Peer, addr string, inv Invitation)

	srv   *http.Server
	lns   []net.Listener
	addrs []netip.AddrPort
	jam   atomic.Pointer[http.Handler]

	mu      sync.Mutex
	invited map[string]time.Time // when each device last invited
	closed  bool
}

// ErrUnspecified is the error of a door asked to open on every address.
var ErrUnspecified = errors.New("jam: a door opens on addresses of its own, not on every one")

// OpenDoor opens d on addrs, at port: on those addresses and no others.
// An address that cannot be had is left out; with none had, the door
// does not open. Port 0 is any, the same for each address.
func OpenDoor(d *Door, addrs []netip.Addr, port int) error {
	if len(addrs) == 0 {
		return errors.New("jam: a door needs an address")
	}
	for _, a := range addrs {
		if !a.IsValid() || a.IsUnspecified() {
			return ErrUnspecified
		}
	}
	var errs []error
	for _, a := range addrs {
		ln, err := net.Listen("tcp", netip.AddrPortFrom(a.Unmap(), uint16(port)).String())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if port == 0 {
			port = ln.Addr().(*net.TCPAddr).Port
		}
		d.lns = append(d.lns, ln)
		d.addrs = append(d.addrs, ln.Addr().(*net.TCPAddr).AddrPort())
	}
	if len(d.lns) == 0 {
		return fmt.Errorf("jam: opening the door: %w", errors.Join(errs...))
	}
	d.invited = map[string]time.Time{}
	d.srv = &http.Server{Handler: d, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute,
		ErrorLog: log.New(io.Discard, "", 0)}
	for _, ln := range d.lns {
		go d.srv.Serve(ln)
	}
	return nil
}

// Addrs are where the door is open.
func (d *Door) Addrs() []netip.AddrPort { return d.addrs }

// Port is the door's.
func (d *Door) Port() int {
	if len(d.addrs) == 0 {
		return 0
	}
	return int(d.addrs[0].Port())
}

// SetJam hands what comes to the door that is not its own to h, the Jam
// this computer holds; nil for none.
func (d *Door) SetJam(h http.Handler) {
	if h == nil {
		d.jam.Store(nil)
		return
	}
	d.jam.Store(&h)
}

// Close closes the door.
func (d *Door) Close() {
	d.mu.Lock()
	closed := d.closed
	d.closed = true
	d.mu.Unlock()
	if closed || d.srv == nil {
		return
	}
	d.srv.Close()
}

func (d *Door) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/aurelia", "/invite":
	default:
		if h := d.jam.Load(); h != nil {
			(*h).ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	var peer *Peer
	if d.Identify != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		peer = d.Identify(ctx, r.RemoteAddr)
		cancel()
	}
	if peer == nil {
		http.Error(w, "not a device of this tailnet", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/aurelia" && r.Method == http.MethodGet:
		var about About
		if d.About != nil {
			about = d.About()
		}
		about.App, about.Proto = AppName, Proto
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(about)
	case r.URL.Path == "/invite" && r.Method == http.MethodPost:
		var inv Invitation
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&inv); err != nil || inv.Port <= 0 || inv.Port > 65535 || NormalizeCode(inv.Code) == "" {
			http.Error(w, "not an invite", http.StatusBadRequest)
			return
		}
		if inv.Proto != Proto {
			http.Error(w, "This device has another version of Aurelia. Update both to the latest.", http.StatusConflict)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "no address", http.StatusBadRequest)
			return
		}
		// One invite a device, now and then: more is a nuisance.
		d.mu.Lock()
		key := peer.Node + "|" + host
		last, seen := d.invited[key]
		if !seen || time.Since(last) > 3*time.Second {
			d.invited[key] = time.Now()
		}
		if len(d.invited) > 256 {
			d.invited = map[string]time.Time{}
		}
		d.mu.Unlock()
		if seen && time.Since(last) <= 3*time.Second {
			http.Error(w, "invited a moment ago", http.StatusTooManyRequests)
			return
		}
		if d.OnInvite != nil {
			d.OnInvite(*peer, net.JoinHostPort(host, strconv.Itoa(inv.Port)), inv)
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		http.Error(w, "no", http.StatusMethodNotAllowed)
	}
}

// Probe asks the door at addr whether it is Aurelia's.
func Probe(ctx context.Context, addr string) (*About, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/aurelia", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("jam: " + resp.Status)
	}
	var about About
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&about); err != nil {
		return nil, err
	}
	if about.App != AppName {
		return nil, errors.New("jam: not Aurelia")
	}
	return &about, nil
}

// SendInvite invites the device whose door is at addr to the Jam this
// computer holds, of code, at port.
func SendInvite(ctx context.Context, addr, code string, port int) error {
	b, _ := json.Marshal(Invitation{Proto: Proto, Code: code, Port: port})
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+addr+"/invite", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &InviteError{Status: resp.StatusCode, Text: string(bytes.TrimSpace(msg))}
	}
	return nil
}

// InviteError is of an invite the device turned away.
type InviteError struct {
	Status int
	Text   string
}

func (e *InviteError) Error() string { return e.Text }

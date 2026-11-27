package jam

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Guests find hosts by a broadcast on UDPPort, of every network the
// computer is on: a query, which hosts answer to the address it came
// from. Only the host listens, which it does for its connections anyway,
// so that a guest's firewall need let nothing in. A query with a code is
// answered by its host alone; one without, by every host that shows
// itself to those nearby.

type query struct {
	Q     string `json:"q"` // queryMagic
	Proto int    `json:"proto"`
	Code  string `json:"code,omitempty"`
}

type answer struct {
	Q     string `json:"q"`
	Proto int    `json:"proto"`
	Name  string `json:"name"`
	Port  int    `json:"port"`
	ID    string `json:"id"` // tells hosts apart, as their answers come over several networks
}

const queryMagic = "aurelia-jam"

// Found is a host found on the network.
type Found struct {
	Name, Addr, ID string
	// Proto is the host's version of the protocol: another than Proto
	// cannot be joined.
	Proto int
}

// announcer answers queries for a host.
type announcer struct {
	conn *net.UDPConn
	open atomic.Bool // answer queries without the code
	once sync.Once
}

// announce answers queries for h, until close. It returns nil when the
// port is taken, as by another Jam on this computer: the host is then
// joined by its invite.
func announce(h *Host, open bool) *announcer {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: UDPPort})
	if err != nil {
		return nil
	}
	an := &announcer{conn: conn}
	an.open.Store(open)
	id := token(6)
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var q query
			if json.Unmarshal(buf[:n], &q) != nil || q.Q != queryMagic {
				continue
			}
			switch {
			case q.Code != "" && NormalizeCode(q.Code) != h.code:
				continue
			case q.Code == "" && !an.open.Load():
				continue
			}
			b, _ := json.Marshal(answer{Q: queryMagic, Proto: Proto, Name: h.name, Port: h.port, ID: id})
			conn.WriteToUDP(b, from)
		}
	}()
	return an
}

func (an *announcer) close() {
	an.once.Do(func() { an.conn.Close() })
}

// Discover looks for hosts until ctx ends, and tells of each as it is
// found, once: those with code, or every host that shows itself for "".
func Discover(ctx context.Context, code string, found func(Found)) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return err
	}
	defer conn.Close()
	context.AfterFunc(ctx, func() { conn.Close() })
	q, _ := json.Marshal(query{Q: queryMagic, Proto: Proto, Code: NormalizeCode(code)})
	go func() {
		// Asked often at first, while the user waits, then now and then.
		wait := 300 * time.Millisecond
		for ctx.Err() == nil {
			for _, to := range broadcasts() {
				conn.WriteToUDP(q, &net.UDPAddr{IP: to, Port: UDPPort})
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = min(wait*2, 3*time.Second)
		}
	}()
	seen := map[string]bool{}
	buf := make([]byte, 1500)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		var an answer
		if json.Unmarshal(buf[:n], &an) != nil || an.Q != queryMagic || an.Port <= 0 || seen[an.ID] {
			continue
		}
		seen[an.ID] = true
		ip := from.IP
		if ip.IsLoopback() {
			// The host is this computer: it is known to others by its
			// address on the network, and to itself by that as well.
			if l := net.ParseIP(LocalIP()); l != nil {
				ip = l
			}
		}
		found(Found{Name: an.Name, Addr: net.JoinHostPort(ip.String(), strconv.Itoa(an.Port)), ID: an.ID, Proto: an.Proto})
	}
}

// broadcasts are where a query goes: the broadcast address of each
// network the computer is on, everyone's, and this computer, for a Jam
// held on it.
func broadcasts() []net.IP {
	out := []net.IP{net.IPv4bcast, net.IPv4(127, 0, 0, 1)}
	ifs, _ := net.Interfaces()
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagBroadcast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := n.IP.To4()
			if ip4 == nil || len(n.Mask) != net.IPv4len {
				continue
			}
			b := make(net.IP, 4)
			for i := range b {
				b[i] = ip4[i] | ^n.Mask[i]
			}
			out = append(out, b)
		}
	}
	return out
}

// LocalIP is the address of this computer on the network it reaches
// others by: that of the route to the internet, which a UDP socket
// learns without sending anything, else the first private address.
func LocalIP() string {
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		ip := c.LocalAddr().(*net.UDPAddr).IP
		c.Close()
		if !ip.IsLoopback() && !ip.IsUnspecified() {
			return ip.String()
		}
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
			return n.IP.String()
		}
	}
	return "127.0.0.1"
}

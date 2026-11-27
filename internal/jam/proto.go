// Package jam lets the Aurelias of one network listen together. One of
// them hosts: its queue is the Jam's, and the others, who join it, play
// what it plays on their own sound cards, each with their own settings,
// in step with it to within a second or two. Everyone may add songs.
//
// The host is a small web server. Guests find it on the network, by a
// broadcast that it answers, or by its address in an invite, and keep a
// WebSocket open to it: down it come the queue and where the song is, up
// it go the guests' wishes, and the host does what they ask.
//
// A song is known by its server and its ID. A guest of the host's server
// plays its songs from that server, as it plays its own. One whose
// server does not have a song plays it from the host, which serves what
// it plays, and the songs of a guest the host itself fetches from that
// guest: the guest uploads them to the host as it is asked, over a
// connection of its own, so that nothing need reach a guest from outside.
package jam

import (
	"crypto/rand"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"aurelia/internal/library"
)

// Proto is the version of the protocol. A host turns away guests of
// another version, who would misread it.
const Proto = 1

// Ports: the host listens for connections on TCPPort, or another when
// that one is taken, and for the broadcasts of guests on UDPPort.
const (
	TCPPort = 47475
	UDPPort = 47474
)

// Msg is what goes over the WebSocket, one JSON object a message. T says
// which it is; the fields it uses follow.
type Msg struct {
	T string `json:"t"`

	// hello, guest to host: who joins, with the code or the token of an
	// earlier connection.
	Hello *Hello `json:"hello,omitempty"`
	// welcome, host to guest: the guest's ID and its token, which its
	// requests carry.
	ID    string `json:"id,omitempty"`
	Token string `json:"token,omitempty"`
	// state, host to guest.
	State *State `json:"state,omitempty"`
	// ping, guest to host, and pong back: the guest's clock when it
	// asked, and the host's when it answered.
	T0 int64 `json:"t0,omitempty"`
	T1 int64 `json:"t1,omitempty"`
	// ask, guest to host.
	Ask *Ask `json:"ask,omitempty"`
	// fetch, host to guest: a song or a picture of the guest's to upload.
	Fetch *Fetch `json:"fetch,omitempty"`
	// Text is why: of denied, ended and removed.
	Text string `json:"text,omitempty"`
}

// The messages.
const (
	tHello   = "hello"
	tWelcome = "welcome"
	tWait    = "wait"   // the host is asked to let the guest in
	tDenied  = "denied" // it did not, or the code was wrong
	tState   = "state"
	tPing    = "ping"
	tPong    = "pong"
	tAsk     = "ask"
	tFetch   = "fetch"
	tResync  = "resync" // the guest lacks a song's details: all of them again
	tBye     = "bye"    // the guest leaves
	tEnded   = "ended"  // the host ended the Jam
	tRemoved = "removed"
)

// Hello is what a guest says of itself.
type Hello struct {
	Proto  int    `json:"proto"`
	Name   string `json:"name"`
	Server string `json:"server,omitempty"` // the ID of its Jellyfin server
	Code   string `json:"code,omitempty"`
	Token  string `json:"token,omitempty"` // of the connection before, to come back
}

// Member is someone in the Jam.
type Member struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Server string `json:"server,omitempty"`
	Host   bool   `json:"host,omitempty"`
	// Away is set while the connection is lost, for the short while its
	// place is kept.
	Away bool `json:"away,omitempty"`
	// Peer is who the member is by the network it came over, when that
	// says: on a tailnet, by Tailscale.
	Peer *Peer `json:"peer,omitempty"`
}

// Peer is who a connection is from, as the network it came over vouches:
// on a tailnet, Tailscale, which knows the device and its owner. What a
// guest says of itself is its own word; this is not.
type Peer struct {
	User   string `json:"user,omitempty"`   // the owner's name: "Sam"
	Login  string `json:"login,omitempty"`  // "sam@github"
	Device string `json:"device,omitempty"` // "Sam's ThinkPad"
	Node   string `json:"node,omitempty"`   // the device's stable ID
	// Mine is set for a device of the same owner as this computer: of
	// the host, for a guest.
	Mine bool `json:"mine,omitempty"`
	// Tags are those of a device without an owner.
	Tags []string `json:"tags,omitempty"`
}

// Who says who a peer is: "Sam's ThinkPad (Sam)", or of the same owner
// as this computer, the device alone.
func (p *Peer) Who() string {
	switch {
	case p == nil:
		return ""
	case p.Mine || p.User == "":
		return p.Device
	case p.Device == "":
		return p.User
	}
	return p.Device + " (" + p.User + ")"
}

// Song is a song as it travels: the library's, with its server.
type Song struct {
	Server string `json:"sv"`
	library.Song
}

// Key names a song across servers.
func Key(server, id string) string { return server + "/" + id }

// SplitKey returns the server and the ID of a key.
func SplitKey(key string) (server, id string) {
	server, id, _ = strings.Cut(key, "/")
	return server, id
}

// Item is a song of the queue. Song is sent to each guest once, with the
// first state that has it: after that the key alone.
type Item struct {
	Key  string `json:"k"`
	Song *Song  `json:"s,omitempty"`
	// By is the name of whoever added it, "" for the host or for songs
	// that were in the queue anyway.
	By string `json:"by,omitempty"`
}

// State is the Jam: who is in it, and what plays. The queue is the songs
// around the one playing, not the whole of a queue of a library.
type State struct {
	Host    string   `json:"host"` // the host's name
	Members []Member `json:"members,omitempty"`

	Queue []Item `json:"queue,omitempty"`
	// Index is the song playing in Queue, -1 for none. Added and
	// PlayNexts count the songs added after it, as the player does.
	Index     int `json:"index"`
	Added     int `json:"added,omitempty"`
	PlayNexts int `json:"playNexts,omitempty"`
	// Playing is set while the song plays, not paused nor ended.
	Playing bool `json:"playing,omitempty"`
	// Position is where in the song the host was at At, by its clock: in
	// milliseconds, and in milliseconds since 1970.
	Position int64 `json:"pos"`
	At       int64 `json:"at"`
	Shuffle  bool  `json:"shuffle,omitempty"`
	Repeat   int   `json:"repeat,omitempty"`
	// Control lets guests play, pause, skip, seek and change the queue;
	// without it they only add songs.
	Control bool `json:"control,omitempty"`
	// Log is what was done in the Jam, and by whom, the latest last.
	Log []Event `json:"log,omitempty"`
}

// Event is something done in the Jam: by whom, when by the host's clock
// (milliseconds since 1970), and what, as an Op of Ask's or one of the
// Ev constants, of the song named.
type Event struct {
	ID   int64  `json:"id"` // counts up, in the Jam
	At   int64  `json:"at"`
	Who  string `json:"who"`
	Host bool   `json:"host,omitempty"` // done by the host
	Op   string `json:"op"`
	Song string `json:"song,omitempty"`
	// N counts the songs of an add, Position is where a seek went, in
	// milliseconds, and On and Mode are shuffle's and repeat's.
	N        int   `json:"n,omitempty"`
	Position int64 `json:"pos,omitempty"`
	On       bool  `json:"on,omitempty"`
	Mode     int   `json:"mode,omitempty"`
}

// The ops of Event that are not of Ask.
const (
	EvJoined = "joined"
	EvLeft   = "left"
	EvAway   = "away" // lost, as a computer gone to sleep
	EvBack   = "back"
	EvStart  = "start" // played something new: Song, of N
)

// Ask is what a guest asks of the host. Places are in the queue of the
// state the guest has, and the keys of the songs there say which songs
// were meant, should the queue have changed since.
type Ask struct {
	// Op is one of the Op constants.
	Op    string   `json:"op"`
	Songs []Song   `json:"songs,omitempty"`
	At    []int    `json:"at,omitempty"`
	Keys  []string `json:"keys,omitempty"`
	To    int      `json:"to,omitempty"`
	// Position, in milliseconds.
	Position int64 `json:"pos,omitempty"`
	On       bool  `json:"on,omitempty"`
	Mode     int   `json:"mode,omitempty"`
}

// The ops of Ask.
const (
	OpAdd      = "add"    // Songs, after the songs added before
	OpNext     = "next"   // Songs, to play next
	OpPlay     = "play"   // the song at At[0]
	OpRemove   = "remove" // the song at At[0]
	OpMove     = "move"   // the songs at At, before the song at To
	OpClear    = "clear"  // the songs after the one playing
	OpPause    = "pause"
	OpResume   = "resume"
	OpSkip     = "skip"
	OpPrevious = "previous"
	OpSeek     = "seek"    // to Position
	OpShuffle  = "shuffle" // On
	OpRepeat   = "repeat"  // Mode
)

// Fetch is what the host asks a guest to upload: a song's file, its
// picture, or its lyrics.
type Fetch struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Key  string `json:"k,omitempty"` // of media and lyrics
	// Of an image.
	Item string `json:"item,omitempty"`
	Tag  string `json:"tag,omitempty"`
	Px   int    `json:"px,omitempty"`
}

// The kinds of Fetch.
const (
	KindMedia  = "media"
	KindImage  = "image"
	KindLyrics = "lyrics"
)

// codeAlphabet has no letters or digits that are taken for one another:
// no 0 nor O, no 1, I nor L.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// CodeLen is how long a code is.
const CodeLen = 6

// NewCode makes a code to join with.
func NewCode() string {
	var b [CodeLen]byte
	rand.Read(b[:])
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b[:])
}

// NormalizeCode returns a code as typed in the form it is compared in:
// capitals, without spaces or dashes.
func NormalizeCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ShowCode spaces a code to be read out: "K7Q X4R".
func ShowCode(code string) string {
	if len(code) == CodeLen {
		return code[:3] + " " + code[3:]
	}
	return code
}

// token makes a secret of n random bytes, in hex.
func token(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	const hex = "0123456789abcdef"
	out := make([]byte, 2*n)
	for i, c := range b {
		out[2*i], out[2*i+1] = hex[c>>4], hex[c&15]
	}
	return string(out)
}

// InviteScheme begins an invite: aurelia://jam/192.168.1.20:47475/K7QX4R.
const InviteScheme = "aurelia://jam/"

// Invite returns the invite of a host at addr with code.
func Invite(addr, code string) string { return InviteScheme + addr + "/" + code }

// InviteVia returns the invite of a host at addr with code that is also
// reached through a relay, at via: aurelia://jam/192.168.1.20:47475/K7QX4R?via=https%3A%2F%2Frelay.example%2Fr%2F….
// A guest tries the address of the network first, then the relay.
func InviteVia(addr, code, via string) string {
	if via == "" {
		return Invite(addr, code)
	}
	return Invite(addr, code) + "?via=" + url.QueryEscape(via)
}

// Target is what a guest joins: a host's address on a network, a relay's
// address of it, or both, and the Jam's code.
type Target struct {
	Addr, Via, Code string
}

// ParseTarget reads what a user typed or pasted to join, as ParseJoin
// does, and the relay an invite names, or a relay's address of a Jam
// followed by its code.
func ParseTarget(s string) (t Target, ok bool) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "?via="); i >= 0 {
		rest := s[i+len("?via="):]
		if j := strings.IndexAny(rest, " \t\r\n"); j >= 0 {
			rest = rest[:j]
		}
		if via, err := url.QueryUnescape(rest); err == nil && validVia(via) {
			t.Via = via
		}
		s = s[:i]
	}
	if IsRelayAddr(s) {
		// "https://relay.example/r/… K7Q X4R"
		via, code, _ := strings.Cut(s, " ")
		if !validVia(via) {
			return t, false
		}
		t.Via, t.Code = strings.TrimSuffix(via, "/"), NormalizeCode(code)
		return t, true
	}
	t.Addr, t.Code, ok = ParseJoin(s)
	if !ok && t.Via != "" && t.Code == "" {
		return t, false
	}
	return t, ok
}

// validVia reports whether a relay's address of a Jam is one.
func validVia(via string) bool {
	u, err := url.Parse(via)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && strings.Contains(u.Path, "/r/")
}

// ParseJoin reads what a user typed to join: an invite, an address and a
// code, or a code alone, which is looked for on the network. addr is ""
// for a code alone. An address is a name with dots, as a MagicDNS one,
// or an IP address, of either kind, with or without its port.
func ParseJoin(s string) (addr, code string, ok bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, InviteScheme)
	s = strings.TrimPrefix(s, "jam://")
	// The address is the first word, when it is one: "K7Q X4R" is a code
	// with a space.
	if i := strings.IndexAny(s, "/ \t"); i > 0 {
		if a, ok := hostPort(s[:i]); ok {
			code = NormalizeCode(s[i+1:])
			return a, code, code != ""
		}
	}
	code = NormalizeCode(s)
	return "", code, len(code) == CodeLen
}

// hostPort reads an address as typed, with the Jam's port when it has
// none.
func hostPort(s string) (string, bool) {
	port := strconv.Itoa(TCPPort)
	if h, p, err := net.SplitHostPort(s); err == nil {
		if n, err := strconv.Atoi(p); err != nil || n <= 0 || n > 65535 {
			return "", false
		}
		s, port = h, p
	}
	s = strings.TrimSuffix(strings.Trim(s, "[]"), ".")
	if ip, err := netip.ParseAddr(s); err == nil {
		return net.JoinHostPort(ip.String(), port), true
	}
	if strings.Contains(s, ".") || s == "localhost" {
		return net.JoinHostPort(s, port), true
	}
	return "", false
}

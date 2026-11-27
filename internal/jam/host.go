package jam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Host is a Jam this computer holds. Its callbacks are called on its own
// goroutines.
type Host struct {
	// OnAsk is what a guest asks.
	OnAsk func(m Member, a Ask)
	// OnRequest is someone without the code who would join: Allow or
	// Deny answers, and doing neither for two minutes denies.
	OnRequest func(r Request)
	// OnMembers tells who is in, as it changes.
	OnMembers func(members []Member)
	// Media opens a song of the queue for a guest, with its size, -1 when
	// not known; Image and Lyrics its picture and its lyrics. server is
	// that of the song, which may be a guest's, from whom the host gets
	// it with Fetch.
	Media  func(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Image  func(ctx context.Context, server, item, tag string, px int) (io.ReadCloser, int64, error)
	Lyrics func(ctx context.Context, key string) (io.ReadCloser, int64, error)
	// Identify, when set, says who a connection is from, by its remote
	// address, as the network vouches: nil for nobody it knows. It is
	// asked of every guest's connection. A device of the host's own owner
	// joins without the code; anyone else it knows asks, as themselves.
	Identify func(ctx context.Context, remoteAddr string) *Peer

	name, server, code, local string
	ln                        net.Listener
	srv                       *http.Server
	port                      int
	announce                  *announcer
	discoverable              bool

	mu       sync.Mutex
	closed   bool
	members  []*member
	requests map[string]*request
	fetches  map[string]chan pushed
	state    State
	wrong    int // codes that were wrong lately
	wrongAt  time.Time
}

// Request is someone who would join without the code: Peer is who the
// network says they are, when it says.
type Request struct {
	ID, Name string
	Peer     *Peer
}

type request struct {
	Request
	answer chan bool
}

// member is a guest, connected or away.
type member struct {
	Member
	token string
	// The connection, nil while away; gone is when it went.
	conn  *websocket.Conn
	stop  context.CancelFunc
	gone  time.Time
	out   chan Msg      // messages, in order
	wake  chan struct{} // a new state to send
	known map[string]bool
}

// How long a guest whose connection is lost keeps its place, how long the
// host waits for a guest that says nothing, and for an upload it asked.
const (
	awayFor    = 90 * time.Second
	silentFor  = 45 * time.Second
	fetchWait  = 20 * time.Second
	writeWait  = 10 * time.Second
	askWaitFor = 2 * time.Minute
)

// ErrNoOwner is the error of a song whose owner left.
var ErrNoOwner = errors.New("jam: nobody in the Jam has that song any more")

// NewHost makes a Jam: name is the host's, server the ID of its server.
// It listens on every address of the computer, and lets guests in once
// Start is called. discoverable has it answer the broadcasts of those
// looking for a Jam to join, not only of those with its code.
func NewHost(name, server string, discoverable bool) (*Host, error) {
	h := &Host{
		name: name, server: server, code: NewCode(), local: token(16),
		requests: map[string]*request{}, fetches: map[string]chan pushed{},
	}
	h.state.Index = -1
	h.state.Host = name
	h.state.Members = []Member{{ID: "host", Name: name, Server: server, Host: true}}
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(TCPPort))
	if err != nil {
		// Another Jam on this computer, or something else, has the port.
		ln, err = net.Listen("tcp", ":0")
		if err != nil {
			return nil, fmt.Errorf("jam: listening: %w", err)
		}
	}
	h.ln, h.port = ln, ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", h.serveWS)
	mux.HandleFunc("GET /media", h.serveMedia)
	mux.HandleFunc("GET /image", h.serveImage)
	mux.HandleFunc("GET /lyrics", h.serveLyrics)
	mux.HandleFunc("GET /pull", h.servePull)
	mux.HandleFunc("POST /push", h.servePush)
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute,
		ErrorLog: log.New(io.Discard, "", 0)}
	h.discoverable = discoverable
	return h, nil
}

// Handler serves the Jam: for a listener of another's, as the one of the
// tailnet, which hands what is not its own to the Jam.
func (h *Host) Handler() http.Handler { return h.srv.Handler }

// Start lets guests in, once the callbacks are set.
func (h *Host) Start() {
	go h.srv.Serve(h.ln)
	h.announce = announce(h, h.discoverable)
	go h.sweep()
}

// Code is the code to join with.
func (h *Host) Code() string { return h.code }

// Port is the port the host listens on.
func (h *Host) Port() int { return h.port }

// Addr is the address guests reach the host at, by the address of the
// computer on its network.
func (h *Host) Addr() string { return net.JoinHostPort(LocalIP(), strconv.Itoa(h.port)) }

// PullURL is where the host's own player gets a song of a guest's, which
// the host fetches from them as it is read.
func (h *Host) PullURL(key string) string {
	return "http://127.0.0.1:" + strconv.Itoa(h.port) + "/pull?k=" + escape(key) + "&t=" + h.local
}

// ImageURL and LyricsURL are where the host's own window gets the
// picture and the lyrics of a song of a guest's.
func (h *Host) ImageURL(server, item, tag string, px int) string {
	return "http://127.0.0.1:" + strconv.Itoa(h.port) + "/image?sv=" + escape(server) + "&item=" + escape(item) + "&tag=" + escape(tag) + "&px=" + strconv.Itoa(px) + "&t=" + h.local
}

func (h *Host) LyricsURL(key string) string {
	return "http://127.0.0.1:" + strconv.Itoa(h.port) + "/lyrics?k=" + escape(key) + "&t=" + h.local
}

// SetDiscoverable has the host answer those looking for a Jam, or only
// those with its code.
func (h *Host) SetDiscoverable(on bool) {
	if h.announce != nil {
		h.announce.open.Store(on)
	}
}

// Close ends the Jam: the guests are told.
func (h *Host) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	var conns []*websocket.Conn
	for _, m := range h.members {
		if m.conn != nil {
			conns = append(conns, m.conn)
		}
	}
	for _, r := range h.requests {
		close(r.answer)
	}
	h.requests = map[string]*request{}
	h.mu.Unlock()
	if h.announce != nil {
		h.announce.close()
	}
	// The guests are told, as far as the moment allows.
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			writeMsg(ctx, c, Msg{T: tEnded, Text: "The Jam has ended"})
			c.Close(websocket.StatusNormalClosure, "ended")
		}()
	}
	wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h.srv.Shutdown(ctx)
	h.srv.Close()
}

// Publish is what plays, as it changes: it goes to every guest.
func (h *Host) Publish(st State) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st.Host = h.name
	st.Members = h.state.Members
	h.state = st
	h.wakeAll()
}

// wakeAll has every guest sent the state. The caller holds mu.
func (h *Host) wakeAll() {
	for _, m := range h.members {
		if m.conn != nil {
			select {
			case m.wake <- struct{}{}:
			default:
			}
		}
	}
}

// membersChanged makes the members of the state anew, and tells. The
// caller holds mu.
func (h *Host) membersChanged() {
	list := []Member{{ID: "host", Name: h.name, Server: h.server, Host: true}}
	var full []Member
	for _, m := range h.members {
		mm := m.Member
		mm.Away = m.conn == nil
		full = append(full, mm)
		// The guests are told the device and its owner's name, not the
		// owner's login nor the device's ID.
		if p := mm.Peer; p != nil {
			mm.Peer = &Peer{User: p.User, Device: p.Device}
		}
		list = append(list, mm)
	}
	h.state.Members = list
	h.wakeAll()
	if h.OnMembers != nil {
		go h.OnMembers(full)
	}
}

// Members returns the guests.
func (h *Host) Members() []Member {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.state.Members) == 0 {
		return nil
	}
	return slices.Clone(h.state.Members[1:])
}

// Allow lets in someone who asked; Deny turns them away.
func (h *Host) Allow(id string) { h.answer(id, true) }
func (h *Host) Deny(id string)  { h.answer(id, false) }

func (h *Host) answer(id string, yes bool) {
	h.mu.Lock()
	r := h.requests[id]
	delete(h.requests, id)
	h.mu.Unlock()
	if r != nil {
		r.answer <- yes
	}
}

// Remove takes a guest out of the Jam.
func (h *Host) Remove(id string) {
	h.mu.Lock()
	var m *member
	for i, x := range h.members {
		if x.ID == id {
			m = x
			h.members = slices.Delete(h.members, i, i+1)
			break
		}
	}
	if m != nil {
		h.membersChanged()
	}
	h.mu.Unlock()
	if m != nil && m.conn != nil {
		select {
		case m.out <- Msg{T: tRemoved, Text: "The host took you out of the Jam"}:
		default:
		}
		// The message goes before the connection.
		time.AfterFunc(time.Second, m.stop)
	}
}

// sweep forgets the guests away for too long.
func (h *Host) sweep() {
	for {
		time.Sleep(5 * time.Second)
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return
		}
		changed := false
		h.members = slices.DeleteFunc(h.members, func(m *member) bool {
			if m.conn == nil && time.Since(m.gone) > awayFor {
				changed = true
				return true
			}
			return false
		})
		if changed {
			h.membersChanged()
		}
		h.mu.Unlock()
	}
}

// serveWS is a guest's connection.
func (h *Host) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// One goroutine reads, for as long as the connection lasts: in is
	// closed once it ends.
	in := make(chan Msg, 16)
	go func() {
		defer close(in)
		for {
			msg, err := readMsg(ctx, conn)
			if err != nil {
				return
			}
			select {
			case in <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	var hello Msg
	select {
	case hello = <-in:
	case <-time.After(15 * time.Second):
		return
	}
	hi := hello.Hello
	if hello.T != tHello || hi == nil {
		return
	}
	if hi.Proto != Proto {
		writeMsg(ctx, conn, Msg{T: tDenied, Text: "This Jam needs the same version of Aurelia. Update both to the latest."})
		return
	}
	var peer *Peer
	if h.Identify != nil {
		ictx, icancel := context.WithTimeout(ctx, 3*time.Second)
		peer = h.Identify(ictx, r.RemoteAddr)
		icancel()
	}
	m, why := h.admit(ctx, conn, in, hi, peer)
	if m == nil {
		writeMsg(ctx, conn, Msg{T: tDenied, Text: why})
		conn.Close(websocket.StatusNormalClosure, "denied")
		return
	}
	h.session(ctx, cancel, conn, in, m)
}

// admit decides whether a guest comes in: back with its token, with the
// code, being a device of the host's owner, or as the host lets it. It
// returns why not.
func (h *Host) admit(ctx context.Context, conn *websocket.Conn, in <-chan Msg, hi *Hello, peer *Peer) (*member, string) {
	name := trimName(hi.Name)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, "The Jam has ended"
	}
	if hi.Token != "" {
		for _, m := range h.members {
			if m.token == hi.Token {
				h.mu.Unlock()
				return m, ""
			}
		}
		if hi.Code == "" {
			h.mu.Unlock()
			return nil, "You are no longer in that Jam"
		}
	}
	if hi.Code != "" {
		// Wrong codes are counted, so that guessing takes forever.
		if time.Since(h.wrongAt) > time.Minute {
			h.wrong = 0
		}
		if h.wrong >= 20 {
			h.mu.Unlock()
			return nil, "Too many wrong codes. Wait a minute and try again."
		}
		if NormalizeCode(hi.Code) != h.code {
			h.wrong++
			h.wrongAt = time.Now()
			h.mu.Unlock()
			return nil, "That is not the code of this Jam"
		}
		m := h.addLocked(name, hi.Server, peer)
		h.mu.Unlock()
		return m, ""
	}
	if peer != nil && peer.Mine {
		// The host's own device, as Tailscale vouches.
		m := h.addLocked(name, hi.Server, peer)
		h.mu.Unlock()
		return m, ""
	}
	// No code: the host is asked.
	if len(h.requests) >= 8 {
		h.mu.Unlock()
		return nil, "The host has too many people asking. Try with the code."
	}
	req := &request{Request: Request{ID: token(6), Name: name, Peer: peer}, answer: make(chan bool, 1)}
	h.requests[req.ID] = req
	h.mu.Unlock()
	if h.OnRequest != nil {
		go h.OnRequest(req.Request)
	}
	writeMsg(ctx, conn, Msg{T: tWait})
	// The guest may give up and go, which ends its connection.
	var yes bool
	timer := time.NewTimer(askWaitFor)
	defer timer.Stop()
wait:
	for {
		select {
		case yes = <-req.answer:
			break wait
		case _, ok := <-in:
			if !ok {
				break wait
			}
		case <-timer.C:
			break wait
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.requests, req.ID)
	if !yes || h.closed {
		return nil, "The host did not let you in"
	}
	return h.addLocked(name, hi.Server, peer), ""
}

// addLocked makes a member. The caller holds mu.
func (h *Host) addLocked(name, server string, peer *Peer) *member {
	m := &member{Member: Member{ID: token(6), Name: name, Server: server, Peer: peer}, token: token(16)}
	h.members = append(h.members, m)
	return m
}

// session serves a guest that is in, until its connection ends.
func (h *Host) session(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, in <-chan Msg, m *member) {
	h.mu.Lock()
	if old := m.stop; old != nil && m.conn != nil {
		old() // an older connection of the same guest
	}
	m.conn, m.stop, m.gone = conn, cancel, time.Time{}
	m.out, m.wake = make(chan Msg, 64), make(chan struct{}, 1)
	m.known = map[string]bool{}
	out, wake := m.out, m.wake
	h.membersChanged()
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if m.conn == conn {
			m.conn, m.gone = nil, time.Now()
			if slices.Contains(h.members, m) {
				h.membersChanged()
			}
		}
		h.mu.Unlock()
	}()

	if err := writeMsg(ctx, conn, Msg{T: tWelcome, ID: m.ID, Token: m.token}); err != nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
	// The writer: messages in order, and the state whenever it changed,
	// only the latest of it.
	go func() {
		defer cancel()
		for {
			var msg Msg
			select {
			case <-ctx.Done():
				return
			case msg = <-out:
			case <-wake:
				msg = Msg{T: tState, State: h.stateFor(m)}
			}
			wctx, wcancel := context.WithTimeout(ctx, writeWait)
			err := writeMsg(wctx, conn, msg)
			wcancel()
			if err != nil || msg.T == tRemoved {
				return
			}
		}
	}()
	// A guest that says nothing is gone: it pings every few seconds.
	quiet := time.NewTicker(5 * time.Second)
	defer quiet.Stop()
	seen := time.Now()
	for {
		var msg Msg
		var ok bool
		select {
		case <-ctx.Done():
			return
		case <-quiet.C:
			if time.Since(seen) > silentFor {
				return
			}
			continue
		case msg, ok = <-in:
			if !ok {
				return
			}
		}
		seen = time.Now()
		switch msg.T {
		case tPing:
			select {
			case out <- Msg{T: tPong, T0: msg.T0, T1: time.Now().UnixNano()}:
			default:
			}
		case tAsk:
			if msg.Ask != nil && h.OnAsk != nil {
				h.OnAsk(m.Member, *msg.Ask)
			}
		case tResync:
			h.mu.Lock()
			m.known = map[string]bool{}
			h.mu.Unlock()
			select {
			case wake <- struct{}{}:
			default:
			}
		case tBye:
			h.mu.Lock()
			if i := slices.Index(h.members, m); i >= 0 {
				h.members = slices.Delete(h.members, i, i+1)
				m.conn = nil
				h.membersChanged()
			}
			h.mu.Unlock()
			return
		}
	}
}

// stateFor is the state as m is sent it: with the details of the songs
// it has not been sent yet.
func (h *Host) stateFor(m *member) *State {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.state
	st.Queue = make([]Item, len(h.state.Queue))
	for i, it := range h.state.Queue {
		if m.known[it.Key] {
			it.Song = nil
		} else if it.Song != nil {
			m.known[it.Key] = true
		}
		st.Queue[i] = it
	}
	return &st
}

// guest returns the member a request's token is of, nil for none.
func (h *Host) guest(r *http.Request) *member {
	t := r.URL.Query().Get("t")
	if t == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.members {
		if m.token == t {
			return m
		}
	}
	return nil
}

// allowed reports whether a request is of a guest, or of the host's own
// window.
func (h *Host) allowed(r *http.Request) bool {
	return r.URL.Query().Get("t") == h.local || h.guest(r) != nil
}

func (h *Host) serveMedia(w http.ResponseWriter, r *http.Request) {
	if h.guest(r) == nil || h.Media == nil {
		http.Error(w, "not in the Jam", http.StatusForbidden)
		return
	}
	rc, size, err := h.Media(r.Context(), r.URL.Query().Get("k"))
	serveBody(w, r, rc, size, err)
}

func (h *Host) serveImage(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) || h.Image == nil {
		http.Error(w, "not in the Jam", http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	px, _ := strconv.Atoi(q.Get("px"))
	rc, size, err := h.Image(r.Context(), q.Get("sv"), q.Get("item"), q.Get("tag"), max(1, min(px, 2048)))
	serveBody(w, r, rc, size, err)
}

func (h *Host) serveLyrics(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) || h.Lyrics == nil {
		http.Error(w, "not in the Jam", http.StatusForbidden)
		return
	}
	rc, size, err := h.Lyrics(r.Context(), r.URL.Query().Get("k"))
	serveBody(w, r, rc, size, err)
}

// servePull is the host's own player getting a song of a guest's.
func (h *Host) servePull(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("t") != h.local {
		http.Error(w, "no", http.StatusForbidden)
		return
	}
	key := r.URL.Query().Get("k")
	server, _ := SplitKey(key)
	rc, size, err := h.Fetch(r.Context(), server, Fetch{Kind: KindMedia, Key: key})
	serveBody(w, r, rc, size, err)
}

// serveBody sends what rc reads. A reader waiting for what has yet to
// come is let go of once the one who asked is gone.
func serveBody(w http.ResponseWriter, r *http.Request, rc io.ReadCloser, size int64, err error) {
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, ErrNoOwner) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	stop := context.AfterFunc(r.Context(), func() { rc.Close() })
	defer func() {
		stop()
		rc.Close()
	}()
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	buf := make([]byte, 64<<10)
	io.CopyBuffer(w, rc, buf)
}

// pushed is an upload of a guest's, for a Fetch that waits for it.
type pushed struct {
	body io.Reader
	size int64
	err  error
	done chan struct{} // closed once read: the upload's request ends
}

// Fetch has a guest of server upload what f asks: it returns the upload
// as it comes.
func (h *Host) Fetch(ctx context.Context, server string, f Fetch) (io.ReadCloser, int64, error) {
	h.mu.Lock()
	var m *member
	for _, x := range h.members {
		if x.Server == server && x.conn != nil {
			m = x
			break
		}
	}
	if m == nil {
		h.mu.Unlock()
		return nil, 0, ErrNoOwner
	}
	f.ID = token(8)
	ch := make(chan pushed, 1)
	h.fetches[f.ID] = ch
	out := m.out
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.fetches, f.ID)
		// An upload handed over as this gave up waiting is let go.
		select {
		case p := <-ch:
			close(p.done)
		default:
		}
		h.mu.Unlock()
	}()
	timer := time.NewTimer(fetchWait)
	defer timer.Stop()
	select {
	case out <- Msg{T: tFetch, Fetch: &f}:
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case <-timer.C:
		return nil, 0, errors.New("jam: the guest who has the song does not answer")
	}
	select {
	case p := <-ch:
		if p.err != nil {
			close(p.done)
			return nil, 0, p.err
		}
		return &pushBody{p: p}, p.size, nil
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case <-timer.C:
		return nil, 0, errors.New("jam: the guest who has the song does not answer")
	}
}

type pushBody struct {
	p    pushed
	once sync.Once
}

func (b *pushBody) Read(p []byte) (int, error) { return b.p.body.Read(p) }
func (b *pushBody) Close() error {
	b.once.Do(func() { close(b.p.done) })
	return nil
}

// servePush is a guest's upload of what it was asked.
func (h *Host) servePush(w http.ResponseWriter, r *http.Request) {
	if h.guest(r) == nil {
		http.Error(w, "not in the Jam", http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	p := pushed{body: r.Body, size: -1, done: make(chan struct{})}
	if s, err := strconv.ParseInt(r.Header.Get("X-Jam-Size"), 10, 64); err == nil && s > 0 {
		p.size = s
	}
	if e := q.Get("err"); e != "" {
		p.err = errors.New(e)
	}
	// Handed over under the lock, so that a Fetch that gives up meanwhile
	// takes it back.
	h.mu.Lock()
	ch := h.fetches[q.Get("id")]
	delete(h.fetches, q.Get("id"))
	if ch != nil {
		ch <- p
	}
	h.mu.Unlock()
	if ch == nil {
		http.Error(w, "not asked", http.StatusNotFound)
		return
	}
	select {
	case <-p.done:
	case <-r.Context().Done():
	}
	w.WriteHeader(http.StatusNoContent)
}

// trimName keeps a name short and printable.
func trimName(s string) string {
	var out []rune
	for _, r := range s {
		if r >= ' ' && r != 0x7f {
			out = append(out, r)
		}
		if len(out) >= 40 {
			break
		}
	}
	if len(out) == 0 {
		return "Someone"
	}
	return string(out)
}

func readMsg(ctx context.Context, c *websocket.Conn) (Msg, error) {
	var m Msg
	_, data, err := c.Read(ctx)
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(data, &m)
	return m, err
}

func writeMsg(ctx context.Context, c *websocket.Conn, m Msg) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, data)
}

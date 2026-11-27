package jam

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Guest is this computer in another's Jam.
type Guest struct {
	Handlers

	addr  string
	hello Hello
	id    string
	token atomic.Pointer[string]

	ctx    context.Context
	cancel context.CancelFunc
	out    chan Msg
	// offset is the host's clock less this one's, in nanoseconds, by
	// the answer of the ping that came back the fastest lately.
	offset  atomic.Int64
	clockMu sync.Mutex
	samples []clockSample

	left atomic.Bool
}

// Handlers are what a guest is told, on its own goroutines; OnState one
// at a time.
type Handlers struct {
	// Waiting is told that the host is asked to let the guest in.
	Waiting func()
	// OnState is the Jam as it changes.
	OnState func(st *State)
	// OnEnd is the end of the Jam for this guest: why, as the host said
	// or as the connection ended. Leave does not call it.
	OnEnd func(why string)
	// OnAway tells that the connection was lost and is being made again,
	// and then that it is back.
	OnAway func(away bool)
	// OnFetch opens what the host asks of this guest: a song, a picture
	// or lyrics of its server's, with its size, -1 when not known.
	OnFetch func(ctx context.Context, f Fetch) (io.ReadCloser, int64, error)
}

type clockSample struct {
	offset, rtt time.Duration
}

// client is for the requests of a Jam: never through a proxy, since the
// host is on the network.
var client = &http.Client{Transport: &http.Transport{
	Proxy:               nil,
	DialContext:         dialJam,
	MaxIdleConnsPerHost: 8,
	IdleConnTimeout:     90 * time.Second,
}}

// Client is the HTTP client of the Jam's requests, for those made
// outside the package, as the player's downloads.
func Client() *http.Client { return client }

// ErrDenied is the error of a Join the host turned away; its text is why.
type ErrDenied struct{ Why string }

func (e *ErrDenied) Error() string { return e.Why }

// Join joins the host at addr. When the host is asked to let this guest
// in, Join waits for it until ctx ends.
func Join(ctx context.Context, addr string, hello Hello, hs Handlers) (*Guest, error) {
	hello.Proto = Proto
	if IsRelayAddr(addr) {
		// The host's fingerprint, which the tunnels check its key by.
		var err error
		if addr, err = pinned(ctx, addr); err != nil {
			return nil, err
		}
	}
	g := &Guest{Handlers: hs, addr: addr, hello: hello, out: make(chan Msg, 64)}
	conn, err := g.dial(ctx, hello, hs.Waiting)
	if err != nil {
		return nil, err
	}
	g.ctx, g.cancel = context.WithCancel(context.Background())
	go g.run(conn)
	return g, nil
}

// dial connects and says hello, until the host lets the guest in.
func (g *Guest) dial(ctx context.Context, hello Hello, waiting func()) (*websocket.Conn, error) {
	dctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	conn, _, err := websocket.Dial(dctx, wsBase(g.addr)+"/ws", &websocket.DialOptions{HTTPClient: client})
	cancel()
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(32 << 20)
	if err := writeMsg(ctx, conn, Msg{T: tHello, Hello: &hello}); err != nil {
		conn.CloseNow()
		return nil, err
	}
	for {
		rctx, cancel := context.WithTimeout(ctx, askWaitFor+30*time.Second)
		msg, err := readMsg(rctx, conn)
		cancel()
		if err != nil {
			conn.CloseNow()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		switch msg.T {
		case tWait:
			if waiting != nil {
				waiting()
			}
		case tWelcome:
			g.id = msg.ID
			t := msg.Token
			g.token.Store(&t)
			return conn, nil
		case tDenied, tEnded, tRemoved:
			conn.CloseNow()
			return nil, &ErrDenied{Why: msg.Text}
		}
	}
}

// ID is this guest's in the Jam.
func (g *Guest) ID() string { return g.id }

// Addr is the host's address.
func (g *Guest) Addr() string { return g.addr }

func (g *Guest) tok() string {
	if t := g.token.Load(); t != nil {
		return *t
	}
	return ""
}

// run keeps the guest in the Jam: when the connection is lost it is made
// again, for a while, with the token, which the host knows it by.
func (g *Guest) run(conn *websocket.Conn) {
	for {
		why, ended := g.session(conn)
		if g.left.Load() {
			return
		}
		if ended {
			g.end(why)
			return
		}
		if g.OnAway != nil {
			g.OnAway(true)
		}
		conn = nil
		began := time.Now()
		wait := 500 * time.Millisecond
		for conn == nil && time.Since(began) < awayFor-10*time.Second {
			select {
			case <-g.ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = min(2*wait, 5*time.Second)
			hello := g.hello
			hello.Code, hello.Token = "", g.tok()
			c, err := g.dial(g.ctx, hello, nil)
			var denied *ErrDenied
			switch {
			case errors.As(err, &denied):
				g.end(denied.Why)
				return
			case err == nil:
				conn = c
			}
		}
		if conn == nil {
			g.end("The host can no longer be reached")
			return
		}
		if g.OnAway != nil {
			g.OnAway(false)
		}
	}
}

func (g *Guest) end(why string) {
	if !g.left.Swap(true) {
		g.cancel()
		if g.OnEnd != nil {
			g.OnEnd(why)
		}
	}
}

// session is one connection: it returns why it ended, and whether the
// Jam did, or only the connection.
func (g *Guest) session(conn *websocket.Conn) (why string, ended bool) {
	ctx, cancel := context.WithCancel(g.ctx)
	defer cancel()
	defer conn.CloseNow()
	// The writer, and the pings that keep the clocks together and tell
	// the host the guest is there.
	go func() {
		defer cancel()
		ping := time.NewTimer(0)
		defer ping.Stop()
		pings := 0
		for {
			var msg Msg
			select {
			case <-ctx.Done():
				return
			case msg = <-g.out:
			case <-ping.C:
				// A few quickly, to set the clock, then every few seconds:
				// of a minute of them, the quickest says it best, as one
				// that waited behind a song on a slow link does not.
				pings++
				next := 2 * time.Second
				if pings < 6 {
					next = 250 * time.Millisecond
				}
				ping.Reset(next)
				msg = Msg{T: tPing, T0: time.Now().UnixNano()}
			}
			wctx, wcancel := context.WithTimeout(ctx, writeWait)
			err := writeMsg(wctx, conn, msg)
			wcancel()
			if err != nil || msg.T == tBye {
				return
			}
		}
	}()
	for {
		// The host answers every ping: silence is a lost connection.
		rctx, rcancel := context.WithTimeout(ctx, 25*time.Second)
		msg, err := readMsg(rctx, conn)
		rcancel()
		if err != nil {
			return "", false
		}
		switch msg.T {
		case tState:
			if msg.State != nil && g.OnState != nil {
				g.OnState(msg.State)
			}
		case tPong:
			g.clock(msg.T0, msg.T1, time.Now().UnixNano())
		case tFetch:
			if msg.Fetch != nil {
				go g.serve(*msg.Fetch)
			}
		case tEnded, tRemoved:
			return msg.Text, true
		}
	}
}

// clock takes a ping's answer: sent at t0, answered at t1 by the host's
// clock, back at t3.
func (g *Guest) clock(t0, t1, t3 int64) {
	if t0 == 0 || t1 == 0 || t3 < t0 {
		return
	}
	s := clockSample{rtt: time.Duration(t3 - t0), offset: time.Duration(t1 - (t0+t3)/2)}
	g.clockMu.Lock()
	defer g.clockMu.Unlock()
	g.samples = append(g.samples, s)
	if len(g.samples) > 30 {
		g.samples = g.samples[1:]
	}
	best := g.samples[0]
	for _, x := range g.samples[1:] {
		if x.rtt < best.rtt {
			best = x
		}
	}
	g.offset.Store(int64(best.offset))
}

// HostNow is the time by the host's clock.
func (g *Guest) HostNow() time.Time {
	return time.Now().Add(time.Duration(g.offset.Load()))
}

// Ask asks the host for something.
func (g *Guest) Ask(a Ask) {
	select {
	case g.out <- Msg{T: tAsk, Ask: &a}:
	default: // the connection is stuck: the next state will tell
	}
}

// Resync asks for the details of every song again.
func (g *Guest) Resync() {
	select {
	case g.out <- Msg{T: tResync}:
	default:
	}
}

// Leave leaves the Jam.
func (g *Guest) Leave() {
	if g.left.Swap(true) {
		return
	}
	select {
	case g.out <- Msg{T: tBye}:
		// The writer sends it, and the session ends with it.
		time.AfterFunc(time.Second, g.cancel)
	default:
		g.cancel()
	}
}

// MediaURL is where a song of the queue is, for this guest to play.
func (g *Guest) MediaURL(key string) string {
	return httpBase(g.addr) + "/media?k=" + escape(key) + "&t=" + g.tok()
}

// ImageURL is where a picture of a song of the queue is.
func (g *Guest) ImageURL(server, item, tag string, px int) string {
	return httpBase(g.addr) + "/image?sv=" + escape(server) + "&item=" + escape(item) + "&tag=" + escape(tag) + "&px=" + strconv.Itoa(px) + "&t=" + g.tok()
}

// LyricsURL is where the lyrics of a song of the queue are, as JSON.
func (g *Guest) LyricsURL(key string) string {
	return httpBase(g.addr) + "/lyrics?k=" + escape(key) + "&t=" + g.tok()
}

// serve uploads what the host asked.
func (g *Guest) serve(f Fetch) {
	ctx, cancel := context.WithTimeout(g.ctx, 30*time.Minute)
	defer cancel()
	u := httpBase(g.addr) + "/push?id=" + escape(f.ID) + "&t=" + g.tok()
	var body io.ReadCloser
	size := int64(-1)
	err := errors.New("this guest cannot share that")
	if g.OnFetch != nil {
		body, size, err = g.OnFetch(ctx, f)
	}
	if err != nil {
		u += "&err=" + escape(err.Error())
		body, size = nil, -1
	}
	var rd io.Reader = http.NoBody
	if body != nil {
		defer body.Close()
		rd = body
	}
	req, rerr := http.NewRequestWithContext(ctx, "POST", u, rd)
	if rerr != nil {
		return
	}
	if size > 0 {
		req.ContentLength = size
		req.Header.Set("X-Jam-Size", strconv.FormatInt(size, 10))
	}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
}

func escape(s string) string { return url.QueryEscape(s) }

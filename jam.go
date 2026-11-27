package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/audio"
	"aurelia/internal/jam"
	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
)

// A Jam (in beta, when the settings turn it on) is listening together
// with others on the same network: the host's queue is everyone's, and
// each plays it on their own computer, with their own sound. The
// networking is in internal/jam; this is what the app does with it.
//
// The host's player is the Jam's: it plays as ever, and what it plays is
// published to the guests every time it changes. A guest's player
// mirrors it, and does not change of itself: what its user asks of it,
// to add a song, pause or skip, is asked of the host, whose answer is
// the next state. The guest plays the host's song on its own engine, and
// is put back in step when it is more than a moment off.
//
// Songs are known across servers by their key (jam.Key). A song of a
// server this app is not signed in to, or which its library lacks, is
// "foreign": it plays from the host, which the guest that added it
// uploads it to, and its picture and lyrics come the same way.

// jamState is the app's part of a Jam.
type jamState struct {
	host  *jam.Host
	guest *jam.Guest

	// keys are the foreign songs, which play through the Jam, by their
	// key; songs are those songs by key, and resolved every song a guest
	// was told of, by key.
	keys     map[*library.Song]string
	songs    map[string]*library.Song
	resolved map[string]*library.Song
	// images is the server of each picture of a foreign song, by item.
	images map[string]string
	// by is who added a song, as the queue shows it.
	by      map[*library.Song]string
	members []jam.Member

	// share is what the Jam's goroutines may serve of this app's.
	share *jamShare

	// The host's: whether guests control what plays, what was last
	// published and when, and who asks to come in.
	pubKey   string
	pubBase  int // where the queue published begins in the player's
	pubPos   time.Duration
	pubAt    time.Time
	pubPlay  bool
	requests []jam.Request
	wire     map[*library.Song]*jam.Song

	// The guest's: the session its callbacks are of, the last state, and
	// the queue it had before, which comes back as it leaves.
	sess    *guestSession
	state   *jam.State
	away    bool
	saved   *savedPlayer
	settled time.Time // the song was started or moved: let be a while
	// catchUp is how late a song started or moved here is heard: the
	// time to have it from where it comes, the host over a slow link
	// most of all. It is learned from how far behind each lands, and
	// started that much further on.
	catchUp  time.Duration
	hostName string

	// The dialog.
	open      bool
	input     string
	joining   bool
	waiting   bool
	joinErr   string
	cancel    context.CancelFunc
	nearby    []jam.Found
	looking   context.CancelFunc
	lookingAt time.Time
}

// guestSession is one stay in a Jam, which its callbacks check they
// are still of; wire is the details of the songs the host sent, gathered
// on the network's goroutine.
type guestSession struct {
	mu     sync.Mutex
	wire   map[string]*jam.Song
	latest *jam.State
}

// savedPlayer is the queue a guest had before it joined.
type savedPlayer struct {
	queue, unshuffled       []*library.Song
	index, added, playNexts int
	at                      time.Duration
}

// jamShare is what the Jam's goroutines serve: the songs that may be
// asked for, by key, and how to open them, and the pictures by item.
type jamShare struct {
	mu     sync.Mutex
	media  map[string]func() (*audio.File, error)
	images map[string]string // item -> its server
	client *jellyfin.Client
	server string
}

func newJamShare(c *jellyfin.Client, server string) *jamShare {
	return &jamShare{media: map[string]func() (*audio.File, error){}, images: map[string]string{}, client: c, server: server}
}

// jamOn is set while a Jam is on, for the clock that keeps it going,
// off the interface's goroutine.
var jamOn atomic.Bool

// in reports whether a Jam is on, either way.
func (j *jamState) in() bool { return j.host != nil || j.guest != nil }

// jamGuest reports whether this app plays another's Jam.
func (a *App) jamGuest() bool { return a.jam.guest != nil }

// jamControls reports whether this app may play, pause and change the
// queue: always, but as a guest of a Jam that does not let it.
func (a *App) jamControls() bool {
	return a.jam.guest == nil || a.jam.state == nil || a.jam.state.Control
}

// serverID is the ID of the server signed in to, "" for none.
func (a *App) serverID() string {
	if s := a.settings.Session; s != nil {
		return s.ServerID
	}
	return ""
}

// jamName is the name this app goes by in a Jam.
func (a *App) jamName() string {
	if s := a.settings.Session; s != nil && s.UserName != "" {
		return s.UserName
	}
	return deviceName()
}

// songKey is a song's key.
func (a *App) songKey(s *library.Song) string {
	if k, ok := a.jam.keys[s]; ok {
		return k
	}
	return jam.Key(a.serverID(), s.ID)
}

// foreign reports whether a song plays through the Jam.
func (a *App) foreign(s *library.Song) bool {
	_, ok := a.jam.keys[s]
	return ok
}

// wireSong is a song as it goes to the others.
func (a *App) wireSong(s *library.Song) *jam.Song {
	if w := a.jam.wire[s]; w != nil {
		return w
	}
	server, _ := jam.SplitKey(a.songKey(s))
	w := &jam.Song{Server: server, Song: *s}
	if a.jam.wire != nil {
		a.jam.wire[s] = w
	}
	return w
}

// jamSong is the song of what came over the network: the library's own
// when it has it, else one made of it, which plays through the Jam.
func (a *App) jamSong(w *jam.Song, key string) *library.Song {
	j := &a.jam
	if s := j.resolved[key]; s != nil {
		return s
	}
	var s *library.Song
	if w.Server != "" && w.Server == a.serverID() {
		s = a.lib.Song(w.ID)
	}
	if s == nil {
		sg := w.Song
		// Its album and artists are of another library, as are its plays.
		sg.AlbumID, sg.ArtistIDs, sg.AlbumArtistIDs = "", nil, nil
		sg.Favorite, sg.Plays, sg.LastPlayed, sg.Entry = false, 0, 0, ""
		library.PrepareSong(&sg)
		s = &sg
		j.keys[s] = key
		j.songs[key] = s
		if s.ImageItem != "" {
			j.images[s.ImageItem] = w.Server
			if j.host != nil {
				// The host's window shows it too, through the host.
				j.share.mu.Lock()
				j.share.images[s.ImageItem] = w.Server
				j.share.mu.Unlock()
			}
		}
	}
	j.resolved[key] = s
	return s
}

// resetJam forgets the Jam's songs and people.
func (a *App) resetJam() {
	j := &a.jam
	j.keys, j.songs, j.resolved = map[*library.Song]string{}, map[string]*library.Song{}, map[string]*library.Song{}
	j.images = map[string]string{}
	j.by, j.wire = map[*library.Song]string{}, map[*library.Song]*jam.Song{}
	j.members, j.requests, j.state, j.away, j.pubKey = nil, nil, nil, false, ""
	j.catchUp = 0
	j.share = newJamShare(a.clientNow(), a.serverID())
}

// startJam hosts a Jam.
func (a *App) startJam() {
	j := &a.jam
	if j.in() || a.settings.Session == nil {
		return
	}
	if a.player.far != nil {
		a.toast("Play on this computer to start a Jam")
		return
	}
	a.stopLooking()
	a.resetJam()
	h, err := jam.NewHost(a.jamName(), a.serverID(), !a.settings.JamHidden)
	if err != nil {
		a.toastError("Could not start a Jam", err)
		return
	}
	share := j.share
	h.OnAsk = func(m jam.Member, ask jam.Ask) { a.update(func() { a.jamAsked(h, m, ask) }) }
	h.OnRequest = func(r jam.Request) { a.update(func() { a.jamRequest(h, r) }) }
	h.OnMembers = func(ms []jam.Member) { a.update(func() { a.jamMembers(h, ms) }) }
	h.Media = func(ctx context.Context, key string) (io.ReadCloser, int64, error) {
		return share.openMedia(key)
	}
	h.Image = func(ctx context.Context, server, item, tag string, px int) (io.ReadCloser, int64, error) {
		if server != share.server {
			return h.Fetch(ctx, server, jam.Fetch{Kind: jam.KindImage, Item: item, Tag: tag, Px: px})
		}
		return share.openImage(ctx, item, tag, px)
	}
	h.Lyrics = func(ctx context.Context, key string) (io.ReadCloser, int64, error) {
		server, _ := jam.SplitKey(key)
		if server != share.server {
			return h.Fetch(ctx, server, jam.Fetch{Kind: jam.KindLyrics, Key: key})
		}
		return share.openLyrics(ctx, key)
	}
	if a.tail.api != nil {
		// Who comes over the tailnet is who Tailscale says.
		h.Identify = a.tailIdentify
	}
	h.Start()
	j.host = h
	j.members = nil
	jamOn.Store(true)
	a.jamPublish()
	a.tailHosted()
}

// endJam ends the Jam this app holds: the guests are told, and the songs
// of theirs leave the queue, having nowhere to play from.
func (a *App) endJam() {
	j := &a.jam
	h := j.host
	if h == nil {
		return
	}
	j.host = nil
	jamOn.Store(false)
	a.tailHosted()
	clear(a.tail.sent)
	go h.Close()
	for _, r := range j.requests {
		dismissToast("jam-ask-" + r.ID)
	}
	a.dropForeign()
	a.resetJam()
	a.toast("The Jam has ended")
}

// dropForeign takes the songs of other servers out of the queue.
func (a *App) dropForeign() {
	p := a.player
	if len(a.jam.keys) == 0 {
		return
	}
	cur := p.current()
	if cur != nil && a.foreign(cur) {
		if p.engine != nil {
			p.engine.Stop()
		}
		p.track, p.next, p.nextIndex, p.finished = nil, nil, -1, true
	}
	var kept []*library.Song
	index, added, playNexts := -1, 0, 0
	lo, hi := p.addedRun()
	for i, s := range p.queue {
		switch {
		case i == p.index && !a.foreign(s):
			index = len(kept)
		case i == p.index && index < 0:
			// The song playing goes: the place is that of the song after.
			index = len(kept) - 1
		case a.foreign(s):
			continue
		case i >= lo && i < hi:
			added++
			if i < lo+p.playNexts {
				playNexts++
			}
		}
		kept = append(kept, s)
	}
	p.queue = kept
	p.index = index
	p.added, p.playNexts = added, playNexts
	if p.unshuffled != nil {
		p.unshuffled = slices.DeleteFunc(p.unshuffled, a.foreign)
	}
	if p.index < 0 && len(p.queue) > 0 {
		p.index = 0
	}
	p.next, p.nextIndex = nil, -1
	p.armNext()
	a.saveQueue()
}

// joinJam joins a Jam: at addr, or the one of code found on the network
// when addr is "".
func (a *App) joinJam(addr, code string) {
	j := &a.jam
	if j.in() || j.joining {
		return
	}
	a.stopLooking()
	ctx, cancel := context.WithCancel(context.Background())
	j.joining, j.waiting, j.joinErr, j.cancel = true, false, "", cancel
	sess := &guestSession{wire: map[string]*jam.Song{}}
	client, server := a.clientNow(), a.serverID()
	share := newJamShare(client, server)
	hello := jam.Hello{Name: a.jamName(), Server: server, Code: code}
	var g *jam.Guest
	hs := jam.Handlers{
		Waiting: func() { a.update(func() { j.waiting = true }) },
		OnState: func(st *jam.State) {
			sess.mu.Lock()
			for _, it := range st.Queue {
				if it.Song != nil {
					sess.wire[it.Key] = it.Song
				}
			}
			sess.latest = st
			sess.mu.Unlock()
			a.update(func() {
				if j.sess == sess {
					a.jamTakeState()
				}
			})
		},
		OnEnd: func(why string) {
			a.update(func() {
				if j.sess == sess {
					a.leaveJam(false)
					if why == "" {
						why = "The Jam has ended"
					}
					a.toast(why)
				}
			})
		},
		OnAway: func(away bool) {
			a.update(func() {
				if j.sess == sess {
					j.away = away
				}
			})
		},
		OnFetch: func(ctx context.Context, f jam.Fetch) (io.ReadCloser, int64, error) {
			switch f.Kind {
			case jam.KindMedia:
				return share.openMedia(f.Key)
			case jam.KindImage:
				share.mu.Lock()
				ok := share.images[f.Item] == share.server
				share.mu.Unlock()
				if !ok {
					return nil, 0, errors.New("not a picture of the Jam")
				}
				return share.openImage(ctx, f.Item, f.Tag, f.Px)
			case jam.KindLyrics:
				return share.openLyrics(ctx, f.Key)
			}
			return nil, 0, errors.New("asked for nothing known")
		},
	}
	go func() {
		defer cancel()
		found := addr
		if found == "" {
			// The host of the code, on the networks of this computer.
			fctx, fcancel := context.WithTimeout(ctx, 5*time.Second)
			jam.Discover(fctx, code, func(f jam.Found) {
				if found == "" && f.Proto == jam.Proto {
					found = f.Addr
					fcancel()
				}
			})
			fcancel()
		}
		var err error
		if found == "" {
			err = errors.New("No Jam with that code was found on this network. Ask the host for their invite, which has their address.")
		} else {
			g, err = jam.Join(ctx, found, hello, hs)
		}
		a.update(func() {
			if j.cancel == nil && err == nil {
				g.Leave() // cancelled meanwhile
				return
			}
			j.joining, j.waiting, j.cancel = false, false, nil
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				var denied *jam.ErrDenied
				switch {
				case errors.As(err, &denied):
					j.joinErr = denied.Why
				case addr == "":
					j.joinErr = err.Error()
				case tailAddr(found):
					j.joinErr = "Could not reach the Jam at " + found + " over Tailscale. If one of your computers is shared with the other on Tailscale, it connects only one way: the one that can be reached hosts. Otherwise, is Aurelia open there?"
				default:
					j.joinErr = "Could not reach the Jam at " + found + ". Is it on this network, and does its firewall let Aurelia in?"
				}
				return
			}
			a.startGuest(g, sess, share)
		})
	}()
}

// cancelJoin gives up joining.
func (a *App) cancelJoin() {
	j := &a.jam
	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
	}
	j.joining, j.waiting = false, false
}

// startGuest makes this app a guest of g: its own queue waits for it to
// leave, and the Jam's takes its place.
func (a *App) startGuest(g *jam.Guest, sess *guestSession, share *jamShare) {
	j := &a.jam
	p := a.player
	if p.far != nil {
		a.playHere(false)
	}
	a.resetJam()
	j.share = share
	j.guest, j.sess = g, sess
	jamOn.Store(true)
	st := p.state()
	j.saved = &savedPlayer{queue: p.queue, unshuffled: p.unshuffled, index: p.index, added: p.added, playNexts: p.playNexts, at: st.Position}
	p.reportStop()
	if p.engine != nil {
		p.engine.Stop()
	}
	p.queue, p.unshuffled, p.index, p.added, p.playNexts = nil, nil, -1, 0, 0
	p.track, p.next, p.nextIndex = nil, nil, -1
	p.finished, p.paused, p.cold = false, false, false
	j.open = false
	a.jamTakeState()
}

// leaveJam leaves the Jam this app is a guest of: the queue it had before
// comes back, paused. tell tells the host.
func (a *App) leaveJam(tell bool) {
	j := &a.jam
	g := j.guest
	if g == nil {
		return
	}
	if tell {
		g.Leave()
	}
	j.guest, j.sess = nil, nil
	jamOn.Store(false)
	p := a.player
	p.reportStop()
	if p.engine != nil {
		p.engine.Stop()
	}
	p.track, p.next, p.nextIndex = nil, nil, -1
	p.queue, p.unshuffled, p.index, p.added, p.playNexts = nil, nil, -1, 0, 0
	p.finished, p.paused, p.cold = false, false, false
	if s := j.saved; s != nil && len(s.queue) > 0 {
		p.queue, p.unshuffled, p.index, p.added, p.playNexts = s.queue, s.unshuffled, s.index, s.added, s.playNexts
		if p.current() != nil {
			p.cold, p.paused, p.resumeAt = true, true, s.at
		}
	}
	j.saved = nil
	a.resetJam()
}

// stopJam ends or leaves the Jam, whichever it is.
func (a *App) stopJam() {
	switch {
	case a.jam.host != nil:
		a.endJam()
	case a.jam.guest != nil:
		a.leaveJam(true)
	}
	a.cancelJoin()
	a.stopLooking()
}

// jamTick is what a Jam does every so often: the host publishes what
// changed, a guest keeps in step.
func (a *App) jamTick() {
	switch {
	case a.jam.host != nil:
		a.jamPublish()
	case a.jam.guest != nil && a.jam.state != nil:
		a.jamFollow(a.player.current(), false)
	}
}

// jamPublish tells the guests what plays, when it changed: the queue,
// the song, or its place otherwise than time moves it.
func (a *App) jamPublish() {
	j := &a.jam
	h := j.host
	if h == nil {
		return
	}
	p := a.player
	st := p.state()
	playing := p.playing() && !st.Buffering && !p.cold
	lo, hi := 0, min(len(p.queue), 200)
	if p.index >= 0 {
		lo, hi = max(0, p.index-20), min(len(p.queue), p.index+200)
	}
	var key strings.Builder
	for _, s := range p.queue[lo:hi] {
		key.WriteString(a.songKey(s))
		key.WriteString(j.by[s])
		key.WriteByte(',')
	}
	key.WriteString(strings.Join([]string{
		strconv.Itoa(p.index - lo), strconv.Itoa(p.added), strconv.Itoa(p.playNexts), boolText(playing), boolText(a.settings.Shuffle),
		strconv.Itoa(a.settings.Repeat), boolText(!a.settings.JamLocked), strconv.Itoa(p.starts),
	}, ","))
	expected := j.pubPos
	if j.pubPlay {
		expected += time.Since(j.pubAt)
	}
	// A moment lost loading, or a jump, is told at once: guests follow
	// it within a fraction of a second.
	moved := st.Position-expected > jamDrift || expected-st.Position > jamDrift
	if key.String() == j.pubKey && !moved && time.Since(j.pubAt) < 10*time.Second {
		return
	}
	changed := key.String() != j.pubKey
	j.pubKey, j.pubBase, j.pubPos, j.pubAt, j.pubPlay = key.String(), lo, st.Position, time.Now(), playing
	out := jam.State{
		Index: -1, Playing: playing, Position: st.Position.Milliseconds(), At: j.pubAt.UnixMilli(),
		Shuffle: a.settings.Shuffle, Repeat: a.settings.Repeat, Control: !a.settings.JamLocked,
	}
	if p.index >= 0 {
		out.Index = p.index - lo
		out.Added = max(0, min(p.added, hi-p.index-1))
		out.PlayNexts = min(p.playNexts, out.Added)
	}
	if changed {
		// What guests may ask for: the songs published, and their pictures.
		j.share.mu.Lock()
		if len(j.share.media) > 2000 {
			j.share.media, j.share.images = map[string]func() (*audio.File, error){}, map[string]string{}
		}
		for _, s := range p.queue[lo:hi] {
			k := a.songKey(s)
			if _, ok := j.share.media[k]; !ok {
				j.share.media[k] = p.trackOf(s).Open
			}
			server, _ := jam.SplitKey(k)
			j.share.images[s.ImageItem] = server
		}
		j.share.mu.Unlock()
		// The details of songs that left are let go.
		inQueue := make(map[*library.Song]bool, hi-lo)
		for _, s := range p.queue[lo:hi] {
			inQueue[s] = true
		}
		for s := range j.wire {
			if !inQueue[s] {
				delete(j.wire, s)
			}
		}
	}
	out.Queue = make([]jam.Item, 0, hi-lo)
	for _, s := range p.queue[lo:hi] {
		out.Queue = append(out.Queue, jam.Item{Key: a.songKey(s), Song: a.wireSong(s), By: j.by[s]})
	}
	h.Publish(out)
}

func boolText(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// locate returns where in the queue the song a guest meant is: at the
// place of the state it had, or the nearest of the same key.
func (a *App) locate(at int, key string) int {
	p := a.player
	if i := a.jam.pubBase + at; at >= 0 && i < len(p.queue) && a.songKey(p.queue[i]) == key {
		return i
	}
	best := -1
	for i, s := range p.queue {
		if a.songKey(s) == key && (best < 0 || abs(i-a.jam.pubBase-at) < abs(best-a.jam.pubBase-at)) {
			best = i
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// jamAsked does what a guest asked.
func (a *App) jamAsked(h *jam.Host, m jam.Member, ask jam.Ask) {
	j := &a.jam
	if j.host != h {
		return
	}
	p := a.player
	at := func(n int) int {
		if n >= len(ask.At) || n >= len(ask.Keys) {
			return -1
		}
		return a.locate(ask.At[n], ask.Keys[n])
	}
	switch ask.Op {
	case jam.OpAdd, jam.OpNext:
		var songs []*library.Song
		for i := range ask.Songs[:min(len(ask.Songs), 100)] {
			w := &ask.Songs[i]
			if w.Server == "" || w.ID == "" {
				continue
			}
			s := a.jamSong(w, jam.Key(w.Server, w.ID))
			j.by[s] = m.Name
			songs = append(songs, s)
		}
		if len(songs) == 0 {
			return
		}
		if ask.Op == jam.OpNext {
			p.playNext(songs)
		} else {
			p.enqueue(songs)
		}
		what := songs[0].Name
		if len(songs) > 1 {
			what = count(len(songs), "song", "songs")
		}
		toasts = append(toasts, ui.Toast{ID: "jam-added", Title: m.Name + " added " + what})
	default:
		if a.settings.JamLocked {
			return
		}
		switch ask.Op {
		case jam.OpPlay:
			if i := at(0); i >= 0 {
				p.failures = 0
				p.playIndex(i)
			}
		case jam.OpRemove:
			if i := at(0); i >= 0 {
				p.remove(i)
			}
		case jam.OpMove:
			var from []int
			for n := range ask.At {
				if i := at(n); i >= 0 && i != p.index {
					from = append(from, i)
				}
			}
			to := len(p.queue)
			if k := len(ask.At); k < len(ask.Keys) {
				if i := a.locate(ask.To, ask.Keys[k]); i >= 0 {
					to = i
				}
			}
			if len(from) > 0 {
				p.move(from, to)
			}
		case jam.OpClear:
			p.clearUpcoming()
		case jam.OpPause:
			if p.playing() {
				p.toggle()
			}
		case jam.OpResume:
			if !p.playing() {
				p.toggle()
			}
		case jam.OpSkip:
			p.skip()
		case jam.OpPrevious:
			p.previous()
		case jam.OpSeek:
			p.seek(time.Duration(ask.Position) * time.Millisecond)
		case jam.OpShuffle:
			p.setShuffle(ask.On)
		case jam.OpRepeat:
			p.setRepeat(max(0, min(ask.Mode, repeatOne)))
		}
	}
	a.jamPublish()
}

// jamRequest is someone without the code who would join.
func (a *App) jamRequest(h *jam.Host, r jam.Request) {
	j := &a.jam
	if j.host != h {
		return
	}
	j.requests = append(j.requests, r)
	title := r.Name + " would like to join your Jam"
	if r.Peer != nil {
		// As Tailscale names them, not as they name themselves.
		title = r.Peer.Who() + " wants to join"
	}
	toasts = append(toasts, ui.Toast{
		ID: "jam-ask-" + r.ID, Title: title, Action: "Let in", Timeout: 2 * time.Minute,
		OnAction: func() { a.answerJam(r.ID, true) },
	})
	if a.win != nil {
		a.win.Invalidate()
	}
}

// answerJam lets in, or turns away, someone who asked.
func (a *App) answerJam(id string, yes bool) {
	j := &a.jam
	if j.host == nil {
		return
	}
	j.requests = slices.DeleteFunc(j.requests, func(r jam.Request) bool { return r.ID == id })
	dismissToast("jam-ask-" + id)
	if yes {
		j.host.Allow(id)
	} else {
		j.host.Deny(id)
	}
}

// jamMembers is who is in the host's Jam, as it changes.
func (a *App) jamMembers(h *jam.Host, ms []jam.Member) {
	j := &a.jam
	if j.host != h {
		return
	}
	was := map[string]jam.Member{}
	for _, m := range j.members {
		was[m.ID] = m
	}
	now := map[string]bool{}
	for _, m := range ms {
		now[m.ID] = true
		if _, ok := was[m.ID]; !ok {
			if m.Peer != nil && m.Peer.Device != "" {
				a.toast(m.Name + " joined your Jam from " + m.Peer.Device)
			} else {
				a.toast(m.Name + " joined your Jam")
			}
		}
	}
	for _, m := range j.members {
		if !now[m.ID] {
			a.toast(m.Name + " left your Jam")
		}
	}
	j.members = ms
	a.tailAbout()
	// The songs of whoever left go: nobody has them any more.
	servers := map[string]bool{a.serverID(): true}
	for _, m := range ms {
		servers[m.Server] = true
	}
	p := a.player
	for i := len(p.queue) - 1; i > p.index; i-- {
		if k, ok := j.keys[p.queue[i]]; ok {
			if server, _ := jam.SplitKey(k); !servers[server] {
				p.remove(i)
			}
		}
	}
}

// jamTakeState applies the latest state the host sent.
func (a *App) jamTakeState() {
	j := &a.jam
	sess := j.sess
	if j.guest == nil || sess == nil {
		return
	}
	sess.mu.Lock()
	st := sess.latest
	sess.latest = nil
	sess.mu.Unlock()
	if st == nil {
		return
	}
	p := a.player
	old := p.current()
	songs := make([]*library.Song, 0, len(st.Queue))
	by := map[*library.Song]string{}
	for _, it := range st.Queue {
		s := j.resolved[it.Key]
		if s == nil {
			sess.mu.Lock()
			w := sess.wire[it.Key]
			sess.mu.Unlock()
			if w == nil {
				// A song it was never told of: all of them again.
				j.guest.Resync()
				return
			}
			s = a.jamSong(w, it.Key)
		}
		songs = append(songs, s)
		if it.By != "" {
			by[s] = it.By
		}
	}
	j.by = by
	j.state, j.members, j.hostName = st, st.Members, st.Host
	// What of this app's the host may ask for: its songs in the queue.
	j.share.mu.Lock()
	for _, s := range songs {
		if !a.foreign(s) {
			j.share.media[a.songKey(s)] = p.trackOf(s).Open
			j.share.images[s.ImageItem] = j.share.server
		}
	}
	j.share.mu.Unlock()
	p.queue, p.unshuffled = songs, nil
	p.added, p.playNexts = 0, 0
	if st.Index >= 0 && st.Index < len(songs) {
		p.added = max(0, min(st.Added, len(songs)-st.Index-1))
		p.playNexts = max(0, min(st.PlayNexts, p.added))
	}
	a.jamFollow(old, true)
}

// jamFollow plays what the host plays, where it plays it: old is the
// song this app played before the queue changed under it. fresh is set
// for a state just sent, as after the host moved in the song, which is
// followed at once: otherwise a song just started or moved here is let
// be a moment, while it loads.
func (a *App) jamFollow(old *library.Song, fresh bool) {
	j := &a.jam
	st, g, p := j.state, j.guest, a.player
	if st == nil || g == nil {
		return
	}
	if st.Index < 0 || st.Index >= len(p.queue) {
		// Nothing plays there.
		if p.track != nil {
			p.reportStop()
			if p.engine != nil {
				p.engine.Stop()
			}
		}
		p.index, p.track, p.next, p.nextIndex = -1, nil, nil, -1
		return
	}
	want := p.queue[st.Index]
	pos := time.Duration(st.Position) * time.Millisecond
	if st.Playing {
		pos += g.HostNow().Sub(time.UnixMilli(st.At))
	}
	if d := want.Duration(); d > 0 && pos > d {
		pos = d
	}
	pos = max(pos, 0)
	// What this engine plays from a place is heard a moment later: it
	// starts that much ahead, to be heard where the host is.
	lead := j.catchUp
	if p.engine != nil {
		lead += p.engine.Lag()
	}
	oldKey := ""
	if old != nil && p.track != nil && !p.cold {
		oldKey = a.songKey(old)
	}
	local := p.state()
	left := local.Duration - local.Position
	hostLeft := want.Duration() - pos
	switch {
	case oldKey == "":
	case st.Playing && st.Index+1 < len(p.queue) && oldKey == a.songKey(p.queue[st.Index+1]) && hostLeft < 20*time.Second && !p.paused:
		// A crossfade here began the next song before the host did.
		p.index = st.Index + 1
		a.jamArm()
		return
	case st.Playing && st.Index > 0 && oldKey == a.songKey(p.queue[st.Index-1]) && pos < 4*time.Second && left < 4*time.Second && !p.paused && !p.finished:
		// This one ends a moment later, as a record's run-out does.
		p.index = st.Index - 1
		a.jamArm()
		return
	}
	if oldKey != a.songKey(want) {
		p.index = st.Index
		if st.Playing {
			p.playFrom(st.Index, pos+lead)
			j.settled = time.Now()
		} else {
			// The host's song, paused where it is: it loads when it plays.
			p.reportStop()
			if p.engine != nil {
				p.engine.Stop()
			}
			p.track, p.next, p.nextIndex = nil, nil, -1
			p.finished, p.cold, p.paused, p.resumeAt = false, true, true, pos
		}
		a.jamArm()
		return
	}
	p.index = st.Index
	drift := local.Position - pos // ahead of the host, or behind it
	off := drift
	if off < 0 {
		off = -off
	}
	switch {
	case !st.Playing:
		if !p.paused && !p.finished {
			p.paused = true
			if p.engine != nil {
				p.engine.Pause()
			}
			p.reportProgress(true)
		}
		if off > 1500*time.Millisecond && p.engine != nil && !p.finished {
			p.engine.SeekTo(pos)
		}
	case p.finished:
		// It ended here first: it plays again where the host is.
		p.playFrom(st.Index, pos+lead)
		j.settled = time.Now()
	case p.paused:
		p.paused = false
		if p.engine != nil {
			p.engine.Resume()
			if off > 1500*time.Millisecond {
				p.engine.SeekTo(pos + lead)
				j.settled = time.Now()
			}
		}
		p.reportProgress(true)
	case off > jamDrift && (time.Since(j.settled) > 3*time.Second || fresh && off > 2*time.Second) && !local.Buffering && p.engine != nil:
		// Out of step: back in. Soon after the last time, how far off it
		// is says how late it was heard, which the next time allows for;
		// not when the host has jumped.
		jumped := fresh && off > 2*time.Second
		if !jumped && time.Since(j.settled) < 20*time.Second && off < 8*time.Second {
			j.catchUp = min(max(j.catchUp-drift, 0), 8*time.Second)
			lead = j.catchUp + p.engine.Lag()
		}
		p.engine.SeekTo(pos + lead)
		j.settled = time.Now()
	}
	a.jamArm()
}

// jamDrift is how far a guest may be from the host before it is moved
// back in step: less than is heard as an echo between two rooms, more
// than the jitter of where each says it is.
const jamDrift = 300 * time.Millisecond

// jamArm has the engine ready the song after, which the queue's change
// may have moved.
func (a *App) jamArm() {
	p := a.player
	if p.next != nil {
		if i := p.following(); i >= 0 && p.queue[i].ID == p.next.ID {
			p.nextIndex = i
		}
	}
	p.armNext()
}

// askJam asks the host for something, as a guest: it reports whether it
// did, which a guest that may not control the Jam does not.
func (a *App) askJam(ask jam.Ask) bool {
	j := &a.jam
	if j.guest == nil {
		return false
	}
	if ask.Op != jam.OpAdd && ask.Op != jam.OpNext && !a.jamControls() {
		toasts = append(toasts, ui.Toast{ID: "jam-control", Title: "Only " + j.hostName + " controls this Jam", Description: "You can add songs to the queue."})
		if a.win != nil {
			a.win.Invalidate()
		}
		return false
	}
	p := a.player
	for _, i := range ask.At {
		if i >= 0 && i < len(p.queue) {
			ask.Keys = append(ask.Keys, a.songKey(p.queue[i]))
		} else {
			ask.Keys = append(ask.Keys, "")
		}
	}
	if ask.Op == jam.OpMove {
		k := ""
		if ask.To >= 0 && ask.To < len(p.queue) {
			k = a.songKey(p.queue[ask.To])
		}
		ask.Keys = append(ask.Keys, k)
	}
	j.guest.Ask(ask)
	return true
}

// askAdd asks the host to add songs, as Play Next does or Add to Queue.
func (a *App) askAdd(songs []*library.Song, next bool) {
	j := &a.jam
	if j.guest == nil || len(songs) == 0 {
		return
	}
	songs = songs[:min(len(songs), 100)]
	ask := jam.Ask{Op: jam.OpAdd}
	if next {
		ask.Op = jam.OpNext
	}
	// The songs of this app's server it may be asked for.
	j.share.mu.Lock()
	for _, s := range songs {
		if !a.foreign(s) {
			j.share.media[a.songKey(s)] = a.player.trackOf(s).Open
			j.share.images[s.ImageItem] = j.share.server
		}
	}
	j.share.mu.Unlock()
	for _, s := range songs {
		ask.Songs = append(ask.Songs, *a.wireSong(s))
	}
	a.askJam(ask)
	what := songs[0].Name
	if len(songs) > 1 {
		what = count(len(songs), "song", "songs")
	}
	toasts = append(toasts, ui.Toast{ID: "jam-added", Title: "Added " + what + " to the Jam"})
	if a.win != nil {
		a.win.Invalidate()
	}
}

// openMedia opens a song that may be asked for, as it downloads.
func (sh *jamShare) openMedia(key string) (io.ReadCloser, int64, error) {
	sh.mu.Lock()
	open := sh.media[key]
	sh.mu.Unlock()
	if open == nil {
		return nil, 0, errors.New("that song is not in the Jam")
	}
	f, err := open()
	if err != nil {
		return nil, 0, err
	}
	_, size, _ := f.Progress()
	return f, size, nil
}

// openImage fetches a picture of the server's.
func (sh *jamShare) openImage(ctx context.Context, item, tag string, px int) (io.ReadCloser, int64, error) {
	sh.mu.Lock()
	known := sh.images[item] != ""
	sh.mu.Unlock()
	if !known || sh.client == nil {
		return nil, 0, errors.New("not a picture of the Jam")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", sh.client.ImageURL(item, "Primary", tag, px), nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, errors.New("the server has no such picture: " + resp.Status)
	}
	return resp.Body, resp.ContentLength, nil
}

// openLyrics gets the lyrics of a song of the server's, as JSON.
func (sh *jamShare) openLyrics(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	sh.mu.Lock()
	ok := sh.media[key] != nil
	sh.mu.Unlock()
	if !ok || sh.client == nil {
		return nil, 0, errors.New("that song is not in the Jam")
	}
	_, id := jam.SplitKey(key)
	lines, err := sh.client.Lyrics(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	b, err := json.Marshal(lines)
	if err != nil {
		return nil, 0, err
	}
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}

// jamOpener is how a foreign song plays: through the Jam, into the cache
// as any song; nil for a song of this app's server.
func (a *App) jamOpener(s *library.Song) func() (*audio.File, error) {
	key, ok := a.jam.keys[s]
	if !ok {
		return nil
	}
	var u string
	switch {
	case a.jam.guest != nil:
		u = a.jam.guest.MediaURL(key)
	case a.jam.host != nil:
		u = a.jam.host.PullURL(key)
	default:
		return func() (*audio.File, error) { return nil, errors.New("the Jam it was of has ended") }
	}
	cache := a.player.cache
	return func() (*audio.File, error) {
		return cache.Open("jam-"+key, func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", u, nil)
		})
	}
}

// jamImageURL is where a picture of a foreign song is, "" for one of
// this app's server.
func (a *App) jamImageURL(item, tag string, px int) string {
	j := &a.jam
	server, ok := j.images[item]
	switch {
	case !ok:
	case j.guest != nil:
		return j.guest.ImageURL(server, item, tag, px)
	case j.host != nil:
		return j.host.ImageURL(server, item, tag, px)
	}
	return ""
}

// imageURL is where the picture of an item is: on the server, or for a
// song of another's in a Jam, through the Jam. It is "" for none.
func (a *App) imageURL(id, tag string, px int) string {
	if id == "" || tag == "" {
		return ""
	}
	if u := a.jamImageURL(id, tag, px); u != "" {
		return u
	}
	if a.client == nil {
		return ""
	}
	return a.client.ImageURL(id, "Primary", tag, px)
}

// jamLyricsURL is where the lyrics of a foreign song are.
func (a *App) jamLyricsURL(s *library.Song) string {
	key, ok := a.jam.keys[s]
	switch {
	case !ok:
	case a.jam.guest != nil:
		return a.jam.guest.LyricsURL(key)
	case a.jam.host != nil:
		return a.jam.host.LyricsURL(key)
	}
	return ""
}

// fetchJamLyrics gets the lyrics of a foreign song.
func (a *App) fetchJamLyrics(s *library.Song, u string) {
	ly := &a.lyrics
	id := s.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var lines []jellyfin.LyricLine
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err == nil {
			var resp *http.Response
			if resp, err = jam.Client().Do(req); err == nil {
				if resp.StatusCode == http.StatusOK {
					err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&lines)
				} else {
					err = errors.New(resp.Status)
				}
				resp.Body.Close()
			}
		}
		a.update(func() {
			if err != nil && a.settings.Lrclib {
				a.fetchLyrics(s, nil)
				return
			}
			ly.bySong[id] = lines
		})
	}()
}

// dismissToast takes away a toast that no longer holds, in the next
// frame.
func dismissToast(id string) { closedToasts = append(closedToasts, id) }

// closedToasts are the toasts the next frame takes away.
var closedToasts []string

// lookForJams finds the Jams on the network, while the dialog shows.
func (a *App) lookForJams() {
	j := &a.jam
	if j.looking != nil || j.in() || a.silent {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	j.looking, j.lookingAt, j.nearby = cancel, time.Now(), nil
	go func() {
		jam.Discover(ctx, "", func(f jam.Found) {
			a.update(func() {
				if j.looking != nil {
					j.nearby = append(j.nearby, f)
				}
			})
		})
	}()
}

func (a *App) stopLooking() {
	j := &a.jam
	if j.looking != nil {
		j.looking()
		j.looking = nil
	}
}

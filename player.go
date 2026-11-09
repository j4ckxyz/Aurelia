package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	mrand "math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"aurelia/internal/audio"
	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
)

// player is the queue: the songs to play, which one plays, and in what
// order. It drives the audio engine and tells the server what plays. Its
// methods run on the interface's goroutine.
type player struct {
	app    *App
	engine *audio.Engine
	cache  *audio.Cache
	err    error // why there is no sound, if the device did not open

	// queue is the songs in the order they play. added counts the songs
	// right after the one playing that Play Next and Add to Queue put
	// there: they play before the rest, as Spotify has them, and stay
	// next when the rest is shuffled or the song playing changes.
	// playNexts counts the first of them, those of Play Next, which go
	// before those of Add to Queue. unshuffled is the order of the others
	// without shuffling, while shuffled, and nil while not.
	queue      []*library.Song
	unshuffled []*library.Song
	index      int // of the song playing in queue, -1 for none
	added      int
	playNexts  int
	finished   bool
	paused     bool
	// cold is a queue brought back from the last run: its song loads
	// when it is first played, from resumeAt.
	cold     bool
	resumeAt time.Duration

	track     *audio.Track // of the song playing
	next      *audio.Track // what the engine plays after it
	nextIndex int
	failures  int
	// starts counts the times a song began to play, or began again: the
	// scrobbler tells a new listen from a seek back by it.
	starts int

	// far is the device that plays in this computer's place, nil while
	// it plays itself (remote.go).
	far *far

	// What the server was told.
	session  string
	reported time.Time
	// queueTold is the queue the server was last told, for the devices
	// that control this one.
	queueTold string
	reports   chan func(ctx context.Context, c *jellyfin.Client)
	// sleep is the timer that stops the music, and sleepGen counts its
	// changes, for a fade that is going to know it was cancelled.
	sleep    sleepTimer
	sleepGen atomic.Int64
	// similarFor is the song that songs like it were asked for, once, as
	// it played last.
	similarFor string
}

func newPlayer(app *App) *player {
	p := &player{app: app, index: -1, nextIndex: -1, reports: make(chan func(context.Context, *jellyfin.Client), 32)}
	client := &http.Client{} // no timeout: a song downloads as long as it takes
	p.cache, p.err = audio.NewCache(app.dirs.audio(), int64(app.settings.AudioCacheMB)<<20, client)
	if p.err == nil && !app.silent {
		// The sound card opens while the window does.
		go func() {
			// The output of the settings, when it is plugged in.
			devs, _ := audio.Devices()
			chosen := ""
			for _, d := range devs {
				if d.ID == app.settings.OutputDevice {
					chosen = d.ID
				}
			}
			engine, err := audio.NewEngine(44100, chosen, func(ev audio.Event) {
				app.update(func() { p.onEvent(ev) })
			})
			app.update(func() {
				p.engine, p.err = engine, err
				if engine != nil {
					app.output.devices, app.output.readAt = devs, time.Now()
					app.output.on = chosen
					engine.SetNormalize(app.settings.Normalize, app.settings.levelDB())
					app.applyEffects()
					engine.SetCrossfade(time.Duration(app.settings.CrossfadeSecs) * time.Second)
					p.applyVolume()
				}
			})
		}()
	}
	// One at a time and in order: a stop never overtakes its start.
	go func() {
		for fn := range p.reports {
			if c := app.clientNow(); c != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				fn(ctx, c)
				cancel()
			}
		}
	}()
	return p
}

func (p *player) applyVolume() {
	if p.engine == nil || p.far != nil {
		return
	}
	v := p.app.settings.Volume
	if p.app.settings.Muted {
		v = 0
	}
	p.engine.SetVolume(v)
}

// current returns the song playing, or nil.
func (p *player) current() *library.Song {
	if p.index < 0 || p.index >= len(p.queue) {
		return nil
	}
	return p.queue[p.index]
}

// state returns what the engine does with the song playing.
func (p *player) state() audio.State {
	song := p.current()
	if song == nil {
		return audio.State{Paused: true}
	}
	if p.far != nil {
		return p.farState(song)
	}
	st := audio.State{}
	switch {
	case p.cold:
		st.Position = p.resumeAt
	case p.engine != nil:
		st = p.engine.State()
	}
	st.Paused = p.paused || p.finished
	if st.Duration == 0 {
		st.Duration = song.Duration()
	}
	return st
}

// playing reports whether sound comes, or is about to.
func (p *player) playing() bool {
	return p.current() != nil && !p.finished && !p.paused
}

func (p *player) trackOf(s *library.Song) *audio.Track {
	app := p.app
	quality := jellyfin.Quality{MaxBitrate: app.settings.MaxBitrate * 1000}
	id := s.ID
	return &audio.Track{
		ID: id, Duration: s.Duration(), GainDB: s.GainDB,
		Open: func() (*audio.File, error) {
			// A song downloaded for offline plays from its file.
			if d := app.downloads; d != nil && d.store != nil && d.store.Has(id) {
				return d.store.Open(id, func(context.Context) (*http.Request, error) {
					return nil, errors.New("the download was removed")
				})
			}
			return p.cache.Open(id+"-"+quality.Key(), func(ctx context.Context) (*http.Request, error) {
				c := app.clientNow()
				if c == nil {
					return nil, jellyfin.ErrUnauthorized
				}
				return c.StreamRequest(ctx, id, quality)
			})
		},
	}
}

// play plays songs in their order from the one at start. Only Shuffle
// shuffles: playing something in order turns shuffling off.
func (p *player) play(songs []*library.Song, start int) {
	if len(songs) == 0 {
		return
	}
	if p.app.settings.Shuffle {
		p.app.settings.Shuffle = false
		p.app.saveSettings()
	}
	start = max(0, min(start, len(songs)-1))
	p.unshuffled, p.added, p.playNexts = nil, 0, 0
	p.queue = slices.Clone(songs)
	p.failures = 0
	p.playIndex(start)
}

// playWithSimilar plays a song on its own, then songs the server finds
// like it, rather than whatever else was beside it, as search results.
func (p *player) playWithSimilar(s *library.Song) {
	p.similarFor = s.ID // asked for here, so the queue's end does not ask again
	p.play([]*library.Song{s}, 0)
	p.app.mixOf("Songs", s.ID, radioSize, func(songs []*library.Song, err error) {
		if p.current() != s {
			return // something else plays now
		}
		var like []*library.Song
		for _, o := range songs {
			if o.ID != s.ID && !slices.Contains(p.queue, o) {
				like = append(like, o)
			}
		}
		p.extend(like)
	})
}

// playShuffled plays songs in a random order, whatever the setting.
func (p *player) playShuffled(songs []*library.Song) {
	if len(songs) == 0 {
		return
	}
	p.app.settings.Shuffle = true
	p.app.saveSettings()
	p.unshuffled, p.added, p.playNexts = slices.Clone(songs), 0, 0
	p.queue = slices.Clone(songs)
	shuffle(p.queue)
	p.failures = 0
	p.playIndex(0)
}

func shuffle(s []*library.Song) {
	mrand.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
}

// playIndex plays the song at i of the queue, from its start.
func (p *player) playIndex(i int) {
	if i < 0 || i >= len(p.queue) {
		return
	}
	if p.far != nil {
		p.goTo(i)
		p.farPlay(0)
		return
	}
	p.reportStop()
	// The song a crossfade has begun to bring in goes on from what was
	// heard of it, not from its start again.
	fading := p.next
	if p.nextIndex != i {
		fading = nil
	}
	p.goTo(i)
	p.finished, p.paused, p.cold = false, false, false
	p.starts++
	song := p.queue[p.index]
	p.track = p.trackOf(song)
	p.next, p.nextIndex = nil, -1
	if p.engine == nil {
		if p.err != nil {
			p.app.toastError("No sound", p.err)
		}
		return
	}
	p.engine.Resume()
	p.engine.Play(p.track, p.engine.Heard(fading))
	p.reportStart()
	p.armNext()
	p.app.saveQueue()
}

// addedRun returns where the songs added start and end in the queue.
func (p *player) addedRun() (lo, hi int) {
	lo = p.index + 1
	return lo, lo + p.added
}

// goTo makes the song at i of the queue the one playing. The songs added
// stay right after it; those of them it passes count as played.
func (p *player) goTo(i int) {
	lo, hi := p.addedRun()
	switch {
	case p.added == 0 || i == p.index:
	case i >= lo && i < hi:
		p.played(p.queue[lo : i+1])
		p.added = hi - i - 1
		p.playNexts = max(0, p.playNexts-(i-lo+1))
	default:
		run := slices.Clone(p.queue[lo:hi])
		p.queue = slices.Delete(p.queue, lo, hi)
		if i >= hi {
			i -= len(run)
		}
		p.queue = slices.Insert(p.queue, i+1, run...)
	}
	p.index = i
}

// played notes songs added that were played or passed: while shuffled,
// they join the order to come back to, after the song that played.
func (p *player) played(songs []*library.Song) {
	if p.unshuffled != nil {
		p.unshuffled = slices.Insert(p.unshuffled, p.afterCurrent(), songs...)
	}
}

// afterCurrent returns where in the order to come back to a song goes to
// follow the one playing.
func (p *player) afterCurrent() int {
	if i := slices.Index(p.unshuffled, p.current()); i >= 0 {
		return i + 1
	}
	return len(p.unshuffled)
}

// following returns the index of the song after the one playing, -1 at
// the end of the queue.
func (p *player) following() int {
	i := p.followingIndex()
	if i >= 0 && p.sleepStops(i) {
		return -1
	}
	return i
}

// followingIndex is following without the sleep timer.
func (p *player) followingIndex() int {
	switch {
	case len(p.queue) == 0 || p.index < 0:
		return -1
	case p.app.settings.Repeat == repeatOne:
		return p.index
	case p.index+1 < len(p.queue):
		return p.index + 1
	case p.app.settings.Repeat == repeatAll:
		return 0
	}
	return -1
}

// armNext tells the engine what to play where the song ends, which makes
// it download ahead and play on without a gap.
func (p *player) armNext() {
	p.similarCheck()
	if p.engine == nil {
		return
	}
	i := p.following()
	if i < 0 {
		p.next, p.nextIndex = nil, -1
		p.engine.SetNext(nil)
		return
	}
	if p.next != nil && p.nextIndex == i && p.next.ID == p.queue[i].ID {
		return
	}
	p.next, p.nextIndex = p.trackOf(p.queue[i]), i
	p.next.NoFade = followsOnAlbum(p.current(), p.queue[i])
	p.engine.SetNext(p.next)
}

func (p *player) onEvent(ev audio.Event) {
	switch ev.Kind {
	case audio.TrackChanged:
		// The engine went on to the song it was given.
		if p.next == nil || ev.TrackID != p.next.ID {
			return
		}
		p.countPlay()
		p.reportStopAt(p.current(), p.current().Duration())
		p.goTo(p.nextIndex)
		p.track = p.next
		p.next, p.nextIndex = nil, -1
		p.failures = 0
		p.starts++
		p.reportStart()
		p.armNext()
		p.app.saveQueue()
	case audio.Ended:
		if cur := p.current(); cur == nil || ev.TrackID != cur.ID {
			return
		}
		p.countPlay()
		p.reportStopAt(p.current(), p.current().Duration())
		// A song should follow but the engine had none: it could not open
		// it ahead. Playing it now skips it, and tells, if it fails again.
		if next := p.following(); next >= 0 {
			p.playIndex(next)
			return
		}
		p.finished = true
		p.sleepEnded()
	case audio.Failed:
		cur := p.current()
		if cur == nil || ev.TrackID != cur.ID {
			return
		}
		p.app.toastError("Could not play "+cur.Name, ev.Err)
		// One song that fails is skipped; several in a row is the
		// server or the network, and skipping on helps nobody.
		p.failures++
		if next := p.following(); next >= 0 && next != p.index && p.failures < 3 {
			p.playIndex(next)
		} else {
			p.engine.Stop()
			p.reportStop()
			p.finished = true
		}
	}
}

// countPlay notes a song heard to its end, as the server does.
func (p *player) countPlay() {
	if s := p.current(); s != nil {
		s.Plays++
		s.LastPlayed = time.Now().Unix()
		p.app.favGen++
	}
}

// toggle pauses, or plays on.
func (p *player) toggle() {
	if p.current() == nil {
		return
	}
	if f := p.far; f != nil {
		switch {
		case f.dev.Playing == nil || f.queueKey == "":
			// The device plays nothing of this yet: it is given the queue.
			p.farPlay(p.state().Position)
		case p.paused:
			f.position, f.at = p.state().Position, time.Now()
			p.paused = false
			p.app.playstate("Unpause", 0)
		default:
			f.position, f.at = p.state().Position, time.Now()
			p.paused = true
			p.app.playstate("Pause", 0)
		}
		return
	}
	if p.finished {
		// Stopped: play on from the song after, as when songs were added
		// at the end or the sleep timer stopped the music, or from the top
		// when the queue ended.
		next := p.index + 1
		if next >= len(p.queue) {
			next = 0
		}
		p.playIndex(next)
		return
	}
	if p.cold {
		// The queue of the last run: its song loads now, where it was.
		if p.engine == nil {
			if p.err != nil {
				p.app.toastError("No sound", p.err)
			}
			return
		}
		p.cold, p.paused = false, false
		p.track = p.trackOf(p.current())
		p.engine.Resume()
		p.engine.Play(p.track, p.resumeAt)
		p.reportStart()
		p.armNext()
		return
	}
	p.paused = !p.paused
	if p.engine != nil {
		if p.paused {
			p.engine.Pause()
		} else {
			p.engine.Resume()
		}
	}
	p.reportProgress(true)
}

// skip goes to the next song.
func (p *player) skip() {
	if p.index < 0 {
		return
	}
	if p.far != nil {
		p.app.playstate("NextTrack", 0)
		return
	}
	switch {
	case p.index+1 < len(p.queue):
		p.failures = 0
		p.playIndex(p.index + 1)
	case p.app.settings.Repeat != repeatOff && len(p.queue) > 0:
		p.failures = 0
		p.playIndex(0)
	}
}

// previous goes to the start of the song, or to the song before it when
// it has barely begun.
func (p *player) previous() {
	if p.index < 0 {
		return
	}
	if p.far != nil {
		p.app.playstate("PreviousTrack", 0)
		return
	}
	if p.state().Position > 3*time.Second || p.index == 0 {
		p.starts++ // played again, which a seek is not
		p.seek(0)
		return
	}
	p.failures = 0
	p.playIndex(p.index - 1)
}

func (p *player) seek(to time.Duration) {
	if p.current() == nil {
		return
	}
	if f := p.far; f != nil {
		f.position, f.at = to, time.Now()
		if f.dev.Playing != nil {
			p.app.playstate("Seek", to)
		}
		return
	}
	if p.cold {
		p.resumeAt = to
		return
	}
	if p.engine == nil {
		return
	}
	if p.finished {
		// It ended: it plays again.
		p.finished, p.paused = false, false
		p.starts++
		p.engine.Resume()
		p.engine.Play(p.track, to)
		p.reportStart()
		p.armNext()
		return
	}
	p.engine.SeekTo(to)
	p.armNext()
	p.reportProgress(true)
}

// setShuffle shuffles the songs to come, or puts them back in order. The
// songs played stay before the one playing, and the songs added stay
// next.
func (p *player) setShuffle(on bool) {
	was := p.app.settings.Shuffle
	p.app.settings.Shuffle = on
	p.app.saveSettings()
	cur := p.current()
	if cur == nil {
		return
	}
	if f := p.far; f != nil {
		mode := map[bool]string{false: "Sorted", true: "Shuffle"}[on]
		p.app.send("shuffle", func(ctx context.Context, c *jellyfin.Client) error {
			return c.Command(ctx, f.dev.ID, "SetShuffleQueue", map[string]string{"ShuffleMode": mode})
		})
		return
	}
	lo, hi := p.addedRun()
	switch {
	case on:
		if !was || p.unshuffled == nil {
			// The order to come back to is the queue's as it is, moves
			// and all, but for the songs added.
			p.unshuffled = slices.Concat(p.queue[:lo], p.queue[hi:])
		}
		shuffle(p.queue[hi:])
	case was && p.unshuffled != nil:
		order := slices.Clone(p.unshuffled)
		if i := slices.Index(order, cur); i >= 0 {
			p.queue = slices.Insert(order, i+1, p.queue[lo:hi]...)
			p.index = i
		}
		p.unshuffled = nil
	default:
		p.unshuffled = nil
	}
	p.next, p.nextIndex = nil, -1
	p.armNext()
	p.app.saveQueue()
}

func (p *player) cycleRepeat() { p.setRepeat((p.app.settings.Repeat + 1) % 3) }

// setRepeat repeats nothing, the queue or the song, here or on the device
// that plays.
func (p *player) setRepeat(mode int) {
	p.app.settings.Repeat = mode
	p.app.saveSettings()
	if f := p.far; f != nil {
		mode := repeatModes[p.app.settings.Repeat]
		p.app.send("repeat", func(ctx context.Context, c *jellyfin.Client) error {
			return c.Command(ctx, f.dev.ID, "SetRepeatMode", map[string]string{"RepeatMode": mode})
		})
		return
	}
	p.next, p.nextIndex = nil, -1
	p.armNext()
}

// playNext puts songs right after the one playing and the songs of Play
// Next before them, ahead of those of Add to Queue.
func (p *player) playNext(songs []*library.Song) {
	if p.addsHere(songs, "PlayNext") {
		lo, _ := p.addedRun()
		at := lo + p.playNexts
		p.added += len(songs)
		p.playNexts += len(songs)
		p.insert(at, songs)
	}
}

// enqueue puts songs after the one playing and the songs added earlier,
// before the rest of what plays, as Add to Queue does.
func (p *player) enqueue(songs []*library.Song) {
	if p.addsHere(songs, "PlayLast") {
		_, hi := p.addedRun()
		p.added += len(songs)
		p.insert(hi, songs)
	}
}

// extend puts songs at the end of the queue, as more of what plays, as
// the songs found like it are.
func (p *player) extend(songs []*library.Song) {
	if p.addsHere(songs, "PlayLast") {
		if p.unshuffled != nil {
			p.unshuffled = append(p.unshuffled, songs...)
		}
		p.insert(len(p.queue), songs)
	}
}

// addsHere reports whether songs are to be put in the queue here: with
// nothing playing they play, and a device that plays adds them itself.
func (p *player) addsHere(songs []*library.Song, command string) bool {
	switch {
	case len(songs) == 0:
		return false
	case p.current() == nil:
		p.play(songs, 0)
		return false
	}
	return !p.farAdd(command, songs)
}

// insert puts songs at i of the queue.
func (p *player) insert(i int, songs []*library.Song) {
	p.queue = slices.Insert(p.queue, i, songs...)
	p.armNext()
	p.app.saveQueue()
}

// farAdd has the device that plays add songs to its queue, and reports
// whether one plays: its queue is its own to change.
func (p *player) farAdd(command string, songs []*library.Song) bool {
	f := p.far
	if f == nil || f.dev.Playing == nil {
		return false
	}
	ids := make([]string, 0, len(songs))
	for _, s := range songs[:min(len(songs), 200)] {
		ids = append(ids, s.ID)
	}
	p.app.send("add to the queue", func(ctx context.Context, c *jellyfin.Client) error {
		return c.PlayOn(ctx, f.dev.ID, command, ids, 0, 0)
	})
	return true
}

// farOwns reports whether the queue is a device's, which only the
// device changes.
func (p *player) farOwns() bool { return p.far != nil && p.far.dev.Playing != nil }

// remove takes the song at i out of the queue.
func (p *player) remove(i int) {
	if i < 0 || i >= len(p.queue) || i == p.index || p.farOwns() {
		return
	}
	if lo, hi := p.addedRun(); i >= lo && i < hi {
		p.added--
		if i < lo+p.playNexts {
			p.playNexts--
		}
	} else {
		p.unshuffled = dropOne(p.unshuffled, p.queue[i])
	}
	p.queue = slices.Delete(p.queue, i, i+1)
	if i < p.index {
		p.index--
	}
	p.next, p.nextIndex = nil, -1
	p.armNext()
	p.app.saveQueue()
}

// dropOne returns list without the first time s is in it.
func dropOne(list []*library.Song, s *library.Song) []*library.Song {
	if i := slices.Index(list, s); i >= 0 {
		return slices.Delete(list, i, i+1)
	}
	return list
}

// The songs of the queue, as move sees them: of what plays, added by Add
// to Queue, or added by Play Next.
const (
	ofRest = iota
	ofAddToQueue
	ofPlayNext
)

// kindAt returns which of those the song at i of the queue is.
func (p *player) kindAt(i int) int {
	lo, hi := p.addedRun()
	switch {
	case i >= lo && i < lo+p.playNexts:
		return ofPlayNext
	case i >= lo && i < hi:
		return ofAddToQueue
	}
	return ofRest
}

// move puts the songs at from, in order, before the song at to of the
// queue; to is the queue's length for its end. The song playing stays
// the one playing. Songs dropped among the songs added, or at their end,
// are added; songs dragged out from them are no longer.
func (p *player) move(from []int, to int) {
	if p.farOwns() {
		return
	}
	cur := p.current()
	lo, hi := p.addedRun()
	into := ofRest
	switch {
	case p.playNexts > 0 && to >= lo && to < lo+p.playNexts:
		into = ofPlayNext
	case p.added > 0 && to >= lo && to <= hi:
		into = ofAddToQueue
	}
	var moved, rest []*library.Song
	var kinds []int
	at := -1
	for i, s := range p.queue {
		if i == to {
			at = len(rest)
		}
		if slices.Contains(from, i) {
			moved = append(moved, s)
			p.regroup(s, p.kindAt(i) != ofRest, into != ofRest)
		} else {
			rest = append(rest, s)
			kinds = append(kinds, p.kindAt(i))
		}
	}
	if at < 0 {
		at = len(rest)
	}
	if len(moved) == 0 {
		return
	}
	p.queue = slices.Insert(rest, at, moved...)
	kinds = slices.Insert(kinds, at, slices.Repeat([]int{into}, len(moved))...)
	if cur != nil {
		// The same song may be in the queue twice: the one playing is
		// the one that was not moved, at the place it had or near it.
		for i, s := range p.queue {
			if s == cur && (i == p.index || !slices.Contains(moved, s)) {
				p.index = i
				break
			}
		}
	}
	// The songs added are those right after the song playing, and those
	// of Play Next the first of them.
	p.added, p.playNexts = 0, 0
	for i := p.index + 1; i < len(kinds) && kinds[i] != ofRest; i++ {
		p.added++
		if kinds[i] == ofPlayNext && p.playNexts == p.added-1 {
			p.playNexts++
		}
	}
	p.next, p.nextIndex = nil, -1
	p.armNext()
	p.app.saveQueue()
}

// regroup keeps the order to come back to, while shuffled, as a song
// moved joins the songs added or leaves them: the songs added are not in
// it, and one that leaves them goes after the song playing.
func (p *player) regroup(s *library.Song, was, now bool) {
	switch {
	case p.unshuffled == nil || was == now:
	case now:
		p.unshuffled = dropOne(p.unshuffled, s)
	default:
		p.unshuffled = slices.Insert(p.unshuffled, p.afterCurrent(), s)
	}
}

// restore brings back the queue of the last run, paused where it was:
// the song at index with the songs added after it, playNexts of them by
// Play Next, and while shuffled the order to come back to, which a queue
// kept by an earlier version does not have.
func (p *player) restore(songs, unshuffled []*library.Song, index, added, playNexts int, at time.Duration) {
	if len(songs) == 0 || p.current() != nil {
		return
	}
	p.queue = songs
	p.index = max(0, min(index, len(songs)-1))
	p.added = max(0, min(added, len(songs)-p.index-1))
	p.playNexts = max(0, min(playNexts, p.added))
	p.unshuffled = nil
	if p.app.settings.Shuffle {
		lo, hi := p.addedRun()
		p.unshuffled = unshuffled
		if len(unshuffled) == 0 {
			p.unshuffled = slices.Concat(songs[:lo], songs[hi:])
		}
	}
	p.cold, p.paused, p.finished, p.resumeAt = true, true, false, at
}

// clearUpcoming empties the queue after the song playing.
func (p *player) clearUpcoming() {
	if p.index < 0 || p.farOwns() {
		return
	}
	_, hi := p.addedRun()
	for _, s := range p.queue[hi:] {
		p.unshuffled = dropOne(p.unshuffled, s)
	}
	p.queue, p.added, p.playNexts = p.queue[:p.index+1], 0, 0
	p.armNext()
	p.app.saveQueue()
}

// stop ends everything, as signing out does.
func (p *player) stop() {
	if p.far != nil {
		p.far = nil // the device plays on: it is its own
	}
	p.reportStop()
	if p.engine != nil {
		p.engine.Stop()
	}
	p.queue, p.unshuffled, p.index, p.finished, p.paused = nil, nil, -1, false, false
	p.added, p.playNexts = 0, 0
	p.track, p.next, p.nextIndex = nil, nil, -1
}

func (p *player) report(fn func(ctx context.Context, c *jellyfin.Client)) {
	select {
	case p.reports <- fn:
	default: // the server is not answering: what plays matters more
	}
}

func (p *player) reportStart() {
	s := p.current()
	if s == nil || p.far != nil {
		return
	}
	var b [8]byte
	rand.Read(b[:])
	p.session = hex.EncodeToString(b[:])
	p.reported = time.Now()
	p.queueTold = ""
	pb := p.playback(s, 0, false)
	p.report(func(ctx context.Context, c *jellyfin.Client) { c.ReportStart(ctx, pb) })
}

// playback is what the server is told of the song playing, with what a
// device that controls this one shows: the volume, the order, and the
// queue when it changed since it was last told.
func (p *player) playback(s *library.Song, at time.Duration, paused bool) jellyfin.Playback {
	set := &p.app.settings
	pb := jellyfin.Playback{
		SongID: s.ID, SessionID: p.session, Position: at, Paused: paused,
		Volume: int(set.Volume*100 + 0.5), Muted: set.Muted, Repeat: repeatModes[set.Repeat], Shuffle: set.Shuffle,
	}
	ids, index := p.window()
	if key := strings.Join(ids, ",") + "@" + strconv.Itoa(index); key != p.queueTold {
		p.queueTold = key
		pb.Queue, pb.Index = ids, index
	}
	return pb
}

// reportProgress tells the server where the song is: now, or when it was
// last told ten seconds ago.
func (p *player) reportProgress(now bool) {
	s := p.current()
	if s == nil || p.session == "" || p.finished || p.far != nil {
		return
	}
	if !now && time.Since(p.reported) < 10*time.Second {
		return
	}
	p.reported = time.Now()
	st := p.state()
	pb := p.playback(s, st.Position, st.Paused)
	p.report(func(ctx context.Context, c *jellyfin.Client) { c.ReportProgress(ctx, pb) })
}

func (p *player) reportStop() {
	if s := p.current(); s != nil && !p.finished && p.far == nil {
		p.reportStopAt(s, p.state().Position)
	}
}

func (p *player) reportStopAt(s *library.Song, at time.Duration) {
	if s == nil || p.session == "" {
		return
	}
	pb := jellyfin.Playback{SongID: s.ID, SessionID: p.session, Position: at}
	p.session = ""
	p.report(func(ctx context.Context, c *jellyfin.Client) { c.ReportStop(ctx, pb) })
}

// engineRate is the rate the sound card plays at, 0 without one.
func (p *player) engineRate() int {
	if p.engine == nil {
		return 0
	}
	return p.engine.Rate()
}

// followsOnAlbum reports whether next is the song after cur on their album,
// or cur again: those are not faded into, as an album played through is
// not, and a song repeated is not mixed into itself.
func followsOnAlbum(cur, next *library.Song) bool {
	switch {
	case cur == nil || next == nil:
		return false
	case cur.ID == next.ID:
		return true
	case cur.AlbumID == "" || cur.AlbumID != next.AlbumID:
		return false
	}
	return next.Disc == cur.Disc && next.Track == cur.Track+1 || next.Disc == cur.Disc+1 && next.Track == 1
}

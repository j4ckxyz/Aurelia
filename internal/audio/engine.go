package audio

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/oto/v3"
)

// Track is something to play.
type Track struct {
	// ID names the track in State and in events.
	ID string
	// Open returns the track's file, as a Cache does.
	Open func() (*File, error)
	// Duration is the length the library gives, for files that do not
	// say theirs.
	Duration time.Duration
	// GainDB is added while the engine normalizes volume.
	GainDB float64
}

// EventKind says what happened.
type EventKind int

const (
	// TrackChanged: the track set with SetNext began, where the one
	// before it ended.
	TrackChanged EventKind = iota
	// Ended: the track ended and none followed.
	Ended
	// Failed: the track could not play.
	Failed
)

// Event is what the engine tells of on its own.
type Event struct {
	Kind    EventKind
	TrackID string
	Err     error
}

// State is what the engine is doing.
type State struct {
	TrackID   string
	Paused    bool
	Buffering bool // waiting for the download
	Position  time.Duration
	Duration  time.Duration
	// Loaded is the part of the track downloaded, from 0 to 1.
	Loaded float64
}

// ringFrames is the sound decoded ahead of the device, about a second
// and a half.
const ringFrames = 1 << 16

// Engine plays one track at a time and the next where it ends.
type Engine struct {
	rate    int
	octx    *oto.Context
	player  *oto.Player
	onEvent func(Event)
	events  chan Event

	mu     sync.Mutex
	cond   *sync.Cond
	closed bool

	// What the pump is asked, and what it holds.
	gen       uint64
	load      *loadReq
	seekTo    time.Duration
	seekSet   bool
	next      *Track
	nextTaken bool     // the pump opened next, or began it
	loading   *loadReq // what the pump is opening
	pumpFile  *File    // what the pump may be blocked reading
	nextFile  *File    // and the file of the track after
	normalize bool
	level     float64 // added to every track's gain while normalizing, in dB

	ring   []float32
	rhead  int // in frames
	rlen   int
	segs   []*segment
	paused bool
	ended  bool
	volume float64
}

type loadReq struct {
	track *Track
	at    time.Duration
}

// segment is a run of one track's frames in the ring.
type segment struct {
	id     string
	dur    time.Duration
	file   *File
	start  int64 // of its first frame in the track, in frames of the device
	played int64 // given to the device
	queued int   // in the ring
	final  bool  // the pump adds no more
}

// source is a track the pump decodes.
type source struct {
	track *Track
	file  *File
	dec   decoder
	rs    *resampler
	gain  float32
	limit float32 // what the limiter turns the level down by now, 1 for nothing
	eof   bool
	seg   *segment
	need  int // room in the ring that one read of the decoder may take, in frames
}

func (s *source) close() {
	if s != nil && s.file != nil {
		s.file.Close()
	}
}

// NewEngine opens the sound card at rate Hz. onEvent is called on a
// goroutine of the engine's, one event at a time.
func NewEngine(rate int, onEvent func(Event)) (*Engine, error) {
	e := &Engine{rate: rate, onEvent: onEvent, events: make(chan Event, 16), volume: 1, paused: false}
	e.cond = sync.NewCond(&e.mu)
	e.ring = make([]float32, 2*ringFrames)
	octx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:      rate,
		ChannelCount:    2,
		Format:          oto.FormatFloat32LE,
		ApplicationName: "Aurelia",
	})
	if err != nil {
		return nil, fmt.Errorf("audio: opening the sound card: %w", err)
	}
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		return nil, errors.New("audio: the sound card did not start")
	}
	if err := octx.Err(); err != nil {
		return nil, fmt.Errorf("audio: opening the sound card: %w", err)
	}
	e.octx = octx
	e.player = octx.NewPlayer(deviceReader{e})
	// A short buffer on the device's side: the ring is the real one, and
	// what the device holds is what a track change and the position lag
	// by.
	e.player.SetBufferSize(rate / 8 * 8)
	go e.pump()
	go func() {
		for ev := range e.events {
			if ev.Kind == Ended {
				// The device rests until the next track.
				e.mu.Lock()
				idle := len(e.segs) == 0
				e.mu.Unlock()
				if idle {
					e.player.Pause()
				}
			}
			if e.onEvent != nil {
				e.onEvent(ev)
			}
		}
	}()
	return e, nil
}

// Rate returns the rate of the device.
func (e *Engine) Rate() int { return e.rate }

func (e *Engine) emit(ev Event) {
	select {
	case e.events <- ev:
	default: // nobody listens fast enough: the state still tells
	}
}

// Play plays track from at, in place of what plays.
func (e *Engine) Play(track *Track, at time.Duration) {
	e.mu.Lock()
	e.gen++
	e.load = &loadReq{track: track, at: at}
	e.seekSet = false
	e.next, e.nextTaken = nil, false
	e.ended = false
	e.rhead, e.rlen = 0, 0
	// The segment stands for the track until its sound comes, so that
	// the state names it at once.
	e.segs = []*segment{{id: track.ID, dur: track.Duration, start: e.frames(at)}}
	e.interrupt()
	paused := e.paused
	e.cond.Broadcast()
	e.mu.Unlock()
	e.player.Seek(0, io.SeekStart) // drops what the device has not played
	if !paused {
		e.player.Play()
	}
}

// SetNext names the track to play where the current one ends, without a
// gap; nil for none.
func (e *Engine) SetNext(track *Track) {
	e.mu.Lock()
	if e.next != track {
		// A next track already decoding into the ring stays: it is too
		// late to change what the device is about to play.
		e.next, e.nextTaken = track, false
		e.cond.Broadcast()
	}
	e.mu.Unlock()
}

// SeekTo moves within the track playing.
func (e *Engine) SeekTo(to time.Duration) {
	e.mu.Lock()
	if len(e.segs) == 0 {
		e.mu.Unlock()
		return
	}
	head := e.segs[0]
	if to < 0 {
		to = 0
	}
	if head.dur > 0 && to > head.dur {
		to = head.dur
	}
	e.gen++
	e.ended = false
	e.rhead, e.rlen = 0, 0
	e.segs = []*segment{{id: head.id, dur: head.dur, file: head.file, start: e.frames(to)}}
	switch {
	case e.load != nil:
		// The track has not begun: it begins there.
		e.load.at = to
	case e.loading != nil:
		// The pump is opening it, which this interrupts: open it anew.
		e.load = &loadReq{track: e.loading.track, at: to}
	default:
		e.seekTo, e.seekSet = to, true
	}
	// The track after this one may be in the ring already: it is asked
	// for again.
	e.nextTaken = false
	e.interrupt()
	e.cond.Broadcast()
	e.mu.Unlock()
	e.player.Seek(0, io.SeekStart)
}

// interrupt makes the reads the pump waits in fail, for a new request to
// be served at once. The caller holds mu.
func (e *Engine) interrupt() {
	if e.pumpFile != nil {
		e.pumpFile.Abort()
	}
	if e.nextFile != nil {
		e.nextFile.Abort()
	}
}

// Pause stops the sound where it is.
func (e *Engine) Pause() {
	e.mu.Lock()
	e.paused = true
	e.mu.Unlock()
	e.player.Pause()
}

// Resume goes on after Pause.
func (e *Engine) Resume() {
	e.mu.Lock()
	e.paused = false
	idle := len(e.segs) == 0
	e.mu.Unlock()
	if !idle {
		e.player.Play()
	}
}

// Stop ends the track playing.
func (e *Engine) Stop() {
	e.mu.Lock()
	e.gen++
	e.load = &loadReq{}
	e.seekSet = false
	e.next, e.nextTaken = nil, false
	e.ended = false
	e.rhead, e.rlen = 0, 0
	e.segs = nil
	e.interrupt()
	e.cond.Broadcast()
	e.mu.Unlock()
	e.player.Pause()
	e.player.Seek(0, io.SeekStart)
}

// SetVolume sets the volume from 0 to 1, as a slider's position: the
// level follows its cube, as loudness is heard.
func (e *Engine) SetVolume(v float64) {
	v = math.Max(0, math.Min(1, v))
	e.mu.Lock()
	e.volume = v
	e.mu.Unlock()
	e.player.SetVolume(v * v * v)
}

// SetNormalize turns on the gain of tracks, from the track decoded next;
// level is added to the gain of each, in dB, for every song to play
// louder or quieter than the server's measure makes them.
func (e *Engine) SetNormalize(on bool, level float64) {
	e.mu.Lock()
	e.normalize, e.level = on, level
	e.mu.Unlock()
}

// limiterCeiling is the level the limiter keeps a track under when its
// gain would take it over full scale, and limiterRelease how much of the
// way back to no limiting each frame after goes: about a fifth of a
// second at 44.1 kHz.
const (
	limiterCeiling = 0.97
	limiterRelease = 1.0 / 8000
)

// amplify applies a track's gain to frames of two samples. A gain over
// 1 can take peaks over full scale: those are turned down as they come
// and let back up slowly, which the ear takes better than clipping.
func (s *source) amplify(buf []float32) {
	if s.gain == 1 {
		return
	}
	if s.gain < 1 {
		for i, v := range buf {
			buf[i] = v * s.gain
		}
		return
	}
	limit := s.limit
	for i := 0; i+1 < len(buf); i += 2 {
		l, r := buf[i]*s.gain, buf[i+1]*s.gain
		peak := max(abs32(l), abs32(r))
		if want := limiterCeiling / peak; peak*limit > limiterCeiling {
			limit = want
		} else {
			limit += (1 - limit) * limiterRelease
		}
		buf[i], buf[i+1] = l*limit, r*limit
	}
	s.limit = limit
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// State returns what plays and where.
func (e *Engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := State{Paused: e.paused}
	if len(e.segs) == 0 {
		return st
	}
	head := e.segs[0]
	st.TrackID, st.Duration = head.id, head.dur
	pos := head.start + head.played
	if head.played > 0 {
		// What the device holds is not heard yet.
		pos -= int64(e.player.BufferedSize() / 8)
		pos = max(pos, head.start)
	}
	st.Position = time.Duration(float64(pos) / float64(e.rate) * float64(time.Second))
	if st.Duration > 0 && st.Position > st.Duration {
		st.Position = st.Duration
	}
	st.Buffering = !e.paused && head.queued == 0 && !head.final
	if head.file != nil {
		have, size, done := head.file.Progress()
		switch {
		case done:
			st.Loaded = 1
		case size > 0:
			st.Loaded = float64(have) / float64(size)
		}
	}
	return st
}

// Close stops the engine.
func (e *Engine) Close() {
	e.mu.Lock()
	e.closed = true
	e.gen++
	e.interrupt()
	e.cond.Broadcast()
	e.mu.Unlock()
	e.player.Pause()
}

func (e *Engine) frames(d time.Duration) int64 {
	return int64(d.Seconds() * float64(e.rate))
}

// deviceReader is the engine as the device reads it.
type deviceReader struct{ e *Engine }

func (r deviceReader) Read(p []byte) (int, error) { return r.e.read(p) }

// Seek is the device's way of dropping its buffer, which Player.Seek
// needs of its source.
func (r deviceReader) Seek(offset int64, whence int) (int64, error) { return 0, nil }

// read gives the device its samples. It never waits: the ring is filled
// by the pump, and silence stands in for what is not there yet.
func (e *Engine) read(p []byte) (int, error) {
	frames := len(p) / 8
	if frames == 0 {
		return 0, nil
	}
	out := unsafe.Slice((*float32)(unsafe.Pointer(&p[0])), 2*frames)
	n := 0
	e.mu.Lock()
	for n < frames && len(e.segs) > 0 {
		seg := e.segs[0]
		if seg.queued == 0 {
			if !seg.final {
				break // the pump is behind
			}
			if len(e.segs) == 1 {
				if e.ended {
					e.segs = nil
					e.emit(Event{Kind: Ended, TrackID: seg.id})
				}
				break
			}
			e.segs = e.segs[1:]
			e.emit(Event{Kind: TrackChanged, TrackID: e.segs[0].id})
			continue
		}
		k := min(frames-n, seg.queued, ringFrames-e.rhead)
		copy(out[2*n:2*(n+k)], e.ring[2*e.rhead:2*(e.rhead+k)])
		e.rhead = (e.rhead + k) % ringFrames
		e.rlen -= k
		seg.queued -= k
		seg.played += int64(k)
		n += k
	}
	e.cond.Broadcast()
	e.mu.Unlock()
	clear(out[2*n:])
	return len(p), nil
}

// pump decodes into the ring, on its own goroutine, which alone waits
// for downloads.
func (e *Engine) pump() {
	var cur, nxt *source
	in := make([]float32, 2*4096)
	var out []float32
	for {
		e.mu.Lock()
		var (
			load     *loadReq
			seek     bool
			seekTo   time.Duration
			openNext *Track
			decode   bool
		)
		for {
			if e.closed {
				e.mu.Unlock()
				cur.close()
				nxt.close()
				return
			}
			if e.load != nil {
				load, e.load = e.load, nil
				e.loading = load
				break
			}
			if e.seekSet {
				seek, seekTo, e.seekSet = true, e.seekTo, false
				break
			}
			if nxt != nil && e.next != nxt.track {
				// The track that was to follow no longer does.
				nxt.close()
				nxt, e.nextFile = nil, nil
				continue
			}
			playing := cur != nil && cur.seg != nil && len(e.segs) > 0
			if playing && e.next != nil && !e.nextTaken && nxt == nil {
				openNext, e.nextTaken = e.next, true
				break
			}
			if playing && cur.eof && nxt != nil {
				// The next track begins where this one ended.
				cur.close()
				cur, nxt = nxt, nil
				e.next, e.nextFile = nil, nil
				cur.seg = &segment{id: cur.track.ID, dur: cur.track.Duration, file: cur.file}
				e.segs = append(e.segs, cur.seg)
				e.pumpFile = cur.file
				continue
			}
			if playing && cur.eof && e.next == nil && !e.ended {
				e.ended = true // Read tells once the ring is empty
			}
			if playing && !cur.eof && ringFrames-e.rlen >= cur.need {
				decode = true
				break
			}
			e.cond.Wait()
		}
		gen := e.gen
		normalize := e.normalize
		level := e.level
		e.mu.Unlock()

		switch {
		case load != nil:
			cur.close()
			nxt.close()
			cur, nxt = nil, nil
			var src *source
			var err error
			if load.track != nil {
				if src, err = e.open(load.track, gen, normalize, level); err == nil && load.at > 0 {
					if err = e.seekSource(src, load.at); err != nil {
						src.close()
						src = nil
					}
				}
			}
			e.mu.Lock()
			e.loading = nil
			if src != nil && e.gen == gen && len(e.segs) > 0 {
				src.seg = e.segs[0]
				src.seg.file = src.file
				if n := src.dec.Len(); n > 0 && src.seg.dur == 0 {
					src.seg.dur = time.Duration(float64(n) / float64(src.dec.SampleRate()) * float64(time.Second))
				}
			}
			e.mu.Unlock()
			cur = src
			if err != nil {
				e.fail(load.track.ID, gen, err)
			}

		case seek:
			nxt.close()
			nxt = nil
			if cur == nil {
				continue
			}
			cur.file.Resume()
			if err := e.seekSource(cur, seekTo); err != nil {
				e.fail(cur.track.ID, gen, err)
				continue
			}
			cur.eof = false
			e.mu.Lock()
			e.nextFile = nil
			if e.gen == gen && len(e.segs) > 0 {
				cur.seg = e.segs[0]
			}
			e.mu.Unlock()

		case openNext != nil:
			src, err := e.openNext(openNext, gen, normalize, level)
			e.mu.Lock()
			switch {
			case err != nil:
				// It fails again, and tells, when its turn to play
				// comes: this track simply ends.
				if e.next == openNext && !errors.Is(err, ErrAborted) {
					e.next = nil
				}
			case e.gen == gen && e.next == openNext:
				nxt, src = src, nil
			}
			e.mu.Unlock()
			src.close()

		case decode:
			n, err := cur.dec.Read(in)
			out = out[:0]
			if n > 0 {
				buf := in[:2*n]
				cur.amplify(buf)
				if cur.rs != nil {
					out = cur.rs.process(buf, out)
				} else {
					out = append(out, buf...)
				}
			}
			eof := false
			if err != nil {
				if errors.Is(err, ErrAborted) {
					continue // a new request waits
				}
				if err != io.EOF {
					e.fail(cur.track.ID, gen, err)
					continue
				}
				eof = true
				if cur.rs != nil {
					out = cur.rs.flush(out)
				}
			}
			e.mu.Lock()
			if e.gen == gen && cur.seg != nil {
				e.push(out, cur.seg)
				if eof {
					cur.eof = true
					cur.seg.final = true
				}
			}
			e.mu.Unlock()
		}
	}
}

// push appends frames of seg to the ring, which has room for them.
func (e *Engine) push(samples []float32, seg *segment) {
	frames := len(samples) / 2
	for done := 0; done < frames; {
		tail := (e.rhead + e.rlen) % ringFrames
		k := min(frames-done, ringFrames-tail, ringFrames-e.rlen)
		if k <= 0 {
			break
		}
		copy(e.ring[2*tail:2*(tail+k)], samples[2*done:2*(done+k)])
		e.rlen += k
		done += k
		seg.queued += k
	}
}

// open opens a track for the pump, which registers its file so that a new
// request can interrupt its reads.
func (e *Engine) open(t *Track, gen uint64, normalize bool, level float64) (*source, error) {
	f, err := t.Open()
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	if e.gen != gen {
		e.mu.Unlock()
		f.Close()
		return nil, ErrAborted
	}
	e.pumpFile = f
	e.mu.Unlock()
	return e.newSource(t, f, normalize, level)
}

// openNext opens the track to play after the current one.
func (e *Engine) openNext(t *Track, gen uint64, normalize bool, level float64) (*source, error) {
	f, err := t.Open()
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	if e.gen != gen {
		e.mu.Unlock()
		f.Close()
		return nil, ErrAborted
	}
	e.nextFile = f
	e.mu.Unlock()
	return e.newSource(t, f, normalize, level)
}

func (e *Engine) newSource(t *Track, f *File, normalize bool, level float64) (*source, error) {
	dec, err := openDecoder(f, t.Duration.Seconds())
	if err != nil {
		f.Close()
		return nil, err
	}
	src := &source{track: t, file: f, dec: dec, gain: 1, limit: 1}
	src.rs = newResampler(dec.SampleRate(), e.rate)
	// One read of the decoder is 4096 frames at its rate, and the
	// resampler's tail at the end of the track.
	src.need = int(4096*float64(e.rate)/float64(dec.SampleRate())) + 256
	if src.need > ringFrames {
		f.Close()
		return nil, fmt.Errorf("audio: a sample rate of %d Hz is too low to play", dec.SampleRate())
	}
	// A track the server has not measured plays as it is: there is no
	// telling how loud it is.
	if normalize && t.GainDB != 0 {
		src.gain = float32(math.Pow(10, (t.GainDB+level)/20))
	}
	return src, nil
}

func (e *Engine) seekSource(src *source, to time.Duration) error {
	if err := src.dec.SeekFrame(int64(to.Seconds() * float64(src.dec.SampleRate()))); err != nil {
		return err
	}
	if src.rs != nil {
		src.rs.reset()
	}
	return nil
}

// fail tells that a track could not play, unless a newer request made it
// moot.
func (e *Engine) fail(id string, gen uint64, err error) {
	if errors.Is(err, ErrAborted) {
		return
	}
	e.mu.Lock()
	current := e.gen == gen
	e.mu.Unlock()
	if current {
		e.emit(Event{Kind: Failed, TrackID: id, Err: err})
	}
}

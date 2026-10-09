package audio

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

const testRate = 44100

// toneFLAC is a FLAC file of a sine of freq Hz, secs long, in both channels.
func toneFLAC(t *testing.T, freq, secs float64, amp float64) []byte {
	t.Helper()
	n := int(secs * testRate)
	var buf bytes.Buffer
	info := &meta.StreamInfo{BlockSizeMin: 4096, BlockSizeMax: 4096, SampleRate: testRate, NChannels: 2, BitsPerSample: 16, NSamples: uint64(n)}
	enc, err := flac.NewEncoder(&buf, info)
	if err != nil {
		t.Fatal(err)
	}
	for at := 0; at < n; at += 4096 {
		size := min(4096, n-at)
		samples := make([]int32, size)
		for i := range samples {
			samples[i] = int32(amp * 32767 * math.Sin(2*math.Pi*freq*float64(at+i)/testRate))
		}
		// Not shared: the encoder keeps what it is given.
		right := append([]int32(nil), samples...)
		f := &frame.Frame{
			Header: frame.Header{HasFixedBlockSize: true, BlockSize: uint16(size), SampleRate: testRate, Channels: frame.ChannelsLR, BitsPerSample: 16},
			Subframes: []*frame.Subframe{
				{SubHeader: frame.SubHeader{Pred: frame.PredVerbatim}, Samples: samples, NSamples: size},
				{SubHeader: frame.SubHeader{Pred: frame.PredVerbatim}, Samples: right, NSamples: size},
			},
		}
		if err := enc.WriteFrame(f); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// collector is a sound card that keeps what it is given: it takes the
// engine's sound a little faster than real time.
type collector struct {
	e       *Engine
	mu      sync.Mutex
	playing bool
	got     []float32
	once    sync.Once
	stop    chan struct{}
}

func (c *collector) Play() {
	c.mu.Lock()
	c.playing = true
	c.mu.Unlock()
	c.once.Do(func() { go c.loop() })
}
func (c *collector) Pause() { c.mu.Lock(); c.playing = false; c.mu.Unlock() }
func (c *collector) IsPlaying() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.playing
}
func (c *collector) Seek(int64, int) (int64, error) { return 0, nil }
func (c *collector) SetVolume(float64)              {}
func (c *collector) BufferedSize() int              { return 0 }
func (c *collector) SetBufferSize(int)              {}

// ready tells that the engine has sound to give: a card that takes it only
// then hears no gaps, whatever the speed of the machine.
func (c *collector) ready() bool {
	c.e.mu.Lock()
	defer c.e.mu.Unlock()
	n := len(c.e.segs)
	return c.e.rlen >= 1024 || (n > 0 && c.e.segs[n-1].final && c.e.rlen > 0)
}

func (c *collector) loop() {
	buf := make([]byte, 8*1024)
	for {
		select {
		case <-c.stop:
			return
		default:
		}
		if !c.IsPlaying() || !c.ready() {
			time.Sleep(time.Millisecond)
			continue
		}
		c.e.read(buf)
		samples := unsafe.Slice((*float32)(unsafe.Pointer(&buf[0])), len(buf)/4)
		c.mu.Lock()
		c.got = append(c.got, samples...)
		c.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
}

// testEngine is an engine on a collector, with tracks to play.
type testEngine struct {
	*Engine
	sink   *collector
	events chan Event
	cache  *Cache
}

func newTestEngine(t *testing.T) *testEngine {
	t.Helper()
	te := &testEngine{events: make(chan Event, 64)}
	te.Engine = newEngine(testRate, func(ev Event) { te.events <- ev })
	te.sink = &collector{e: te.Engine, stop: make(chan struct{})}
	te.Engine.player = te.sink
	te.Engine.start()
	var err error
	if te.cache, err = NewCache(t.TempDir(), 0, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(te.sink.stop); te.Engine.Close() })
	return te
}

// track makes a track of a tone.
func (te *testEngine) track(t *testing.T, id string, freq, secs float64, noFade bool) *Track {
	t.Helper()
	path := filepath.Join(t.TempDir(), id+".flac")
	if err := os.WriteFile(path, toneFLAC(t, freq, secs, 0.5), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := te.cache.Import(id, path); err != nil {
		t.Fatal(err)
	}
	return &Track{
		ID: id, Duration: time.Duration(secs * float64(time.Second)), NoFade: noFade,
		Open: func() (*File, error) {
			return te.cache.Open(id, func(context.Context) (*http.Request, error) { return nil, errors.New("not here") })
		},
	}
}

// played plays a then b, with the crossfade asked for, and returns the
// sound, without the silence before and after it, and the events.
func (te *testEngine) played(t *testing.T, a, b *Track, xfade time.Duration) ([]float32, []Event) {
	t.Helper()
	te.SetCrossfade(xfade)
	te.Play(a, 0)
	te.SetNext(b)
	var evs []Event
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev := <-te.events:
			evs = append(evs, ev)
			if ev.Kind == Ended {
				te.sink.mu.Lock()
				got := append([]float32(nil), te.sink.got...)
				te.sink.mu.Unlock()
				first, last := 0, len(got)/2
				for first < last && got[2*first] == 0 && got[2*first+1] == 0 {
					first++
				}
				for last > first && got[2*last-2] == 0 && got[2*last-1] == 0 {
					last--
				}
				return got[2*first : 2*last], evs
			}
			if ev.Kind == Failed {
				t.Fatalf("a track failed: %v", ev.Err)
			}
		case <-deadline:
			t.Fatal("the tracks did not end")
		}
	}
}

// amplitude is how strong a tone of freq Hz is in the frames from to to of
// the sound, as the peak of a sine of that strength.
func amplitude(snd []float32, freq float64, from, to int) float64 {
	var re, im float64
	for i := from; i < to; i++ {
		ph := 2 * math.Pi * freq * float64(i) / testRate
		v := float64(snd[2*i])
		re += v * math.Cos(ph)
		im += v * math.Sin(ph)
	}
	return 2 * math.Hypot(re, im) / float64(to-from)
}

func maxStep(snd []float32) float64 {
	m := 0.0
	for i := 1; i < len(snd)/2; i++ {
		m = math.Max(m, math.Abs(float64(snd[2*i]-snd[2*i-2])))
	}
	return m
}

func TestCrossfade(t *testing.T) {
	te := newTestEngine(t)
	a, b := te.track(t, "a", 440, 5, false), te.track(t, "b", 880, 5, false)
	snd, evs := te.played(t, a, b, 2*time.Second)
	frames := len(snd) / 2
	// Two tracks of five seconds, two of them heard together.
	if want := 8 * testRate; math.Abs(float64(frames-want)) > testRate/10 {
		t.Errorf("the sound is %.2f s, want 8", float64(frames)/testRate)
	}
	win := 4096
	at := func(sec float64) (low, high float64) {
		from := int(sec*testRate) - win/2
		return amplitude(snd, 440, from, from+win), amplitude(snd, 880, from, from+win)
	}
	// Before the crossfade only the first, after it only the second.
	if low, high := at(1); low < 0.45 || high > 0.03 {
		t.Errorf("at 1 s: 440 Hz %.3f, 880 Hz %.3f, want only 440", low, high)
	}
	if low, high := at(7); high < 0.45 || low > 0.03 {
		t.Errorf("at 7 s: 440 Hz %.3f, 880 Hz %.3f, want only 880", low, high)
	}
	// In the middle both, with equal power: each at cos(45°) of its level.
	if low, high := at(4); math.Abs(low-0.354) > 0.05 || math.Abs(high-0.354) > 0.05 {
		t.Errorf("at 4 s: 440 Hz %.3f, 880 Hz %.3f, want both about 0.354", low, high)
	}
	// The first goes down and the second comes up.
	l1, h1 := at(3.3)
	l2, h2 := at(4.7)
	if !(l1 > l2 && h1 < h2) {
		t.Errorf("not crossing: at 3.3 s %.3f/%.3f, at 4.7 s %.3f/%.3f", l1, h1, l2, h2)
	}
	if step := maxStep(snd); step > 0.2 {
		t.Errorf("a jump of %.3f between two samples: a click", step)
	}
	for _, v := range snd {
		if v > 1 || v < -1 {
			t.Fatalf("a sample of %v is over full scale", v)
		}
	}
	var changed, ended []string
	for _, ev := range evs {
		switch ev.Kind {
		case TrackChanged:
			changed = append(changed, ev.TrackID)
		case Ended:
			ended = append(ended, ev.TrackID)
		}
	}
	if len(changed) != 1 || changed[0] != "b" || len(ended) != 1 || ended[0] != "b" {
		t.Errorf("the events were changed %v, ended %v", changed, ended)
	}
}

func TestNoCrossfadeIsGapless(t *testing.T) {
	te := newTestEngine(t)
	a, b := te.track(t, "a", 440, 4, false), te.track(t, "b", 880, 4, false)
	snd, _ := te.played(t, a, b, 0)
	if want := 8 * testRate; math.Abs(float64(len(snd)/2-want)) > testRate/10 {
		t.Errorf("the sound is %.2f s, want 8", float64(len(snd)/2)/testRate)
	}
	if low := amplitude(snd, 440, 3*testRate, 3*testRate+4096); low < 0.45 {
		t.Errorf("the first track is %.3f at 3 s", low)
	}
	if high, low := amplitude(snd, 880, 5*testRate, 5*testRate+4096), amplitude(snd, 440, 5*testRate, 5*testRate+4096); high < 0.45 || low > 0.03 {
		t.Errorf("at 5 s: 880 Hz %.3f, 440 Hz %.3f", high, low)
	}
}

func TestSongsOfAnAlbumAreNotFaded(t *testing.T) {
	te := newTestEngine(t)
	a, b := te.track(t, "a", 440, 4, false), te.track(t, "b", 880, 4, true)
	snd, _ := te.played(t, a, b, 2*time.Second)
	if want := 8 * testRate; math.Abs(float64(len(snd)/2-want)) > testRate/10 {
		t.Errorf("the sound is %.2f s, want 8: no overlap", float64(len(snd)/2)/testRate)
	}
	// The last second of the first is still the first alone.
	if low, high := amplitude(snd, 440, 3*testRate+8000, 3*testRate+8000+4096), amplitude(snd, 880, 3*testRate+8000, 3*testRate+8000+4096); low < 0.45 || high > 0.03 {
		t.Errorf("the end of the first: 440 Hz %.3f, 880 Hz %.3f", low, high)
	}
}

// A crossfade longer than a track does not hang or click: the whole of the
// short track is the zone.
func TestCrossfadeLongerThanTheTrack(t *testing.T) {
	te := newTestEngine(t)
	a, b := te.track(t, "a", 440, 1.5, false), te.track(t, "b", 880, 4, false)
	snd, _ := te.played(t, a, b, 4*time.Second)
	// The first is all overlap: 1.5 s of the second under it, 4 s in all.
	if frames := len(snd) / 2; math.Abs(float64(frames-4*testRate)) > testRate/10 {
		t.Errorf("the sound is %.2f s, want 4", float64(frames)/testRate)
	}
	if step := maxStep(snd); step > 0.2 {
		t.Errorf("a jump of %.3f between two samples", step)
	}
}

package audio

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testFile serves the FLAC file that AURELIA_TEST_FLAC names, a little at
// a time, as a track downloading, and returns a way to open it in a cache.
func testFile(t *testing.T, bytesPerSecond int) func() (*File, error) {
	t.Helper()
	path := os.Getenv("AURELIA_TEST_FLAC")
	if path == "" {
		t.Skip("AURELIA_TEST_FLAC names no file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", itoa(len(data)))
		if bytesPerSecond <= 0 {
			w.Write(data)
			return
		}
		chunk := bytesPerSecond / 50
		for off := 0; off < len(data); off += chunk {
			if _, err := w.Write(data[off:min(off+chunk, len(data))]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}))
	t.Cleanup(srv.Close)
	cache, err := NewCache(t.TempDir(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	return func() (*File, error) {
		return cache.Open("track", func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func decodeAll(t *testing.T, d decoder) []float32 {
	t.Helper()
	var all []float32
	buf := make([]float32, 8192)
	for {
		n, err := d.Read(buf)
		all = append(all, buf[:2*n]...)
		if err == io.EOF {
			return all
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// A seek lands on the very sample asked for, wherever it is, in a file
// that is still downloading.
func TestFLACSeeksToTheSample(t *testing.T) {
	open := testFile(t, 0)
	f, err := open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := openDecoder(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	all := decodeAll(t, d)
	frames := int64(len(all) / 2)
	secs := float64(frames) / float64(d.SampleRate())
	t.Logf("decoded %d frames (%.1f s of sound) in %v: %.0f× real time", frames, secs, time.Since(start), secs/time.Since(start).Seconds())
	if d.Len() != frames {
		t.Errorf("the file says %d frames, decoded %d", d.Len(), frames)
	}
	buf := make([]float32, 2*1000)
	for _, at := range []int64{frames / 2, 1, frames - 500, frames / 10, 0, frames * 9 / 10, 4096, 4095, frames / 3} {
		start := time.Now()
		if err := d.SeekFrame(at); err != nil {
			t.Fatalf("seek to %d: %v", at, err)
		}
		n, err := d.Read(buf)
		if err != nil {
			t.Fatalf("read at %d: %v", at, err)
		}
		for i := 0; i < 2*n; i++ {
			if buf[i] != all[2*at+int64(i)] {
				t.Fatalf("after a seek to %d, sample %d is %v, not %v", at, i, buf[i], all[2*at+int64(i)])
			}
		}
		if took := time.Since(start); took > 100*time.Millisecond {
			t.Errorf("the seek to %d took %v", at, took)
		}
	}
}

// A file plays from its start while the rest downloads, and a seek waits
// only for the part it needs.
func TestFLACPlaysWhileDownloading(t *testing.T) {
	open := testFile(t, 2<<20) // 2 MB a second
	f, err := open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	start := time.Now()
	d, err := openDecoder(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]float32, 8192)
	if _, err := d.Read(buf); err != nil {
		t.Fatal(err)
	}
	// The sound starts as soon as the download reaches it, past the
	// file's pictures and padding.
	dataStart := d.(*flacDecoder).dataStart
	have, size, _ := f.Progress()
	if have > dataStart+512<<10 {
		t.Errorf("the first samples waited for %d bytes; the sound starts at %d", have, dataStart)
	}
	t.Logf("first samples after %v, %d bytes in (sound starts at %d)", time.Since(start), have, dataStart)
	// A fifth into the track needs about a fifth of the file.
	if err := d.SeekFrame(d.Len() / 5); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read(buf); err != nil {
		t.Fatal(err)
	}
	have, _, done := f.Progress()
	want := dataStart + (size-dataStart)/5
	if done || have > want+(size-dataStart)/8 {
		t.Errorf("a seek a fifth into the track waited for %d of %d bytes; about %d would do", have, size, want)
	}
	t.Logf("seek to 20%% served after %v with %d%% downloaded", time.Since(start), 100*have/size)
}

// An aborted read fails at once and the file reads again after Resume.
func TestAbort(t *testing.T) {
	open := testFile(t, 256<<10)
	f, err := open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	go func() {
		time.Sleep(50 * time.Millisecond)
		f.Abort()
	}()
	f.Seek(8<<20, io.SeekStart) // far from downloaded
	buf := make([]byte, 16)
	if _, err := f.Read(buf); err != ErrAborted {
		t.Fatalf("the read returned %v", err)
	}
	f.Resume()
	f.Seek(0, io.SeekStart)
	if _, err := io.ReadFull(f, buf[:4]); err != nil || string(buf[:4]) != "fLaC" {
		t.Fatalf("after Resume: %q, %v", buf[:4], err)
	}
}

// A sine keeps its frequency and its level through the resampler.
func TestResampler(t *testing.T) {
	for _, rates := range [][2]int{{48000, 44100}, {96000, 44100}, {22050, 44100}, {192000, 48000}, {44100, 48000}} {
		in, out := rates[0], rates[1]
		r := newResampler(in, out)
		const freq = 1000.0
		src := make([]float32, 2*in) // a second
		for i := 0; i < in; i++ {
			v := float32(0.5 * math.Sin(2*math.Pi*freq*float64(i)/float64(in)))
			src[2*i], src[2*i+1] = v, -v
		}
		var got []float32
		for off := 0; off < len(src); off += 2 * 1000 { // in pieces, as a stream
			got = r.process(src[off:min(off+2*1000, len(src))], got)
		}
		got = r.flush(got)
		frames := len(got) / 2
		if d := frames - out; d < -2 || d > 2*r.half {
			t.Errorf("%d→%d: %d frames for a second", in, out, frames)
		}
		// Compare with the sine at the new rate, past the filter's start.
		var worst float64
		for i := 200; i < out-200; i++ {
			want := 0.5 * math.Sin(2*math.Pi*freq*float64(i)/float64(out))
			worst = math.Max(worst, math.Abs(float64(got[2*i])-want))
			worst = math.Max(worst, math.Abs(float64(got[2*i+1])+want))
		}
		if worst > 0.002 {
			t.Errorf("%d→%d: off by %.5f", in, out, worst)
		}
	}
	if newResampler(44100, 44100) != nil {
		t.Error("a resampler for one rate")
	}
}

// The engine plays on the real sound card, when asked: a track, a seek,
// and the next track where the first ends.
func TestEngineOnTheDevice(t *testing.T) {
	if os.Getenv("AURELIA_TEST_DEVICE") == "" {
		t.Skip("AURELIA_TEST_DEVICE is not set")
	}
	open := testFile(t, 4<<20)
	events := make(chan Event, 8)
	e, err := NewEngine(44100, "", func(ev Event) { events <- ev })
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.SetVolume(0) // silent: the test watches the clock, not the sound
	dur := 185 * time.Second
	a := &Track{ID: "a", Open: open, Duration: dur}
	b := &Track{ID: "b", Open: open, Duration: dur}
	e.Play(a, 0)
	if st := e.State(); st.TrackID != "a" {
		t.Fatalf("state %+v right after Play", st)
	}
	// The sound starts once the download reaches it; from then on the
	// position follows the clock.
	for deadline := time.Now().Add(5 * time.Second); e.State().Position == 0; {
		if time.Now().After(deadline) {
			t.Fatalf("nothing played; state %+v", e.State())
		}
		time.Sleep(5 * time.Millisecond)
	}
	p0 := e.State().Position
	time.Sleep(time.Second)
	st := e.State()
	t.Logf("a second later: %+v", st)
	if d := st.Position - p0; d < 900*time.Millisecond || d > 1100*time.Millisecond || st.Buffering {
		t.Errorf("the position advanced %v in a second", d)
	}
	e.Pause()
	p1 := e.State().Position
	time.Sleep(300 * time.Millisecond)
	if p2 := e.State().Position; p2 != p1 {
		t.Errorf("paused, the position went from %v to %v", p1, p2)
	}
	e.Resume()
	// Near the end, with another track to follow.
	e.SetNext(b)
	e.SeekTo(dur - 2*time.Second)
	if st := e.State(); st.Position < dur-2100*time.Millisecond {
		t.Errorf("position %v right after the seek", st.Position)
	}
	select {
	case ev := <-events:
		if ev.Kind != TrackChanged || ev.TrackID != "b" {
			t.Fatalf("event %+v", ev)
		}
	case <-time.After(6 * time.Second):
		t.Fatalf("no track change; state %+v", e.State())
	}
	time.Sleep(700 * time.Millisecond)
	st = e.State()
	t.Logf("in the next track: %+v", st)
	if st.TrackID != "b" || st.Position < 400*time.Millisecond || st.Position > 1200*time.Millisecond {
		t.Errorf("state %+v after the change", st)
	}
	// And the end of the last track.
	e.SeekTo(dur - time.Second)
	select {
	case ev := <-events:
		if ev.Kind != Ended {
			t.Fatalf("event %+v", ev)
		}
	case <-time.After(6 * time.Second):
		t.Fatalf("no end; state %+v", e.State())
	}
	if st := e.State(); st.TrackID != "" {
		t.Errorf("state %+v after the end", st)
	}
}

// A file copied into a cache is whole there; a removed one is gone.
func TestImportAndRemove(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "song.flac")
	if err := os.WriteFile(src, []byte("fLaC and the rest"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := NewCache(filepath.Join(dir, "cache"), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Has("a") || c.Path("a") != "" {
		t.Error("an empty cache has the file")
	}
	if err := c.Import("a", src); err != nil {
		t.Fatal(err)
	}
	if !c.Has("a") || c.Path("a") == "" || len(c.Keys()) != 1 || c.Size() != 17 {
		t.Errorf("after Import: has %v, keys %v, size %d", c.Has("a"), c.Keys(), c.Size())
	}
	f, err := c.Open("a", nil) // whole: no request is made
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Wait(); err != nil {
		t.Error(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "fLaC and the rest" {
		t.Errorf("read %q", b)
	}
	if err := c.Remove("a"); err != nil || c.Has("a") {
		t.Errorf("after Remove: %v, has %v", err, c.Has("a"))
	}
	if err := c.Remove("a"); err != nil {
		t.Errorf("removing what is not there: %v", err)
	}
}

// A gain that would take a track over full scale is held under it, and
// let back once the loud moment is past; a gain under 1 only scales.
func TestAmplifyLimits(t *testing.T) {
	s := &source{gain: 2, limit: 1}
	buf := make([]float32, 2*44100)
	for i := 0; i < len(buf); i += 2 {
		v := float32(0.1)
		if i < 2000 {
			v = 0.9 // a loud start: 1.8 after the gain
		}
		buf[i], buf[i+1] = v, -v
	}
	s.amplify(buf)
	for i, v := range buf {
		if v > 1 || v < -1 {
			t.Fatalf("sample %d is %v, over full scale", i, v)
		}
	}
	if got := buf[0]; got < 0.9 || got > limiterCeiling+0.001 {
		t.Errorf("the loud start plays at %v, want just under full scale", got)
	}
	// Long after, the quiet part has its whole gain back.
	if got := buf[len(buf)-2]; got < 0.19 || got > 0.2001 {
		t.Errorf("the quiet end plays at %v, want 0.2", got)
	}
	quiet := &source{gain: 0.5, limit: 1}
	half := []float32{0.8, -0.4}
	quiet.amplify(half)
	if half[0] != 0.4 || half[1] != -0.2 {
		t.Errorf("a gain of a half gave %v", half)
	}
}

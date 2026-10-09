package audio

import (
	"math"
	"testing"
)

// sine is n frames of a sine at f Hz, the same in both channels.
func sine(f, rate float64, n int, amp float64) []float32 {
	buf := make([]float32, 2*n)
	for i := 0; i < n; i++ {
		v := float32(amp * math.Sin(2*math.Pi*f*float64(i)/rate))
		buf[2*i], buf[2*i+1] = v, v
	}
	return buf
}

// level is the RMS of the left channel from frame skip on, in dB.
func level(buf []float32, skip int) float64 {
	var sum float64
	n := 0
	for i := skip; 2*i < len(buf); i++ {
		v := float64(buf[2*i])
		sum += v * v
		n++
	}
	return 10 * math.Log10(sum/float64(n))
}

func TestEQGainAtBand(t *testing.T) {
	const rate = 44100
	for i, f := range EQBands {
		for _, g := range []float64{-9, 6} {
			var fx Effects
			fx.EQ = true
			fx.Gains[i] = g
			var c chain
			c.set(fx, rate)
			// The headroom turns everything down by the boost: measured
			// against that.
			want := g + fx.Headroom()
			in := sine(f, rate, rate, 0.1)
			before := level(in, 8000)
			c.process(in)
			got := level(in, 8000) - before
			if math.Abs(got-want) > 0.15 {
				t.Errorf("band %.0f Hz at %+.0f dB: the sound changed by %.2f dB, want %.2f", f, g, got, want)
			}
		}
	}
}

func TestEQLeavesOtherBandsAlone(t *testing.T) {
	const rate = 48000
	var fx Effects
	fx.EQ = true
	fx.Gains[2] = -12 // 125 Hz
	var c chain
	c.set(fx, rate)
	in := sine(8000, rate, rate, 0.1)
	before := level(in, 8000)
	c.process(in)
	if got := level(in, 8000) - before; math.Abs(got) > 0.5 {
		t.Errorf("a cut at 125 Hz changed 8 kHz by %.2f dB", got)
	}
}

func TestEQFlatAndOffChangeNothing(t *testing.T) {
	for name, fx := range map[string]Effects{
		"zero":         {},
		"flat":         {EQ: true},
		"off, a curve": {Gains: [10]float64{6, 6, 6, 6, 6, 6, 6, 6, 6, 6}},
	} {
		var c chain
		c.set(fx, 44100)
		in := sine(440, 44100, 4096, 0.5)
		out := append([]float32(nil), in...)
		c.process(out)
		for i := range in {
			if in[i] != out[i] {
				t.Errorf("%s: sample %d changed from %v to %v", name, i, in[i], out[i])
				break
			}
		}
	}
}

func TestEQDoesNotClip(t *testing.T) {
	var fx Effects
	fx.EQ = true
	fx.Preamp = 12
	for i := range fx.Gains {
		fx.Gains[i] = 12
	}
	var c chain
	c.set(fx, 44100)
	in := sine(1000, 44100, 44100, 0.95)
	c.process(in)
	for i, v := range in {
		if math.Abs(float64(v)) > 1 {
			t.Fatalf("sample %d is %v, over full scale", i, v)
		}
	}
}

func TestMonoAndBalance(t *testing.T) {
	var c chain
	c.set(Effects{Mono: true}, 44100)
	buf := []float32{1, 0, 0, 1, 0.5, 0.25}
	c.process(buf)
	for i := 0; i < len(buf); i += 2 {
		if buf[i] != buf[i+1] {
			t.Errorf("mono: frame %d is %v and %v", i/2, buf[i], buf[i+1])
		}
	}
	if buf[0] != 0.5 {
		t.Errorf("mono of 1 and 0 is %v, want 0.5", buf[0])
	}
	c.set(Effects{Balance: 1}, 44100)
	buf = []float32{0.5, 0.5}
	c.process(buf)
	if buf[0] != 0 || buf[1] != 0.5 {
		t.Errorf("balance right: %v, want the left channel silent", buf)
	}
	c.set(Effects{Balance: -0.5}, 44100)
	buf = []float32{0.8, 0.8}
	c.process(buf)
	if buf[0] != 0.8 || math.Abs(float64(buf[1])-0.4) > 1e-6 {
		t.Errorf("balance a half left: %v, want 0.8 and 0.4", buf)
	}
}

// A slider moved while music plays must not click: the filter states stay.
func TestEQMovingABandIsSmooth(t *testing.T) {
	const rate = 44100
	var fx Effects
	fx.EQ = true
	fx.Gains[5] = 3
	var c chain
	c.set(fx, rate)
	in := sine(1000, rate, 2*rate, 0.3)
	c.process(in[:rate])
	fx.Gains[5] = 3.5
	c.set(fx, rate)
	c.process(in[rate:])
	worst := 0.0
	for i := rate - 4; i < rate+4; i++ {
		worst = math.Max(worst, math.Abs(float64(in[2*i+2]-in[2*i])))
	}
	if worst > 0.12 { // a 1 kHz sine at 0.3 moves at most ~0.04 per frame
		t.Errorf("a step of %.3f between frames as a band moved", worst)
	}
}

func BenchmarkEffects(b *testing.B) {
	var fx Effects
	fx.EQ = true
	for i := range fx.Gains {
		fx.Gains[i] = float64(i%5) - 2
	}
	var c chain
	c.set(fx, 44100)
	buf := sine(440, 44100, 1024, 0.3)
	b.SetBytes(int64(len(buf)) * 4)
	for i := 0; i < b.N; i++ {
		c.process(buf)
	}
}

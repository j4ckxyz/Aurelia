package audio

import (
	"math"
	"testing"
)

// earLevel plays a sine of f Hz, as l and r make it in either channel, through
// spatial audio, and returns the level of each ear after it settles, in
// dB against the sine's.
func earLevel(t *testing.T, f, rate, l, r float64) (left, right float64) {
	return earLevelOf(t, f, rate, l, r, false)
}

func earLevelOf(t *testing.T, f, rate, l, r float64, dry bool) (left, right float64) {
	t.Helper()
	var s spatial
	s.set(rate)
	s.dry = dry
	n := int(rate) // a second: the room's tail has long settled
	buf := make([]float32, 2*n)
	for i := range n {
		v := math.Sin(2 * math.Pi * f * float64(i) / rate)
		buf[2*i], buf[2*i+1] = float32(v*l), float32(v*r)
	}
	s.process(buf)
	var el, er, ein float64
	for i := n / 2; i < n; i++ {
		v := math.Sin(2 * math.Pi * f * float64(i) / rate)
		el += float64(buf[2*i]) * float64(buf[2*i])
		er += float64(buf[2*i+1]) * float64(buf[2*i+1])
		ein += v * v
	}
	db := func(e float64) float64 { return 10 * math.Log10(e/ein) }
	return db(el), db(er)
}

// A voice in the middle keeps its level, and its tone within what two
// speakers in a room color it by.
func TestSpatialKeepsTheMiddle(t *testing.T) {
	for _, rate := range []float64{44100, 48000, 96000} {
		centers, db := bands(rate)
		sum := 0.0
		for i, f := range centers {
			if testing.Verbose() {
				t.Logf("%5.0f Hz at %5.0f: %+.1f dB", f, rate, db[i])
			}
			sum += db[i]
			if math.Abs(db[i]) > 3 {
				t.Errorf("%v Hz at %v: the middle comes out at %+.1f dB", f, rate, db[i])
			}
		}
		if avg := sum / float64(len(db)); math.Abs(avg) > 1 {
			t.Errorf("at %v the middle is %+.1f dB louder", rate, avg)
		}
	}
}

// bands plays noise, alike in both channels, through spatial audio with
// its room, and returns how much louder or quieter each third of an
// octave comes out, in dB, as an ear hears the tone of music.
func bands(rate float64) (centers, db []float64) {
	centers, db, _ = bandsOf(rate, 1, 1)
	return centers, db
}

// bandsOf is bands of noise played in the channels as l and r make it,
// at both ears.
func bandsOf(rate, gl, gr float64) (centers, left, right []float64) {
	var s spatial
	s.set(rate)
	const n = 1 << 16
	in := make([]float64, 2*n)
	seed := uint32(1)
	for i := range in {
		seed = seed*1664525 + 1013904223
		in[i] = float64(int32(seed)) / (1 << 31) * 0.5
	}
	buf := make([]float32, 2*len(in))
	for i, v := range in {
		buf[2*i], buf[2*i+1] = float32(v*gl), float32(v*gr)
	}
	s.process(buf)
	// Past the first part, where the room fills.
	x := in[n:]
	yl, yr := make([]float64, len(x)), make([]float64, len(x))
	for i := range yl {
		yl[i], yr[i] = float64(buf[2*(n+i)]), float64(buf[2*(n+i)+1])
	}
	X, L, R := dft(x), dft(yl), dft(yr)
	for f := 125.0; f <= 12500; f *= math.Pow(2, 1.0/3) {
		lo, hi := f/math.Pow(2, 1.0/6), f*math.Pow(2, 1.0/6)
		var ex, el, er float64
		for k := int(lo / rate * float64(len(x))); k <= int(hi/rate*float64(len(x))); k++ {
			ex += X[k]
			el += L[k]
			er += R[k]
		}
		centers = append(centers, f)
		left = append(left, 10*math.Log10(el/ex))
		right = append(right, 10*math.Log10(er/ex))
	}
	return centers, left, right
}

// dft returns the power of each frequency of x, whose length is a power
// of two.
func dft(x []float64) []float64 {
	n := len(x)
	re, im := make([]float64, n), make([]float64, n)
	copy(re, x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		ang := -2 * math.Pi / float64(size)
		for start := 0; start < n; start += size {
			for k := range size / 2 {
				c, s := math.Cos(ang*float64(k)), math.Sin(ang*float64(k))
				a, b := start+k, start+k+size/2
				tr, ti := re[b]*c-im[b]*s, re[b]*s+im[b]*c
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a], im[a] = re[a]+tr, im[a]+ti
			}
		}
	}
	p := make([]float64, n/2)
	for i := range p {
		p[i] = re[i]*re[i] + im[i]*im[i]
	}
	return p
}

// What plays on the left alone is heard by both ears, as from speakers,
// the left more; the bass passes as it is.
func TestSpatialSpeakers(t *testing.T) {
	const rate = 48000
	centers, l, r := bandsOf(rate, 1, 0)
	var dl, dr float64
	n := 0
	for i, f := range centers {
		if testing.Verbose() {
			t.Logf("left alone, %5.0f Hz: %+.1f dB, %+.1f dB", f, l[i], r[i])
		}
		if f < 500 {
			continue
		}
		dl += l[i]
		dr += r[i]
		n++
		if r[i] < -30 {
			t.Errorf("left alone, %v Hz: the right ear hears nothing, %+.1f dB", f, r[i])
		}
	}
	dl, dr = dl/float64(n), dr/float64(n)
	if dl-dr < 3 || dl < -4 || dl > 3 {
		// Heard as from speakers: louder on its side, both ears hearing it.
		t.Errorf("left alone: the left ear at %+.1f dB, the right at %+.1f dB", dl, dr)
	}
	if l, r := earLevel(t, 40, rate, 1, 0); math.Abs(l) > 1 || r > -20 {
		t.Errorf("the bass on the left is at %+.1f dB and %+.1f dB", l, r)
	}
	// What differs between the channels is heard by both ears.
	if l, r := earLevel(t, 1000, rate, 1, -1); l < -15 || r < -15 {
		t.Errorf("the channels against each other are at %+.1f dB and %+.1f dB", l, r)
	}
}

// Silence stays silence, and the loudest music stays a number.
func TestSpatialSafe(t *testing.T) {
	var s spatial
	s.set(44100)
	buf := make([]float32, 8192)
	s.process(buf)
	for _, v := range buf {
		if v != 0 {
			t.Fatal("silence made a sound")
		}
	}
	for range 50 {
		for i := range buf {
			buf[i] = 1
			if i%3 == 0 {
				buf[i] = -1
			}
		}
		s.process(buf)
		for _, v := range buf {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || math.Abs(float64(v)) > 4 {
				t.Fatalf("full scale made %v", v)
			}
		}
	}
	s.reset()
	clear(buf)
	s.process(buf)
	for _, v := range buf {
		if v != 0 {
			t.Fatal("after a reset, the room still rang")
		}
	}
}

// Through the chain, spatial audio stays under full scale, and mono
// leaves it out.
func TestSpatialChain(t *testing.T) {
	var c chain
	c.set(Effects{Spatial: true}, 48000)
	buf := make([]float32, 2*48000)
	for i := 0; i < len(buf); i += 2 {
		v := float32(0.98 * math.Sin(2*math.Pi*700*float64(i/2)/48000))
		buf[i], buf[i+1] = v, -v
	}
	c.process(buf)
	for _, v := range buf {
		if math.Abs(float64(v)) > limitCeiling*1.02 {
			t.Fatalf("spatial audio went to %v", v)
		}
	}
	c.set(Effects{Spatial: true, Mono: true}, 48000)
	in := []float32{0.5, 0.1, 0.5, 0.1}
	c.process(in)
	if in[0] != in[1] || math.Abs(float64(in[0])-0.3) > 1e-6 {
		t.Errorf("mono with spatial audio made %v", in)
	}
}

func BenchmarkSpatial(b *testing.B) {
	var s spatial
	s.set(48000)
	buf := make([]float32, 2*1024)
	for i := range buf {
		buf[i] = float32(math.Sin(float64(i) * 0.01))
	}
	b.SetBytes(int64(len(buf) * 4))
	for b.Loop() {
		s.process(buf)
	}
}

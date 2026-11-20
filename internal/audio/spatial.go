package audio

import (
	"math"
	"math/cmplx"
	"sync"
)

// Spatial audio for headphones: the music as if it played from speakers
// around the listener in a small room, rather than from inside the head,
// in the manner of the spatial audio of Apple's AirPods. Apple's own
// renderer takes Dolby Atmos' objects and a head tracked by the AirPods;
// a stereo song has neither, and Apple's "spatialize stereo" does what is
// done here:
//
//   - The bass, under 120 Hz, which the ears do not place, passes as it
//     is: no comb filters muddy it.
//   - The rest plays from four virtual speakers: the left and right
//     channels from in front, 30° to either side, as a stereo pair of
//     speakers would; and each again, quieter, a little later and darker,
//     from behind on its side, 110° off, through allpass chains unlike on
//     either side: the music around the listener, as surround speakers
//     fill a room, while what is in front stays there, heard first. (What
//     is on the left alone is never heard from the right, as it would be
//     from a matrix of the channels' difference.)
//   - Each speaker is heard by both ears as through a head: the near ear
//     brighter, the far one later (by the time sound takes around the
//     head, Woodworth's formula) and darker (the head's shadow, Brown and
//     Duda's spherical model).
//   - A few early reflections, and a short, quiet tail, place it all in a
//     room, out of the head.
//   - The level and tone of a voice in the middle are kept as they were,
//     the same with or without spatial audio.
//
// Every step is a few multiplications a sample: it costs next to nothing
// of a processor.

const (
	spatialCrossover = 120.0  // Hz: under it, the sound passes as it is
	headRadius       = 0.0875 // m, an average head
	speedOfSound     = 343.0  // m/s
	frontAngle       = 30.0   // degrees off center of the front speakers
	rearAngle        = 110.0  // and of the rear ones
	rearLevel        = 0.4    // the speakers behind, linear
	rearDelayMS      = 4.0    // and how much later: the front is heard first
	rearShelfDB      = -4.0   // darker behind: a shelf above 5 kHz
	reflectionLP     = 5000.0 // Hz: walls take the treble
	tailRT60         = 0.32   // s: the room's tail
	tailLevel        = 0.035  // linear: quiet, as the tail rings on
	// toneWeight is how much the tone kept is that of a sound in the
	// middle, which two speakers color with their comb, rather than that
	// of the sound all around, which they do not.
	toneWeight = 0.85
)

// The early reflections of each ear: when, after the sound, and how loud.
var (
	reflectionsL = [4]struct{ ms, db float64 }{{7.1, -19}, {11.3, -22}, {17.9, -25}, {23.3, -28}}
	reflectionsR = [4]struct{ ms, db float64 }{{8.3, -19}, {13.1, -22}, {19.7, -25}, {25.9, -28}}
	// tailLines are the delays of the tail's four lines, in ms: far from
	// multiples of each other, not to ring.
	tailLines = [4]float64{29.7, 37.1, 41.1, 43.7}
)

func sq(x float64) float64 { return x * x }

// onePole is a filter of one pole and one zero.
type onePole struct {
	b0, b1, a1 float64
	x1, y1     float64
}

func (f *onePole) run(x float64) float64 {
	y := f.b0*x + f.b1*f.x1 - f.a1*f.y1
	f.x1, f.y1 = x, y
	return y
}

// response is the filter's gain at the angle w of the unit circle.
func (f *onePole) response(w float64) complex128 {
	z1 := cmplx.Exp(complex(0, -w))
	return (complex(f.b0, 0) + complex(f.b1, 0)*z1) / (1 + complex(f.a1, 0)*z1)
}

// headShadow sets the filter to Brown and Duda's model of the head's
// shadow on an ear theta degrees from where the sound comes from: up to 6
// dB brighter facing it, much darker behind the head.
func (f *onePole) headShadow(theta, rate float64) {
	const alphaMin, thetaMin = 0.1, 150.0
	alpha := (1 + alphaMin/2) + (1-alphaMin/2)*math.Cos(theta/thetaMin*math.Pi)
	w0 := speedOfSound / headRadius
	k := 2 * rate
	d := 2*w0 + k
	f.b0 = (2*w0 + alpha*k) / d
	f.b1 = (2*w0 - alpha*k) / d
	f.a1 = (2*w0 - k) / d
}

// lowpass sets the filter to let through what is under fc, falling 6 dB
// an octave above.
func (f *onePole) lowpass(fc, rate float64) {
	x := math.Exp(-2 * math.Pi * fc / rate)
	f.b0, f.b1, f.a1 = 1-x, 0, -x
}

// filter2 is a biquad of one channel, in transposed direct form II.
type filter2 struct {
	b0, b1, b2, a1, a2 float64
	s1, s2             float64
}

func (f *filter2) run(x float64) float64 {
	y := f.b0*x + f.s1
	f.s1 = f.b1*x - f.a1*y + f.s2
	f.s2 = f.b2*x - f.a2*y
	return y
}

func (f *filter2) response(w float64) complex128 {
	z1, z2 := cmplx.Exp(complex(0, -w)), cmplx.Exp(complex(0, -2*w))
	return (complex(f.b0, 0) + complex(f.b1, 0)*z1 + complex(f.b2, 0)*z2) /
		(1 + complex(f.a1, 0)*z1 + complex(f.a2, 0)*z2)
}

// butterworth sets a filter of Butterworth's, low or high pass at fc;
// two in a row are a Linkwitz–Riley crossover, whose two halves add up to
// the sound as it was.
func (f *filter2) butterworth(high bool, fc, rate float64) {
	w := 2 * math.Pi * fc / rate
	cos, alpha := math.Cos(w), math.Sin(w)/math.Sqrt2 // Q of 1/√2
	a0 := 1 + alpha
	if high {
		f.b0, f.b1, f.b2 = (1+cos)/2/a0, -(1+cos)/a0, (1+cos)/2/a0
	} else {
		f.b0, f.b1, f.b2 = (1-cos)/2/a0, (1-cos)/a0, (1-cos)/2/a0
	}
	f.a1, f.a2 = -2*cos/a0, (1-alpha)/a0
}

// peaking sets a band of an equalizer: gain dB around fc, q wide.
func (f *filter2) peaking(fc, gain, q, rate float64) {
	a := math.Pow(10, gain/40)
	w := 2 * math.Pi * fc / rate
	alpha := math.Sin(w) / (2 * q)
	cos := math.Cos(w)
	a0 := 1 + alpha/a
	f.b0, f.b1, f.b2 = (1+alpha*a)/a0, -2*cos/a0, (1-alpha*a)/a0
	f.a1, f.a2 = -2*cos/a0, (1-alpha/a)/a0
}

// shelf sets a high shelf of gain dB above fc, as in Robert
// Bristow-Johnson's cookbook.
func (f *filter2) shelf(high bool, fc, gain, rate float64) {
	a := math.Pow(10, gain/40)
	w := 2 * math.Pi * fc / rate
	cos, sin := math.Cos(w), math.Sin(w)
	alpha := sin / 2 * math.Sqrt2 // a slope of 1
	sq := 2 * math.Sqrt(a) * alpha
	var a0 float64
	if high {
		a0 = (a + 1) - (a-1)*cos + sq
		f.b0 = a * ((a + 1) + (a-1)*cos + sq) / a0
		f.b1 = -2 * a * ((a - 1) + (a+1)*cos) / a0
		f.b2 = a * ((a + 1) + (a-1)*cos - sq) / a0
		f.a1 = 2 * ((a - 1) - (a+1)*cos) / a0
		f.a2 = ((a + 1) - (a-1)*cos - sq) / a0
	} else {
		a0 = (a + 1) + (a-1)*cos + sq
		f.b0 = a * ((a + 1) - (a-1)*cos + sq) / a0
		f.b1 = 2 * a * ((a - 1) - (a+1)*cos) / a0
		f.b2 = a * ((a + 1) - (a-1)*cos - sq) / a0
		f.a1 = -2 * ((a - 1) + (a+1)*cos) / a0
		f.a2 = ((a + 1) + (a-1)*cos - sq) / a0
	}
}

// delayLine holds the last of a signal, to read it again later, between
// samples too.
type delayLine struct {
	buf []float64
	at  int
}

func newDelay(samples int) delayLine {
	return delayLine{buf: make([]float64, max(2, samples+2))}
}

func (d *delayLine) push(x float64) {
	d.at++
	if d.at == len(d.buf) {
		d.at = 0
	}
	d.buf[d.at] = x
}

// tap reads the signal of n samples ago, n at most the line's length.
func (d *delayLine) tap(n int) float64 {
	i := d.at - n
	if i < 0 {
		i += len(d.buf)
	}
	return d.buf[i]
}

// frac reads the signal of t samples ago, between two samples as a line
// between them.
func (d *delayLine) frac(t float64) float64 {
	n := int(t)
	k := t - float64(n)
	return d.tap(n)*(1-k) + d.tap(n+1)*k
}

func (d *delayLine) clear() { clear(d.buf) }

// allpass is Schroeder's allpass: it passes every frequency as loud as it
// came, and smears its phase, which makes two copies of a sound unlike.
type allpass struct {
	line delayLine
	n    int
	g    float64
}

func (a *allpass) run(x float64) float64 {
	d := a.line.tap(a.n - 1)
	v := x + a.g*d
	a.line.push(v)
	return d - a.g*v
}

// speaker is what a virtual speaker on one side does to the ears: the near
// ear's shadow, and the far ear's, later.
type speaker struct {
	near, far [2]onePole // of the speaker on the left, and on the right
	itd       float64    // how much later the far ear hears it, in samples
	lines     [2]delayLine
}

func (s *speaker) set(angle, rate float64) {
	for i := range 2 {
		s.near[i].headShadow(math.Abs(90-angle), rate)
		s.far[i].headShadow(math.Min(90+angle, 360-90-angle), rate)
	}
	// Woodworth: the way around a sphere, for a sound angle off center
	// to the side (behind, as far to the side as in front).
	lat := angle
	if lat > 90 {
		lat = 180 - lat
	}
	r := lat * math.Pi / 180
	s.itd = headRadius / speedOfSound * (r + math.Sin(r)) * rate
	for i := range 2 {
		if len(s.lines[i].buf) < int(s.itd)+4 {
			s.lines[i] = newDelay(int(s.itd) + 4)
		}
	}
}

// ears plays the speakers on the left and on the right to both ears.
func (s *speaker) ears(left, right float64) (l, r float64) {
	s.lines[0].push(s.far[0].run(left))
	s.lines[1].push(s.far[1].run(right))
	l = s.near[0].run(left) + s.lines[1].frac(s.itd)
	r = s.near[1].run(right) + s.lines[0].frac(s.itd)
	return l, r
}

// response is what the speakers do to a sound that both play alike, at
// one ear, at the angle w.
func (s *speaker) response(w float64) complex128 {
	return s.near[0].response(w) + s.far[1].response(w)*cmplx.Exp(complex(0, -w*s.itd))
}

// spatial is the state of spatial audio for both channels.
type spatial struct {
	rate float64
	// The crossover: the bass of each channel, and the rest.
	low, high [2][2]filter2
	front     speaker
	rear      speaker
	// What goes behind: later, made unlike on the two sides, and darker.
	rearLine  [2]delayLine
	rearDelay int
	diffuse   [2][3]allpass
	shelf     [2]filter2
	// The room.
	room    [2]delayLine // what came in, for the reflections
	reflect [2][4]struct {
		n int
		g float64
	}
	reflectLP [2]onePole
	tail      [4]delayLine
	tailLen   [4]int
	tailGain  [4]float64
	tailDamp  [4]onePole
	// The tone of the middle kept: a gain, and a few bands of an
	// equalizer on what both channels have alike.
	gain float64
	tone [2][]filter2
	// dry leaves the room out, as the tests of the speakers alone do.
	dry bool
}

// set makes the state ready for sound of rate frames a second; all it
// held is forgotten if the rate changed.
func (s *spatial) set(rate float64) {
	if s.rate == rate {
		return
	}
	*s = spatial{rate: rate}
	for ch := range 2 {
		for k := range 2 {
			s.low[ch][k].butterworth(false, spatialCrossover, rate)
			s.high[ch][k].butterworth(true, spatialCrossover, rate)
		}
		s.shelf[ch].shelf(true, 5000, rearShelfDB, rate)
		s.reflectLP[ch].lowpass(reflectionLP, rate)
	}
	s.front.set(frontAngle, rate)
	s.rear.set(rearAngle, rate)
	s.rearDelay = int(rearDelayMS * rate / 1000)
	for ch := range 2 {
		s.rearLine[ch] = newDelay(s.rearDelay + 1)
	}
	// Allpasses of a few ms, unlike on either side, as Schroeder's.
	ms := [2][3]float64{{4.7, 7.3, 11.1}, {5.3, 8.1, 12.7}}
	for ch := range 2 {
		for k := range 3 {
			n := max(1, int(ms[ch][k]*rate/1000))
			s.diffuse[ch][k] = allpass{line: newDelay(n), n: n, g: 0.5}
		}
	}
	longest := 0
	for ch, taps := range [2][4]struct{ ms, db float64 }{reflectionsL, reflectionsR} {
		for k, t := range taps {
			n := int(t.ms * rate / 1000)
			s.reflect[ch][k].n, s.reflect[ch][k].g = n, math.Pow(10, t.db/20)
			longest = max(longest, n)
		}
	}
	for ch := range 2 {
		s.room[ch] = newDelay(longest + 1)
	}
	for k, ms := range tailLines {
		n := int(ms * rate / 1000)
		s.tail[k] = newDelay(n + 1)
		s.tailLen[k] = n
		// Each line loses what makes the whole fall 60 dB in tailRT60.
		s.tailGain[k] = math.Pow(10, -3*ms/1000/tailRT60)
		s.tailDamp[k].lowpass(4000, rate)
	}
	s.gain, s.tone = toneFor(s)
}

// keepTone sets the gain and the bands that keep a sound in the middle as
// loud, and of the same tone, as without spatial audio. Two speakers heard
// by one ear add up to a comb: louder in the low mids, quieter where the
// far one comes half a wave late, around 2 kHz. The bands fill that in, on
// what both channels have alike alone: what is on one side, which no
// comb colors, keeps its tone.
// The tone kept, fitted once for each rate: the fit takes a few
// milliseconds, which the engine does not wait for (WarmSpatial).
var (
	tonesMu sync.Mutex
	tones   = map[float64]fittedTone{}
)

type fittedTone struct {
	gain float64
	tone []filter2
}

// toneFor returns the gain and the bands for the rate of s, whose
// speakers are set, fitting them the first time.
func toneFor(s *spatial) (float64, [2][]filter2) {
	tonesMu.Lock()
	ft, ok := tones[s.rate]
	tonesMu.Unlock()
	if !ok {
		s.keepTone()
		ft = fittedTone{s.gain, s.tone[0]}
		tonesMu.Lock()
		tones[s.rate] = ft
		tonesMu.Unlock()
	}
	// Each channel its own filters, with their own state.
	return ft.gain, [2][]filter2{append([]filter2(nil), ft.tone...), append([]filter2(nil), ft.tone...)}
}

// WarmSpatial fits spatial audio's tone for a rate ahead of its use.
func WarmSpatial(rate int) {
	var s spatial
	s.set(float64(rate))
}

func (s *spatial) keepTone() {
	s.gain, s.tone = 1, [2][]filter2{}
	grid := make([]float64, 0, 64)
	for f := 100.0; f <= 16000; f *= math.Pow(2, 1.0/12) {
		grid = append(grid, f)
	}
	// dev is how far the middle is, in dB, at f.
	dev := func(f float64) float64 {
		w := 2 * math.Pi * f / s.rate
		hp := s.high[0][0].response(w) * s.high[0][1].response(w)
		lp := s.low[0][0].response(w) * s.low[0][1].response(w)
		fix := complex(s.gain, 0)
		for i := range s.tone[0] {
			fix *= s.tone[0][i].response(w)
		}
		// The speakers behind add what they play, unlike the front's:
		// as power.
		rn, rf := s.rear.near[0].response(w), s.rear.far[0].response(w)
		rear := rearLevel * rearLevel * sq(cmplx.Abs(s.shelf[0].response(w))) * (sq(cmplx.Abs(rn)) + sq(cmplx.Abs(rf)))
		hpf := hp * fix
		coherent := 10 * math.Log10(sq(cmplx.Abs(lp+hpf*s.front.response(w)))+sq(cmplx.Abs(hpf))*rear)
		near, far := s.front.near[0].response(w), s.front.far[0].response(w)
		power := 10 * math.Log10(sq(cmplx.Abs(lp+hpf*complex(math.Hypot(cmplx.Abs(near), cmplx.Abs(far)), 0)))+sq(cmplx.Abs(hpf))*rear)
		return toneWeight*coherent + (1-toneWeight)*power
	}
	// The level, as an average over the voice's range.
	sum, n := 0.0, 0
	for _, f := range grid {
		if f >= 200 && f <= 5000 {
			sum += dev(f)
			n++
		}
	}
	s.gain = math.Pow(10, -sum/float64(n)/20)
	// Then the bands of a graphic equalizer, half an octave apart, each
	// moved by what its part of the range is off on average, until
	// together they settle.
	var fs, gains []float64
	for f := 150.0; f <= 12000; f *= math.Sqrt2 {
		fs = append(fs, f)
		gains = append(gains, 0)
	}
	s.tone = [2][]filter2{make([]filter2, len(fs)), make([]filter2, len(fs))}
	apply := func() {
		for i := range fs {
			s.tone[0][i].peaking(fs[i], gains[i], 2, s.rate)
			s.tone[1][i] = s.tone[0][i]
		}
	}
	apply()
	for range 80 {
		for i, fc := range fs {
			sum, n := 0.0, 0
			for _, f := range grid {
				if math.Abs(math.Log2(f/fc)) <= 0.25 {
					sum += dev(f)
					n++
				}
			}
			if n > 0 {
				gains[i] = max(-9, min(9, gains[i]-0.5*sum/float64(n)))
			}
		}
		apply()
	}
}

// reset forgets the sound held, as after a seek.
func (s *spatial) reset() {
	if s.rate == 0 {
		return
	}
	rate := s.rate
	s.rate = 0
	s.set(rate)
}

// process plays frames of two samples as spatial audio.
func (s *spatial) process(buf []float32) {
	const half = 0.5
	for i := 0; i+1 < len(buf); i += 2 {
		in := [2]float64{float64(buf[i]), float64(buf[i+1])}
		var lo, hi [2]float64
		for ch := range 2 {
			// Linkwitz–Riley's halves are in phase: they add up to the
			// sound again.
			lo[ch] = s.low[ch][1].run(s.low[ch][0].run(in[ch]))
			hi[ch] = s.high[ch][1].run(s.high[ch][0].run(in[ch]))
		}
		// The front: the channels as they are, what they have alike in
		// the tone kept.
		var feed [2]float64
		for ch := range 2 {
			feed[ch] = hi[ch] * s.gain
			for k := range s.tone[ch] {
				feed[ch] = s.tone[ch][k].run(feed[ch])
			}
		}
		fl, fr := s.front.ears(feed[0], feed[1])
		// Behind: each channel again, on its side.
		var amb [2]float64
		for ch := range 2 {
			s.rearLine[ch].push(hi[ch])
			amb[ch] = s.rearLine[ch].tap(s.rearDelay)
			for k := range s.diffuse[ch] {
				amb[ch] = s.diffuse[ch][k].run(amb[ch])
			}
			amb[ch] = s.shelf[ch].run(amb[ch]) * rearLevel
		}
		rl, rr := s.rear.ears(amb[0], amb[1])
		// The room: reflections of either channel, by turns, and a tail.
		s.room[0].push(hi[0])
		s.room[1].push(hi[1])
		var refl [2]float64
		for ch := range 2 {
			for k, t := range s.reflect[ch] {
				refl[ch] += s.room[(ch+k)%2].tap(t.n) * t.g
			}
			refl[ch] = s.reflectLP[ch].run(refl[ch])
		}
		var out [4]float64
		for k := range s.tail {
			out[k] = s.tailDamp[k].run(s.tail[k].tap(s.tailLen[k])) * s.tailGain[k]
		}
		// Householder's mixing of four: each line feeds all of them.
		sum := (out[0] + out[1] + out[2] + out[3]) * half
		into := (hi[0] + hi[1]) * half
		for k := range s.tail {
			s.tail[k].push(into + out[k] - sum)
		}
		tailL := (out[0] + out[2]) * tailLevel
		tailR := (out[1] + out[3]) * tailLevel
		if s.dry {
			refl, tailL, tailR = [2]float64{}, 0, 0
		}
		l := fl + rl*s.gain + refl[0] + tailL
		r := fr + rr*s.gain + refl[1] + tailR
		buf[i], buf[i+1] = float32(lo[0]+l), float32(lo[1]+r)
	}
}

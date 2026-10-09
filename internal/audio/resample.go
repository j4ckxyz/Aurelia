package audio

import "math"

// resampler converts interleaved stereo from one sample rate to another
// with a windowed sinc filter, as a stream: what a call cannot finish
// waits for the next.
type resampler struct {
	step  float64 // input frames per output frame
	half  int     // taps on each side of the center
	table []float32
	// buf holds input frames not consumed yet, with the half taps before
	// them; pos is where the next output frame is, in frames of buf.
	buf []float32
	pos float64
}

const resamplePhases = 256

// newResampler returns a resampler from in to out Hz, or nil when they
// are the same.
func newResampler(in, out int) *resampler {
	if in == out || in <= 0 || out <= 0 {
		return nil
	}
	step := float64(in) / float64(out)
	// Going down, the filter cuts at the new Nyquist frequency and is as
	// many times longer.
	scale := 1.0
	if step > 1 {
		scale = 1 / step
	}
	half := int(math.Ceil(16 / scale))
	r := &resampler{step: step, half: half}
	taps := 2 * half
	r.table = make([]float32, (resamplePhases+1)*taps)
	const beta = 8.6
	i0b := bessel0(beta)
	for p := 0; p <= resamplePhases; p++ {
		frac := float64(p) / resamplePhases
		var sum float64
		row := r.table[p*taps : (p+1)*taps]
		for t := 0; t < taps; t++ {
			x := float64(t-half+1) - frac // distance from the center, in input frames
			w := 0.0
			if a := x / float64(half); a > -1 && a < 1 {
				w = bessel0(beta*math.Sqrt(1-a*a)) / i0b
			}
			v := sinc(x*scale) * scale * w
			row[t] = float32(v)
			sum += v
		}
		// Unity gain at every phase, so that a steady level stays steady.
		if sum != 0 {
			for t := range row {
				row[t] = float32(float64(row[t]) / sum)
			}
		}
	}
	r.reset()
	return r
}

func (r *resampler) reset() {
	r.buf = r.buf[:0]
	for i := 0; i < 2*(r.half-1); i++ {
		r.buf = append(r.buf, 0)
	}
	r.pos = 0
}

// process appends to out the frames that in gives, and returns it.
func (r *resampler) process(in []float32, out []float32) []float32 {
	r.buf = append(r.buf, in...)
	taps := 2 * r.half
	frames := len(r.buf) / 2
	for {
		i := int(r.pos)
		if i+taps > frames {
			break
		}
		frac := r.pos - float64(i)
		ph := frac * resamplePhases
		p := int(ph)
		mix := float32(ph - float64(p))
		a := r.table[p*taps : (p+1)*taps]
		b := r.table[(p+1)*taps : (p+2)*taps]
		src := r.buf[2*i : 2*(i+taps)]
		var l, rr float32
		for t := 0; t < taps; t++ {
			k := a[t] + (b[t]-a[t])*mix
			l += src[2*t] * k
			rr += src[2*t+1] * k
		}
		out = append(out, l, rr)
		r.pos += r.step
	}
	// Drop what no output frame needs any more.
	if drop := int(r.pos); drop > 0 {
		if drop > frames {
			drop = frames
		}
		n := copy(r.buf, r.buf[2*drop:])
		r.buf = r.buf[:n]
		r.pos -= float64(drop)
	}
	return out
}

// flush appends the frames still inside the filter, at the end of a
// track.
func (r *resampler) flush(out []float32) []float32 {
	return r.process(make([]float32, 2*r.half), out)
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	x *= math.Pi
	return math.Sin(x) / x
}

// bessel0 is the modified Bessel function of the first kind, of order 0,
// for the Kaiser window.
func bessel0(x float64) float64 {
	sum, term := 1.0, 1.0
	for k := 1; k < 40; k++ {
		term *= (x / 2) / float64(k)
		sum += term * term
		if term*term < sum*1e-12 {
			break
		}
	}
	return sum
}

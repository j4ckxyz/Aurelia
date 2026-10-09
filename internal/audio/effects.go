package audio

import "math"

// EQBands are the centers of the equalizer's ten bands, in Hz: an octave
// apart.
var EQBands = [10]float64{31.25, 62.5, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}

// EQRange is how far a band or the preamp goes either way, in dB.
const EQRange = 12

// Effects is what is done to the sound on its way to the device. The zero
// value does nothing.
type Effects struct {
	// EQ turns the equalizer on: Gains are each band's boost or cut in
	// dB, and Preamp is added to all of them.
	EQ     bool
	Gains  [10]float64
	Preamp float64
	// Mono plays the left and right channels as one in both.
	Mono bool
	// Balance goes from -1, the left channel alone, to 1, the right.
	Balance float64
}

// eqActive reports that the equalizer changes the sound.
func (fx Effects) eqActive() bool {
	if !fx.EQ {
		return false
	}
	if math.Abs(fx.Preamp) >= 0.05 {
		return true
	}
	for _, g := range fx.Gains {
		if math.Abs(g) >= 0.05 {
			return true
		}
	}
	return false
}

// idle reports that the effects change nothing, which the engine then
// does not spend time on.
func (fx Effects) idle() bool {
	return !fx.eqActive() && !fx.Mono && math.Abs(fx.Balance) < 0.005
}

// Headroom is how many dB, at most 0, the equalizer turns the sound down
// by of itself to leave room for its boosts, so that raising a band does
// not clip the music: the most any band raises it.
func (fx Effects) Headroom() float64 {
	if !fx.eqActive() {
		return 0
	}
	most := 0.0
	for _, g := range fx.Gains {
		most = math.Max(most, g)
	}
	return -most
}

// biquad is a filter of two poles and two zeros, in transposed direct form
// II, with the state of both channels.
type biquad struct {
	b0, b1, b2, a1, a2 float64
	l, r               [2]float64
}

// peaking sets the filter to boost or cut gain dB around f, as in Robert
// Bristow-Johnson's cookbook, with a width of an octave.
func (b *biquad) peaking(f, gain, rate float64) {
	const q = 1.41
	a := math.Pow(10, gain/40)
	w := 2 * math.Pi * f / rate
	alpha := math.Sin(w) / (2 * q)
	cos := math.Cos(w)
	a0 := 1 + alpha/a
	b.b0 = (1 + alpha*a) / a0
	b.b1 = -2 * cos / a0
	b.b2 = (1 - alpha*a) / a0
	b.a1 = -2 * cos / a0
	b.a2 = (1 - alpha/a) / a0
}

const (
	// limitCeiling is the level the limiter after the equalizer keeps the
	// sound under, and limitRelease how much of the way back to no
	// limiting each frame goes.
	limitCeiling = 0.98
	limitRelease = 1.0 / 6000
)

// chain is the engine's working copy of an Effects: the filters of the
// bands that do something, and the gains around them.
type chain struct {
	fx    Effects
	rate  float64
	band  [10]biquad
	on    [10]bool
	gain  float64 // linear: the preamp and the headroom
	limit float64 // what the limiter turns the sound down by now, 1 for nothing
	left  float32 // the balance's gain of each channel
	right float32
}

// set makes the chain follow fx. The filters that stay keep their state,
// so that moving a slider makes no click.
func (c *chain) set(fx Effects, rate float64) {
	c.fx, c.rate = fx, rate
	if c.limit == 0 {
		c.limit = 1
	}
	c.gain = math.Pow(10, (fx.Preamp+fx.Headroom())/20)
	for i, f := range EQBands {
		g := fx.Gains[i]
		c.on[i] = fx.eqActive() && math.Abs(g) >= 0.05
		if c.on[i] {
			c.band[i].peaking(f, g, rate)
		} else {
			c.band[i].l, c.band[i].r = [2]float64{}, [2]float64{}
		}
	}
	c.left, c.right = 1, 1
	if b := math.Max(-1, math.Min(1, fx.Balance)); b > 0 {
		c.left = float32(1 - b)
	} else {
		c.right = float32(1 + b)
	}
}

// reset forgets what the filters hold, as after a seek.
func (c *chain) reset() {
	for i := range c.band {
		c.band[i].l, c.band[i].r = [2]float64{}, [2]float64{}
	}
	c.limit = 1
}

// process changes frames of two samples as the effects ask.
func (c *chain) process(buf []float32) {
	if c.fx.idle() {
		return
	}
	eq := c.fx.eqActive()
	mono := c.fx.Mono
	balanced := c.left != 1 || c.right != 1
	for i := 0; i+1 < len(buf); i += 2 {
		l, r := float64(buf[i]), float64(buf[i+1])
		if eq {
			l, r = l*c.gain, r*c.gain
			for k := range c.band {
				if !c.on[k] {
					continue
				}
				b := &c.band[k]
				y := b.b0*l + b.l[0]
				b.l[0] = b.b1*l - b.a1*y + b.l[1]
				b.l[1] = b.b2*l - b.a2*y
				l = y
				y = b.b0*r + b.r[0]
				b.r[0] = b.b1*r - b.a1*y + b.r[1]
				b.r[1] = b.b2*r - b.a2*y
				r = y
			}
			// What the boosts still take over full scale is turned down as
			// it comes and let back up slowly, as the gain's limiter does.
			peak := math.Max(math.Abs(l), math.Abs(r))
			if peak*c.limit > limitCeiling {
				c.limit = limitCeiling / peak
			} else {
				c.limit += (1 - c.limit) * limitRelease
			}
			l, r = l*c.limit, r*c.limit
		}
		if mono {
			l = (l + r) / 2
			r = l
		}
		if balanced {
			l, r = l*float64(c.left), r*float64(c.right)
		}
		buf[i], buf[i+1] = float32(l), float32(r)
	}
}

// Response is how much the equalizer's bands boost or cut a sound of f Hz,
// in dB, with the preamp but not the headroom: what a picture of the curve
// shows.
func (fx Effects) Response(f, rate float64) float64 {
	if !fx.eqActive() {
		return 0
	}
	w := 2 * math.Pi * f / rate
	cos1, sin1 := math.Cos(w), math.Sin(w)
	cos2, sin2 := math.Cos(2*w), math.Sin(2*w)
	db := fx.Preamp
	for i, g := range fx.Gains {
		if math.Abs(g) < 0.05 {
			continue
		}
		var b biquad
		b.peaking(EQBands[i], g, rate)
		nr := b.b0 + b.b1*cos1 + b.b2*cos2
		ni := -(b.b1*sin1 + b.b2*sin2)
		dr := 1 + b.a1*cos1 + b.a2*cos2
		di := -(b.a1*sin1 + b.a2*sin2)
		db += 10 * math.Log10((nr*nr+ni*ni)/(dr*dr+di*di))
	}
	return db
}

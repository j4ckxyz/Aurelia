package audio

import (
	"math"
	"math/cmplx"
	"sync"
	"time"
)

// Vinyl: the music as if played from a record of its album's, on a
// turntable. Nothing here is a recording of a record, nor noise made to
// sound like one: the sound is what a stylus does as it traces a groove,
// worked out from the groove's geometry.
//
//   - The music is cut into the groove as a lathe cuts it: with the RIAA
//     curve's pre-emphasis, as the displacement of the two walls of a
//     45/45 stereo groove, the left channel the inner wall and the right
//     the outer, at a level set by how much music the side holds (a long
//     side is cut tighter, and so quieter).
//   - The record has been played as its condition has it: the old stylus
//     that played it, round and wide, could not follow the tightest of
//     the curves, and wore the crests it pressed flat; the walls' finest
//     detail has flowed away, the more so on the inner grooves, where the
//     groove passes slowest; and where the stylus was hammered hardest,
//     on the loudest of the highs, the walls are rough, and heard as grit.
//   - Dust lies in the groove: grains, flakes and fibres of every size,
//     most of them a few hundredths of a µm high and a few as high as the
//     groove is deep (Pareto's law of sizes, as of dust in the air),
//     more where the record was held; on one wall, so heard on one side,
//     or across both, which lifts the stylus and is heard on both, in
//     opposite phase, as the vertical motion of a 45/45 groove is.
//     Scratches cross the grooves, a trench with the material it ploughed
//     raised along either side.
//   - A stylus of today traces it all: an elliptical tip, whose scanning
//     radius rides over what is narrower than itself, so that a grain h
//     high is felt over 2√(2rh) of groove (shorter on the outer grooves,
//     where the groove passes faster) and the tightest curves of the music
//     are not followed (tracing distortion, mostly as the vertical pinch of
//     the second harmonic).
//   - The cartridge gives the stylus' velocity, and the phono stage the
//     RIAA curve's de-emphasis, with the IEC's cut of the subsonic: dust,
//     scratches and the roughness of the compound itself are heard through
//     them, as on a turntable, and the music as it is, with what the
//     groove did to it.
//   - The record turns, at its speed: a scratch ticks once a revolution,
//     for as long as the stylus takes to cross it; a smudge crackles once
//     a revolution; the needle drops into the lead-in at the start of a
//     side, and lifts after the run-out and a turn of the locked groove at
//     its end.
//
// The noise is heard where a record's is: under quiet passages, between
// songs and at the start and the end of a side, where no music covers it,
// as Goldmine's grading has it; on the loudest highs of a worn record; and
// more towards the end of a side.
//
// The groove is traced at twice the device's rate, for the curves of the
// tip and the grains to be followed, and brought back down; the music
// comes out a little under a millisecond late, to meet its groove's sound.
// It all costs a few percent of a core.

const (
	// halfTaps is the length of the halfband filters that take the groove
	// to twice the rate and back.
	halfTaps = 47
	halfMid  = halfTaps / 2
	// lookOld and lookNew are how many samples of the traced rate the old
	// stylus and today's reach either side of where they are, at most:
	// enough for the widest on the slowest groove.
	lookOld = 16
	lookNew = 16
	// vinylDelay is how many frames late the music comes out, for the
	// groove's sound to meet it: the filters' and the tracing's lag.
	vinylDelay = (2*halfMid + lookOld + lookNew) / 2
	vring      = 128 // the rings of the traced rate
	bring      = 64  // and of the device's
	// dustCell is how much groove, in seconds, has its dust made at once.
	dustCell = 0.02
	// hMin is the height of the smallest particles, m, and dustAlpha
	// Pareto's exponent of the heights.
	hMin      = 0.04e-6
	dustAlpha = 2.4
	// gritFloor is the load, as a part of the tracking force, that the
	// walls bear without harm, and gritRef the load the grit of a
	// condition is given at.
	gritFloor = 0.05
	gritRef   = 0.2
	// bandPitch is the pitch of the band between two songs, m a
	// revolution.
	bandPitch = 1e-3
	// cutHighs is where the lathe's limiter of the highs takes over, Hz,
	// and cutMu how tight it lets the groove's curves be at the end of a
	// side, as a part of the curve of the stylus' tip.
	cutHighs = 4000
	cutMu    = 0.15
)

// The roughness of the compound, as three scales of roughness along the
// groove, each a process of Ornstein and Uhlenbeck: how far it holds, in
// m, and its part of the noise's power, A-weighted. The finest is the
// grain of the compound, smoothed over the stylus' contact; the others,
// the waviness of the pressing.
var roughScales = [3]struct{ length, share float64 }{{5e-6, 0.5}, {25e-6, 0.42}, {200e-6, 0.08}}

// halfband is the prototype of the filters to twice the rate and back,
// Kaiser-windowed, flat to about 0.4 of the device's rate.
var halfband = func() [halfTaps]float64 {
	var h [halfTaps]float64
	const beta = 7.0
	i0 := func(x float64) float64 {
		sum, term := 1.0, 1.0
		for k := 1; k < 40; k++ {
			term *= sq(x / 2 / float64(k))
			sum += term
		}
		return sum
	}
	odd := 0.0
	for i := range h {
		k := i - halfMid
		switch {
		case k == 0:
			h[i] = 0.5
		case k%2 != 0:
			x := float64(k)
			t := x / float64(halfMid+1)
			h[i] = math.Sin(math.Pi*x/2) / (math.Pi * x) * i0(beta*math.Sqrt(1-t*t)) / i0(beta)
			odd += h[i]
		}
	}
	for i := range h {
		if (i-halfMid)%2 != 0 {
			h[i] *= 0.5 / odd // a gain of 1, and halfband still
		}
	}
	return h
}()

// riaa returns the lathe's pre-emphasis and the phono stage's
// de-emphasis, its exact inverse, both of no gain at 1 kHz: the RIAA's
// time constants of 3180, 318 and 75 µs, with the cutting amplifier's pole
// of 3.18 µs that makes the pre-emphasis one a filter can be; or, for a
// 78, cut without treble pre-emphasis, the turnover alone.
func riaa(rate float64, flat78 bool) (rec, play filter2) {
	k := 2 * rate
	// A term 1 + sT of the bilinear transform, without the 1 + 1/z that
	// cancels between the numerator and the denominator.
	term := func(t float64) [2]float64 { return [2]float64{1 + k*t, 1 - k*t} }
	mul := func(a, b [2]float64) [3]float64 {
		return [3]float64{a[0] * b[0], a[0]*b[1] + a[1]*b[0], a[1] * b[1]}
	}
	var num, den [3]float64
	if flat78 {
		a, b := term(3180e-6), term(318e-6)
		num, den = [3]float64{a[0], a[1]}, [3]float64{b[0], b[1]}
	} else {
		num, den = mul(term(3180e-6), term(75e-6)), mul(term(318e-6), term(3.18e-6))
	}
	rec = filter2{b0: num[0] / den[0], b1: num[1] / den[0], b2: num[2] / den[0], a1: den[1] / den[0], a2: den[2] / den[0]}
	play = filter2{b0: den[0] / num[0], b1: den[1] / num[0], b2: den[2] / num[0], a1: num[1] / num[0], a2: num[2] / num[0]}
	g := cmplx.Abs(rec.response(2 * math.Pi * 1000 / rate))
	rec.b0, rec.b1, rec.b2 = rec.b0/g, rec.b1/g, rec.b2/g
	play.b0, play.b1, play.b2 = play.b0*g, play.b1*g, play.b2*g
	return rec, play
}

// subsonic is the IEC's amendment to the RIAA curve: a cut under 20 Hz, of
// 7950 µs, that keeps the warps and the arm's swaying out of the speakers.
func subsonic(rate float64) filter2 {
	k, t := 2*rate, 7950e-6
	a0 := 1 + k*t
	return filter2{b0: k * t / a0, b1: -k * t / a0, a1: (1 - k*t) / a0}
}

// aWeighting is the A-weighting of IEC 61672, as three biquads, of no gain
// at 1 kHz: what the noise of records is measured through.
func aWeighting(rate float64) [3]filter2 {
	k := 2 * rate
	pole := func(f float64) float64 { w := 2 * math.Pi * f; return (k - w) / (k + w) }
	p1, p2, p3, p4 := pole(20.598997), pole(107.65265), pole(737.86223), pole(12194.217)
	fs := [3]filter2{
		{b0: 1, b1: -2, b2: 1, a1: -2 * p1, a2: p1 * p1},
		{b0: 1, b1: -2, b2: 1, a1: -(p2 + p3), a2: p2 * p3},
		{b0: 1, b1: 2, b2: 1, a1: -2 * p4, a2: p4 * p4},
	}
	w := 2 * math.Pi * 1000 / rate
	g := cmplx.Abs(fs[0].response(w) * fs[1].response(w) * fs[2].response(w))
	fs[0].b0, fs[0].b1, fs[0].b2 = fs[0].b0/g, fs[0].b1/g, fs[0].b2/g
	return fs
}

// cutOf is how the side of g is cut: the pitch of its groove, m a
// revolution, how far in its music ends, and the walls' velocity at full
// scale, m/s. The lathe cuts as wide as the side leaves room for, and as
// loud as the pitch does: 7 dB over 3.54 cm/s a wall, for a groove of 100
// µm, more for a short side and less for a long one.
func cutOf(g *Groove) (pitch, rEnd, vfs float64) {
	pitch, rEnd, vfs = cutLevel(g)
	// As loud in the groove whatever the master: a loud one is cut lower
	// for its full scale, a quiet one higher, as far as its peaks allow.
	if g.Gain != 0 {
		vfs *= math.Pow(10, max(-10, min(3, g.Gain+4))/20)
	}
	return pitch, rEnd, vfs
}

// cutLevel is the cut of the side of g for a master of the usual loudness.
func cutLevel(g *Groove) (pitch, rEnd, vfs float64) {
	f := &formats[g.Speed]
	rev := f.rpm / 60
	t := max(g.SideLen.Seconds(), 30)
	pitch = max(f.pitchMin, min(f.pitchMax, (f.first-f.inner)/(rev*t)))
	rEnd = max(f.inner-3e-3, f.first-pitch*rev*t)
	db := max(1, min(12, 7+20*math.Log10(pitch/100e-6)))
	switch g.Speed {
	case Single:
		db++ // 45s are cut hot
	case Shellac:
		db = 6
	}
	return pitch, rEnd, 0.0354 * math.Pow(10, db/20)
}

// The level of the hiss each scale of roughness makes for a deviation of
// 1 m, A-weighted, on a groove at the middle of a side cut at the usual
// level: fitted once for each rate and speed.
var (
	unitsMu sync.Mutex
	units   = map[[2]float64][3]float64{}
)

// refCut is a side cut at the usual level, of the speed: its velocity at
// full scale, and its groove's speed half way in.
func refCut(sp Speed) (vfs, speed float64) {
	f := &formats[sp]
	side := [...]time.Duration{LP: 20 * time.Minute, Single: 4 * time.Minute, Shellac: 3 * time.Minute}[sp]
	_, rEnd, vfs := cutOf(&Groove{Speed: sp, SideLen: side})
	return vfs, 2 * math.Pi * (f.first + rEnd) / 2 * f.rpm / 60
}

// hissUnits returns what each scale of roughness, of a deviation of 1 m,
// is heard as: the A-weighted level of the sound it makes, under a sine at
// full scale.
func hissUnits(rate float64, sp Speed) [3]float64 {
	key := [2]float64{rate, float64(sp)}
	unitsMu.Lock()
	u, ok := units[key]
	unitsMu.Unlock()
	if ok {
		return u
	}
	f := &formats[sp]
	vfs, speed := refCut(sp)
	_, play := riaa(rate, f.flat78)
	leak := math.Exp(-2 * math.Pi * 2 / rate)
	dx := speed / rate
	for k, sc := range roughScales {
		a := math.Exp(-dx / sc.length)
		pl, hp, aw := play, subsonic(rate), aWeighting(rate)
		x, prev, energy := math.Sqrt(1-a*a), 0.0, 0.0
		for range 1 << 15 {
			y := pl.run((x - leak*prev) * rate)
			prev, x = x, x*a
			y = hp.run(y) / vfs
			for i := range aw {
				y = aw[i].run(y)
			}
			energy += y * y
		}
		u[k] = math.Sqrt(energy)
	}
	unitsMu.Lock()
	units[key] = u
	unitsMu.Unlock()
	return u
}

// WarmVinyl fits what vinyl playback needs for a rate ahead of its use.
func WarmVinyl(rate int) {
	for sp := range formats {
		hissUnits(float64(rate), Speed(sp))
	}
}

// VinylGrade is the condition a record of year plays in: grade itself, or
// below 0 what the year makes it, moved by more, from -1, much better, to
// 1, much worse.
func VinylGrade(year int, grade, more float64) float64 {
	if grade < 0 {
		grade = AgeGrade(year, time.Now().Year())
	}
	return max(0, min(Fair, grade+2.5*more))
}

// GradeName is the grade nearest g.
func GradeName(g float64) string {
	return GradeNames[max(0, min(Fair, int(math.Round(g))))]
}

// particle is a grain, a flake or a fibre of dust in the groove, or the
// ridges of a scratch.
type particle struct {
	c     float64 // where it is, in samples of the traced rate
	h     float64 // how high it stands, m
	flat  float64 // how long its top is, along the groove, m: 0 for a grain
	walls uint8   // 1 on the left wall, 2 the right, 3 both
	reach float64 // how far either side of c the stylus feels it, in samples
	// A scratch: the trench's width along the groove and depth, m; its
	// ridges stand where the trench ends, h high and flat long.
	gap, depth float64
}

// thump is the sway of the arm, after a jolt of the stylus, as Esquef,
// Biscainho and Välimäki model it: a sine of falling frequency, dying
// away.
type thump struct {
	t, phase    float64
	amp         float64
	fmax, fmin  float64
	tauE, tauF  float64
	left, right float64 // its part in each channel
}

// phase is a stretch of groove played without music, between songs and
// at either end of a side, or of the stylus off the record.
type phase struct {
	frames int
	g      Groove
	r, dr  float64 // the radius at its start, and how it moves a frame
	tau    float64 // the groove's time at its start
	cycle  float64 // the locked groove: its time comes round each cycle seconds
	up     bool    // the stylus is off the record
	drop   bool    // it lands as the phase begins
	lift   bool    // it leaves as the phase begins
	begun  bool
}

// vinyl is the state of vinyl playback: the record, where the stylus is,
// and the groove as it is traced.
type vinyl struct {
	rate float64
	now  int // the year
	// What the condition is set to: a grade, or below 0 the year's, and
	// how much worse.
	grade, more float64

	// The side playing: its groove, kind, cut, surface and condition.
	g          Groove
	have       bool
	f          *format
	rev        float64 // revolutions a second
	pitch      float64
	rEnd       float64
	vfs        float64
	sd         side
	cond       condition
	oldScan    float64
	sigma      [3]float64 // the roughness of each scale, m
	sigmaGrit  float64
	scratchWas []float64 // where each scratch was, as an angle from the stylus, the frame before

	// The filters of the cut and of the phono stage.
	rec, phono [2]filter2
	hp         [2]filter2
	leak       float64
	disp       [2]float64 // each wall's displacement as the lathe cut it, m
	eprev      [2]float64

	// The groove at the traced rate.
	hist     [2][32]float64 // the walls at the device's rate, to go up from
	w, ws    [2][vring]float64
	worn     [2][vring]float64
	e2       [2][64]float64
	smooth   [2]float64
	smA      float64 // the smoothing's coefficient, at the groove's speed
	n        int64   // the newest sample of the traced rate
	dx2      float64
	shapedAt float64 // the groove's speed the shapes below are for
	profOld  [lookOld - 1]float64
	profNew  [lookNew - 1]float64
	kOld     int
	kNew     int
	reachOld int
	reachNew int
	// The walls as the old stylus traced them, smoothed, and as today's
	// does, worn.
	old, tip [2]tipPath
	// The lathe's limiter of the highs: the lows of each wall's velocity,
	// the highs' peak, the most the highs may be, and its filters.
	cutLP, hfEnv [2]float64
	hfMax        float64
	lpK, hfEnvK  float64

	// At the device's rate.
	frame int64
	dry   [2][bring]float32 // the music, late by vinylDelay
	wb    [2][bring]float64 // the walls, for the grit
	rough [2][3]float64
	ouA   [3]float64
	ouB   [3]float64
	grit  [2]float64
	env   [2]float64
	envK  float64
	noise *rng
	thump []thump
	lim   float64
	gain  float64 // of the groove's sound: 0 with the stylus off the record
	burst float64 // more dust where the stylus lands, as it settles

	// Where the stylus is on the record.
	tau, r, dr, theta, dtheta float64
	cycle, cycleEnd           float64
	anchorC, anchorTau        float64
	cell                      int64
	dust                      []particle
	smudgeAt                  float64 // the smudges' roughness, where the stylus is
	smudgeN                   int

	// The needle: off the record, and the stretches to play without music.
	up    bool
	holds []phase
	on    *segment // the segment the stylus is in
	left  *segment // the segment the stylus was last taken out of
}

func newVinyl(rate float64) *vinyl {
	v := &vinyl{rate: rate, now: time.Now().Year(), grade: -1, lim: 1, gain: 1, noise: newRNG(uint64(time.Now().UnixNano()))}
	v.leak = math.Exp(-2 * math.Pi * 2 / rate)
	v.envK = math.Exp(-1 / (0.002 * rate))
	v.lpK = 1 - math.Exp(-2*math.Pi*cutHighs/rate)
	v.hfEnvK = math.Exp(-1 / (0.005 * rate))
	return v
}

// setGrade sets the condition: a grade, or below 0 the album's year's,
// moved by more.
func (v *vinyl) setGrade(grade, more float64) {
	if grade == v.grade && more == v.more {
		return
	}
	v.grade, v.more = grade, more
	if v.have {
		v.surface()
	}
}

// context makes g's side the one playing, as it is cut and as worn.
func (v *vinyl) context(g Groove) {
	if v.have && g.Record == v.g.Record && g.Side == v.g.Side && g.Speed == v.g.Speed && g.SideLen == v.g.SideLen && g.Year == v.g.Year {
		v.g = g
		return
	}
	speedChanged := !v.have || g.Speed != v.g.Speed
	v.g, v.have = g, true
	v.f = &formats[g.Speed]
	v.rev = v.f.rpm / 60
	v.dtheta = 2 * math.Pi * v.rev / v.rate
	v.pitch, v.rEnd, v.vfs = cutOf(&g)
	// The highs' velocity at which the groove's curves at the end of the
	// side are cutMu of the tip's, for a sine at the highs' middle.
	inner := 2 * math.Pi * max(v.rEnd, v.f.inner-3e-3) * v.rev
	v.hfMax = cutMu * inner * inner / v.f.scan / (2 * math.Pi * 8000)
	v.oldScan = v.f.oldScan
	if g.Speed != Shellac && g.Year > 0 && g.Year < 1958 {
		v.oldScan = 25.4e-6 // the 1 mil styli of the first LPs
	}
	if speedChanged {
		rec, play := riaa(v.rate, v.f.flat78)
		v.rec = [2]filter2{rec, rec}
		v.phono = [2]filter2{play, play}
		hp := subsonic(v.rate)
		v.hp = [2]filter2{hp, hp}
	}
	v.surface()
}

// surface works out the side's condition, and makes its smudges and
// scratches.
func (v *vinyl) surface() {
	g := v.grade
	if g < 0 {
		g = AgeGrade(v.g.Year, v.now)
	}
	g = max(0, min(Fair, g+2.5*v.more))
	c := conditionAt(g)
	db, mul, filler := era(v.g.Year, v.g.Speed)
	c.hiss += db
	c.dust = c.dust*mul + filler
	v.cond = c
	v.sd = cutSide(mix64(v.g.Record, uint64(v.g.Side)+1), c, g, v.f, v.rEnd)
	v.scratchWas = make([]float64, len(v.sd.scratches))
	for i := range v.scratchWas {
		v.scratchWas[i] = math.NaN()
	}
	u := hissUnits(v.rate, v.g.Speed)
	target := math.Pow(10, c.hiss/20) / math.Sqrt2
	for k, sc := range roughScales {
		v.sigma[k] = target * math.Sqrt(sc.share) / u[k]
	}
	v.sigmaGrit = 0
	if c.grit > 0 {
		v.sigmaGrit = target / u[0] * math.Pow(10, c.grit/20)
	}
	v.shapedAt = 0 // the smoothing is the condition's
}

// radiusAt is where the groove is at time t of the side's music.
func (v *vinyl) radiusAt(t float64) float64 {
	return max(v.rEnd-2e-3, v.f.first-v.pitch*v.rev*t)
}

// place puts the stylus at the groove's time tau, at the radius r that
// moves dr a frame, from the frame the music is at now. A stylus moved on
// from where it was carries on in its groove; one moved elsewhere finds
// other dust.
func (v *vinyl) place(tau, r, dr, cycle float64) {
	jump := math.Abs(tau-v.tau) > 0.05 || cycle != v.cycle
	v.tau, v.r, v.dr = tau, r, dr
	v.cycle = cycle
	if cycle > 0 {
		v.cycleEnd = tau + cycle
	}
	v.theta = math.Mod(v.sd.theta0+2*math.Pi*v.rev*tau, 2*math.Pi)
	v.anchorC = float64(2*v.frame + halfMid)
	v.anchorTau = tau
	if jump {
		v.dust = v.dust[:0]
		v.cell = int64(math.Floor(tau / dustCell))
		for i := range v.scratchWas {
			v.scratchWas[i] = math.NaN()
		}
	}
	v.smudgeN = 0
}

// shape works out, for the groove's speed now, how far the groove moves a
// sample, the profiles of the two styli, and the roughness' steps.
func (v *vinyl) shape() {
	speed := 2 * math.Pi * v.r * v.rev
	if v.shapedAt > 0 && math.Abs(speed-v.shapedAt) < 0.002*v.shapedAt {
		return
	}
	v.shapedAt = speed
	v.dx2 = speed / (2 * v.rate)
	// The tip resting on the samples it spans, and how far from its
	// centre it can touch the wall: its radius, in samples.
	profile := func(r float64, prof []float64) (int, int) {
		k := min(len(prof)-1, int(r/v.dx2*0.999))
		for i := 0; i <= k; i++ {
			x := float64(i) * v.dx2
			prof[i] = math.Sqrt(r*r-x*x) - r
		}
		return k, min(len(prof)-1, int(math.Ceil(r/v.dx2))+1)
	}
	v.kOld, v.reachOld = profile(v.oldScan, v.profOld[:])
	v.kNew, v.reachNew = profile(v.f.scan, v.profNew[:])
	v.smA = 0
	if sm := v.cond.smooth; sm > 0 {
		v.smA = math.Exp(-v.dx2 / sm)
	}
	dx := speed / v.rate
	for k, sc := range roughScales {
		v.ouA[k] = math.Exp(-dx / sc.length)
		v.ouB[k] = math.Sqrt(1 - v.ouA[k]*v.ouA[k])
	}
}

// play gives frames of the music of the segment, its frame at onwards, the
// sound of its groove.
func (v *vinyl) play(buf []float32, seg *segment, at int64) {
	g := grooveOf(seg)
	v.context(g)
	tau := g.At.Seconds() + float64(at)/v.rate
	v.place(tau, v.radiusAt(tau), -v.pitch*v.rev/v.rate, 0)
	v.run(buf)
}

// grooveOf is the groove of a segment: a single of its own when its track
// has none.
func grooveOf(seg *segment) Groove {
	if seg.groove != nil {
		return *seg.groove
	}
	return Groove{Record: HashString(seg.id), Speed: LP, At: 0, SideLen: max(seg.dur, time.Minute), First: true, Last: true}
}

// holding reports that the stylus plays a stretch without music.
func (v *vinyl) holding() bool { return len(v.holds) > 0 }

// hold plays what it can of the stretch without music into buf, and
// returns how many frames.
func (v *vinyl) hold(buf []float32) int {
	ph := &v.holds[0]
	if !ph.begun {
		ph.begun = true
		if !ph.up {
			v.context(ph.g)
			v.place(ph.tau, ph.r, ph.dr, ph.cycle)
		}
		switch {
		case ph.lift:
			v.lift()
		case ph.drop:
			v.drop()
		}
	}
	k := min(len(buf)/2, ph.frames)
	clear(buf[:2*k])
	v.run(buf[:2*k])
	ph.frames -= k
	if ph.frames <= 0 {
		v.holds = v.holds[1:]
	}
	return k
}

// enter is told the stylus is to play seg: from where it is, or after a
// drop of the needle, which enter begins and reports.
func (v *vinyl) enter(seg *segment) bool {
	if v.on == seg {
		return false
	}
	v.on = seg
	if !v.up {
		return false
	}
	v.up = false
	if seg.start > 0 {
		// Played from the middle, as after a restart: the music at once.
		v.gain = 1
		return false
	}
	g := grooveOf(seg)
	f := &formats[g.Speed]
	rev := f.rpm / 60
	pitch, _, _ := cutOf(&g)
	// Into the lead-in, a revolution before the music; or, for a song in
	// the middle of a side, onto the band before it.
	revs, r0, inward := 1.0, f.first+f.leadIn, f.leadIn
	tau := -1 / rev
	if !g.First {
		revs, inward = 0.6, bandPitch
		r0 = max(f.first-pitch*rev*g.At.Seconds(), f.inner) + revs*inward
		tau = g.At.Seconds() - revs/rev
	}
	frames := int(revs / rev * v.rate)
	v.holds = append(v.holds, phase{frames: frames, g: g, r: r0, dr: -revs * inward / float64(frames), tau: tau, drop: true})
	return true
}

// leave is told the stylus is done with seg, and next follows it (nil for
// nothing): it goes on into next's groove when next is cut after seg on
// the same side, or comes in under seg's end; otherwise it plays the
// run-out if the side ends there, and lifts. leave reports that it begins
// that.
func (v *vinyl) leave(seg, next *segment) bool {
	if v.left == seg {
		return false
	}
	v.left = seg
	if v.up {
		return false
	}
	g := grooveOf(seg)
	if next != nil && next.start > 0 {
		return false // a crossfade: the next record is already playing
	}
	if next != nil {
		h := grooveOf(next)
		if g.follows(&h, seg.dur) {
			return false
		}
	}
	if g.Last {
		f := &formats[g.Speed]
		rev := f.rpm / 60
		_, rEnd, _ := cutOf(&g)
		revs := max(0.6, min(2, (rEnd-f.lock)/f.leadOut))
		frames := int(revs / rev * v.rate)
		v.holds = append(v.holds, phase{frames: frames, g: g, r: rEnd, dr: -(rEnd - f.lock) / float64(frames), tau: g.SideLen.Seconds()})
		locked := 1.0
		if next == nil {
			locked = 2
		}
		cycle := 1 / rev
		v.holds = append(v.holds, phase{frames: int(locked * cycle * v.rate), g: g, r: f.lock, tau: g.SideLen.Seconds() + revs/rev, cycle: cycle})
	}
	gap := 1.0 // a record changed, or turned over
	if next == nil {
		gap = 0.3 // for the lift's thump
	}
	v.holds = append(v.holds, phase{frames: int(gap * v.rate), g: g, up: true, lift: true})
	return true
}

// cue is told a new track plays from the start: the stylus is lifted, to
// be dropped there.
func (v *vinyl) cue() {
	v.holds = v.holds[:0]
	v.on, v.left = nil, nil
	v.up = true
	v.gain = 0
}

// seek is told the music moved: the stylus is in its groove at once,
// there.
func (v *vinyl) seek() {
	v.holds = v.holds[:0]
	v.on, v.left = nil, nil
	if v.up {
		v.up = false
		v.gain = 1
	}
}

// stop is told the music stopped.
func (v *vinyl) stop() {
	v.holds = v.holds[:0]
	v.on, v.left = nil, nil
	v.up = true
	v.gain = 0
}

// drop lands the stylus: it touches the lead-in at the speed of the cue
// lever, and the arm sways as it settles.
func (v *vinyl) drop() {
	v.gain = 1
	v.burst = 1
	v.thump = append(v.thump, thump{amp: 0.05, fmax: 140, fmin: 32, tauE: 0.07, tauF: 0.04, left: 1, right: -0.8})
	// The tip's landing: both walls at once.
	v.insert(particle{c: float64(2*v.frame + halfMid + 8), h: 0.5e-6, flat: 40e-6, walls: 3, reach: 64})
}

// lift raises the stylus off the record: the next enter drops it.
func (v *vinyl) lift() {
	v.thump = append(v.thump, thump{amp: 0.015, fmax: 110, fmin: 40, tauE: 0.03, tauF: 0.02, left: 1, right: -0.7})
	v.gain = 0
	v.up = true
	v.cycle = 0
	v.dust = v.dust[:0]
}

// run plays frames of music, or of silence for a stretch without it,
// from the groove at the place set.
func (v *vinyl) run(buf []float32) {
	rate := v.rate
	f := v.f
	if f == nil {
		return
	}
	tipG := [2]float64{1, -1} // the walls' sign: right is the outer wall, facing the other way
	forceWall := f.force / math.Sqrt2
	for i := 0; i+1 < len(buf); i += 2 {
		if v.frame&63 == 0 || v.smudgeN == 0 {
			v.shape()
			_, v.smudgeAt = v.sd.smudged(v.r, v.theta)
			v.smudgeN = 1
		}
		x := [2]float64{float64(buf[i]), float64(buf[i+1])}
		m := v.frame
		// The lathe: the music as each wall's displacement.
		for ch := range 2 {
			vel := v.rec[ch].run(x[ch]) * v.vfs
			// The lathe's limiter of the highs keeps the tightest curves of
			// the groove to what a stylus traces at the end of the side, as
			// a cutting engineer has it: of the groove the stylus follows,
			// not of the music heard.
			v.cutLP[ch] += v.lpK * (vel - v.cutLP[ch])
			hf := vel - v.cutLP[ch]
			v.hfEnv[ch] = fmax(math.Abs(hf), v.hfEnv[ch]*v.hfEnvK)
			if v.hfEnv[ch] > v.hfMax {
				vel = v.cutLP[ch] + hf*v.hfMax/v.hfEnv[ch]
			}
			v.disp[ch] = v.leak*v.disp[ch] + vel/rate
			wall := tipG[ch] * v.disp[ch]
			v.hist[ch][m&31] = wall
			v.wb[ch][m&(bring-1)] = wall
			v.dry[ch][m&(bring-1)] = buf[i+ch]
		}
		// The dust of the groove ahead.
		if !v.up {
			for float64(v.cell)*dustCell < v.tau+0.006 {
				v.makeCell(v.cell)
				v.cell++
			}
			if m&7 == 0 {
				v.scratches(m)
			}
		}
		// The groove at twice the rate, traced.
		var eb [2]float64
		for half := range 2 {
			var w [2]float64
			for ch := range 2 {
				if half == 0 {
					s := 0.0
					for j := 0; j < 24; j++ {
						s += 2 * halfband[2*j] * v.hist[ch][(m-int64(j))&31]
					}
					w[ch] = s
				} else {
					w[ch] = v.hist[ch][(m-11)&31]
				}
			}
			if d, ok := v.trace(w); ok {
				eb = d
			}
		}
		// What the stylus felt besides: the roughness of the walls, and the
		// grit where they are worn.
		var out [2]float64
		rough := 1 + v.smudgeAt + v.burst
		for ch := range 2 {
			e := eb[ch]
			if v.gain > 0 {
				s := 0.0
				for k := range roughScales {
					v.rough[ch][k] = v.ouA[k]*v.rough[ch][k] + v.ouB[k]*v.sigma[k]*v.noise.gauss()
					s += v.rough[ch][k]
				}
				e += s * rough
				if v.sigmaGrit > 0 {
					// The load the music put on the wall where the stylus is
					// heard now, of the tip's mass as it turned, against the
					// tracking force.
					c := m - vinylDelay + 1
					a := (v.wb[ch][c&(bring-1)] - 2*v.wb[ch][(c-1)&(bring-1)] + v.wb[ch][(c-2)&(bring-1)]) * rate * rate
					load := math.Abs(a) * f.tipMass / forceWall
					v.env[ch] = fmax(load, v.env[ch]*v.envK)
					v.grit[ch] = v.ouA[0]*v.grit[ch] + v.ouB[0]*v.noise.gauss()
					if hard := v.env[ch] - gritFloor; hard > 0 {
						e += v.sigmaGrit * hard / (gritRef - gritFloor) * v.grit[ch]
					}
				}
			}
			// The cartridge gives the velocity, and the phono stage the
			// RIAA's de-emphasis.
			vel := (e - v.leak*v.eprev[ch]) * rate
			v.eprev[ch] = e
			y := v.hp[ch].run(v.phono[ch].run(vel))
			out[ch] = tipG[ch] * y / v.vfs * v.gain
		}
		for k := 0; k < len(v.thump); k++ {
			t := &v.thump[k]
			fr := (t.fmax-t.fmin)*math.Exp(-t.t/t.tauF) + t.fmin
			t.phase += 2 * math.Pi * fr / rate
			s := t.amp * math.Exp(-t.t/t.tauE) * math.Sin(t.phase-math.Pi/4) * (1 - math.Exp(-t.t/0.0004))
			out[0] += s * t.left
			out[1] += s * t.right
			t.t += 1 / rate
			if t.t > 8*t.tauE {
				v.thump = append(v.thump[:k], v.thump[k+1:]...)
				k--
			}
		}
		d := (m - vinylDelay) & (bring - 1)
		l := float64(v.dry[0][d]) + out[0]
		r := float64(v.dry[1][d]) + out[1]
		l, r = limitPeak(&v.lim, l, r)
		buf[i], buf[i+1] = float32(l), float32(r)
		// The record turns.
		v.frame++
		v.tau += 1 / rate
		v.r += v.dr
		v.theta += v.dtheta
		if v.theta >= 2*math.Pi {
			v.theta -= 2 * math.Pi
		}
		v.burst *= 0.9998
		if v.cycle > 0 && v.tau >= v.cycleEnd {
			// The locked groove comes round to where it was, dust and all,
			// past the join where the run-out met it, which kicks the
			// stylus sideways.
			v.place(v.tau-v.cycle, v.r, v.dr, v.cycle)
			v.thump = append(v.thump, thump{amp: 0.006, fmax: 90, fmin: 30, tauE: 0.05, tauF: 0.03, left: 1, right: 1})
			v.insert(particle{c: float64(2*v.frame + halfMid), h: 0.8e-6, flat: 30e-6, walls: 3, reach: 64})
		}
	}
}

// trace takes the walls' next sample at the traced rate and traces the
// groove there, lookOld + lookNew samples behind: the old stylus' wear,
// then today's stylus over the worn walls and the dust. It returns, every
// other sample, what the stylus did besides following the music, brought
// down to the device's rate.
func (v *vinyl) trace(w [2]float64) ([2]float64, bool) {
	n := v.n
	v.n++
	c1 := n - lookOld
	c := c1 - lookNew
	const mask = vring - 1
	wear := v.cond.wear
	var z [2]float64
	for ch := range 2 {
		v.w[ch][n&mask] = w[ch]
		ws := w[ch]
		if a := v.smA; a > 0 {
			// The finest detail of the walls flowed away under the old
			// stylus, over a length of the groove: a low pass in space,
			// lower in frequency where the groove passes slower.
			v.smooth[ch] = a*v.smooth[ch] + (1-a)*ws
			ws = v.smooth[ch]
		}
		v.ws[ch][n&mask] = ws
		// The old stylus' path, and the crests it pressed worn flat.
		worn := v.ws[ch][c1&mask]
		if wear > 0 {
			v.old[ch].offset(&v.ws[ch], n-1, v.oldScan, v.dx2)
			d := v.old[ch].ride(&v.ws[ch], c1, v.reachOld, v.profOld[:v.kOld+1], v.oldScan, v.dx2)
			worn -= fmax(0, worn-(d-wear))
		}
		v.worn[ch][c1&mask] = worn
		// Today's stylus, at c.
		v.tip[ch].offset(&v.worn[ch], c1-1, v.f.scan, v.dx2)
		z[ch] = v.tip[ch].ride(&v.worn[ch], c, v.reachNew, v.profNew[:v.kNew+1], v.f.scan, v.dx2)
	}
	// The dust and the scratches under it.
	if len(v.dust) > 0 && !v.up {
		z = v.defects(z, float64(c))
	}
	for ch := range 2 {
		v.e2[ch][c&63] = z[ch] - v.w[ch][c&mask]
	}
	if c&1 != 0 {
		return [2]float64{}, false
	}
	// Down to the device's rate.
	var out [2]float64
	for ch := range 2 {
		s := 0.5 * v.e2[ch][(c-halfMid)&63]
		for j := 0; j < 24; j++ {
			s += halfband[2*j] * v.e2[ch][(c-int64(2*j))&63]
		}
		out[ch] = s
	}
	return out, true
}

// tipPath is a wall as a tip traces it: the offset points of its samples,
// where the tip's centre is, less its radius, as it touches each, and the
// wall's slope and curvature there; and lately, in blocks of 16 samples,
// how far those points moved from their samples at most, and how tight a
// curve the wall made for the tip.
type tipPath struct {
	x, y, s, k [vring]float64
	moved      [4]float64
	bent       [4]float64
}

// offset works out the offset point of sample i of the wall, whose
// neighbours are in, for a tip of radius r: the wall moved out along its
// normal.
func (p *tipPath) offset(wall *[vring]float64, i int64, r, dx float64) {
	const mask = vring - 1
	prev, here, next := wall[(i-1)&mask], wall[i&mask], wall[(i+1)&mask]
	inv := 1 / dx
	s := (next - prev) * 0.5 * inv
	k := (next - 2*here + prev) * inv * inv
	s2 := s * s
	var norm float64
	if s2 < 0.01 {
		norm = 1 - s2*(0.5-0.375*s2) // 1/√(1+s²), to 1e-7 of the tip's radius
	} else {
		norm = 1 / math.Sqrt(1+s2)
	}
	shift := r * inv * s * norm
	p.x[i&mask] = float64(i) - shift
	p.y[i&mask] = here + r*(norm-1)
	p.s[i&mask] = s
	p.k[i&mask] = k
	b := (i >> 4) & 3
	if i&15 == 0 {
		p.moved[b], p.bent[b] = 0, 0
	}
	if shift < 0 {
		shift = -shift
	}
	if k < 0 {
		k = -k
	}
	p.moved[b] = fmax(p.moved[b], shift)
	p.bent[b] = fmax(p.bent[b], r*k)
}

// ride is how high a tip of radius r rides at sample c of the wall. Where
// the wall curves gently for it, as it mostly does, that is the wall less
// a second-order term in r, (r/2)s²(1 + rk) of its slope s and curvature
// k, to O(r²k²). Elsewhere it is the upper envelope of the path the tip's
// centre follows, which is the wall offset by its radius, between the
// samples' offset points by Hermite's cubic (the offset's tangent is the
// wall's); and no lower than the tip resting on the samples, for a curve
// tighter than they show. A tip wider than a trough rides over it, on its
// sides, where the offset folds. Only the samples whose points have moved
// as far as c can reach it.
func (p *tipPath) ride(wall *[vring]float64, c int64, reach int, prof []float64, r, dx float64) float64 {
	const mask = vring - 1
	if fmax(fmax(p.bent[0], p.bent[1]), fmax(p.bent[2], p.bent[3])) < 0.2 {
		s := p.s[c&mask]
		return wall[c&mask] + 0.5*r*s*s*(1+r*p.k[c&mask])
	}
	m := min(reach, int(fmax(fmax(p.moved[0], p.moved[1]), fmax(p.moved[2], p.moved[3])))+1)
	z := wall[c&mask]
	for k := 1; k < min(len(prof), 2*m+2); k++ {
		z = fmax(z, fmax(wall[(c-int64(k))&mask], wall[(c+int64(k))&mask])+prof[k])
	}
	fc := float64(c)
	for i := c - int64(m) - 1; i <= c+int64(m); i++ {
		x0, x1 := p.x[i&mask], p.x[(i+1)&mask]
		if !(x0 <= fc && fc <= x1 || x1 <= fc && fc <= x0) {
			continue
		}
		y0, y1 := p.y[i&mask], p.y[(i+1)&mask]
		d := x1 - x0
		if d < 1e-9 && d > -1e-9 {
			z = fmax(z, fmax(y0, y1))
			continue
		}
		t := (fc - x0) / d
		m0, m1 := p.s[i&mask]*dx*d, p.s[(i+1)&mask]*dx*d
		t2 := t * t
		t3 := t2 * t
		z = fmax(z, (2*t3-3*t2+1)*y0+(t3-2*t2+t)*m0+(3*t2-2*t3)*y1+(t3-t2)*m1)
	}
	return z
}

// fmax is the larger of a and b, as the hot loops want it: without the
// care for NaN of the built-in max, which none of them holds.
func fmax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// defects adds the dust and the scratches the stylus is over at c to z,
// where it rides over the walls.
func (v *vinyl) defects(z [2]float64, c float64) [2]float64 {
	r := v.f.scan
	const mask = vring - 1
	ci := int64(c)
	drop := 0
	for k := range v.dust {
		p := &v.dust[k]
		if p.c+p.reach < c {
			if k == drop {
				drop++
			}
			continue
		}
		if p.c-p.reach > c {
			break
		}
		u := (c - p.c) * v.dx2
		for ch := range 2 {
			if p.walls&(1<<ch) == 0 {
				continue
			}
			base := v.worn[ch][ci&mask]
			if p.gap > 0 {
				// A scratch: the stylus dips into its trench, and over the
				// ridges either side.
				z[ch] += trench(u, p.gap, p.depth, r)
				edge := p.gap/2 + p.flat/2 + p.h
				z[ch] = fmax(z[ch], base+fmax(grain(u-edge, p.h, p.flat, r), grain(u+edge, p.h, p.flat, r)))
				continue
			}
			z[ch] = fmax(z[ch], base+grain(u, p.h, p.flat, r))
		}
	}
	if drop > 0 {
		v.dust = v.dust[:copy(v.dust, v.dust[drop:])]
	}
	return z
}

// grain is how high the stylus' tip, of radius r, rides at u from the
// middle of a particle h high whose top is flat for flat along the
// groove: a sphere's, h across, rolled over by the tip, at either end.
func grain(u, h, flat, r float64) float64 {
	u = math.Abs(u) - flat/2
	if u <= 0 {
		return h
	}
	rho := h / 2
	d := sq(r+rho) - u*u
	if d <= 0 {
		return -1
	}
	return math.Sqrt(d) - r + rho
}

// trench is how far the tip of radius r dips at u from the middle of a
// trench gap long along the groove and depth deep: where it rests on the
// nearer edge, or the floor.
func trench(u, gap, depth, r float64) float64 {
	d := gap/2 - math.Abs(u)
	switch {
	case d <= 0:
		return 0
	case d >= r:
		return -depth
	}
	return max(-depth, math.Sqrt(r*r-d*d)-r)
}

// reach is how far either side of its middle the tip feels a particle,
// in samples of the traced rate.
func (v *vinyl) reach(h, flat float64) float64 {
	return (flat/2+math.Sqrt(2*v.f.scan*h))/v.dx2 + 1
}

// cAt is the sample of the traced rate the stylus is at, at time tau of
// the groove.
func (v *vinyl) cAt(tau float64) float64 {
	return v.anchorC + (tau-v.anchorTau)*2*v.rate
}

// makeCell lays the dust of cell k of the groove, the same each time: as
// much as the condition, the smudges and the stylus' settling make it.
func (v *vinyl) makeCell(k int64) {
	if v.dx2 == 0 {
		return
	}
	rnd := newRNG(mix64(v.sd.key, uint64(k)))
	t0 := float64(k) * dustCell
	mid := t0 + dustCell/2
	r := v.r + (mid-v.tau)*v.dr*v.rate
	theta := v.sd.theta0 + 2*math.Pi*v.rev*mid
	smudge, _ := v.sd.smudged(r, math.Mod(theta, 2*math.Pi))
	speed := 2 * math.Pi * r * v.rev
	mean := v.cond.dust * speed * dustCell * (1 + smudge + 5*v.burst)
	count := poisson(mean, rnd.float())
	hMax := 20e-6
	if v.g.Speed == Shellac {
		hMax = 40e-6
	}
	for range count {
		tau := t0 + rnd.float()*dustCell
		h := min(hMax, hMin*math.Pow(1-rnd.float(), -1/dustAlpha))
		flat := 0.0
		switch kind := rnd.float(); {
		case kind < 0.6: // a grain, hard
		case kind < 0.9: // a flake, soft, crushed under the tip
			flat = h * (2 + 7*rnd.float())
			h *= 0.25 + 0.5*rnd.float()
		default: // a fibre
			flat = h * (10 + 30*rnd.float())
			h *= 0.15 + 0.4*rnd.float()
		}
		var walls uint8
		switch w := rnd.float(); {
		case w < 0.42:
			walls = 1
		case w < 0.84:
			walls = 2
		default:
			walls = 3 // across the groove: it lifts the stylus
		}
		v.insert(particle{c: v.cAt(tau), h: h, flat: flat, walls: walls, reach: v.reach(h, flat)})
	}
}

// scratches finds the scratches the stylus crosses at frame m, and lays
// them in its way.
func (v *vinyl) scratches(m int64) {
	for i := range v.sd.scratches {
		s := &v.sd.scratches[i]
		if v.r > s.r1 || v.r < s.r2 {
			v.scratchWas[i] = math.NaN()
			continue
		}
		d := v.theta - s.angleAt(v.r)
		d -= 2 * math.Pi * math.Round(d/(2*math.Pi))
		was := v.scratchWas[i]
		v.scratchWas[i] = d
		if math.IsNaN(was) || was >= 0 || d < 0 || d-was > 1 {
			continue
		}
		// Across the groove at the angle it lies at: its trench and ridges
		// are longer along the groove the more slanting it is.
		// It crosses the two walls where the stylus touches them, 25 µm
		// apart across the groove: the one later than the other, the more
		// so the more it slants.
		frac := -was / (d - was) // of the 8 frames since it was looked for
		c := float64(2*(m-8)+halfMid) + 16*frac
		apart := 25e-6 * math.Sqrt(1-s.sinA*s.sinA) / s.sinA / v.dx2
		rnd := newRNG(mix64(v.sd.key^0x5c, uint64(i)))
		for wall := range 2 {
			p := particle{
				c: c + (float64(wall)-0.5)*apart, h: s.lip * (0.6 + 0.4*rnd.float()), flat: s.lipR / s.sinA,
				walls: uint8(1 << wall), gap: s.width / s.sinA, depth: s.depth,
			}
			p.reach = (p.gap/2+p.flat+p.h+math.Sqrt(2*v.f.scan*p.h))/v.dx2 + 2
			v.insert(p)
		}
		if s.depth > 6e-6 {
			// A deep one jolts the arm.
			v.thump = append(v.thump, thump{amp: 0.004 * (s.depth - 6e-6) / 1e-6, fmax: 120, fmin: 35, tauE: 0.035, tauF: 0.02, left: 1, right: -0.8})
		}
	}
}

// insert lays a particle among the dust, in order.
func (v *vinyl) insert(p particle) {
	i := len(v.dust)
	v.dust = append(v.dust, p)
	for ; i > 0 && v.dust[i-1].c > p.c; i-- {
		v.dust[i] = v.dust[i-1]
	}
	v.dust[i] = p
}

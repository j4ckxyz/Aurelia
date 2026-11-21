package audio

import (
	"math"
	"time"
)

// The records that vinyl playback (vinyl.go) plays the music from: how an
// album is laid out on them, what the years and the playing have done to
// them, and the dust and scratches of each side.

// Speed is the kind of record: how fast it turns, and how large it is.
type Speed int

const (
	// LP is a long-playing record of 12 inches, at 33⅓ revolutions a
	// minute.
	LP Speed = iota
	// Single is a record of 7 inches, at 45.
	Single
	// Shellac is a record of before vinyl, of 10 inches, at 78.
	Shellac
)

// format is the size of a kind of record, as the RIAA's and the IEC's
// standards have it, and the stylus that plays it today.
type format struct {
	rpm float64
	// The radii, in m, of the edge, the first groove of music, the last
	// the standard allows, and the locked groove past the run-out.
	edge, first, inner, lock float64
	// pitch is how far the groove moves inwards a revolution, in m, at
	// most and at least: how tightly the music is cut.
	pitchMin, pitchMax float64
	// The stylus: its scanning radius, the radius it shows the groove
	// along its length, in m; its tip's effective mass, kg; and the
	// tracking force, N.
	scan, tipMass, force float64
	// oldScan is the radius of the stylus that wore the record, for a
	// record of after 1958: a spherical tip of 0.7 mil, as most were.
	oldScan float64
	// leadIn and leadOut are the pitches of the lead-in groove and the
	// run-out, m a revolution: wide, for the stylus to cross quickly.
	leadIn, leadOut float64
	// flat78 plays it back without the RIAA curve's treble cut: 78s were
	// cut without its pre-emphasis.
	flat78 bool
}

var formats = [...]format{
	LP: {
		rpm: 100.0 / 3, edge: 0.1463, first: 0.1435, inner: 0.0603, lock: 0.0532,
		pitchMin: 40e-6, pitchMax: 160e-6,
		scan: 7.6e-6, tipMass: 0.4e-6, force: 0.0196, // an elliptical 0.3 × 0.7 mil at 2 g
		oldScan: 17.8e-6, leadIn: 1.2e-3, leadOut: 6.4e-3,
	},
	Single: {
		rpm: 45, edge: 0.0875, first: 0.0840, inner: 0.0603, lock: 0.0532,
		pitchMin: 40e-6, pitchMax: 150e-6,
		scan: 7.6e-6, tipMass: 0.4e-6, force: 0.0196,
		oldScan: 17.8e-6, leadIn: 1.2e-3, leadOut: 6.4e-3,
	},
	Shellac: {
		rpm: 78.26, edge: 0.1270, first: 0.1200, inner: 0.0500, lock: 0.0450,
		pitchMin: 150e-6, pitchMax: 300e-6,
		scan: 63.5e-6, tipMass: 1.0e-6, force: 0.044, // a 78 stylus of 2.5 mil at 4.5 g
		oldScan: 76e-6, leadIn: 2.5e-3, leadOut: 8e-3, // steel needles
		flat78: true,
	},
}

// Groove is where a track is cut on a record: the place on the record
// that vinyl playback plays it from. CutRecord lays an album out.
type Groove struct {
	// Record names the record: its dust and scratches are its own, the
	// same each time it plays.
	Record uint64
	// Side is the side of the record, 0 for the first, and Face the side's
	// place among the album's, 0 for side A.
	Side, Face int
	Speed      Speed
	// Year is the album's, 0 when it is not known.
	Year int
	// At is where the track begins on its side, and SideLen how long the
	// side plays.
	At, SideLen time.Duration
	// First and Last tell that the track begins its side and ends it.
	First, Last bool
	// Gain is what the server measured would bring the loudest song of the
	// side to -18 LUFS, in dB, and 0 when it is not known: the lathe cuts
	// a loud master lower, for the groove to hold any master alike.
	Gain float64
}

// follows reports whether the track of h is cut right after that of g,
// d long, on the same side: the stylus goes on from one to the other
// without being lifted.
func (g *Groove) follows(h *Groove, d time.Duration) bool {
	if g == nil || h == nil || g.Record != h.Record || g.Side != h.Side || g.Speed != h.Speed {
		return false
	}
	gap := h.At - (g.At + d)
	return gap > -2*time.Second && gap < 2*time.Second
}

// The longest side an album is cut on: past it, the groove is cut so
// tightly and so quietly that it takes two more sides.
const (
	maxSideLP     = 24 * time.Minute
	maxSingleSide = 15 * time.Minute
)

// CutRecord lays an album out on records as a pressing plant would: each
// disc of it on records of its own, its songs in order, never one across
// two sides, and the sides as even as they can be. An album of before
// 1950 is on 78s, a song a side; one of four songs or fewer, a quarter of
// an hour in all, on a 45; the rest on LPs, of as many sides of 24
// minutes at most as it takes. seed names the album, year is its year,
// and discs holds the lengths of the songs of each disc; the result holds
// a groove for each of them.
func CutRecord(seed uint64, year int, discs [][]time.Duration) [][]Groove {
	var total time.Duration
	n := 0
	for _, disc := range discs {
		for _, d := range disc {
			total += d
			n++
		}
	}
	speed := LP
	switch {
	case year > 0 && year < 1950:
		speed = Shellac
	case n <= 4 && total <= maxSingleSide:
		speed = Single
	}
	out := make([][]Groove, len(discs))
	record := uint64(0)
	face := 0
	for di, disc := range discs {
		out[di] = make([]Groove, len(disc))
		if len(disc) == 0 {
			continue
		}
		var sides int
		switch speed {
		case Shellac:
			sides = len(disc)
		case Single:
			sides = min(2, len(disc))
		default:
			var sum time.Duration
			for _, d := range disc {
				sum += d
			}
			sides = max(2, int((sum+maxSideLP-1)/maxSideLP))
			sides += sides % 2 // a record has two
			sides = min(sides, len(disc))
		}
		bounds := evenSides(disc, sides)
		for s := range sides {
			if s > 0 && s%2 == 0 {
				record++
			}
			from, to := bounds[s], bounds[s+1]
			var length time.Duration
			for _, d := range disc[from:to] {
				length += d
			}
			var at time.Duration
			for i := from; i < to; i++ {
				out[di][i] = Groove{
					Record: mix64(seed, record), Side: s % 2, Face: face + s, Speed: speed, Year: year,
					At: at, SideLen: length, First: i == from, Last: i == to-1,
				}
				at += disc[i]
			}
		}
		record++ // the next disc is a record of its own
		face += sides + sides%2
	}
	return out
}

// evenSides parts songs, in order, into sides whose longest is as short
// as it can be: the indexes where each side begins, and the end.
func evenSides(songs []time.Duration, sides int) []int {
	n := len(songs)
	sum := make([]time.Duration, n+1)
	for i, d := range songs {
		sum[i+1] = sum[i] + d
	}
	// best[k][i] is the longest side of the best way to put the first i
	// songs on k sides, and cut[k][i] where its last side begins.
	const inf = time.Duration(math.MaxInt64)
	best := make([][]time.Duration, sides+1)
	cut := make([][]int, sides+1)
	for k := range best {
		best[k] = make([]time.Duration, n+1)
		cut[k] = make([]int, n+1)
		for i := range best[k] {
			best[k][i] = inf
		}
	}
	best[0][0] = 0
	for k := 1; k <= sides; k++ {
		for i := k; i <= n; i++ {
			for j := k - 1; j < i; j++ {
				if best[k-1][j] == inf {
					continue
				}
				if m := max(best[k-1][j], sum[i]-sum[j]); m < best[k][i] {
					best[k][i], cut[k][i] = m, j
				}
			}
		}
	}
	bounds := make([]int, sides+1)
	bounds[sides] = n
	for k, i := sides, n; k > 0; k-- {
		i = cut[k][i]
		bounds[k-1] = i
	}
	return bounds
}

// The grades of a record's condition, as Goldmine's standard has them,
// which collectors grade by.
const (
	Mint = iota
	NearMint
	VeryGoodPlus
	VeryGood
	GoodPlus
	Good
	Fair
)

// GradeNames are the grades as they are written.
var GradeNames = [...]string{"Mint", "Near Mint", "Very Good Plus", "Very Good", "Good Plus", "Good", "Fair"}

// AgeGrade is the condition a record of year is likely in, now: played
// most in its first years and less after, and handled, and gathering
// dust, all along. A record of the year is between Mint and Near Mint;
// one of twenty years is Very Good Plus; one of fifty or more, nearer
// Very Good. Those of before 1963 were played on heavy arms with wide
// styli, which wore them more, and 78s more still. A year not known is
// taken as Very Good Plus.
func AgeGrade(year, now int) float64 {
	if year <= 0 {
		return VeryGoodPlus
	}
	age := float64(max(0, now-year))
	g := 0.8 + 2.4*(1-math.Exp(-age/30))
	switch {
	case year < 1950:
		g += 0.8
	case year < 1963:
		g += 0.5
	}
	return min(g, Good)
}

// condition is what a grade means for a record's surface.
type condition struct {
	// hiss is the level of the surface noise of the compound, as worn, in
	// dB(A) under a sine at full scale, of a side cut at the usual level.
	hiss float64
	// dust is how many particles a metre of groove holds on each wall,
	// the smallest among them a few hundredths of a µm high.
	dust float64
	// smudges and scratches are how many of each a side has, on average.
	smudges, scratches float64
	// wear is how deep, in m, the crests of the walls that the old
	// stylus pressed hardest are worn flat, and smooth the length in m
	// over which the walls' finest detail has flowed away.
	wear, smooth float64
	// grit is how much rougher, in dB over the surface noise, the walls
	// are where the old stylus was hammered hardest (by a load of a fifth
	// of the tracking force).
	grit float64
}

// conditions are the grades' surfaces. A new record's noise is that of a
// quiet modern pressing, 70 dB and more under the music's peaks; each
// grade adds dust, smudges where hands held it, scratches, and wear.
// Goldmine's Very Good has "surface noise ... evident upon playing,
// especially in soft passages and during a song's intro and fade, but
// [it] will not overpower the music"; Good, "significant surface noise
// and scratches and visible groove wear".
var conditions = [...]condition{
	Mint:         {hiss: -72, dust: 40},
	NearMint:     {hiss: -68, dust: 120, smudges: 0.4, scratches: 0.1, wear: 0.01e-6, smooth: 0.5e-6},
	VeryGoodPlus: {hiss: -63, dust: 350, smudges: 1.2, scratches: 0.8, wear: 0.03e-6, smooth: 1.0e-6, grit: 8},
	VeryGood:     {hiss: -58, dust: 900, smudges: 2.5, scratches: 2, wear: 0.07e-6, smooth: 1.8e-6, grit: 14},
	GoodPlus:     {hiss: -53, dust: 2000, smudges: 4, scratches: 4, wear: 0.12e-6, smooth: 2.6e-6, grit: 19},
	Good:         {hiss: -48, dust: 4000, smudges: 6, scratches: 7, wear: 0.18e-6, smooth: 3.4e-6, grit: 23},
	Fair:         {hiss: -43, dust: 8000, smudges: 9, scratches: 12, wear: 0.28e-6, smooth: 4.5e-6, grit: 27},
}

// conditionAt is the surface of grade g, between the grades.
func conditionAt(g float64) condition {
	g = max(0, min(Fair, g))
	i := min(int(g), Fair-1)
	t := g - float64(i)
	a, b := conditions[i], conditions[i+1]
	lerp := func(x, y float64) float64 { return x + (y-x)*t }
	return condition{
		hiss: lerp(a.hiss, b.hiss), dust: math.Exp(lerp(math.Log(a.dust), math.Log(b.dust))),
		smudges: lerp(a.smudges, b.smudges), scratches: lerp(a.scratches, b.scratches),
		wear: lerp(a.wear, b.wear), smooth: lerp(a.smooth, b.smooth), grit: lerp(a.grit, b.grit),
	}
}

// era is what the compound of a year's records adds to its noise, in dB,
// and how much more dust it takes, as fillers and voids crackle: the
// shellac of 78s, loud with the slate dust it held; the vinyl of the first
// LPs; the thin, reground vinyl of the 1970s and 1980s, which pressed
// noisier; and the virgin vinyl of today's pressings.
func era(year int, speed Speed) (db, dust, filler float64) {
	switch {
	case speed == Shellac:
		return 28, 4, 2500
	case year <= 0:
		return 1, 1, 0
	case year < 1958:
		return 3, 1.2, 0
	case year < 1970:
		return 1, 1, 0
	case year < 1990:
		return 3, 1.4, 0
	case year < 2006:
		return 1, 1, 0
	}
	return 0, 1, 0
}

// smudge is a part of a side that was held, or slid against the sleeve:
// more dust there, and a rougher surface, crossed once a revolution for as
// many revolutions as it is wide.
type smudge struct {
	r, theta   float64 // where it is: m, radians
	dr, dtheta float64 // how far it reaches, as a Gaussian's deviations
	dust       float64 // how much more dust it holds, at its middle
	rough      float64 // and how much rougher the walls are there
}

// scratch is a line across the grooves: a trench, with the material it
// ploughed up raised along either side.
type scratch struct {
	r1, r2 float64 // the radii it spans, m, r1 the outer
	theta  float64 // its angle at r1
	slant  float64 // how its angle changes inwards, radians a metre
	sinA   float64 // the sine of the angle it crosses the groove at
	lip    float64 // how high the ridges along it stand, m
	lipR   float64 // and their radius
	width  float64 // how wide the trench is across itself, m
	depth  float64 // and how deep
}

// angleAt is the scratch's angle at the radius r.
func (s *scratch) angleAt(r float64) float64 { return s.theta + s.slant*(s.r1-r) }

// side is what the surface of a side of a record holds besides its dust.
type side struct {
	key       uint64
	theta0    float64 // the record's angle as the side's music begins
	smudges   []smudge
	scratches []scratch
}

// candidates is how many smudges and scratches a side could have: a
// worse grade has more of the same ones, so that turning the condition up
// adds to a record's scratches rather than giving it others.
const candidates = 24

// cutSide makes the surface of a side of the grade's condition, from the
// first groove of music inwards to inner.
func cutSide(key uint64, c condition, g float64, f *format, inner float64) side {
	rng := newRNG(key)
	s := side{key: key, theta0: rng.float() * 2 * math.Pi}
	span := f.first - inner
	for range candidates {
		u := rng.float()
		sm := smudge{
			r:      inner + rng.float()*span,
			theta:  rng.float() * 2 * math.Pi,
			dr:     (1.5 + 6*rng.float()) * 1e-3,
			dtheta: (4 + 22*rng.float()) * math.Pi / 180,
			dust:   2 + 10*rng.float(),
			rough:  0.3 + 1.2*rng.float(),
		}
		if u*candidates < c.smudges {
			s.smudges = append(s.smudges, sm)
		}
	}
	// Scratches are worse on worse records: deeper, with higher ridges.
	harm := 0.5 + g/4
	for range candidates {
		u := rng.float()
		r1 := inner + rng.float()*span
		length := min(40e-3, -6e-3*math.Log(1-0.999*rng.float()))
		sc := scratch{
			r1: r1, r2: max(inner, r1-length),
			theta: rng.float() * 2 * math.Pi,
			slant: (rng.float() - 0.5) * 60, // a scratch that is not straight across
			sinA:  math.Sin((25 + 65*rng.float()) * math.Pi / 180),
			lip:   (0.25 + 1.8*rng.float()*rng.float()) * harm * 1e-6,
			lipR:  (2 + 4*rng.float()) * 1e-6,
			width: (6 + 24*rng.float()) * 1e-6,
			depth: (2 + 10*rng.float()) * harm * 1e-6,
		}
		if u*candidates < c.scratches {
			s.scratches = append(s.scratches, sc)
		}
	}
	return s
}

// smudged is how much more dust, and how much rougher, the side is at r
// and theta, for its smudges.
func (s *side) smudged(r, theta float64) (dust, rough float64) {
	for i := range s.smudges {
		m := &s.smudges[i]
		dt := math.Remainder(theta-m.theta, 2*math.Pi)
		x := sq((r-m.r)/m.dr) + sq(dt/m.dtheta)
		if x > 12 {
			continue
		}
		w := math.Exp(-x / 2)
		dust += m.dust * w
		rough += m.rough * w
	}
	return dust, rough
}

// rng is splitmix64: fast, and the same numbers from the same seed, for
// a record to be the same each time.
type rng struct{ s uint64 }

func newRNG(seed uint64) *rng { return &rng{s: seed} }

func (r *rng) next() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// float is uniform in [0, 1).
func (r *rng) float() float64 { return float64(r.next()>>11) / (1 << 53) }

// gauss is a deviate of mean 0 and deviation 1, the sum of two uniforms:
// for noise, which is filtered into a normal one, and needs it fast.
func (r *rng) gauss() float64 {
	a := r.next()
	return ((float64(a>>32)+float64(a&0xffffffff))/(1<<32) - 1) * math.Sqrt(6)
}

// poisson is a count of events with mean m, by inversion of u in [0, 1):
// the same u gives at least as many for a larger m.
func poisson(m, u float64) int {
	if m <= 0 {
		return 0
	}
	if m > 50 {
		// Normal, for counts that large.
		return max(0, int(m+math.Sqrt(m)*normalQuantile(u)+0.5))
	}
	p := math.Exp(-m)
	cum := p
	k := 0
	for u > cum && k < 500 {
		k++
		p *= m / float64(k)
		cum += p
	}
	return k
}

// normalQuantile is the inverse of the normal distribution, after Acklam,
// within about 1e-9.
func normalQuantile(p float64) float64 {
	p = max(1e-12, min(1-1e-12, p))
	a := [6]float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := [5]float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := [6]float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := [4]float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}
	switch {
	case p < 0.02425:
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p > 1-0.02425:
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
	q := p - 0.5
	r := q * q
	return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q / (((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
}

// mix64 makes one seed of two.
func mix64(a, b uint64) uint64 {
	r := rng{s: a ^ (b * 0x9e3779b97f4a7c15)}
	return r.next()
}

// HashString makes a seed of an ID, as of an album, by FNV-1a.
func HashString(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

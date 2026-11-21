package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pressed is a record of grade with g's side on it, ready to play: newly
// made, its surface as the grade has it.
func pressed(g Groove, grade float64) *vinyl {
	v := newVinyl(testRate)
	v.setGrade(grade, 0)
	v.context(g)
	return v
}

// press plays in through v from at into g's side, in runs as the engine
// gives them, and returns what comes out, the music's delay taken off.
func press(v *vinyl, g Groove, at time.Duration, in []float32) []float32 {
	seg := &segment{id: "test", groove: &g, dur: g.SideLen}
	out := append([]float32(nil), in...)
	at0 := int64(at.Seconds() * v.rate)
	for i := 0; i < len(out)/2; i += 512 {
		j := min(len(out)/2, i+512)
		v.play(out[2*i:2*j], seg, at0+int64(i))
	}
	return out[2*vinylDelay:]
}

// clean takes the dust and the scratches off v's side, for its hiss alone.
func clean(v *vinyl) {
	v.cond.dust = 0
	v.sd.scratches, v.sd.smudges = nil, nil
	v.scratchWas = nil
}

// silence is secs of nothing, in two channels.
func silence(secs float64) []float32 { return make([]float32, 2*int(secs*testRate)) }

// toneOf is a sine of f Hz, amp at its peak, in both channels.
func toneOf(f, amp, secs float64) []float32 {
	n := int(secs * testRate)
	out := make([]float32, 2*n)
	for i := range n {
		s := float32(amp * math.Sin(2*math.Pi*f*float64(i)/testRate))
		out[2*i], out[2*i+1] = s, s
	}
	return out
}

// aLevel is the A-weighted level of a channel of snd, from skip frames on,
// in dB under a sine at full scale.
func aLevel(snd []float32, ch, skip int) float64 {
	aw := aWeighting(testRate)
	sum, n := 0.0, 0
	for i := ch; i < len(snd); i += 2 {
		y := float64(snd[i])
		for k := range aw {
			y = aw[k].run(y)
		}
		if i/2 >= skip {
			sum += y * y
			n++
		}
	}
	return 10*math.Log10(sum/float64(n)) - 20*math.Log10(1/math.Sqrt2)
}

// lpSide is a side of a record of 2000 with 20 minutes of music.
var lpSide = Groove{Record: 7, Speed: LP, Year: 2000, SideLen: 20 * time.Minute, First: true, Last: true}

// The surface noise of each grade is as loud as the grade says, at the
// middle of a side cut at the usual level: the fit of the roughness holds.
func TestVinylHissLevels(t *testing.T) {
	for grade := Mint; grade <= Fair; grade++ {
		v := pressed(lpSide, float64(grade))
		clean(v)
		out := press(v, lpSide, 10*time.Minute, silence(4))
		want := conditions[grade].hiss + 1 // a record of 2000 adds 1 dB
		for ch := range 2 {
			got := aLevel(out, ch, testRate/2)
			if math.Abs(got-want) > 1 {
				t.Errorf("%s, channel %d: hiss %.1f dB(A), want %.1f", GradeNames[grade], ch, got, want)
			}
		}
	}
}

// The noise is that of the groove the stylus is in: the inner grooves, which
// pass slower, are duller, as the same roughness goes by more slowly; and a
// side cut long, and so quieter, is noisier for its music.
func TestVinylHissFollowsTheGroove(t *testing.T) {
	play := func(g Groove, at time.Duration) []float32 {
		v := pressed(g, VeryGood)
		clean(v)
		return press(v, g, at, silence(3))
	}
	// highs is the part of the noise's power over 6 kHz.
	highs := func(snd []float32) float64 {
		var hp filter2
		hp.butterworth(true, 6000, testRate)
		var all, hi float64
		for i := testRate; i < len(snd)/2; i++ {
			x := float64(snd[2*i])
			y := hp.run(x)
			all += x * x
			hi += y * y
		}
		return hi / all
	}
	outer, inner := highs(play(lpSide, 0)), highs(play(lpSide, 19*time.Minute))
	t.Logf("VG: %.0f%% of the noise over 6 kHz on the outer grooves, %.0f%% on the inner", 100*outer, 100*inner)
	if inner > outer*0.9 {
		t.Errorf("the inner grooves' noise is %.2f over 6 kHz, the outer's %.2f: the inner should be duller", inner, outer)
	}
	long := lpSide
	long.SideLen = 28 * time.Minute
	l, u := aLevel(play(long, 14*time.Minute), 0, testRate/2), aLevel(play(lpSide, 10*time.Minute), 0, testRate/2)
	if l < u+1.5 {
		t.Errorf("a long side %.1f dB(A), a usual one %.1f: the long should be noisier", l, u)
	}
}

// clicks counts the clicks of a channel of snd louder than each of the
// levels, in dB under full scale, a second: peaks at least 3 ms apart.
func clicks(snd []float32, ch int, levels []float64) []float64 {
	counts := make([]float64, len(levels))
	last := make([]int, len(levels))
	for i := range last {
		last[i] = -1 << 30
	}
	for i := ch; i < len(snd); i += 2 {
		a := math.Abs(float64(snd[i]))
		for k, db := range levels {
			if a > math.Pow(10, db/20) && i/2-last[k] > testRate*3/1000 {
				counts[k]++
				last[k] = i / 2
			}
		}
	}
	secs := float64(len(snd)/2) / testRate
	for k := range counts {
		counts[k] /= secs
	}
	return counts
}

// The worse the record, the more it ticks and pops: a new one now and
// then, a worn one all the time.
func TestVinylClicksGrowWithWear(t *testing.T) {
	levels := []float64{-40, -30, -20}
	var before []float64
	for _, grade := range []int{Mint, NearMint, VeryGoodPlus, VeryGood, GoodPlus, Good, Fair} {
		v := pressed(lpSide, float64(grade))
		out := press(v, lpSide, 5*time.Minute, silence(20))
		got := clicks(out, 0, levels)
		t.Logf("%-15s %5.2f /s over -40 dB, %5.2f over -30, %5.3f over -20; %.1f dB(A) in all", GradeNames[grade], got[0], got[1], got[2], aLevel(out, 0, testRate/2))
		if before != nil && got[1] < before[1] {
			t.Errorf("%s ticks less than the grade before it: %.2f against %.2f a second", GradeNames[grade], got[1], before[1])
		}
		before = got
	}
	if before[1] < 2 {
		t.Errorf("a Fair record ticks over -30 dB %.2f times a second, want it all the time", before[1])
	}
}

// Worn walls lose treble as the physics has it: the smoothing of the walls
// over a length of groove is a low pass of exactly its frequency, which
// tests that the groove's sound and the music meet to the sample.
func TestVinylWearLosesTreble(t *testing.T) {
	for _, f := range []float64{1000, 5000, 10000, 15000} {
		v := pressed(lpSide, VeryGood)
		clean(v)
		v.cond.wear = 0
		v.sigma = [3]float64{}
		at := 19 * time.Minute // the inner grooves
		// Quiet, for the stylus to follow it: a loud tone this high loses
		// to the tracing too, at the inner grooves.
		out := press(v, lpSide, at, toneOf(f, 0.01, 1))
		got := 20 * math.Log10(amplitude(out, f, testRate/4, testRate*3/4)/0.01)
		// The smoothing is a low pass of one pole, at the traced rate.
		r := v.radiusAt(at.Seconds())
		dx := 2 * math.Pi * r * v.rev / (2 * testRate)
		a := math.Exp(-dx / v.cond.smooth)
		w := 2 * math.Pi * f / (2 * testRate)
		want := 20 * math.Log10(cmplxAbs(1-a, 0)/cmplxAbs(1-a*math.Cos(w), a*math.Sin(w)))
		t.Logf("%5.0f Hz: %.2f dB, the low pass %.2f dB", f, got, want)
		if math.Abs(got-want) > 0.15 {
			t.Errorf("%.0f Hz is %.2f dB, want %.2f", f, got, want)
		}
	}
}

func cmplxAbs(re, im float64) float64 { return math.Hypot(re, im) }

// A new record plays the music as it is: no louder, no quieter, and only
// the faintest of distortion where the groove is wide.
func TestVinylKeepsTheMusic(t *testing.T) {
	v := pressed(lpSide, Mint)
	clean(v)
	v.sigma = [3]float64{}
	in := toneOf(1000, 0.25, 1)
	out := press(v, lpSide, 0, in)
	if got := amplitude(out, 1000, testRate/4, testRate*3/4); math.Abs(20*math.Log10(got/0.25)) > 0.02 {
		t.Errorf("1 kHz comes out at %.3f, want 0.25", got)
	}
	// The rest is what the groove adds: far under it.
	var diff, sig float64
	for i := testRate / 4; i < testRate*3/4; i++ {
		d := float64(out[2*i] - in[2*i])
		diff += d * d
		sig += float64(in[2*i]) * float64(in[2*i])
	}
	if db := 10 * math.Log10(diff/sig); db > -45 {
		t.Errorf("the groove adds %.1f dB to the music, want under -45", db)
	}
}

// pink is pink noise, as loud masters are close to, at db RMS under full
// scale, after Paul Kellet's filter.
func pink(db, secs float64) []float32 {
	rnd := newRNG(3)
	n := int(secs * testRate)
	out := make([]float32, 2*n)
	var b [7]float64
	var sum float64
	for i := range n {
		w := rnd.gauss()
		b[0] = 0.99886*b[0] + w*0.0555179
		b[1] = 0.99332*b[1] + w*0.0750759
		b[2] = 0.96900*b[2] + w*0.1538520
		b[3] = 0.86650*b[3] + w*0.3104856
		b[4] = 0.55000*b[4] + w*0.5329522
		b[5] = -0.7616*b[5] - w*0.0168980
		y := b[0] + b[1] + b[2] + b[3] + b[4] + b[5] + b[6] + w*0.5362
		b[6] = w * 0.115926
		out[2*i], out[2*i+1] = float32(y), float32(y)
		sum += y * y
	}
	scale := float32(math.Pow(10, db/20) / math.Sqrt(sum/float64(n)))
	for i := range out {
		out[i] *= scale
	}
	return out
}

// A loud master, as a record is not cut from, is traced without falling
// apart: the lathe's limiter keeps its highs to what the stylus follows,
// and a side whose loudness is known is cut as loud as any other.
func TestVinylLoudMasterTraces(t *testing.T) {
	in := pink(-14, 3)
	distortion := func(g Groove, at time.Duration) float64 {
		v := pressed(g, Mint)
		clean(v)
		v.sigma = [3]float64{}
		out := press(v, g, at, in)
		var e, sig float64
		for i := testRate / 2; i < len(out)/2; i++ {
			d := float64(out[2*i] - in[2*i])
			e += d * d
			sig += float64(in[2*i]) * float64(in[2*i])
		}
		return -10 * math.Log10(e/sig)
	}
	loud := lpSide
	loud.Gain = -10 // a master at -8 LUFS
	outer, inner, cut := distortion(lpSide, 0), distortion(lpSide, 19*time.Minute), distortion(loud, 19*time.Minute)
	t.Logf("pink noise at -14 dBFS: the groove's distortion %.1f dB under it on the outer grooves, %.1f on the inner, %.1f cut for its loudness", outer, inner, cut)
	if outer < 32 || inner < 20 || cut < inner+3 {
		t.Errorf("distortion %.1f dB under the music outside, %.1f inside, %.1f cut for its loudness: want 32, 20 and 3 more", outer, inner, cut)
	}
}

// harmonic2 is how strong the second harmonic of f is, against f, in the
// difference of the channels and in their sum: the vertical motion of the
// stylus, and the lateral.
func harmonic2(out []float32, f float64) (vertical, lateral float64) {
	n := len(out) / 2
	side, mid := make([]float32, 2*n), make([]float32, 2*n)
	for i := range n {
		d := (out[2*i] - out[2*i+1]) / 2
		s := (out[2*i] + out[2*i+1]) / 2
		side[2*i], mid[2*i] = d, s
	}
	fund := amplitude(mid, f, testRate/4, n-testRate/4)
	return amplitude(side, 2*f, testRate/4, n-testRate/4) / fund, amplitude(mid, 2*f, testRate/4, n-testRate/4) / fund
}

// The stylus cannot follow the tightest curves: the second harmonic of a
// loud tone is the pinch of the groove, vertical, and grows as the groove
// slows towards the label, where it is a few percent.
func TestVinylTracingDistortion(t *testing.T) {
	at := func(when time.Duration) float64 {
		v := pressed(lpSide, Mint)
		clean(v)
		v.sigma = [3]float64{}
		vert, lat := harmonic2(press(v, lpSide, when, toneOf(1000, 0.5, 1.5)), 1000)
		if lat > vert/3 {
			t.Errorf("the second harmonic is %.3f%% lateral and %.3f%% vertical: it should be the vertical pinch", lat*100, vert*100)
		}
		return vert
	}
	outer, inner := at(0), at(19*time.Minute)
	t.Logf("1 kHz at -6 dBFS: second harmonic %.2f%% on the outer grooves, %.2f%% on the inner", outer*100, inner*100)
	if !(inner > 2*outer && inner > 0.003 && inner < 0.06) {
		t.Errorf("second harmonic %.3f outer, %.3f inner: want it a few times worse inside, at a few percent", outer, inner)
	}
}

// Dust across the groove lifts the stylus, which is heard in both channels
// in opposite phase: the noise of a record is the quieter in mono, as the
// collectors' trick has it.
func TestVinylCrackleIsQuieterInMono(t *testing.T) {
	v := pressed(lpSide, Good)
	v.sigma = [3]float64{}
	out := press(v, lpSide, 5*time.Minute, silence(20))
	var ll, rr, lr float64
	for i := 0; i+1 < len(out); i += 2 {
		l, r := float64(out[i]), float64(out[i+1])
		ll += l * l
		rr += r * r
		lr += l * r
	}
	corr := lr / math.Sqrt(ll*rr)
	mono := (ll + rr + 2*lr) / 4 / ((ll + rr) / 2)
	t.Logf("the crackle's correlation between the channels %.3f; in mono %.1f dB", corr, 10*math.Log10(mono))
	if corr > -0.02 {
		t.Errorf("the channels' crackle correlates by %.3f: want it in opposite phase in part", corr)
	}
}

// A scratch ticks once a revolution: 1.8 s apart on an LP.
func TestVinylScratchTicksEachRevolution(t *testing.T) {
	v := pressed(lpSide, Mint)
	clean(v)
	v.sigma = [3]float64{}
	r := v.radiusAt(300)
	v.sd.scratches = []scratch{{r1: r + 2e-3, r2: r - 2e-3, theta: 1, sinA: 0.8, lip: 1.5e-6, lipR: 4e-6, width: 20e-6, depth: 5e-6}}
	v.scratchWas = []float64{math.NaN()}
	out := press(v, lpSide, 5*time.Minute, silence(8))
	var ticks []float64
	last := -1.0
	for i := 0; i < len(out); i += 2 {
		if math.Abs(float64(out[i])) > 0.003 {
			at := float64(i/2) / testRate
			if at-last > 0.2 {
				ticks = append(ticks, at)
			}
			last = at
		}
	}
	period := 60 / formats[LP].rpm
	if len(ticks) < 4 {
		t.Fatalf("the scratch ticked %d times in 8 s: %v", len(ticks), ticks)
	}
	for i := 1; i < len(ticks); i++ {
		if d := ticks[i] - ticks[i-1]; math.Abs(d-period) > 0.005 {
			t.Errorf("ticks %.3f s apart, want %.3f: %v", d, period, ticks)
		}
	}
}

// A record is the same record each time: its dust where it was.
func TestVinylRecordIsTheSameEachTime(t *testing.T) {
	play := func() []float32 {
		v := pressed(lpSide, Good)
		v.sigma = [3]float64{}
		v.sigmaGrit = 0
		return press(v, lpSide, 3*time.Minute, silence(2))
	}
	a, b := play(), play()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("frame %d is %v once and %v again", i/2, a[i], b[i])
		}
	}
	other := lpSide
	other.Record = 8
	v := pressed(other, Good)
	v.sigma = [3]float64{}
	c := press(v, other, 3*time.Minute, silence(2))
	same := true
	for i := range a {
		same = same && a[i] == c[i]
	}
	if same {
		t.Error("another record has the same dust")
	}
}

// Worn records are gritty on the loud highs, and only there.
func TestVinylGritOnTheHighs(t *testing.T) {
	grit := func(f, amp float64) float64 {
		v := pressed(lpSide, Good)
		clean(v)
		v.sigma = [3]float64{}
		v.cond.wear, v.cond.smooth, v.smA = 0, 0, 0
		in := toneOf(f, amp, 1.5)
		out := press(v, lpSide, 19*time.Minute, in)
		// What is not the tone: the grit, around it.
		var e float64
		n := 0
		for i := testRate / 2; i < len(out)/2-testRate/4; i++ {
			d := float64(out[2*i] - in[2*i])
			e += d * d
			n++
		}
		return 10 * math.Log10(e/float64(n))
	}
	quiet, loud, bass := grit(8000, 0.02), grit(8000, 0.3), grit(100, 0.5)
	t.Logf("Good, inner groove: grit %.1f dB at 8 kHz -34 dBFS, %.1f dB at 8 kHz -10 dBFS, %.1f dB at 100 Hz -6 dBFS", quiet, loud, bass)
	if loud < quiet+20 || loud < bass+20 {
		t.Errorf("the grit of loud highs %.1f dB, of quiet highs %.1f, of loud bass %.1f: want it on the loud highs", loud, quiet, bass)
	}
}

func TestCutRecord(t *testing.T) {
	mins := func(n float64) time.Duration { return time.Duration(n * float64(time.Minute)) }
	// Ten songs, 44 minutes: an LP, five and five, or near.
	songs := []time.Duration{mins(4), mins(5), mins(3), mins(6), mins(4), mins(5), mins(4), mins(5), mins(4), mins(4)}
	sides := CutRecord(1, 1975, [][]time.Duration{songs})[0]
	var lens [2]time.Duration
	for i, g := range sides {
		if g.Speed != LP || g.Year != 1975 {
			t.Fatalf("song %d: %+v", i, g)
		}
		lens[g.Side] += songs[i]
	}
	if d := lens[0] - lens[1]; d > mins(5) || d < -mins(5) || sides[0].Record != sides[9].Record {
		t.Errorf("the sides are %v and %v, on records %d and %d", lens[0], lens[1], sides[0].Record, sides[9].Record)
	}
	if !sides[0].First || sides[0].At != 0 || !sides[9].Last || sides[1].At != songs[0] {
		t.Errorf("the grooves: %+v", sides)
	}
	// Seventy-five minutes take two records.
	long := make([]time.Duration, 15)
	for i := range long {
		long[i] = mins(5)
	}
	g := CutRecord(1, 1995, [][]time.Duration{long})[0]
	if g[0].Record == g[14].Record || g[14].Side != 1 {
		t.Errorf("75 minutes on one record: %+v", g)
	}
	// Before 1950, a song a side of a 78.
	old := CutRecord(1, 1938, [][]time.Duration{{mins(3), mins(3), mins(3)}})[0]
	if old[0].Speed != Shellac || old[0].Side != 0 || old[1].Side != 1 || old[2].Record == old[0].Record || !old[1].First || !old[1].Last {
		t.Errorf("78s: %+v", old)
	}
	// A few songs, a 45.
	if s := CutRecord(1, 1965, [][]time.Duration{{mins(3), mins(2.5)}})[0]; s[0].Speed != Single || s[1].Side != 1 {
		t.Errorf("a single: %+v", s)
	}
	// Each disc its own records.
	two := CutRecord(1, 2001, [][]time.Duration{songs, songs})
	if two[0][0].Record == two[1][0].Record {
		t.Error("two discs on one record")
	}
}

func TestAgeGrade(t *testing.T) {
	for _, c := range []struct {
		year     int
		lo, high float64
	}{{2026, 0.7, 1}, {2006, 1.6, 2.2}, {1975, 2.5, 3.1}, {1955, 3, 3.6}, {1930, 3.4, 4.2}, {0, 2, 2}} {
		if g := AgeGrade(c.year, 2026); g < c.lo || g > c.high {
			t.Errorf("a record of %d is %.2f (%s), want %.1f to %.1f", c.year, g, GradeName(g), c.lo, c.high)
		}
	}
}

// vinylTrack is a tone track on a side of a record.
func (te *testEngine) vinylTrack(t *testing.T, id string, freq, secs float64, g Groove) *Track {
	tr := te.track(t, id, freq, secs, true)
	tr.Groove = &g
	return tr
}

// onset is where a tone of freq starts in snd, in seconds: the first 50 ms
// it is half as loud as amp in.
func onset(snd []float32, freq, amp float64, from float64) float64 {
	step := testRate / 20
	for i := int(from * testRate); i+step < len(snd)/2; i += step / 4 {
		if amplitude(snd, freq, i, i+step) > amp/2 {
			return float64(i) / testRate
		}
	}
	return -1
}

// The needle drops into the lead-in, a revolution before the music; songs
// that follow on a side follow without a gap; at the end of the side the
// stylus plays the run-out and the locked groove, and lifts; the other side
// begins with its own lead-in.
func TestVinylSidesOnTheEngine(t *testing.T) {
	te := newTestEngine(t)
	te.SetEffects(Effects{Vinyl: true, VinylGrade: Mint})
	rec := uint64(42)
	side := 4 * time.Second
	a := te.vinylTrack(t, "a", 440, 2, Groove{Record: rec, Speed: LP, SideLen: side, First: true})
	b := te.vinylTrack(t, "b", 660, 2, Groove{Record: rec, Speed: LP, SideLen: side, At: 2 * time.Second, Last: true})
	c := te.vinylTrack(t, "c", 880, 2, Groove{Record: rec, Side: 1, Speed: LP, SideLen: 2 * time.Second, First: true, Last: true})
	te.Play(a, 0)
	te.SetNext(b)
	var evs []Event
	deadline := time.After(30 * time.Second)
	for done := false; !done; {
		select {
		case ev := <-te.events:
			evs = append(evs, ev)
			switch ev.Kind {
			case TrackChanged:
				if ev.TrackID == "b" {
					te.SetNext(c)
				}
			case Ended:
				done = true
			case Failed:
				t.Fatal(ev.Err)
			}
		case <-deadline:
			t.Fatal("the record did not end")
		}
	}
	te.sink.mu.Lock()
	got := append([]float32(nil), te.sink.got...)
	te.sink.mu.Unlock()
	rev := 60 / formats[LP].rpm
	at := onset(got, 440, 0.5, 0)
	if math.Abs(at-rev) > 0.1 {
		t.Errorf("the music begins %.2f s in, want after the lead-in's %.2f s", at, rev)
	}
	if lead := aLevel(got[:2*int(rev*0.9*testRate)], 0, testRate/10); lead < -90 {
		t.Errorf("the lead-in is silent: %.1f dB(A)", lead)
	}
	// b straight after a.
	if bAt := onset(got, 660, 0.5, at); math.Abs(bAt-(at+2)) > 0.1 {
		t.Errorf("b begins %.2f s in, want %.2f, straight after a", bAt, at+2)
	}
	// Then the run-out, a turn of the locked groove, the lift, the other
	// side's lead-in.
	cAt := onset(got, 880, 0.5, at+4)
	if gap := cAt - (at + 4); gap < 3*rev || gap > 3*rev+3 {
		t.Errorf("c begins %.2f s after b ends, want the run-out, the locked groove, the flip and the lead-in", gap)
	}
	t.Logf("music at %.2f s; b at %.2f; the other side at %.2f; %d events", at, onset(got, 660, 0.5, at), cAt, len(evs))
	if n := len(evs); n < 3 || evs[n-1].Kind != Ended || evs[0].TrackID != "b" || evs[1].TrackID != "c" {
		t.Errorf("events: %+v", evs)
	}
}

// The music plays as it was without vinyl: the engine is not touched.
func TestVinylOffChangesNothing(t *testing.T) {
	var c chain
	c.set(Effects{}, testRate)
	if c.vin != nil {
		t.Fatal("vinyl without being asked")
	}
	c.set(Effects{Vinyl: true}, testRate)
	if c.vin == nil {
		t.Fatal("no vinyl when asked")
	}
	c.set(Effects{EQ: true}, testRate)
	if c.vin != nil {
		t.Fatal("vinyl left on")
	}
}

func BenchmarkVinyl(b *testing.B) {
	v := pressed(lpSide, VeryGood)
	in := toneOf(1000, 0.3, 1)
	seg := &segment{id: "bench", groove: &lpSide, dur: lpSide.SideLen}
	buf := make([]float32, 2*512)
	b.SetBytes(int64(len(in) * 4))
	for b.Loop() {
		for i := 0; i+len(buf) <= len(in); i += len(buf) {
			copy(buf, in[i:])
			v.play(buf, seg, int64(i/2))
		}
	}
}

// TestVinylDemo writes what records of each grade sound like, to listen
// to, into the directory AURELIA_VINYL_WAV names: the groove between songs,
// and the music of AURELIA_TEST_FLAC (or a tone) played from a side.
func TestVinylDemo(t *testing.T) {
	dir := os.Getenv("AURELIA_VINYL_WAV")
	if dir == "" {
		t.Skip("AURELIA_VINYL_WAV names no directory")
	}
	music := toneOf(440, 0.2, 10)
	var songLen time.Duration
	if path := os.Getenv("AURELIA_TEST_FLAC"); path != "" {
		cache, err := NewCache(t.TempDir(), 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := cache.Import("demo", path); err != nil {
			t.Fatal(err)
		}
		f, err := cache.Open("demo", func(context.Context) (*http.Request, error) { return nil, errors.New("not here") })
		if err != nil {
			t.Fatal(err)
		}
		d, err := openDecoder(f, 0)
		if err != nil {
			t.Fatal(err)
		}
		all := decodeAll(t, d)
		f.Close()
		if d.SampleRate() != testRate {
			rs := newResampler(d.SampleRate(), testRate)
			all = rs.flush(rs.process(all, nil))
		}
		music = all[:min(len(all), 2*40*testRate)]
		songLen = time.Duration(float64(len(all)/2) / testRate * float64(time.Second))
	}
	for _, grade := range []int{Mint, NearMint, VeryGoodPlus, VeryGood, GoodPlus, Good, Fair} {
		name := GradeNames[grade]
		g := lpSide
		g.Year = 1975
		v := pressed(g, float64(grade))
		writeWAV(t, filepath.Join(dir, "groove-"+name+".wav"), press(v, g, 6*time.Minute, silence(10)))
		v = pressed(g, float64(grade))
		writeWAV(t, filepath.Join(dir, "music-"+name+".wav"), press(v, g, 15*time.Minute, music))
	}
	old := Groove{Record: 9, Speed: Shellac, Year: 1938, SideLen: 3 * time.Minute, First: true, Last: true}
	writeWAV(t, filepath.Join(dir, "music-78.wav"), press(pressed(old, AgeGrade(1938, 2026)), old, time.Minute, music))

	// A side turned over on the engine: the end of side A's last song,
	// the run-out, the locked groove, the lift, and the needle dropped
	// into side B's lead-in before its first song.
	if songLen == 0 {
		return
	}
	te := newTestEngine(t)
	te.SetEffects(Effects{Vinyl: true, VinylGrade: VeryGood})
	if err := te.cache.Import("song", os.Getenv("AURELIA_TEST_FLAC")); err != nil {
		t.Fatal(err)
	}
	song := func(g Groove) *Track {
		return &Track{ID: "song", Duration: songLen, Groove: &g, NoFade: true, Open: func() (*File, error) {
			return te.cache.Open("song", func(context.Context) (*http.Request, error) { return nil, errors.New("not here") })
		}}
	}
	side := 20 * time.Minute
	a := song(Groove{Record: 5, Year: 1975, SideLen: side, At: side - songLen, Last: true})
	b := song(Groove{Record: 5, Side: 1, Face: 1, Year: 1975, SideLen: side, First: true})
	te.Play(a, songLen-10*time.Second)
	te.SetNext(b)
	const want = 2 * 30 * testRate
	deadline := time.After(60 * time.Second)
	for {
		te.sink.mu.Lock()
		n := len(te.sink.got)
		te.sink.mu.Unlock()
		if n >= want {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the side did not turn over")
		case <-time.After(10 * time.Millisecond):
		}
	}
	te.Stop()
	te.sink.mu.Lock()
	writeWAV(t, filepath.Join(dir, "side-turned-over.wav"), te.sink.got[:want])
	te.sink.mu.Unlock()
}

// writeWAV writes snd as a WAV file of 16 bits.
func writeWAV(t *testing.T, path string, snd []float32) {
	t.Helper()
	b := make([]byte, 44+2*len(snd))
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+2*len(snd)))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 2)
	binary.LittleEndian.PutUint32(b[24:], testRate)
	binary.LittleEndian.PutUint32(b[28:], testRate*4)
	binary.LittleEndian.PutUint16(b[32:], 4)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(2*len(snd)))
	for i, s := range snd {
		binary.LittleEndian.PutUint16(b[44+2*i:], uint16(int16(max(-1, min(1, s))*32767)))
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

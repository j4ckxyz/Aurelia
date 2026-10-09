package main

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func testPicture(n int) *image.RGBA {
	pic := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			pic.SetRGBA(x, y, color.RGBA{R: uint8(255 * x / n), G: uint8(255 * y / n), B: 128, A: 255})
		}
	}
	return pic
}

// The record's bitmap is one whose pixels MyGo lets the app rewrite, by
// a count it keeps: an update of MyGo that lays its types out otherwise
// must fail here, not leave the record still without anyone knowing.
func TestTurner(t *testing.T) {
	const n = 200
	tr := newTurner(testPicture(n), n)
	if !tr.ok {
		t.Fatal("MyGo's bitmap is not laid out as the turner expects: the record would not turn")
	}
	before := *tr.version
	tr.turn(math.Pi / 2)
	if *tr.version != before+1 {
		t.Errorf("turning did not count a change: %d, then %d", before, *tr.version)
	}
	// A quarter turn clockwise: what was at the left is at the top.
	c := n / 2
	at := func(img *image.RGBA, x, y int) color.RGBA {
		if img == tr.src {
			return img.RGBAAt(2*x, 2*y) // twice as large as it shows
		}
		return img.RGBAAt(x, y)
	}
	near := func(a, b color.RGBA) bool {
		d := func(x, y uint8) int { return int(math.Abs(float64(x) - float64(y))) }
		return d(a.R, b.R) < 6 && d(a.G, b.G) < 6 && d(a.B, b.B) < 6
	}
	if got, want := at(tr.dst, c, 45), at(tr.src, 45, c); !near(got, want) {
		t.Errorf("the top after a quarter turn is %v, want the left's %v", got, want)
	}
	if got, want := at(tr.dst, n-45, c), at(tr.src, c, 45); !near(got, want) {
		t.Errorf("the right after a quarter turn is %v, want the top's %v", got, want)
	}
	// No turn at all is the picture itself.
	tr.turn(0)
	if got, want := at(tr.dst, 60, 140), at(tr.src, 60, 140); !near(got, want) {
		t.Errorf("unturned, a pixel is %v, want %v", got, want)
	}
	// It is a record: clear outside its circle, dark at its middle.
	if got := at(tr.dst, 2, 2); got.A != 0 {
		t.Errorf("the corner is %v, want it clear", got)
	}
	if got := at(tr.dst, c+8, c); got.R > 20 || got.A != 255 {
		t.Errorf("the middle of the label is %v, want it dark", got)
	}
	if bg := backdrop(tr.src); bg == nil {
		t.Error("no backdrop")
	}
}

func benchmarkTurn(b *testing.B, n int) {
	tr := newTurner(testPicture(n), n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.turn(float64(i) * 0.37)
	}
}

func BenchmarkTurn400(b *testing.B) { benchmarkTurn(b, 400) }
func BenchmarkTurn600(b *testing.B) { benchmarkTurn(b, 600) }

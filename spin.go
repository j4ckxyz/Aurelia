package main

import (
	"image"
	"image/draw"
	"math"
	"reflect"
	"unsafe"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

// MyGo draws pictures upright: nothing of it turns one. The record of
// the full screen is a picture the app turns itself, a little for every
// frame, into one bitmap whose pixels it rewrites.
//
// A bitmap's pixels are not meant to change: the renderer keeps what it
// was first given. It does look at a count of changes that MyGo keeps
// with each picture and exposes to nothing, and that count is reached
// here through the layout of MyGo's types, which is checked as it is
// read. Where a later MyGo is laid out otherwise, the record stays
// still: turner.ok is false.

// turner is a square picture turned around its center.
type turner struct {
	// src is the picture, twice as large as it shows: each pixel of the
	// picture turned is then the nearest of src, which costs a third of
	// weighing four of them and looks the same.
	src  *image.RGBA
	dst  *image.RGBA // the picture turned, as the bitmap shows it
	bmp  *ui.Bitmap
	size int
	// version is the count of changes of bmp's picture, nil where it was
	// not found.
	version *uint64
	ok      bool
}

// newTurner makes a turner of a picture, shown at size × size pixels.
func newTurner(pic image.Image, size int) *turner {
	t := &turner{size: size}
	t.src = image.NewRGBA(image.Rect(0, 0, 2*size, 2*size))
	b := pic.Bounds()
	// The middle square of a picture that is not one.
	if b.Dx() > b.Dy() {
		b.Min.X += (b.Dx() - b.Dy()) / 2
		b.Max.X = b.Min.X + b.Dy()
	} else if b.Dy() > b.Dx() {
		b.Min.Y += (b.Dy() - b.Dx()) / 2
		b.Max.Y = b.Min.Y + b.Dx()
	}
	xdraw.CatmullRom.Scale(t.src, t.src.Bounds(), pic, b, draw.Src, nil)
	press(t.src)
	// Clear outside the record, which is round.
	t.dst = image.NewRGBA(image.Rect(0, 0, size, size))
	t.turnRows(0)
	t.bmp = ui.NewBitmap(t.dst)
	t.version = versionOf(t.bmp, t.dst)
	t.ok = t.version != nil
	return t
}

// The record's grooves, as parts of its radius: the light catches some
// and others lie in shadow. Its label's middle and its hole are parts
// of the radius too.
var (
	grooveLight = []float64{0.955, 0.89, 0.815, 0.73, 0.635, 0.53, 0.415}
	grooveDark  = []float64{0.925, 0.855, 0.775, 0.685, 0.585, 0.475}
)

const (
	recordHub  = 0.2
	recordHole = 0.035
)

// press makes a square picture a record's: its grooves, its rim, the
// dark middle of its label and the hole. They are the same all the way
// round, so they turn with the picture unseen, and cost nothing to draw.
func press(img *image.RGBA) {
	n := img.Bounds().Dx()
	c := float64(n-1) / 2
	radius := c - 1
	shade := func(p []byte, toward uint8, by float64) {
		for k := 0; k < 3; k++ {
			p[k] = uint8(float64(p[k]) + (float64(toward)-float64(p[k]))*by)
		}
	}
	near := func(d float64, at []float64) bool {
		for _, f := range at {
			if math.Abs(d-f*radius) < 1.5 { // as wide as a pixel shown

				return true
			}
		}
		return false
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			p := img.Pix[y*img.Stride+x*4:]
			d := math.Hypot(float64(x)-c, float64(y)-c)
			switch {
			case d > radius+1:
				continue // never shown
			case d < recordHole*radius-1:
				p[0], p[1], p[2] = 0, 0, 0
			case d < recordHole*radius+1:
				p[0], p[1], p[2] = 58, 58, 62 // the hole's edge
			case d < recordHub*radius-1:
				p[0], p[1], p[2] = 14, 14, 17
			case d < recordHub*radius+1:
				p[0], p[1], p[2] = 50, 50, 54 // the label's edge
			case d > radius*0.988:
				shade(p, 0, 0.6) // the rim
			case near(d, grooveLight):
				shade(p, 255, 0.07)
			case near(d, grooveDark):
				shade(p, 0, 0.16)
			}
			p[3] = 255
		}
	}
}

// versionOf finds the count of changes of a bitmap made of img, which
// tells the renderer to read its pixels again; nil when the bitmap is not
// laid out as expected, or does not show img's own pixels.
func versionOf(bmp *ui.Bitmap, img *image.RGBA) (version *uint64) {
	defer func() {
		if recover() != nil {
			version = nil
		}
	}()
	field := reflect.ValueOf(bmp).Elem().FieldByName("img")
	if !field.IsValid() || field.Kind() != reflect.Pointer || field.IsNil() || field.Type().Elem().Kind() != reflect.Struct {
		return nil
	}
	picture := field.Type().Elem()
	count, ok := picture.FieldByName("version")
	pix, ok2 := picture.FieldByName("Pix")
	if !ok || !ok2 || count.Type.Kind() != reflect.Uint64 || pix.Type != reflect.TypeOf([]byte(nil)) {
		return nil
	}
	base := field.UnsafePointer()
	if shown := *(*[]byte)(unsafe.Add(base, pix.Offset)); len(shown) != len(img.Pix) || len(shown) == 0 || &shown[0] != &img.Pix[0] {
		return nil // a copy: rewriting img would change nothing
	}
	return (*uint64)(unsafe.Add(base, count.Offset))
}

// turn shows the picture turned by angle, in radians clockwise. It
// works on the goroutine that asks: shared out among several it is done
// sooner, and costs four times as much, for the cores it wakes from
// their sleep sixty times a second.
func (t *turner) turn(angle float64) {
	if !t.ok {
		return
	}
	t.turnRows(angle)
	*t.version++
}

// turnRows fills the turned picture: each pixel inside the circle is
// the picture's nearest to the place it comes from. Outside the circle
// nothing is written: the record is round.
func (t *turner) turnRows(angle float64) {
	const one = 1 << 16
	n, sn := t.size, 2*t.size
	sin, cos := math.Sincos(angle)
	// A pixel of the turned picture at (x, y) from the center shows the
	// picture's at (x, y) turned back, and twice as far in its pixels.
	dux, dvx := int(cos*2*one), int(-sin*2*one)
	c, sc := float64(n-1)/2, float64(sn-1)/2
	radius := c - 1
	src := unsafe.Slice((*uint32)(unsafe.Pointer(&t.src.Pix[0])), sn*sn)
	dst := unsafe.Slice((*uint32)(unsafe.Pointer(&t.dst.Pix[0])), n*n)
	for y := 0; y < n; y++ {
		dy := float64(y) - c
		if dy <= -radius || dy >= radius {
			continue
		}
		half := math.Sqrt(radius*radius - dy*dy)
		x0, x1 := int(math.Ceil(c-half)), int(math.Floor(c+half))
		dx := float64(x0) - c
		u := int((sc+2*(dx*cos+dy*sin))*one) + one/2
		v := int((sc+2*(-dx*sin+dy*cos))*one) + one/2
		row := dst[y*n+x0 : y*n+x1+1]
		for i := range row {
			row[i] = src[(v>>16)*sn+(u>>16)]
			u += dux
			v += dvx
		}
	}
}

// backdrop makes a small, blurred, dimmed picture of a picture, to
// stretch over the window behind the record: its colors, and none of
// its shapes.
func backdrop(pic image.Image) *ui.Bitmap {
	const n = 36
	small := image.NewRGBA(image.Rect(0, 0, n, n))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), pic, pic.Bounds(), draw.Src, nil)
	tmp := make([]byte, len(small.Pix))
	for pass := 0; pass < 3; pass++ {
		blur(small.Pix, tmp, n, 4, 4*n) // along the rows
		blur(tmp, small.Pix, n, 4*n, 4) // along the columns
	}
	// Dimmed here, once, for what is written over it to read: a veil
	// drawn over the whole window in every frame would cost as much as
	// the picture itself.
	for i := 0; i < len(small.Pix); i += 4 {
		for k := 0; k < 3; k++ {
			small.Pix[i+k] = uint8(float64(small.Pix[i+k])*0.4 + 4)
		}
		small.Pix[i+3] = 255
	}
	return ui.NewBitmap(small)
}

// blur averages each pixel of an n × n picture with the two before and
// the two after it along a line: step is the distance between pixels
// along the line, and line between lines.
func blur(src, dst []byte, n, step, line int) {
	for l := 0; l < n; l++ {
		for i := 0; i < n; i++ {
			var sum [4]int
			count := 0
			for k := max(0, i-2); k <= min(n-1, i+2); k++ {
				p := src[l*line+k*step:]
				sum[0] += int(p[0])
				sum[1] += int(p[1])
				sum[2] += int(p[2])
				sum[3] += int(p[3])
				count++
			}
			d := dst[l*line+i*step:]
			d[0], d[1], d[2], d[3] = byte(sum[0]/count), byte(sum[1]/count), byte(sum[2]/count), byte(sum[3]/count)
		}
	}
}

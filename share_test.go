package main

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// solid is a picture of one color, or of two halves.
func solid(c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestCardColors(t *testing.T) {
	// A cover of red on grey: the vivid hue is the red, the average is
	// something between.
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			if x < 20 {
				img.SetRGBA(x, y, color.RGBA{210, 30, 30, 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{120, 120, 120, 255})
			}
		}
	}
	v, b := cardColors(img)
	if int(v.R)-int(v.G) < 100 {
		t.Errorf("the vivid color is %v, want a red", v)
	}
	if b.R > v.R || b.R < 100 {
		t.Errorf("the average is %v, want a grey between", b)
	}
	// Grey has no hue to bring out.
	v, b = cardColors(solid(color.RGBA{130, 130, 130, 255}))
	if v != b {
		t.Errorf("a grey cover has two colors: %v and %v", v, b)
	}
}

func TestParseHexColor(t *testing.T) {
	for in, want := range map[string]ui.Color{
		"#7C5CFF": ui.RGB(0x7c, 0x5c, 0xff), "7c5cff": ui.RGB(0x7c, 0x5c, 0xff),
		"#fff": ui.RGB(255, 255, 255), " #000000 ": ui.RGB(0, 0, 0),
	} {
		if got, ok := parseHexColor(in); !ok || got != want {
			t.Errorf("%q is %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "#12", "#12345", "#gggggg", "#1234567"} {
		if _, ok := parseHexColor(in); ok {
			t.Errorf("%q was taken for a color", in)
		}
	}
}

func TestCardFileName(t *testing.T) {
	if got := cardFileName("AC/DC", `Back In Black: "Live"?`); got != "AC-DC - Back In Black- -Live--.png" {
		t.Errorf("got %q", got)
	}
	if got := cardFileName("", "Solo"); got != "Solo.png" {
		t.Errorf("got %q", got)
	}
	if got := cardFileName("", ""); got != "Aurelia.png" {
		t.Errorf("got %q", got)
	}
	if got := cardFileName("A", strings.Repeat("é", 200)); len([]rune(got)) > 90 {
		t.Errorf("a name of %d characters", len([]rune(got)))
	}
}

// The text is dark on light backgrounds and light on dark ones, and the
// cover's own colors are always dark enough for white text.
func TestCardLookReads(t *testing.T) {
	for _, tc := range []struct {
		sh        shareState
		darkText  bool
		wantFrom  *ui.Color
		gradients bool
	}{
		{sh: shareState{bg: bgWhite}, darkText: true},
		{sh: shareState{bg: bgBlack}},
		{sh: shareState{bg: bgColor, color: ui.RGB(0xf2, 0xc9, 0x4c)}, darkText: true},
		{sh: shareState{bg: bgColor, color: ui.RGB(0x1f, 0x2a, 0x44)}},
		{sh: shareState{bg: bgColor, color: ui.RGB(255, 255, 255)}, darkText: true},
		{sh: shareState{bg: bgBlend}, gradients: true},
	} {
		l := tc.sh.look()
		if got := luminance(l.text) < 0.3; got != tc.darkText {
			t.Errorf("%s %v: dark text is %v, want %v", tc.sh.bg, tc.sh.color, got, tc.darkText)
		}
		if l.gradient != tc.gradients {
			t.Errorf("%s: gradient is %v", tc.sh.bg, l.gradient)
		}
	}
	// A cover of pale yellow, the worst for white text.
	v, b := cardColors(solid(color.RGBA{250, 235, 120, 255}))
	l := (&shareState{bg: bgBlend, cover: solid(color.RGBA{}), vibrant: v, base: b}).look()
	if luminance(l.from) > 0.31 || luminance(l.to) > 0.31 {
		t.Errorf("the blend of a pale cover is %v to %v: white would not read", l.from, l.to)
	}
}

// pixel is the color of a pixel of the picture.
func pixel(img image.Image, x, y int) color.RGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func near(a, b color.RGBA, d int) bool {
	abs := func(x int) int {
		if x < 0 {
			return -x
		}
		return x
	}
	return abs(int(a.R)-int(b.R)) <= d && abs(int(a.G)-int(b.G)) <= d && abs(int(a.B)-int(b.B)) <= d
}

func TestRenderCard(t *testing.T) {
	a := testApp(t)
	cover := solid(color.RGBA{200, 40, 40, 255})
	a.covers = func(string) image.Image { return cover }
	al := a.lib.Album("al1")
	a.shareAlbum(al)
	sh := &a.share
	sh.title, sh.artist = "A Very Long Name Of An Album That Goes On And On Past One Line", "An Artist"
	a.setShareCover(cover)

	for i, f := range cardFormats {
		sh.format = i
		for _, tc := range []struct {
			bg   string
			want color.RGBA
		}{
			{bgWhite, color.RGBA{255, 255, 255, 255}},
			{bgBlack, color.RGBA{0x0b, 0x0b, 0x0d, 255}},
			{bgColor, color.RGBA{0x7c, 0x5c, 0xff, 255}},
		} {
			sh.bg = tc.bg
			img := a.renderCard()
			if b := img.Bounds(); b.Dx() != f.w || b.Dy() != f.h {
				t.Fatalf("%s: the picture is %v, want %d × %d", f.id, b.Size(), f.w, f.h)
			}
			for _, at := range [][2]int{{3, 3}, {f.w - 4, 3}, {3, f.h - 4}, {f.w - 4, f.h - 4}} {
				if got := pixel(img, at[0], at[1]); !near(got, tc.want, 2) {
					t.Errorf("%s on %s: the corner %v is %v, want %v", f.id, tc.bg, at, got, tc.want)
				}
			}
			// The cover shows, in the middle of its square: red.
			cx, cy := f.w/2, f.h/2
			if f.id == "wide" {
				cx = 70 + 470/2
			} else if f.id == "square" {
				cy = 70 + (f.h-140)/4
			}
			if got := pixel(img, cx, cy); !near(got, color.RGBA{200, 40, 40, 255}, 12) {
				t.Errorf("%s on %s: the cover at %d,%d is %v, want its red", f.id, tc.bg, cx, cy, got)
			}
		}
		// The blend: its two ends differ, and both are reds (the cover's).
		sh.bg = bgBlend
		img := a.renderCard()
		top, foot := pixel(img, 3, 3), pixel(img, f.w-4, f.h-4)
		if top == foot {
			t.Errorf("%s: the blend is flat: %v", f.id, top)
		}
		if int(top.R) < int(top.G)+20 || int(foot.R) < int(foot.G)+8 {
			t.Errorf("%s: the blend %v to %v is not of the cover's red", f.id, top, foot)
		}
	}
}

func TestShareDialog(t *testing.T) {
	a := testApp(t)
	a.covers = func(string) image.Image { return solid(color.RGBA{40, 90, 200, 255}) }
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/albums")
	tt.Frame()
	a.shareAlbum(a.lib.Album("al1"))
	tt.Frame()
	wantTexts(t, tt, "Share as a picture", "Square", "Story", "Wide", "Copy", "Save…")
	if a.share.cover == nil {
		t.Error("the cover was not loaded")
	}
	click(t, tt, "Story")
	if cardFormats[a.share.format].id != "story" {
		t.Errorf("the shape is %d", a.share.format)
	}
	click(t, tt, "Black")
	if a.share.bg != bgBlack {
		t.Errorf("the background is %q", a.share.bg)
	}
	click(t, tt, "Your color")
	tt.Frame()
	wantTexts(t, tt, "#7C5CFF")
	// Closing the dialog lets go of the large cover.
	a.closeShare()
	tt.Frame()
	if a.share.open || a.share.cover != nil || a.share.bmp != nil {
		t.Errorf("the dialog is still holding %+v", a.share)
	}
}

// The way a user takes: a right click on an album, and the menu's item.
func TestShareFromTheMenus(t *testing.T) {
	a := testApp(t)
	a.covers = func(string) image.Image { return solid(color.RGBA{40, 90, 200, 255}) }
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/albums")
	tt.Frame()
	al := a.lib.Album("al1")
	if err := tt.RightClick(al.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Share as a Picture…"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	tt.Frame()
	if !a.share.open || a.share.title != al.Name {
		t.Fatalf("the album's menu did not open the dialog: %+v", a.share)
	}
	wantTexts(t, tt, "Share as a picture")
	a.closeShare()
	tt.Frame()

	// And from a song, on its album's page.
	a.router.Push("/album/al1")
	tt.Frame()
	s := a.lib.AlbumSongs("al1")[0]
	if err := tt.RightClick(s.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Share as a Picture…"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	tt.Frame()
	if !a.share.open || a.share.title != s.Name || a.share.artist != s.Artist {
		t.Fatalf("the song's menu did not open the dialog: %+v", a.share)
	}
}

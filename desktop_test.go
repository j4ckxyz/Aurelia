package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// The tray's icon is a picture with a see-through background and something
// drawn in it.
func TestTrayIcon(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 800, 600)
	tt.Frame()
	data := a.trayIcon()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the icon is not a picture: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 44 || b.Dy() != 44 {
		t.Errorf("the icon is %v, want 44 × 44 (22 points at 2×)", b.Size())
	}
	if _, _, _, alpha := img.At(0, 0).RGBA(); alpha != 0 {
		t.Errorf("the corner is not see-through: alpha %d", alpha)
	}
	solid := 0
	for y := 0; y < 44; y++ {
		for x := 0; x < 44; x++ {
			if _, _, _, alpha := img.At(x, y).RGBA(); alpha > 0x8000 {
				solid++
			}
		}
	}
	if solid < 100 {
		t.Errorf("only %d pixels of the icon are drawn", solid)
	}
}

// Aurelia's icon comes in the color chosen; the cover's icon is the cover,
// square, its corners rounded.
func TestTrayLooks(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 800, 600)
	tt.Frame()
	songs := a.lib.AlbumSongs("al1")
	s := songs[0]
	s.ImageItem, s.ImageTag = "al1", "tag"

	// The color of the logo's pixels.
	tintOf := func(data []byte) (r, g, b uint32) {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		best := uint32(0)
		for y := 0; y < trayIconSize; y++ {
			for x := 0; x < trayIconSize; x++ {
				cr, cg, cb, ca := img.At(x, y).RGBA()
				if ca > best {
					best, r, g, b = ca, cr*0xffff/ca>>8, cg*0xffff/ca>>8, cb*0xffff/ca>>8
				}
			}
		}
		return r, g, b
	}
	a.settings.TrayColor = "gold"
	icon, template, _, _ := a.trayLook(s)
	if r, g, b := tintOf(icon); template || r < 0xf0 || g < 0xa0 || g > 0xd0 || b > 0x60 {
		t.Errorf("the gold icon is %02x%02x%02x, template %v", r, g, b, template)
	}
	a.settings.TrayColor = "white"
	icon, _, _, _ = a.trayLook(s)
	if r, g, b := tintOf(icon); r < 0xf0 || g < 0xf0 || b < 0xf0 {
		t.Errorf("the white icon is %02x%02x%02x", r, g, b)
	}
	a.settings.TrayColor = ""
	if _, template, _, _ := a.trayLook(s); template != (runtime.GOOS == "darwin") {
		t.Errorf("automatic is the system's to tint: %v", template)
	}

	// The cover.
	red := image.NewRGBA(image.Rect(0, 0, 300, 200))
	draw.Draw(red, red.Bounds(), image.NewUniform(color.RGBA{200, 20, 20, 255}), image.Point{}, draw.Src)
	a.covers = func(string) image.Image { return red }
	a.settings.TrayIcon = "art"
	icon, template, key, _ := a.trayLook(s)
	img, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		t.Fatal(err)
	}
	if template || img.Bounds().Dx() != trayIconSize || img.Bounds().Dy() != trayIconSize {
		t.Fatalf("the cover's icon is %v, template %v", img.Bounds(), template)
	}
	if _, _, _, alpha := img.At(0, 0).RGBA(); alpha != 0 {
		t.Error("the cover's corner is not rounded")
	}
	if r, g, _, alpha := img.At(22, 22).RGBA(); alpha != 0xffff || r>>8 < 190 || g>>8 > 30 {
		t.Errorf("the middle of the cover's icon is not the cover: %v %v %v", r>>8, g>>8, alpha)
	}
	// Nothing playing: Aurelia's icon.
	if _, _, k, _ := a.trayLook(nil); k == key {
		t.Error("with nothing playing, the icon is still a cover")
	}
	// The cover's icon is made once.
	_, _, key2, _ := a.trayLook(s)
	if key2 != key {
		t.Errorf("the cover's key changed: %q, %q", key, key2)
	}
}

// What shows beside the icon: nothing, the song, or the song and the
// artist, cut short; nothing on Windows, which shows no text there.
func TestTrayText(t *testing.T) {
	a := testApp(t)
	s := &library.Song{Name: "Shape of You", Artist: "Ed Sheeran"}
	cases := map[string]string{"": "", "song": "Shape of You", "artist": "Shape of You — Ed Sheeran"}
	for id, want := range cases {
		a.settings.TrayText = id
		if !trayTextShows() {
			want = ""
		}
		if _, _, _, got := a.trayLook(s); got != want {
			t.Errorf("%q: %q, want %q", id, got, want)
		}
	}
	long := &library.Song{Name: strings.Repeat("Long ", 20), Artist: "Someone"}
	a.settings.TrayText = "artist"
	if _, _, _, got := a.trayLook(long); trayTextShows() && (utf8.RuneCountInString(got) != 40 || !strings.HasSuffix(got, "…")) {
		t.Errorf("a long name shows as %q", got)
	}
	if _, _, _, got := a.trayLook(nil); got != "" {
		t.Errorf("with nothing playing, %q shows", got)
	}
}

// The settings of the icon show while it does, and change it.
func TestTraySettings(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 2400)
	a.goTo("/settings")
	tt.Frame()
	if tt.HasText("Icon color") {
		t.Fatal("the icon's settings show without the icon")
	}
	a.settings.Tray = true
	tt.Frame()
	for _, want := range []string{"Looks like", "Icon", "Icon color"} {
		if !tt.HasText(want) {
			t.Errorf("no %q", want)
		}
	}
	if tt.HasText("Beside the icon") != trayTextShows() {
		t.Errorf("Beside the icon shows: %v", tt.HasText("Beside the icon"))
	}
	if err := tt.Click("Rose"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.TrayColor != "rose" {
		t.Errorf("the color is %q", a.settings.TrayColor)
	}
}

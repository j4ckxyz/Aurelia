package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // covers
	"image/png"
	"log"
	"math"
	"os"
	"runtime"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"

	"aurelia/internal/library"
)

// What Aurelia shows of itself outside its window: an icon in the menu bar
// or the tray, with the song and its buttons, and a notification as the
// song changes.
//
// The icon is Aurelia's, in a color of the settings, or the cover of the
// song playing; beside it, if asked, the song, and the artist. Text beside
// the icon shows in macOS's menu bar and in the panels of Linux that show
// an AppIndicator's label (GNOME with its AppIndicator extension, Ubuntu,
// Xfce, Cinnamon, MATE, Budgie); KDE Plasma and Windows show an icon
// alone, and the song when the pointer is on it.

// desktopState is what was last shown outside the window.
type desktopState struct {
	tray      *mygo.Tray
	trayKey   string // what the menu says, so that it is made again only when it changes
	iconKey   string // what the icon shows
	title     string // what shows beside it
	announced string // the song the notification was last of
	// art is the icon last made of a cover, and the file it was made of;
	// logo the last of Aurelia's, and its color.
	art      []byte
	artFrom  string
	logo     []byte
	logoTint ui.Color
	// palFor is the theme the icon was last drawn for, whose accent the
	// automatic color is.
	palFor string
	// preview is the icon's picture in Settings, and what it shows.
	preview    *ui.Bitmap
	previewKey string
}

// decodePNG reads a PNG.
func decodePNG(data []byte) (image.Image, error) { return png.Decode(bytes.NewReader(data)) }

// trayName is what the icon is called on this system.
func trayName() string {
	if runtime.GOOS == "darwin" {
		return "menu bar"
	}
	return "tray"
}

// trayTextShows reports whether this system shows text beside the icon.
func trayTextShows() bool { return runtime.GOOS != "windows" }

// trayColors are the colors Aurelia's icon comes in: the system's own
// first, then Aurelia's themes' and plain white and black, for a panel of
// either kind.
var trayColors = []struct {
	id, name string
	hex      string
}{
	{"", "Automatic", ""},
	{"sea", "Sea glass", "#7dd3c0"},
	{"lavender", "Lavender", "#a48cff"},
	{"gold", "Gold", "#fabd2f"},
	{"rose", "Rose", "#f38ba8"},
	{"sky", "Sky", "#7aa2f7"},
	{"white", "White", "#ffffff"},
	{"black", "Black", "#1c1c1e"},
}

// trayTexts are what may show beside the icon.
var trayTexts = []struct{ id, name string }{
	{"", "Nothing"},
	{"song", "The song"},
	{"artist", "The song and the artist"},
}

// trayTint is the color of Aurelia's icon, and whether it is the system's
// to tint: on macOS, Automatic is the menu bar's own black or white.
func (a *App) trayTint() (ui.Color, bool) {
	for _, c := range trayColors {
		if c.id == a.settings.TrayColor && c.hex != "" {
			return ui.Hex(c.hex), false
		}
	}
	if runtime.GOOS == "darwin" {
		return ui.RGB(0, 0, 0), true
	}
	if a.pal != nil {
		return a.pal.accent, false
	}
	return ui.Hex(trayColors[1].hex), false
}

// trayIconSize is the size of the icon's picture: 22 points at 2×.
const trayIconSize = 44

// trayIcon draws Aurelia's icon, the jellyfish of the logo, in the color
// of the settings.
func (a *App) trayIcon() []byte {
	tint, _ := a.trayTint()
	return logoPNG(tint)
}

// logoPNG draws the logo in a color, as a PNG of trayIconSize.
func logoPNG(tint ui.Color) []byte {
	const size = trayIconSize / 2
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Icon(c, icon("logo")).Size(size, size).TextColor(tint)
	}, size, size)
	tt.SetScale(2)
	tt.Frame()
	var buf bytes.Buffer
	if err := png.Encode(&buf, tt.Image()); err != nil {
		return nil
	}
	return buf.Bytes()
}

// coverIcon makes the icon of a cover: square, the size of the icon, with
// its corners rounded as the system's own pictures in menus are.
func coverIcon(pic image.Image) []byte {
	b := pic.Bounds()
	side := min(b.Dx(), b.Dy())
	if side <= 0 {
		return nil
	}
	// The middle of a cover that is not square.
	src := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
	src.Max = src.Min.Add(image.Pt(side, side))
	const n = trayIconSize
	out := image.NewRGBA(image.Rect(0, 0, n, n))
	draw.CatmullRom.Scale(out, out.Bounds(), pic, src, draw.Src, nil)
	// Rounded corners, smoothed over a pixel.
	const r = 8.0
	for y := range n {
		for x := range n {
			cx, cy := float64(x)+0.5, float64(y)+0.5
			dx := max(r-cx, cx-(n-r), 0)
			dy := max(r-cy, cy-(n-r), 0)
			if dx == 0 || dy == 0 {
				continue
			}
			cover := min(max(r-math.Hypot(dx, dy)+0.5, 0), 1)
			p := out.RGBAAt(x, y)
			out.SetRGBA(x, y, color.RGBA{uint8(float64(p.R) * cover), uint8(float64(p.G) * cover), uint8(float64(p.B) * cover), uint8(float64(p.A) * cover)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil
	}
	return buf.Bytes()
}

// trayArt returns the icon of the cover of the song, made once for its
// picture, and false while the picture is not there yet.
func (a *App) trayArt(s *library.Song) ([]byte, string, bool) {
	if s == nil || s.ImageItem == "" {
		return nil, "", false
	}
	d := &a.desk
	if a.covers != nil {
		// Drawn in place of the server's, as in tests and screenshots.
		from := "covers:" + s.ImageItem
		if d.artFrom != from {
			d.art, d.artFrom = coverIcon(a.covers(s.ImageItem)), from
		}
		return d.art, from, d.art != nil
	}
	c := a.clientNow()
	if c == nil || s.ImageTag == "" {
		return nil, "", false
	}
	path := a.images.file(imageKey(s.ImageItem, s.ImageTag, coverPixels), c.ImageURL(s.ImageItem, "Primary", s.ImageTag, coverPixels))
	if path == "" {
		return nil, "", false // being fetched: the system is told again once it is there
	}
	if d.artFrom != path {
		d.art, d.artFrom = nil, path
		if data, err := os.ReadFile(path); err == nil {
			if pic, _, err := image.Decode(bytes.NewReader(data)); err == nil {
				d.art = coverIcon(pic)
			}
		}
	}
	return d.art, path, d.art != nil
}

// trayLook is what the icon shows, and what beside it: the PNG, whether
// the system tints it, a key that changes when the picture does, and the
// text.
func (a *App) trayLook(s *library.Song) (icon []byte, template bool, key, title string) {
	if a.settings.TrayIcon == "art" {
		if art, from, ok := a.trayArt(s); ok {
			icon, key = art, "art:"+from
		}
	}
	if icon == nil {
		tint, tmpl := a.trayTint()
		d := &a.desk
		if d.logo == nil || d.logoTint != tint {
			d.logo, d.logoTint = logoPNG(tint), tint
		}
		icon, template = d.logo, tmpl
		key = fmt.Sprint("logo:", tint, tmpl)
	}
	if s != nil && trayTextShows() {
		switch a.settings.TrayText {
		case "song":
			title = s.Name
		case "artist":
			title = s.Name
			if s.Artist != "" {
				title += " — " + s.Artist
			}
		}
	}
	return icon, template, key, shorten(title, 40)
}

// shorten cuts text to n characters, with an ellipsis where it was cut:
// the menu bar has room for others' icons too.
func shorten(text string, n int) string {
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	r := []rune(text)
	return string(r[:n-1]) + "…"
}

// desktopSync keeps the tray and the notification as the song and the
// settings are; it is called as the system is told what plays.
func (a *App) desktopSync(s *library.Song, paused bool) {
	if !a.native {
		return // no system to show them: tests have none
	}
	d := &a.desk
	func() {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("the tray: %v", err)
			}
		}()
		a.syncTray(s, paused)
	}()
	switch {
	case s == nil:
		d.announced = ""
	case s.ID != d.announced && !paused:
		d.announced = s.ID
		if a.settings.Notify && (a.win == nil || !a.win.IsFocused()) {
			n := mygo.NewNotification(mygo.NotificationOptions{
				ID: "song", Title: s.Name, Subtitle: s.Artist, Body: s.Album, Silent: true, Group: "Aurelia",
			})
			n.OnClick(func() { a.update(a.show) })
			if err := n.Show(); err != nil {
				log.Print("the notification: ", err)
			}
		}
	}
}

// syncTray shows or removes the icon, and makes it, what is beside it and
// its menu say what plays.
func (a *App) syncTray(s *library.Song, paused bool) {
	d := &a.desk
	if !a.settings.Tray {
		if d.tray != nil {
			d.tray.Destroy()
			d.tray, d.trayKey, d.iconKey, d.title = nil, "", "", ""
		}
		return
	}
	title, tip := "Nothing is playing", "Aurelia"
	if s != nil {
		title = s.Name
		if s.Artist != "" {
			title += " — " + s.Artist
		}
		tip = title
	}
	pic, template, iconKey, beside := a.trayLook(s)
	key := fmt.Sprint(title, paused, s != nil)
	if d.tray != nil && d.trayKey == key && d.iconKey == iconKey && d.title == beside {
		return
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: title, Disabled: true},
		mygo.Separator(),
		{Label: map[bool]string{true: "Play", false: "Pause"}[paused || s == nil], Disabled: s == nil,
			Click: func(*mygo.MenuItem, *mygo.Window) { a.update(func() { a.player.toggle() }) }},
		{Label: "Next", Disabled: s == nil, Click: func(*mygo.MenuItem, *mygo.Window) { a.update(func() { a.player.skip() }) }},
		{Label: "Previous", Disabled: s == nil, Click: func(*mygo.MenuItem, *mygo.Window) { a.update(func() { a.player.previous() }) }},
		mygo.Separator(),
		{Label: "Show Aurelia", Click: func(*mygo.MenuItem, *mygo.Window) { a.update(a.show) }},
		{Label: "Quit Aurelia", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
	if d.tray == nil {
		t, err := mygo.NewTray(mygo.TrayOptions{
			Icon: pic, IconIsTemplate: template, Title: beside, ToolTip: tip, Menu: menu,
		})
		if err != nil {
			log.Print("the ", trayName(), " icon: ", err)
			a.settings.Tray = false
			return
		}
		d.tray = t
	} else {
		if d.trayKey != key {
			d.tray.SetMenu(menu)
			d.tray.SetToolTip(tip)
		}
		if d.iconKey != iconKey {
			if err := d.tray.SetIcon(pic, template); err != nil {
				log.Print("the ", trayName(), " icon: ", err)
			}
		}
		if d.title != beside {
			d.tray.SetTitle(beside)
		}
	}
	d.trayKey, d.iconKey, d.title = key, iconKey, beside
}

// followTheme draws the icon again when the theme whose accent it takes
// changed.
func (a *App) followTheme() {
	d := &a.desk
	if d.palFor != a.palFor {
		d.palFor = a.palFor
		if d.tray != nil {
			a.told = ""
		}
	}
}

// trayChanged has the icon drawn again, as the settings now say.
func (a *App) trayChanged() {
	a.saveSettings()
	a.told = "" // so that the next frame tells it
}

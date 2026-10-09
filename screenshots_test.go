package main

import (
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/vector"

	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
)

// The screenshots of the README, when AURELIA_SCREENSHOTS names the
// directory to write them to:
//
//	AURELIA_SCREENSHOTS=docs go test -run Screenshots .
//
// They show the app itself, drawn without a window, on a library made up
// for them: no artist, album or song of it exists, and its pictures are
// drawn here.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("AURELIA_SCREENSHOTS")
	if dir == "" {
		t.Skip("AURELIA_SCREENSHOTS names no directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := testApp(t)
	a.settings.Session.UserName, a.settings.Session.ServerName, a.settings.Session.Server = "Robin", "Home", "https://jellyfin.example.com"
	a.setLibrary(demoLibrary())
	art := map[string]*ui.Bitmap{}
	a.pictures = func(id string) *ui.Bitmap {
		if art[id] == nil {
			art[id] = ui.NewBitmap(demoPicture(id))
		}
		return art[id]
	}
	a.covers = func(id string) image.Image { return demoPicture(id) }
	const width, height = 1240, 780
	tt := ui.NewTester(a.view, width, height)
	tt.SetScale(2)
	shot := func(name string) {
		t.Helper()
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	show := func(theme, path string) {
		a.settings.Theme = theme
		a.router.Push(path)
		tt.Frame()
	}
	// A song playing, a minute in, with more to come.
	album := a.lib.AlbumSongs("al03")
	p := a.player
	p.play(album, 2)
	p.enqueue(a.lib.AlbumSongs("al07")[:4])
	p.cold, p.resumeAt = true, 72*time.Second
	a.lyrics.bySong = map[string][]jellyfin.LyricLine{album[2].ID: demoLyrics()}
	a.lyrics.asked = map[string]bool{}
	a.downloads.albums["al03"], a.downloads.albums["al11"] = true, true
	for _, id := range []string{"al03", "al11"} {
		for _, s := range a.lib.AlbumSongs(id) {
			a.downloads.songs[s.ID], a.downloads.done[s.ID] = s, true
		}
	}
	a.downloads.bytes, a.downloads.counted = 412<<20, true

	show("aurelia-dark", "/home")
	shot("home")
	show("aurelia-dark", "/album/al03")
	shot("album")
	a.settings.QueueOpen = true
	show("aurelia-dark", "/lyrics")
	shot("lyrics")
	a.settings.QueueOpen = false
	show("aurelia-light", "/albums")
	shot("albums-light")
	show("tokyo-night", "/artist/ar02")
	shot("artist-tokyo-night")
	show("catppuccin-mocha", "/search?q=light")
	a.search.query = "light"
	shot("search-catppuccin")
	show("aurelia-dark", "/settings")
	shot("themes")
	show("nord", "/downloads")
	shot("downloads-nord")

	// The record, a little turned, at a line of the song.
	show("aurelia-dark", "/home")
	a.openStage()
	a.stage.angle, a.stage.pointerAt = 0.7, time.Now().Add(-time.Hour)
	p.paused = false // the buttons hide while a song plays
	tt.Frame()
	shot("record")
	p.paused = true
	a.closeStage()

	// Browsing, the equalizer and the picture to share.
	show("aurelia-dark", "/genres")
	shot("browse")
	a.settings.EQ = EQSettings{On: true, Bands: [10]float64{5, 4, 3, 1, -1, -1, 1, 3, 4, 5}}
	show("aurelia-dark", "/equalizer")
	shot("equalizer")
	show("aurelia-dark", "/album/al03")
	a.shareAlbum(a.lib.Album("al03"))
	shot("share")
	a.closeShare()
	tt.Frame()

	// The window at its smallest.
	tt.SetSize(windowMinW, windowMinH)
	show("aurelia-dark", "/album/al03")
	shot("small")
}

// demoGenres are the genres the made-up albums are given.
var demoGenres = [][]string{
	{"Indie Folk"}, {"Dream Pop", "Indie"}, {"Ambient"}, {"Synthwave"}, {"Jazz", "Soul"}, {"Indie"}, {"Electronic", "Ambient"},
	{"Singer-Songwriter"}, {"Post-Rock"}, {"Soul", "R&B"}, {"Classical"}, {"Folk"},
}

// demoLibrary is a library of music that does not exist.
func demoLibrary() *library.Library {
	artists := []string{"Halden Moor", "Vesper Lane", "The Quiet Tides", "Marlowe & Finch", "Lumen Drift", "Kaya Ostrander",
		"Northbound Atlas", "Ilse Varga", "Juniper Static", "The Glasshouse Choir", "Odalys Reyes", "Soren Achterberg"}
	albums := []struct {
		name   string
		artist int
		year   int
	}{
		{"Tidewater", 2, 2021}, {"Slow Light", 1, 2023}, {"Cartography", 6, 2019}, {"Glass Hours", 0, 2024},
		{"Afterglow Season", 4, 2022}, {"Paper Constellations", 5, 2020}, {"Northern Rooms", 7, 2018}, {"Signal & Static", 8, 2025},
		{"The Long Way Down to the Sea", 2, 2017}, {"Honeycomb", 3, 2021}, {"Low Tide Hymns", 9, 2016}, {"Marigold", 10, 2023},
		{"Winter Lights", 1, 2020}, {"Field Notes", 11, 2022}, {"Undertow", 0, 2021}, {"Small Hours", 4, 2019},
		{"Kites", 5, 2024}, {"A Map of Rain", 6, 2022}, {"Saltwater Letters", 9, 2020}, {"Lanterns", 10, 2019},
		{"Second Sun", 8, 2023}, {"Driftwood", 3, 2018}, {"Night Bus Home", 7, 2024}, {"Evergreen", 11, 2017},
	}
	titles := []string{"First Light", "Harbor Lights", "Slow Light", "Paper Boats", "Northern Line", "Salt and Silver", "Lighthouse Keeper",
		"Every Small Hour", "Borrowed Weather", "Low Sun", "Night Swimming Lessons", "Telegraph Hill", "Soft Machines", "Away Days",
		"Light Years Apart", "The Orchard", "Moth Season", "Windowsill", "Quiet Engines", "Last Train East", "Glasshouse", "Tin Roof Rain"}
	d := library.Data{Version: 1, SyncedAt: time.Now().Add(-4 * time.Minute)}
	for i, name := range artists {
		d.Artists = append(d.Artists, library.Artist{ID: fmt.Sprintf("ar%02d", i), Name: name, ImageTag: "t", Favorite: i == 1 || i == 6})
	}
	now := time.Now().Unix()
	n := 0
	for i, al := range albums {
		id := fmt.Sprintf("al%02d", i)
		artist := d.Artists[al.artist]
		d.Albums = append(d.Albums, library.Album{
			ID: id, Name: al.name, Artist: artist.Name, ArtistIDs: []string{artist.ID}, Year: al.year, ImageTag: "t",
			Added: now - int64(i)*86400*3, Favorite: i == 1 || i == 3 || i == 11,
			Genres: demoGenres[(i*7+al.artist)%len(demoGenres)],
		})
		for tr := 0; tr < 9+i%4; tr++ {
			title := titles[(i*5+tr*3)%len(titles)]
			h := hash(id + title)
			s := library.Song{
				ID: fmt.Sprintf("s%03d", n), Name: title, Album: al.name, AlbumID: id, Artist: artist.Name,
				ArtistIDs: []string{artist.ID}, AlbumArtistIDs: []string{artist.ID}, Track: tr + 1, Disc: 1, Year: al.year,
				Seconds: float64(150 + h%160), ImageItem: id, ImageTag: "t", HasLyrics: true,
				Favorite: h%7 == 0, Added: now - int64(i)*86400*3,
			}
			if i < 9 && h%3 == 0 {
				s.Plays, s.LastPlayed = int(3+h%40), now-int64(h%9)*3600*(int64(i)+1)
			}
			d.Songs = append(d.Songs, s)
			n++
		}
	}
	for i, name := range []string{"Late Night Drive", "Sunday Kitchen", "Focus", "Rainy Day"} {
		d.Playlists = append(d.Playlists, library.Playlist{ID: fmt.Sprintf("pl%d", i), Name: name, ImageTag: "t", Songs: 18 + 7*i})
	}
	return library.Build(d)
}

// demoLyrics are words written for the screenshot.
func demoLyrics() []jellyfin.LyricLine {
	words := []string{"The harbor wakes before the town", "Boats like commas on the grey", "I count the lamps as they go down",
		"And keep the last one for the day", "", "Slow light, come over the water", "Slow light, come find me here",
		"I left a window for the morning", "And the morning's getting near", "The gulls rehearse an older song", "The tide forgets what it was told"}
	lines := make([]jellyfin.LyricLine, len(words))
	for i, w := range words {
		lines[i] = jellyfin.LyricLine{Text: w, Start: time.Duration(40+i*6) * time.Second}
	}
	return lines
}

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// palettes are the colors of the made-up covers.
var palettes = [][3]color.RGBA{
	{{0x1b, 0x2a, 0x49, 255}, {0xf2, 0x8f, 0x6b, 255}, {0xfc, 0xe3, 0xb5, 255}},
	{{0x0f, 0x3d, 0x3e, 255}, {0x5c, 0xc9, 0xa7, 255}, {0xf4, 0xf1, 0xde, 255}},
	{{0x2d, 0x1e, 0x4f, 255}, {0xc7, 0x7d, 0xff, 255}, {0xff, 0xd6, 0xa5, 255}},
	{{0xf5, 0xe6, 0xca, 255}, {0xd9, 0x48, 0x3b, 255}, {0x26, 0x32, 0x38, 255}},
	{{0x10, 0x18, 0x20, 255}, {0x3a, 0x86, 0xff, 255}, {0xff, 0xbe, 0x0b, 255}},
	{{0xe8, 0xf1, 0xf2, 255}, {0x13, 0x69, 0x8c, 255}, {0xf7, 0x7f, 0x00, 255}},
	{{0x3c, 0x16, 0x42, 255}, {0xff, 0x5d, 0x8f, 255}, {0xff, 0xe5, 0xec, 255}},
	{{0x1a, 0x1a, 0x1a, 255}, {0xe9, 0xc4, 0x6a, 255}, {0x2a, 0x9d, 0x8f, 255}},
	{{0xfa, 0xf3, 0xdd, 255}, {0x5e, 0x60, 0xce, 255}, {0x48, 0xbf, 0xe3, 255}},
	{{0x22, 0x33, 0x22, 255}, {0xa7, 0xc9, 0x57, 255}, {0xf2, 0xe8, 0xcf, 255}},
}

// demoPicture draws the cover of an album, the picture of an artist or
// of a playlist: shapes and colors that follow from its ID.
func demoPicture(id string) image.Image {
	const size = 512
	h := hash(id)
	pal := palettes[h%uint32(len(palettes))]
	bg, mid, hi := pal[0], pal[1], pal[2]
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	// A gradient, from a corner.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			t := (float64(x)*0.4 + float64(y)) / (size * 1.4)
			img.SetRGBA(x, y, mix(bg, mid, t*t*0.55))
		}
	}
	circle := func(cx, cy, r float64, c color.RGBA) {
		var ras vector.Rasterizer
		ras.Reset(size, size)
		const k = 0.5522847498
		x, y, rr := float32(cx), float32(cy), float32(r)
		ras.MoveTo(x+rr, y)
		ras.CubeTo(x+rr, y+rr*k, x+rr*k, y+rr, x, y+rr)
		ras.CubeTo(x-rr*k, y+rr, x-rr, y+rr*k, x-rr, y)
		ras.CubeTo(x-rr, y-rr*k, x-rr*k, y-rr, x, y-rr)
		ras.CubeTo(x+rr*k, y-rr, x+rr, y-rr*k, x+rr, y)
		ras.ClosePath()
		ras.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
	}
	poly := func(c color.RGBA, pts ...float64) {
		var ras vector.Rasterizer
		ras.Reset(size, size)
		ras.MoveTo(float32(pts[0]), float32(pts[1]))
		for i := 2; i < len(pts); i += 2 {
			ras.LineTo(float32(pts[i]), float32(pts[i+1]))
		}
		ras.ClosePath()
		ras.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
	}
	alpha := func(c color.RGBA, a float64) color.RGBA {
		return color.RGBA{uint8(float64(c.R) * a), uint8(float64(c.G) * a), uint8(float64(c.B) * a), uint8(255 * a)}
	}
	f := func(shift uint) float64 { return float64(h>>shift&0xff) / 255 }
	switch id[:2] {
	case "ar":
		// An artist: soft lights in the dark.
		draw.Draw(img, img.Bounds(), image.NewUniform(mix(bg, color.RGBA{10, 10, 14, 255}, 0.55)), image.Point{}, draw.Src)
		for i := 0; i < 5; i++ {
			g := hash(fmt.Sprint(id, i))
			c := mid
			if i%2 == 1 {
				c = hi
			}
			for r := 150.0; r > 10; r -= 14 {
				circle(float64(g%size), float64(g>>9%size), r, alpha(c, 0.045))
			}
		}
	case "pl":
		// A playlist: four squares of its colors.
		for i, c := range []color.RGBA{mid, hi, bg, mix(mid, hi, 0.5)} {
			x, y := float64(i%2)*size/2, float64(i/2)*size/2
			poly(c, x, y, x+size/2, y, x+size/2, y+size/2, x, y+size/2)
			circle(x+size/4, y+size/4, 60+40*f(uint(i*4)), alpha(pal[(i+1)%3], 0.85))
		}
	default:
		switch h % 6 {
		case 0: // a sun over a horizon
			circle(size*(0.3+0.4*f(4)), size*0.42, size*(0.18+0.1*f(8)), hi)
			for i := 0; i < 5; i++ {
				y := size * (0.58 + 0.09*float64(i))
				poly(alpha(mix(bg, mid, 0.2*float64(i)), 0.92), 0, y, size, y-18*f(uint(i)), size, size, 0, size)
			}
		case 1: // rings
			cx, cy := size*(0.35+0.3*f(3)), size*(0.35+0.3*f(11))
			for i := 9; i >= 0; i-- {
				c := mid
				if i%2 == 0 {
					c = hi
				}
				if i%3 == 0 {
					c = bg
				}
				circle(cx, cy, float64(i+1)*size*0.07, c)
			}
		case 2: // hills
			circle(size*0.72, size*0.3, size*0.11, hi)
			for i := 0; i < 4; i++ {
				peak := size * (0.2 + 0.2*float64(i) + 0.1*f(uint(i*5)))
				base := size * (0.45 + 0.13*float64(i))
				poly(mix(mid, bg, 0.25*float64(i)), peak-size*0.6, size, peak, base-size*0.2, peak+size*0.7, size)
			}
		case 3: // circles over one another
			for i := 0; i < 6; i++ {
				g := hash(fmt.Sprint(id, "c", i))
				circle(float64(60+g%400), float64(60+g>>10%400), 60+float64(g>>20%110), alpha(pal[1+i%2], 0.6))
			}
		case 4: // bands across
			for i := 0; i < 7; i++ {
				x := size * (float64(i)*0.2 - 0.2)
				poly(alpha(pal[i%3], 0.9), x, 0, x+size*0.12, 0, x+size*0.5, size, x+size*0.38, size)
			}
		default: // a moon, and its light on water
			circle(size*0.5, size*0.36, size*0.2, hi)
			circle(size*0.57, size*0.32, size*0.2, mix(bg, mid, 0.18))
			for i := 0; i < 7; i++ {
				w := size * (0.3 - 0.035*float64(i))
				y := size * (0.66 + 0.045*float64(i))
				poly(alpha(hi, 0.8-0.1*float64(i)), size*0.5-w, y, size*0.5+w, y, size*0.5+w, y+7, size*0.5-w, y+7)
			}
		}
	}
	return img
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	t = math.Max(0, math.Min(1, t))
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
}

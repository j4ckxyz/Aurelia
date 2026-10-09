package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// Browsing by what albums are: their genres and the decades they came
// out in.

// facet is a genre or a decade with the albums that are in it.
type facet struct {
	name   string // "Rock", "1990s"
	slug   string // "rock", "1990s", as the page's address has it
	path   string // the page that shows it
	albums []*library.Album
}

// facets are the genres and the decades of a library, found once for each
// version of it.
type facets struct {
	key     string
	genres  []facet
	decades []facet
}

// facetsOf reads the genres and decades off the albums.
func (a *App) facetsOf() *facets {
	key := fmt.Sprint(a.libGen, len(a.lib.Albums))
	if a.browse.key == key {
		return &a.browse
	}
	byGenre := map[string]*facet{}
	byDecade := map[int]*facet{}
	for i := range a.lib.Albums {
		al := &a.lib.Albums[i]
		for _, g := range al.Genres {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			f := byGenre[slugOf(g)]
			if f == nil {
				f = &facet{name: g, slug: slugOf(g)}
				f.path = "/genre/" + f.slug
				byGenre[f.slug] = f
			}
			f.albums = append(f.albums, al)
		}
		if al.Year >= 1900 {
			d := al.Year / 10 * 10
			f := byDecade[d]
			if f == nil {
				f = &facet{name: fmt.Sprintf("%ds", d), slug: strconv.Itoa(d)}
				f.path = "/decade/" + f.slug
				byDecade[d] = f
			}
			f.albums = append(f.albums, al)
		}
	}
	out := facets{key: key}
	for _, f := range byGenre {
		out.genres = append(out.genres, *f)
	}
	// The most albums first, and by name among as many.
	slices.SortFunc(out.genres, func(x, y facet) int {
		return cmp.Or(cmp.Compare(len(y.albums), len(x.albums)), cmp.Compare(strings.ToLower(x.name), strings.ToLower(y.name)))
	})
	for _, f := range byDecade {
		out.decades = append(out.decades, *f)
	}
	slices.SortFunc(out.decades, func(x, y facet) int { return cmp.Compare(y.name, x.name) })
	a.browse = out
	return &a.browse
}

// slugOf makes a name fit an address: "R&B/Soul" is "r-b-soul". Names
// that differ only in what is dropped are one genre.
func slugOf(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	if b.Len() == 0 {
		return "other"
	}
	return b.String()
}

// hueColor is a color of a hue (0 to 360), for a tile that has no picture.
func hueColor(h, s, v float64) ui.Color {
	h = math.Mod(h, 360)
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return ui.RGB(uint8((r+m)*255), uint8((g+m)*255), uint8((b+m)*255))
}

// nameHue gives a name a hue of its own, the same every time.
func nameHue(name string) float64 {
	h := uint32(2166136261)
	for _, b := range []byte(strings.ToLower(name)) {
		h = (h ^ uint32(b)) * 16777619
	}
	return float64(h % 360)
}

// facetTile is a tile of a genre or a decade: a color of its own, its name
// and how many albums it has.
func (a *App) facetTile(c *ui.Context, f facet) {
	p := a.pal
	hue := nameHue(f.name)
	b := ui.ButtonBase(c.Key(f.path)).Grow(1).Basis(0).MinWidth(0).Height(96).Padding(14, 16).Radius(p.radius + 4).Cursor(ui.CursorPointer).
		Label(f.name).Column().Justify(ui.End).Gap(2).Margin(tilePad)
	top, bottom := hueColor(hue, 0.55, 0.62), hueColor(hue+28, 0.62, 0.42)
	if b.Hovered() {
		top, bottom = hueColor(hue, 0.55, 0.7), hueColor(hue+28, 0.62, 0.5)
	}
	b.LinearGradient(ui.LinearGradient{From: top, To: bottom, Angle: 150, Oklab: true})
	b.Transition(fade)
	b.Children(func() {
		ui.Text(c, f.name).FontSize(17).FontWeight(700).TextColor(ui.RGB(255, 255, 255)).SingleLine()
		ui.Text(c, count(len(f.albums), "album", "albums")).FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.78)).SingleLine()
	})
	open := func() { a.goTo(f.path) }
	if a.cursorItem(b, open) {
		b.Border(2, p.accent)
	}
	if b.Clicked() {
		open()
	}
}

// genresPage lists the genres and the decades.
func (a *App) genresPage(c *ui.Context) {
	f := a.facetsOf()
	if len(f.genres) == 0 && len(f.decades) == 0 {
		a.loadingOr(c, "library", "Nothing to browse", "The albums of this library have no genres or years yet.")
		return
	}
	cols := max(2, a.columns(c)*2/3)
	type row struct {
		heading string
		tiles   []facet
	}
	var rows []row
	section := func(title string, list []facet) {
		for i := 0; i < len(list); i += cols {
			h := ""
			if i == 0 {
				h = title
			}
			rows = append(rows, row{h, list[i:min(i+cols, len(list))]})
		}
	}
	section("Genres", f.genres)
	section("Decades", f.decades)
	ui.List(c.Key("genres"), &a.pages.genreList, 1+len(rows), func(i int) {
		a.cursor.in(&a.pages.genreList, i)
		defer a.cursor.in(nil, 0)
		if i == 0 {
			a.pageTitle(c, "Browse", count(len(f.genres), "genre", "genres")+" and "+count(len(f.decades), "decade", "decades"), nil)
			return
		}
		r := rows[i-1]
		if r.heading != "" {
			a.sectionTitle(c, r.heading).Padding(14, pagePad, 6)
		}
		ui.Row(c).AlignItems(ui.Start).Padding(0, pagePad-tilePad).Children(func() {
			for j := 0; j < cols; j++ {
				if j < len(r.tiles) {
					a.facetTile(c, r.tiles[j])
				} else {
					ui.Box(c).Grow(1).Basis(0).MinWidth(0)
				}
			}
		})
	}).Grow(1).MinHeight(0).Padding(0, 0, 28)
}

// facetPage shows the albums of a genre or a decade.
func (a *App) facetPage(c *ui.Context, kind, slug string) {
	f := a.facetsOf()
	var found *facet
	list := f.genres
	if kind == "decade" {
		list = f.decades
	}
	for i := range list {
		if list[i].slug == slug {
			found = &list[i]
		}
	}
	if found == nil {
		a.emptyState(c, "library", "Nothing here", "No album of this library is "+map[string]string{"genre": "in that genre", "decade": "of that decade"}[kind]+".")
		return
	}
	albums := slices.Clone(found.albums)
	slices.SortStableFunc(albums, func(x, y *library.Album) int {
		if kind == "decade" {
			return cmp.Or(cmp.Compare(x.Year, y.Year), cmp.Compare(x.SortKey, y.SortKey))
		}
		return cmp.Compare(x.SortKey, y.SortKey)
	})
	cols := a.columns(c)
	n := 1 + (len(albums)+cols-1)/cols
	ui.List(c.Key("facet:"+found.path), &a.pages.facetList, n, func(i int) {
		a.cursor.in(&a.pages.facetList, i)
		defer a.cursor.in(nil, 0)
		if i == 0 {
			a.pageTitle(c, found.name, count(len(albums), "album", "albums"), func() {
				if a.pillButton(c, "shuffle", "Shuffle", false).Clicked() {
					var songs []*library.Song
					for _, al := range albums {
						songs = append(songs, a.lib.AlbumSongs(al.ID)...)
					}
					a.player.playShuffled(songs)
				}
			})
			return
		}
		start := (i - 1) * cols
		tileRow(c, min(cols, len(albums)-start), cols, func(j int) {
			al := albums[start+j]
			sub := al.Artist
			if kind == "decade" && al.Year > 0 {
				sub = fmt.Sprintf("%d · %s", al.Year, al.Artist)
			}
			a.albumTile(c, al, sub)
		})
	}).Grow(1).MinHeight(0).Padding(0, 0, 28)
}

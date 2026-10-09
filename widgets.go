package main

import (
	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// tile is a picture over a title and a line under it, as an album in a
// grid: it opens on a click, and the button on its picture plays it.
type tile struct {
	key        string
	id, tag    string
	glyph      string
	title, sub string
	round      bool // the picture is a disc, as an artist's
	play       func()
	open       func()
	menu       func(m *ui.Menu)
}

func (a *App) tile(c *ui.Context, t tile) {
	p := a.pal
	b := ui.ButtonBase(c.Key(t.key)).Column().AlignItems(ui.Stretch).Justify(ui.Start).
		Grow(1).Basis(0).MinWidth(0).Padding(tilePad).Gap(0).Radius(p.radius + 4).Cursor(ui.CursorPointer).Label(t.title)
	hovered := b.Hovered()
	if hovered {
		b.Background(p.surface)
	}
	b.Transition(fade)
	played := false
	b.Children(func() {
		pic := a.art(c, t.id, t.tag, tileArt, t.glyph, func() {
			if t.play == nil || !hovered {
				return
			}
			// The play button, over the picture's corner.
			pb := ui.ButtonBase(c).Size(42, 42).Radius(21).Attach(ui.AnchorBottomRight, ui.AnchorBottomRight).Right(8).Bottom(8).
				Background(p.accent).Shadow(0, 4, 12, 0, p.shadow).Label("Play " + t.title).Cursor(ui.CursorPointer)
			if pb.Hovered() {
				pb.Background(p.accentHover)
			}
			pb.Children(func() {
				ui.Icon(c, icon("play-fill")).Size(17, 17).TextColor(p.onAcc).Margin(0, 0, 0, 2)
			})
			if pb.Clicked() {
				played = true
			}
		}).AspectRatio(1)
		if t.round {
			pic.Radius(999)
		} else {
			pic.Radius(p.radius)
		}
		title := ui.Text(c, t.title).FontWeight(600).SingleLine().Margin(10, 0, 0)
		if t.round {
			title.TextAlign(ui.Center)
		}
		if t.sub != "" {
			sub := ui.Text(c, t.sub).FontSize(12).TextColor(p.muted).SingleLine().Margin(2, 0, 2)
			if t.round {
				sub.TextAlign(ui.Center)
			}
		}
	})
	if t.menu != nil {
		b.ContextMenu(t.menu)
	}
	switch {
	case played:
		t.play()
	case b.Clicked():
		t.open()
	}
}

func (a *App) albumTile(c *ui.Context, al *library.Album, sub string) {
	if sub == "" {
		sub = al.Artist
	}
	a.tile(c, tile{
		key: al.ID, id: al.ID, tag: al.ImageTag, glyph: "disc-3", title: al.Name, sub: sub,
		play: func() { a.playAlbum(al, false) },
		open: func() { a.goTo("/album/" + al.ID) },
		menu: func(m *ui.Menu) { a.albumMenu(m, al) },
	})
}

func (a *App) artistTile(c *ui.Context, ar *library.Artist) {
	a.tile(c, tile{
		key: ar.ID, id: ar.ID, tag: ar.ImageTag, glyph: "mic-vocal", title: ar.Name, round: true,
		sub:  count(len(a.lib.ArtistAlbums(ar.ID)), "album", "albums"),
		play: func() { a.player.play(a.artistSongs(ar.ID), 0) },
		open: func() { a.goTo("/artist/" + ar.ID) },
	})
}

func (a *App) playlistTile(c *ui.Context, pl *library.Playlist) {
	a.tile(c, tile{
		key: pl.ID, id: pl.ID, tag: pl.ImageTag, glyph: "list-music", title: pl.Name,
		sub:  count(pl.Songs, "song", "songs"),
		open: func() { a.goTo("/playlist/" + pl.ID) },
	})
}

// tileRow lays n tiles out in a row of cols columns; a row with fewer
// keeps their width.
func tileRow(c *ui.Context, n, cols int, build func(i int)) {
	ui.Row(c).AlignItems(ui.Start).Padding(0, pagePad-tilePad).Children(func() {
		for i := 0; i < cols; i++ {
			if i < n {
				build(i)
			} else {
				ui.Box(c).Grow(1).Basis(0).MinWidth(0)
			}
		}
	})
}

// playAlbum plays an album from its first song, or shuffled.
func (a *App) playAlbum(al *library.Album, shuffled bool) {
	songs := a.lib.AlbumSongs(al.ID)
	if len(songs) == 0 {
		// The library's songs are still coming: ask for this album's.
		a.fetchAlbumSongs(al.ID, func(songs []*library.Song) {
			if shuffled {
				a.player.playShuffled(songs)
			} else {
				a.player.play(songs, 0)
			}
		})
		return
	}
	if shuffled {
		a.player.playShuffled(songs)
	} else {
		a.player.play(songs, 0)
	}
}

// artistSongs returns the songs of an artist's albums in order, the
// newest album first, then the songs on others' albums.
func (a *App) artistSongs(id string) []*library.Song {
	var out []*library.Song
	for _, al := range a.lib.ArtistAlbums(id) {
		out = append(out, a.lib.AlbumSongs(al.ID)...)
	}
	seen := make(map[*library.Song]bool, len(out))
	for _, s := range out {
		seen[s] = true
	}
	for _, s := range a.lib.ArtistSongs(id) {
		if !seen[s] {
			out = append(out, s)
		}
	}
	return out
}

// songRow is how a song shows in a list.
type songRow struct {
	song *library.Song
	// number shows in place of the picture, as in an album; 0 shows the
	// picture.
	number int
	// showAlbum adds the album's column.
	showAlbum bool
	// hideArtist leaves the artist out, as in an album all by one.
	hideArtist bool
	play       func()
	// extra adds items at the top of the row's menu.
	extra func(m *ui.Menu)
}

func (a *App) songRow(c *ui.Context, r songRow) ui.Element {
	p := a.pal
	s := r.song
	cur := a.player.current()
	current := cur != nil && cur.ID == s.ID
	row := ui.Row(c).Height(rowHeight).Padding(0, 12).Gap(12).Radius(p.radius).Margin(0, pagePad-12)
	hovered := row.Hovered()
	if hovered {
		row.Background(p.surface)
	}
	titleColor := p.text
	switch {
	case current:
		titleColor = p.accent
	case a.offline && !a.downloads.done[s.ID]:
		titleColor = p.faint // not here, and the server is away
	}
	play := false
	row.Children(func() {
		// The lead: a number or the picture, the play button over it
		// under the pointer.
		lead := ui.Box(c).Size(40, 40).Center().Shrink(0)
		lead.Children(func() {
			switch {
			case hovered:
				b := ui.ButtonBase(c).Size(32, 32).Radius(16).Label("Play " + s.Name)
				if b.Hovered() {
					b.Background(p.hover)
				}
				b.Children(func() {
					ui.Icon(c, icon("play-fill")).Size(14, 14).TextColor(p.text).Margin(0, 0, 0, 1)
				})
				if b.Clicked() {
					play = true
				}
			case current:
				ui.Icon(c, icon("audio-lines")).Size(18, 18).TextColor(p.accent)
			case r.number > 0:
				ui.Textf(c, "%d", r.number).TextColor(p.muted).FontFeatures("tnum")
			default:
				a.art(c, s.ImageItem, s.ImageTag, thumbArt, "music", nil).Size(40, 40).Radius(max(p.radius-3, 3))
			}
		})
		ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, s.Name).TextColor(titleColor).FontWeight(500).SingleLine()
			if !r.hideArtist && s.Artist != "" {
				ui.Text(c, s.Artist).FontSize(12).TextColor(p.muted).SingleLine()
			}
		})
		if r.showAlbum {
			ui.Text(c, s.Album).TextColor(p.muted).SingleLine().Grow(2).Basis(0).MinWidth(0)
		}
		// Whether it is downloaded, or how far.
		ui.Box(c).Size(16, 16).Shrink(0).Children(func() {
			if st, part := a.downloads.state(s.ID); st != dlNone {
				a.downloadMark(c, st, part, 16)
			}
		})
		// The heart shows on favorites, and under the pointer.
		heart := ui.Box(c).Size(30, 30).Center().Shrink(0)
		heart.Children(func() {
			if !s.Favorite && !hovered {
				return
			}
			glyph, label := "heart", "Add to Favorites"
			if s.Favorite {
				glyph, label = "heart-fill", "Remove from Favorites"
			}
			if a.toggleIcon(c, glyph, label, s.Favorite, 30, 15).Clicked() {
				a.setFavorite(s.ID, &s.Favorite, !s.Favorite)
			}
		})
		ui.Text(c, clock(s.Duration())).TextColor(p.muted).FontFeatures("tnum").Width(44).TextAlign(ui.End).Shrink(0)
	})
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Play").Chosen() && r.play != nil {
			r.play()
		}
		if r.extra != nil {
			r.extra(m)
		}
		a.songMenu(m, s)
	})
	if (play || row.DoubleClicked()) && r.play != nil {
		r.play()
	}
	return row
}

// listHeader heads the columns of a list of songs.
func (a *App) listHeader(c *ui.Context, lead string, showAlbum bool) {
	p := a.pal
	ui.Row(c).Height(30).Padding(0, 12).Gap(12).Margin(0, pagePad-12).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		head := func(s string) ui.Element {
			return ui.Text(c, s).FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(p.faint).SingleLine()
		}
		head(lead).Width(40).TextAlign(ui.Center).Shrink(0)
		head("TITLE").Grow(3).Basis(0)
		if showAlbum {
			head("ALBUM").Grow(2).Basis(0)
		}
		ui.Box(c).Size(16+12+30, 1).Shrink(0)
		ui.Box(c).Width(44).Shrink(0).AlignItems(ui.End).Children(func() {
			ui.Icon(c, icon("clock")).Size(13, 13).TextColor(p.faint)
		})
	})
}

// pageTitle heads a page: its name, a line under it, and what its
// builder adds at the right.
func (a *App) pageTitle(c *ui.Context, title, sub string, right func()) {
	p := a.pal
	ui.Row(c).Padding(8, pagePad, 14).Gap(12).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, title).FontSize(28).FontWeight(700).SingleLine()
			if sub != "" {
				ui.Text(c, sub).TextColor(p.muted).SingleLine()
			}
		})
		if right != nil {
			right()
		}
	})
}

// sortSelect is the drop-down that orders a page.
func (a *App) sortSelect(c *ui.Context, key string, value *string, options []string) bool {
	changed := false
	ui.Row(c).Gap(8).Shrink(0).Children(func() {
		ui.Text(c, "Sort by").TextColor(a.pal.muted).SingleLine()
		if ui.Select(c.Key(key), value, options).Width(170).Changed() {
			changed = true
		}
	})
	return changed
}

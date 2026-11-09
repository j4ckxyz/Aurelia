package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// searchState is the search field and what it found.
type searchState struct {
	query string
	// focus asks the next frame to put the keyboard in the field.
	focus bool
	// play asks the page to play the best of what it found: Enter in the
	// field. leave asks it to take the keyboard from the field, as Down
	// put the marker on what it found.
	play, leave bool
	// loc is the page the field was last matched with.
	loc string

	key     string
	results library.Results
}

// follow keeps the field showing the query of the page: going back to a
// search shows its words again, and leaving it empties the field.
func (s *searchState) follow(a *App) {
	loc := a.router.Location()
	if loc == s.loc {
		return
	}
	s.loc = loc
	if a.router.Path() == "/search" {
		s.query = a.router.Query("q")
	} else {
		s.query = ""
	}
}

// searchGlyph is the magnifying glass of the field, MyGo's own: MingCute's
// search icon (Apache License 2.0,
// https://github.com/Richard9394/MingCute/blob/main/LICENSE).
var searchGlyph = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-linecap="round" stroke-width="2" d="M14.5 14.5L20 20m-4-10a6 6 0 1 1-12 0a6 6 0 0 1 12 0Z"/></svg>`))

// field builds the search field, which searches as its text changes.
//
// It is MyGo's SearchField built from its parts, drawn as that is: a
// SearchField focuses only its frame, so the shortcuts that go to search
// could not put the keyboard in its text. Here the text input is at hand.
func (s *searchState) field(a *App, c *ui.Context) {
	t := a.pal.widgets
	var in ui.Element
	cleared := false
	f := ui.Row(c.Key("search")).AlignItems(ui.Center).Gap(t.Space(1.5)).Padding(t.Space(1.5), t.Space(2)).
		Radius(17).Background(t.Surface).Role(ui.RoleNone).Cursor(ui.CursorText).
		Grow(1).Basis(0).MinWidth(110).MaxWidth(320)
	f.Children(func() {
		ui.Icon(c, searchGlyph).Size(t.Space(3.75), t.Space(3.75)).Shrink(0).TextColor(t.TextMuted)
		in = ui.TextInputBase(c, &s.query).Grow(1).MinWidth(t.Space(15)).Placeholder("Search").Label("Search")
		if s.query == "" {
			return
		}
		// The button clearing the text, as Escape does: not a stop of
		// Tab, as AppKit's is not.
		x := ui.Box(c).Size(t.Space(4), t.Space(4)).Radius(t.Space(2)).Shrink(0).Background(t.TextMuted.Alpha(0.5)).
			Role(ui.RoleButton).Label("Clear")
		x.Draw(func(p *ui.Painter, r ui.Rect) {
			k := r.W * 0.3
			var path ui.Path
			path.MoveTo(r.X+k, r.Y+k).LineTo(r.X+r.W-k, r.Y+r.H-k).MoveTo(r.X+r.W-k, r.Y+k).LineTo(r.X+k, r.Y+r.H-k)
			p.StrokePath(&path, 1.5, t.Background)
		})
		if x.Clicked() || in.Shortcut(0, ui.KeyEscape) {
			s.query = ""
			cleared = true
			in.Focus()
		}
	})
	// A click anywhere in the field puts the keyboard in its text.
	if f.Clicked() || s.focus {
		in.Focus()
		s.focus = false
	}
	hovered := f.Hovered()
	switch {
	case in.Focused():
		f.Border(1, t.Accent)
	case hovered:
		f.Border(1, t.Border.Mix(t.Text, 0.25))
	default:
		f.Border(1, t.Border)
	}
	if in.Changed() || cleared {
		target := "/search?q=" + url.QueryEscape(s.query)
		if a.router.Path() == "/search" {
			// One page of the history for a search, not one for each
			// letter typed.
			a.router.Replace(target)
		} else {
			a.router.Push(target)
		}
		s.loc = a.router.Location()
	}
	if a.router.Path() != "/search" {
		return
	}
	// Enter plays the best of what was found. Down goes into it, with
	// the marker on the first item: the arrows and Enter are the page's
	// then, not the field's.
	if in.Submitted() {
		s.play = true
	}
	if in.Shortcut(0, ui.KeyDown) {
		a.cursor.hide()
		a.cursor.step(0, 1)
		s.leave = true
	}
}

// besideTop is how many songs show beside the top result.
const besideTop = 4

// searchPage shows what the library has for the words in the field: the
// best of it first, large, with the songs that match best beside it, then
// the rest by kind.
func (a *App) searchPage(c *ui.Context, r *ui.Route) {
	s := &a.search
	play, leave := s.play, s.leave
	s.play, s.leave = false, false
	q := s.query
	if library.Fold(q) == "" {
		a.emptyState(c, "search", "Search your library", "Find artists, albums, songs and playlists as you type.")
		return
	}
	cols := a.columns(c)
	if key := a.rowsKey(c) + fmt.Sprint(a.settings.OthersPlaylists) + q; s.key != key {
		s.key = key
		s.results = a.lib.Search(q, library.Limits{Artists: cols, Albums: 2 * cols, Songs: 60, Playlists: 4 * cols})
		// Not the playlists of others, unless they are asked for.
		shown := s.results.Playlists[:0]
		for _, pl := range s.results.Playlists {
			if (!pl.Others || a.settings.OthersPlaylists) && len(shown) < cols {
				shown = append(shown, pl)
			}
		}
		s.results.Playlists = shown
		best := s.results.Best[:0]
		for _, m := range s.results.Best {
			if m.Playlist == nil || !m.Playlist.Others || a.settings.OthersPlaylists {
				best = append(best, m)
			}
		}
		s.results.Best = best
	}
	res := &s.results
	// While the library's songs are still being read, the server finds
	// them.
	songs := res.Songs
	if len(a.lib.Songs) == 0 && a.syncing {
		term := q
		a.fetch("search:"+term, func(ctx context.Context) ([]*library.Song, error) {
			items, err := a.clientNow().SearchSongs(ctx, term, 40)
			if err != nil {
				return nil, err
			}
			out := make([]*library.Song, len(items))
			for i := range items {
				sg := library.SongOf(&items[i])
				library.PrepareSong(&sg)
				out[i] = &sg
			}
			return out, nil
		}, nil)
		songs = a.pages.fetched["search:"+term]
	}
	var top library.Match
	switch {
	case len(res.Best) > 0:
		top = res.Best[0]
	case len(songs) > 0:
		top = library.Match{Song: songs[0]}
	}
	if play {
		a.playTop(top)
	}
	if res.Empty() && len(songs) == 0 {
		a.emptyState(c, "search", "Nothing found for “"+q+"”", "Check the spelling, or try fewer words.")
		return
	}
	beside := songs[:min(besideTop, len(songs))]
	more := songs[len(beside):]
	playSong := func(sg *library.Song) func() { return func() { a.player.playWithSimilar(sg) } }
	// Side by side where the page is wide enough; one over the other
	// where it is not.
	wide := cols >= 5
	rows := a.rows(c, "/search", fmt.Sprint(a.settings.OthersPlaylists)+q+string(rune(len(songs))), func(add func(func(c *ui.Context))) {
		if wide {
			add(func(c *ui.Context) {
				ui.Row(c).AlignItems(ui.Stretch).Padding(22, 0, 0, pagePad).Children(func() {
					ui.Column(c).Grow(2).Basis(0).MinWidth(0).Children(func() {
						a.sectionTitle(c, "Top result").Margin(0, 0, 8)
						a.topCard(c, top)
					})
					ui.Column(c).Grow(3).Basis(0).MinWidth(0).Children(func() {
						if len(beside) == 0 {
							return
						}
						ui.Row(c).Padding(0, pagePad, 8).Children(func() { a.sectionTitle(c, "Songs") })
						for i, sg := range beside {
							// One under another, beside the top result: Down
							// and Up move the marker among them.
							a.cursor.lineAt(i, 1)
							a.songRow(c.Key(sg.ID), songRow{song: sg, play: playSong(sg), list: songs, at: i})
						}
					})
				})
			})
		} else {
			a.section(add, "Top result", nil)
			add(func(c *ui.Context) {
				ui.Row(c).Padding(0, pagePad).Children(func() { a.topCard(c, top) })
			})
			if len(beside) > 0 {
				a.section(add, "Songs", nil)
				for i, sg := range beside {
					add(func(c *ui.Context) {
						a.songRow(c.Key(sg.ID), songRow{song: sg, play: playSong(sg), list: songs, at: i})
					})
				}
			}
		}
		if len(res.Artists) > 0 {
			a.section(add, "Artists", nil)
			tileRows(add, len(res.Artists), cols, func(c *ui.Context, i int) { a.artistTile(c, res.Artists[i]) })
		}
		if len(res.Albums) > 0 {
			a.section(add, "Albums", nil)
			tileRows(add, len(res.Albums), cols, func(c *ui.Context, i int) { a.albumTile(c, res.Albums[i], "") })
		}
		if len(more) > 0 {
			a.section(add, "More songs", nil)
			for i, sg := range more {
				add(func(c *ui.Context) {
					a.songRow(c.Key(sg.ID), songRow{song: sg, showAlbum: true, play: playSong(sg), list: songs, at: len(beside) + i})
				})
			}
		}
		if len(res.Playlists) > 0 {
			a.section(add, "Playlists", nil)
			tileRows(add, len(res.Playlists), cols, func(c *ui.Context, i int) { a.playlistTile(c, res.Playlists[i]) })
		}
	})
	a.list(c, rows)
	if leave {
		// The keyboard leaves the field for the page, where the marker is.
		r.Page().Focus()
	}
}

// topCard is the best of what a search found, larger than the rest: its
// picture, its name, what it is, and the button that plays it under the
// pointer. A click opens it, or plays it when it is a song.
func (a *App) topCard(c *ui.Context, m library.Match) {
	p := a.pal
	var (
		id, tag, glyph   string
		title, kind, sub string
		round            bool
		menu             func(m *ui.Menu)
	)
	switch {
	case m.Artist != nil:
		ar := m.Artist
		id, tag, glyph, title, kind, round = ar.ID, ar.ImageTag, "mic-vocal", ar.Name, "Artist", true
		menu = func(mu *ui.Menu) { a.artistMenu(mu, ar) }
	case m.Album != nil:
		al := m.Album
		id, tag, glyph, title, kind, sub = al.ID, al.ImageTag, "disc-3", al.Name, "Album", al.Artist
		menu = func(mu *ui.Menu) { a.albumMenu(mu, al) }
	case m.Song != nil:
		sg := m.Song
		id, tag, glyph, title, kind, sub = sg.ImageItem, sg.ImageTag, "music", sg.Name, "Song", sg.Artist
		menu = func(mu *ui.Menu) { a.songMenu(mu, sg) }
	case m.Playlist != nil:
		pl := m.Playlist
		id, tag, glyph, title, kind, sub = pl.ID, pl.ImageTag, "list-music", pl.Name, "Playlist", count(pl.Songs, "song", "songs")
		menu = func(mu *ui.Menu) { a.pinMenu(mu, "playlist", pl.ID, pl.Name) }
	default:
		return
	}
	const size = 92
	b := ui.ButtonBase(c.Key("top")).Column().AlignItems(ui.Stretch).Justify(ui.Start).Grow(1).MinWidth(0).
		Padding(20).Gap(0).Radius(p.radius + 4).Background(p.surface).Cursor(ui.CursorPointer).Label(title)
	hovered := b.Hovered()
	if hovered {
		b.Background(p.hover)
	}
	b.Transition(fade)
	played := false
	b.Children(func() {
		pic := a.art(c, id, tag, size, glyph, nil).Size(size, size)
		if round {
			pic.Radius(size / 2)
		} else {
			pic.Radius(p.radius)
		}
		ui.Text(c, title).FontSize(28).FontWeight(800).SingleLine().Margin(18, 0, 0)
		ui.Row(c).Gap(8).AlignItems(ui.Center).MinWidth(0).Margin(6, 0, 0).Children(func() {
			ui.Box(c).Padding(3, 10).Radius(999).Background(p.bg.Alpha(0.6)).Shrink(0).Children(func() {
				ui.Text(c, kind).FontSize(12).FontWeight(700).SingleLine()
			})
			if sub != "" {
				ui.Text(c, sub).FontSize(13).TextColor(p.muted).SingleLine().MinWidth(0)
			}
		})
		if hovered {
			pb := ui.ButtonBase(c.Key("play")).Size(48, 48).Radius(24).Transition(tween(risesIn)).Attach(ui.AnchorBottomRight, ui.AnchorBottomRight).Right(20).Bottom(20).
				Background(p.accent).Shadow(0, 4, 12, 0, p.shadow).Label("Play " + title).Cursor(ui.CursorPointer)
			if pb.Hovered() {
				pb.Background(p.accentHover)
			}
			pb.Children(func() {
				ui.Icon(c, icon("play-fill")).Size(19, 19).TextColor(p.onAcc).Margin(0, 0, 0, 2)
			})
			played = pb.Clicked()
		}
	})
	b.ContextMenu(menu)
	open := func() { a.openTop(m) }
	if a.cursorItem(b, open) {
		b.Background(p.hover)
	}
	switch {
	case played:
		a.playTop(m)
	case b.Clicked():
		open()
	}
}

// playTop plays what a search found best: a song, then songs like it,
// as a click on the song does; an album, an artist or a playlist in its
// order.
func (a *App) playTop(m library.Match) {
	switch {
	case m.Song != nil:
		a.player.playWithSimilar(m.Song)
	case m.Album != nil:
		a.playAlbum(m.Album, false)
	case m.Artist != nil:
		a.player.play(a.artistSongs(m.Artist.ID), 0)
	case m.Playlist != nil:
		a.fetchPlaylist(m.Playlist.ID, func(songs []*library.Song) { a.player.play(songs, 0) })
	}
}

// openTop opens the page of what a search found best, or plays it when
// it is a song, as a click on it does.
func (a *App) openTop(m library.Match) {
	switch {
	case m.Song != nil:
		a.player.playWithSimilar(m.Song)
	case m.Album != nil:
		a.goTo("/album/" + m.Album.ID)
	case m.Artist != nil:
		a.goTo("/artist/" + m.Artist.ID)
	case m.Playlist != nil:
		a.goTo("/playlist/" + m.Playlist.ID)
	}
}

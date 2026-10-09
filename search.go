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

// field builds the search field, which searches as its text changes.
func (s *searchState) field(a *App, c *ui.Context) {
	f := ui.SearchField(c.Key("search"), &s.query).Width(320).Radius(17).Label("Search").Placeholder("Search artists, albums and songs")
	if s.focus {
		f.Focus()
		s.focus = false
	}
	if f.Changed() {
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
}

// searchPage shows what the library has for the words in the field.
func (a *App) searchPage(c *ui.Context) {
	q := a.search.query
	if library.Fold(q) == "" {
		a.emptyState(c, "search", "Search your library", "Find artists, albums, songs and playlists as you type.")
		return
	}
	s := &a.search
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
	if res.Empty() && len(songs) == 0 {
		a.emptyState(c, "search", "Nothing found for “"+q+"”", "Check the spelling, or try fewer words.")
		return
	}
	rows := a.rows(c, "/search", fmt.Sprint(a.settings.OthersPlaylists)+q+string(rune(len(songs))), func(add func(func(c *ui.Context))) {
		if len(res.Artists) > 0 {
			a.section(add, "Artists", nil)
			tileRows(add, len(res.Artists), cols, func(c *ui.Context, i int) { a.artistTile(c, res.Artists[i]) })
		}
		if len(res.Albums) > 0 {
			a.section(add, "Albums", nil)
			tileRows(add, len(res.Albums), cols, func(c *ui.Context, i int) { a.albumTile(c, res.Albums[i], "") })
		}
		if len(songs) > 0 {
			a.section(add, "Songs", nil)
			for i, sg := range songs {
				add(func(c *ui.Context) {
					a.songRow(c.Key(sg.ID), songRow{song: sg, showAlbum: true, play: func() { a.player.play(songs, i) }})
				})
			}
		}
		if len(res.Playlists) > 0 {
			a.section(add, "Playlists", nil)
			tileRows(add, len(res.Playlists), cols, func(c *ui.Context, i int) { a.playlistTile(c, res.Playlists[i]) })
		}
	})
	a.list(c, rows)
}

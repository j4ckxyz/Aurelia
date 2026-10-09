package main

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// What has been listened to, from the plays the server counts: how much,
// and the songs, albums and artists most.

// listening is the library's plays, added up.
type listening struct {
	plays   int
	time    time.Duration
	songs   []*library.Song
	albums  []*library.Album
	artists []*library.Artist
	// plays by album and by artist, for the lines under them.
	byAlbum, byArtist map[string]int
}

// listeningOf adds up the plays of every song.
func (a *App) listeningOf() *listening {
	l := &listening{byAlbum: map[string]int{}, byArtist: map[string]int{}}
	for i := range a.lib.Songs {
		s := &a.lib.Songs[i]
		if s.Plays == 0 {
			continue
		}
		l.plays += s.Plays
		l.time += time.Duration(float64(s.Plays) * s.Seconds * float64(time.Second))
		l.songs = append(l.songs, s)
		l.byAlbum[s.AlbumID] += s.Plays
		for _, id := range s.ArtistIDs {
			l.byArtist[id] += s.Plays
		}
	}
	slices.SortStableFunc(l.songs, func(x, y *library.Song) int {
		return cmp.Or(cmp.Compare(y.Plays, x.Plays), cmp.Compare(x.SortKey, y.SortKey))
	})
	for id := range l.byAlbum {
		if al := a.lib.Album(id); al != nil {
			l.albums = append(l.albums, al)
		}
	}
	slices.SortStableFunc(l.albums, func(x, y *library.Album) int {
		return cmp.Or(cmp.Compare(l.byAlbum[y.ID], l.byAlbum[x.ID]), cmp.Compare(x.SortKey, y.SortKey))
	})
	for id := range l.byArtist {
		if ar := a.lib.Artist(id); ar != nil {
			l.artists = append(l.artists, ar)
		}
	}
	slices.SortStableFunc(l.artists, func(x, y *library.Artist) int {
		return cmp.Or(cmp.Compare(l.byArtist[y.ID], l.byArtist[x.ID]), cmp.Compare(x.SortKey, y.SortKey))
	})
	return l
}

// listeningPage shows how much was listened to and to what.
func (a *App) listeningPage(c *ui.Context) {
	l := a.listeningOf()
	if l.plays == 0 {
		a.loadingOr(c, "audio-lines", "Nothing played yet", "The songs you play are counted, and show here: the most played, and for how long.")
		return
	}
	cols := a.columns(c)
	top := func(n int, list []*library.Song) []*library.Song { return list[:min(n, len(list))] }
	songs := top(10, l.songs)
	albums := l.albums[:min(len(l.albums), cols)]
	artists := l.artists[:min(len(l.artists), cols)]
	rows := a.rows(c, "/listening", fmt.Sprint(l.plays, a.libGen), func(add func(func(c *ui.Context))) {
		add(func(c *ui.Context) {
			a.pageTitle(c, "Your listening", count(l.plays, "play", "plays")+" · "+long(l.time.Seconds()), nil)
		})
		a.section(add, "Songs you play most", nil)
		for i, s := range songs {
			add(func(c *ui.Context) {
				a.songRow(c.Key("most-"+s.ID), songRow{song: s, showAlbum: true, play: func() { a.player.play(songs, i) },
					number: i + 1})
			})
		}
		a.section(add, "Albums", nil)
		tileRows(add, len(albums), cols, func(c *ui.Context, i int) {
			a.albumTile(c, albums[i], count(l.byAlbum[albums[i].ID], "play", "plays"))
		})
		a.section(add, "Artists", nil)
		tileRows(add, len(artists), cols, func(c *ui.Context, i int) { a.artistTile(c, artists[i]) })
	})
	a.list(c, rows)
}

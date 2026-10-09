package main

import (
	"cmp"
	"fmt"
	mrand "math/rand/v2"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// quickMin is the least width of a card of "Jump back in".
const quickMin = 250

// homePage opens with what was playing, to go on with, then what was
// played lately, what is new, and what is played most.
func (a *App) homePage(c *ui.Context) {
	if a.lib.IsEmpty() {
		a.loadingOr(c, "disc-3", "No music yet", "This server's library has no albums. Add music to Jellyfin and it shows here.")
		return
	}
	cols := a.columns(c)
	quickCols := max(2, min(4, int(a.contentWidth(c)-2*pagePad)/quickMin))
	rows := a.rows(c, "/home", fmt.Sprint(quickCols, a.settings.OthersPlaylists), func(add func(func(c *ui.Context))) {
		add(a.homeHead)
		played, most := a.playedAlbums()

		// Two rows of what was played last, small.
		if quick := played[:min(len(played), 2*quickCols)]; len(quick) > 0 {
			a.section(add, "Jump back in", nil)
			for start := 0; start < len(quick); start += quickCols {
				row := quick[start:min(len(quick), start+quickCols)]
				add(func(c *ui.Context) {
					ui.Row(c).Padding(0, pagePad, 10).Gap(10).Children(func() {
						for i := 0; i < quickCols; i++ {
							if i < len(row) {
								a.quickCard(c, row[i])
							} else {
								ui.Box(c).Grow(1).Basis(0).MinWidth(0)
							}
						}
					})
				})
			}
		}

		shelf := func(title string, n int, more func(), build func(c *ui.Context, i int)) {
			if n == 0 {
				return
			}
			var right func(c *ui.Context)
			if more != nil && n > cols {
				right = func(c *ui.Context) {
					if a.textButton(c, "Show all").Clicked() {
						more()
					}
				}
			}
			a.section(add, title, right)
			tileRows(add, min(n, cols), cols, build)
		}
		albums := func(title string, list []*library.Album, more func()) {
			shelf(title, len(list), more, func(c *ui.Context, i int) { a.albumTile(c, list[i], "") })
		}
		added := a.albumsBy("Recently Added")
		albums("Recently added", added, func() {
			a.settings.AlbumSort = "Recently Added"
			a.goTo("/albums")
		})
		artists := a.topArtists()
		shelf("Your top artists", len(artists), nil, func(c *ui.Context, i int) { a.artistTile(c, artists[i]) })
		albums("Most played", most, nil)
		playlists := a.playlists()
		shelf("Your playlists", len(playlists), func() { a.goTo("/playlists") }, func(c *ui.Context, i int) { a.playlistTile(c, playlists[i]) })
		var favorites []*library.Album
		for i := range a.lib.Albums {
			if a.lib.Albums[i].Favorite {
				favorites = append(favorites, &a.lib.Albums[i])
			}
		}
		albums("Favorite albums", favorites, nil)
		// A different handful each time the library is read.
		if n := len(a.lib.Albums); n > cols {
			rng := mrand.New(mrand.NewPCG(uint64(a.libGen), uint64(n)))
			var picks []*library.Album
			for _, i := range rng.Perm(n)[:cols] {
				picks = append(picks, &a.lib.Albums[i])
			}
			albums("Rediscover", picks, nil)
		}
	})
	a.list(c, rows)
}

// homeHead is the top of Home: the greeting, and the song to go on with.
// The wash of the theme's accent behind it is the window's (view).
func (a *App) homeHead(c *ui.Context) {
	p := a.pal
	ui.Column(c).Children(func() {
		ui.Column(c).Padding(14, pagePad, 14).Gap(4).Children(func() {
			ui.Text(c, greeting()).FontSize(32).FontWeight(800).SingleLine()
			ui.Text(c, time.Now().Format("Monday, 2 January")).TextColor(p.muted).SingleLine()
		})
		a.continueCard(c)
	})
}

// continueCard is the song that was playing, with how far it is, to go
// on with at a click: the queue of the last run, or what plays now.
func (a *App) continueCard(c *ui.Context) {
	p := a.pal
	pl := a.player
	song := pl.current()
	if song == nil {
		return
	}
	st := pl.state()
	playing := pl.playing()
	if playing {
		c.After(500 * time.Millisecond) // the time moves
	}
	const artSize = 132
	ui.Row(c).Margin(2, pagePad, 8).Padding(16).Gap(22).Radius(p.radius+8).Background(p.surface.Alpha(0.78)).Border(1, p.border).
		Shadow(0, 12, 32, 0, p.shadow).Children(func() {
		a.art(c, song.ImageItem, song.ImageTag, artSize, "music", nil).Size(artSize, artSize).Radius(p.radius+2).Shadow(0, 6, 18, 0, p.shadow)
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			label := "CONTINUE PLAYING"
			if playing {
				label = "PLAYING NOW"
			}
			ui.Text(c, label).FontSize(11).FontWeight(700).LetterSpacing(1).TextColor(p.accent).SingleLine()
			ui.Text(c, song.Name).FontSize(24).FontWeight(800).SingleLine()
			from := song.Artist
			if song.Album != "" {
				from += " · " + song.Album
			}
			ui.Text(c, from).TextColor(p.muted).SingleLine()

			// How far the song is.
			frac := float32(0)
			if st.Duration > 0 {
				frac = float32(max(0, min(1, float64(st.Position)/float64(st.Duration))))
			}
			ui.Row(c).Gap(10).Margin(8, 0, 0).Children(func() {
				ui.Text(c, clock(st.Position)).FontSize(11).TextColor(p.muted).FontFeatures("tnum").Shrink(0)
				ui.Box(c).Grow(1).Height(4).Radius(2).Background(p.hover).Children(func() {
					ui.Box(c).WidthPercent(100 * frac).Height(4).Radius(2).Background(p.accent)
				})
				ui.Text(c, clock(st.Duration)).FontSize(11).TextColor(p.muted).FontFeatures("tnum").Shrink(0)
			})
			ui.Row(c).Gap(10).Margin(8, 0, 0).Children(func() {
				glyph, name := "play-fill", "Resume"
				if playing {
					glyph, name = "pause-fill", "Pause"
				}
				if a.pillButton(c, glyph, name, true).Clicked() {
					pl.toggle()
				}
				if song.AlbumID != "" && a.pillButton(c, "disc-3", "Go to album", false).Clicked() {
					a.goTo("/album/" + song.AlbumID)
				}
				if after := len(pl.queue) - pl.index - 1; after > 0 {
					ui.Text(c, count(after, "more song", "more songs")+" in the queue").FontSize(12).TextColor(p.muted).SingleLine().Margin(0, 0, 0, 6)
				}
			})
		})
	})
}

// quickCard is an album in a row of "Jump back in": its picture, its
// name, and a button to play it under the pointer.
func (a *App) quickCard(c *ui.Context, al *library.Album) {
	p := a.pal
	const h = 62
	b := ui.ButtonBase(c.Key("quick-" + al.ID)).Row().Grow(1).Basis(0).MinWidth(0).Height(h).Padding(0).Gap(12).Radius(p.radius + 2).
		Justify(ui.Start).Background(p.surface).Cursor(ui.CursorPointer).Label(al.Name).Clip()
	hovered := b.Hovered()
	if hovered {
		b.Background(p.hover)
	}
	b.Transition(fade)
	played := false
	b.Children(func() {
		a.art(c, al.ID, al.ImageTag, h, "disc-3", nil).Size(h, h).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, al.Name).FontWeight(600).SingleLine()
			ui.Text(c, al.Artist).FontSize(12).TextColor(p.muted).SingleLine()
		})
		if hovered {
			pb := ui.ButtonBase(c).Size(36, 36).Radius(18).Margin(0, 12, 0, 0).Shrink(0).Background(p.accent).
				Shadow(0, 4, 12, 0, p.shadow).Label("Play " + al.Name).Cursor(ui.CursorPointer)
			if pb.Hovered() {
				pb.Background(p.accentHover)
			}
			pb.Children(func() {
				ui.Icon(c, icon("play-fill")).Size(15, 15).TextColor(p.onAcc).Margin(0, 0, 0, 2)
			})
			played = pb.Clicked()
		}
	})
	b.ContextMenu(func(m *ui.Menu) { a.albumMenu(m, al) })
	open := func() { a.goTo("/album/" + al.ID) }
	a.cursorItem(b, open)
	switch {
	case played:
		a.playAlbum(al, false)
	case b.Clicked():
		open()
	}
}

// topArtists returns the artists whose songs were played most, the most
// played first.
func (a *App) topArtists() []*library.Artist {
	s := &a.pages.topArtists
	key := fmt.Sprint(a.libGen, a.favGen)
	if s.key == key {
		return s.list
	}
	plays := map[string]int{}
	for i := range a.lib.Songs {
		sg := &a.lib.Songs[i]
		if sg.Plays == 0 {
			continue
		}
		ids := sg.AlbumArtistIDs
		if len(ids) == 0 {
			ids = sg.ArtistIDs
		}
		for _, id := range ids {
			plays[id] += sg.Plays
		}
	}
	var list []*library.Artist
	for id := range plays {
		if ar := a.lib.Artist(id); ar != nil {
			list = append(list, ar)
		}
	}
	slices.SortFunc(list, func(x, y *library.Artist) int {
		return cmp.Or(cmp.Compare(plays[y.ID], plays[x.ID]), cmp.Compare(x.SortKey, y.SortKey))
	})
	s.key, s.list = key, list[:min(len(list), 24)]
	return s.list
}

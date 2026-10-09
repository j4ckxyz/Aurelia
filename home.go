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
	quickCols := max(1, min(4, int(a.contentWidth(c)-2*pagePad)/quickMin))
	rows := a.rows(c, "/home", fmt.Sprint(quickCols, a.settings.OthersPlaylists, a.hiddenGen, a.player.current() != nil), func(add func(func(c *ui.Context))) {
		add(a.homeHead)
		played, most := a.recentAlbums()

		// Two rows of small cards: the song to go on with first, then the
		// albums played last. Whether there is a song is asked as the
		// rows are built, for the queue changes without them.
		if len(played) > 0 || a.player.current() != nil {
			a.section(add, "Jump back in", nil)
			for line := 0; line < 2; line++ {
				add(func(c *ui.Context) {
					// The album of the song is the song's card.
					song := a.player.current()
					room := 2 * quickCols
					if song != nil {
						room--
					}
					albums := make([]*library.Album, 0, room)
					for _, al := range played {
						if (song == nil || al.ID != song.AlbumID) && len(albums) < room {
							albums = append(albums, al)
						}
					}
					cards := len(albums)
					if song != nil {
						cards++
					}
					first := line * quickCols
					if first >= cards {
						return
					}
					ui.Row(c).Padding(0, pagePad, 10).Gap(10).Children(func() {
						for i := first; i < first+quickCols; i++ {
							switch {
							case i == 0 && song != nil:
								a.continueCard(c, song)
							case i < cards && song != nil:
								a.quickCard(c, albums[i-1])
							case i < cards:
								a.quickCard(c, albums[i])
							default:
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

// homeHead is the top of Home: the greeting. The wash of the theme's
// accent behind it is the window's (view).
func (a *App) homeHead(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(14, pagePad, 6).Gap(4).Children(func() {
		ui.Text(c, greeting()).FontSize(32).FontWeight(800).SingleLine()
		ui.Text(c, time.Now().Format("Monday, 2 January")).TextColor(p.muted).SingleLine()
	})
}

// quickH is the height of a card of "Jump back in".
const quickH = 62

// continueCard is the song of the queue among the cards of "Jump back
// in", to go on with: as small as an album's, with a button that plays
// it on and a line along its foot that tells how far it is.
func (a *App) continueCard(c *ui.Context, song *library.Song) {
	p := a.pal
	pl := a.player
	st := pl.state()
	playing := pl.playing()
	if playing {
		c.After(time.Second) // the line moves
	}
	b := ui.ButtonBase(c.Key("continue")).Row().Grow(1).Basis(0).MinWidth(0).Height(quickH).Padding(0).Gap(12).Radius(p.radius+2).
		Justify(ui.Start).Background(p.surface).Border(1, p.accent.Alpha(0.45)).Cursor(ui.CursorPointer).Label("Continue " + song.Name).Clip()
	if b.Hovered() {
		b.Background(p.hover)
	}
	b.Transition(fade)
	toggled := false
	b.Children(func() {
		a.art(c, song.ImageItem, song.ImageTag, quickH, "music", nil).Size(quickH, quickH).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, song.Name).FontWeight(600).SingleLine()
			what := "Continue"
			if playing {
				what = "Playing"
			}
			ui.Text(c, what+" · "+clock(st.Position)+" of "+clock(st.Duration)).FontSize(12).TextColor(p.accent).SingleLine().FontFeatures("tnum")
		})
		glyph, label := "play-fill", "Resume"
		if playing {
			glyph, label = "pause-fill", "Pause"
		}
		pb := ui.ButtonBase(c).Size(36, 36).Radius(18).Margin(0, 12, 0, 0).Shrink(0).Background(p.accent).Label(label).Tooltip(label).Cursor(ui.CursorPointer)
		if pb.Hovered() {
			pb.Background(p.accentHover)
		}
		pb.Children(func() { ui.Icon(c, icon(glyph)).Size(15, 15).TextColor(p.onAcc) })
		toggled = pb.Clicked()
		// How far the song is, along the foot.
		if st.Duration > 0 {
			frac := float32(max(0, min(1, float64(st.Position)/float64(st.Duration))))
			ui.Box(c).Attach(ui.AnchorBottomLeft, ui.AnchorBottomLeft).WidthPercent(100 * frac).Height(2).Background(p.accent)
		}
	})
	open := func() {
		if song.AlbumID != "" {
			a.goTo("/album/" + song.AlbumID)
		}
	}
	a.cursorItem(b, open)
	switch {
	case toggled:
		pl.toggle()
	case b.Clicked():
		open()
	}
}

// quickCard is an album in a row of "Jump back in": its picture, its
// name, and a button to play it under the pointer.
func (a *App) quickCard(c *ui.Context, al *library.Album) {
	p := a.pal
	const h = quickH
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
	b.ContextMenu(func(m *ui.Menu) {
		if m.Item("Remove from Jump back in").Chosen() {
			a.hideRecent(al)
		}
		m.Separator()
		a.albumMenu(m, al)
	})
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

// recentAlbums returns the albums played last, without those taken out
// of "Jump back in" since they were last played, and those played most.
func (a *App) recentAlbums() (recent, most []*library.Album) {
	all, most, last := a.playedAlbums()
	for _, al := range all {
		if at, hidden := a.settings.HiddenRecent[al.ID]; hidden && last[al] <= at {
			continue
		}
		recent = append(recent, al)
	}
	return recent, most
}

// hideRecent takes an album out of "Jump back in", until it is played
// again.
func (a *App) hideRecent(al *library.Album) {
	if a.settings.HiddenRecent == nil {
		a.settings.HiddenRecent = map[string]int64{}
	}
	a.settings.HiddenRecent[al.ID] = time.Now().Unix()
	a.hiddenGen++
	a.saveSettings()
}

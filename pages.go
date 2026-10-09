package main

import (
	"cmp"
	"context"
	"fmt"
	mrand "math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// pageStates is what pages derive from the library, kept until the
// library or what orders them changes.
type pageStates struct {
	albums, added sorted[*library.Album]
	artistList    sorted[*library.Artist]
	songs, titles sorted[*library.Song]
	rows          map[string]*rowList
	// The places of the long lists, which stay as other pages show.
	albumList, artistsList, songList, queueList ui.ListState
	queueChosen                                 int
	fetched                                     map[string][]*library.Song // songs asked of the server, by album or playlist
	fetching                                    map[string]bool
	fetchErr                                    map[string]string
}

// sorted is a list in an order, with what it was made for.
type sorted[T any] struct {
	key  string
	list []T
}

// rowList is the rows of a page, each a function building it, with what
// they were made for.
type rowList struct {
	key  string
	rows []func(c *ui.Context)
}

// rows returns the rows of a page, made by build when the library, the
// favorites, the width or extra changed since they were.
func (a *App) rows(c *ui.Context, page, extra string, build func(add func(func(c *ui.Context)))) []func(c *ui.Context) {
	if a.pages.rows == nil {
		a.pages.rows = map[string]*rowList{}
	}
	key := a.rowsKey(c) + extra
	rl := a.pages.rows[page]
	if rl == nil || rl.key != key {
		rl = &rowList{key: key}
		build(func(fn func(c *ui.Context)) { rl.rows = append(rl.rows, fn) })
		// A few pages' worth: those of the history.
		if len(a.pages.rows) > 24 {
			clear(a.pages.rows)
		}
		a.pages.rows[page] = rl
	}
	return rl.rows
}

// rowsKey names what the rows of every page depend on.
func (a *App) rowsKey(c *ui.Context) string {
	return fmt.Sprint(a.libGen, " ", a.favGen, " ", a.columns(c), " ")
}

// list shows rows in a list that builds only those in view.
func (a *App) list(c *ui.Context, rows []func(c *ui.Context)) {
	ui.List(c, nil, len(rows), func(i int) { rows[i](c) }).Grow(1).MinHeight(0).Padding(0, 0, 28)
}

// tileRows adds the rows of a grid of n tiles.
func tileRows(add func(func(c *ui.Context)), n, cols int, build func(c *ui.Context, i int)) {
	for start := 0; start < n; start += cols {
		count := min(cols, n-start)
		add(func(c *ui.Context) {
			tileRow(c, count, cols, func(i int) { build(c, start+i) })
		})
	}
}

// section adds a title over what follows.
func (a *App) section(add func(func(c *ui.Context)), title string, right func(c *ui.Context)) {
	add(func(c *ui.Context) {
		ui.Row(c).Padding(22, pagePad, 8).Gap(12).AlignItems(ui.Center).Children(func() {
			a.sectionTitle(c, title).Grow(1)
			if right != nil {
				right(c)
			}
		})
	})
}

// homePage shows what is new and what was played.
func (a *App) homePage(c *ui.Context) {
	if a.lib.IsEmpty() {
		a.loadingOr(c, "disc-3", "No music yet", "This server's library has no albums. Add music to Jellyfin and it shows here.")
		return
	}
	cols := a.columns(c)
	rows := a.rows(c, "/home", "", func(add func(func(c *ui.Context))) {
		add(func(c *ui.Context) { a.pageTitle(c, greeting(), "", nil) })
		shelf := func(title string, albums []*library.Album, sub func(*library.Album) string, more string) {
			if len(albums) == 0 {
				return
			}
			var right func(c *ui.Context)
			if more != "" && len(albums) > cols {
				right = func(c *ui.Context) {
					if a.textButton(c, "Show all").Clicked() {
						a.settings.AlbumSort = more
						a.goTo("/albums")
					}
				}
			}
			a.section(add, title, right)
			shown := albums[:min(len(albums), cols)]
			tileRows(add, len(shown), cols, func(c *ui.Context, i int) {
				s := ""
				if sub != nil {
					s = sub(shown[i])
				}
				a.albumTile(c, shown[i], s)
			})
		}
		played, most := a.playedAlbums()
		shelf("Recently played", played, nil, "")
		added := slices.Clone(a.albumsBy("Recently Added"))
		shelf("Recently added", added, nil, "Recently Added")
		shelf("Most played", most, nil, "")
		var favorites []*library.Album
		for i := range a.lib.Albums {
			if a.lib.Albums[i].Favorite {
				favorites = append(favorites, &a.lib.Albums[i])
			}
		}
		shelf("Favorite albums", favorites, nil, "")
		// A different handful each time the library is read.
		if n := len(a.lib.Albums); n > cols {
			rng := mrand.New(mrand.NewPCG(uint64(a.libGen), uint64(n)))
			var picks []*library.Album
			for _, i := range rng.Perm(n)[:cols] {
				picks = append(picks, &a.lib.Albums[i])
			}
			shelf("Rediscover", picks, nil, "")
		}
	})
	a.list(c, rows)
}

func greeting() string {
	switch h := time.Now().Hour(); {
	case h < 5:
		return "Good night"
	case h < 12:
		return "Good morning"
	case h < 18:
		return "Good afternoon"
	}
	return "Good evening"
}

// playedAlbums returns the albums played last, the last first, and those
// played most.
func (a *App) playedAlbums() (recent, most []*library.Album) {
	last := map[*library.Album]int64{}
	plays := map[*library.Album]int{}
	for i := range a.lib.Songs {
		s := &a.lib.Songs[i]
		if s.Plays == 0 && s.LastPlayed == 0 {
			continue
		}
		al := a.lib.Album(s.AlbumID)
		if al == nil {
			continue
		}
		plays[al] += s.Plays
		last[al] = max(last[al], s.LastPlayed)
	}
	for al, at := range last {
		if at > 0 {
			recent = append(recent, al)
		}
	}
	slices.SortFunc(recent, func(x, y *library.Album) int {
		return cmp.Or(cmp.Compare(last[y], last[x]), cmp.Compare(x.SortKey, y.SortKey))
	})
	for al, n := range plays {
		if n > 0 {
			most = append(most, al)
		}
	}
	slices.SortFunc(most, func(x, y *library.Album) int {
		return cmp.Or(cmp.Compare(plays[y], plays[x]), cmp.Compare(x.SortKey, y.SortKey))
	})
	return recent[:min(len(recent), 24)], most[:min(len(most), 24)]
}

var albumSorts = []string{"Name", "Artist", "Year", "Recently Added"}

// albumsBy returns the library's albums in an order.
func (a *App) albumsBy(order string) []*library.Album {
	key := fmt.Sprint(a.libGen, order)
	s := &a.pages.albums
	if order == "Recently Added" {
		s = &a.pages.added // a list of its own, so that Home and Albums keep theirs
	}
	if s.key == key {
		return s.list
	}
	list := make([]*library.Album, len(a.lib.Albums))
	for i := range a.lib.Albums {
		list[i] = &a.lib.Albums[i]
	}
	byName := func(x, y *library.Album) int { return cmp.Compare(x.SortKey, y.SortKey) }
	switch order {
	case "Artist":
		slices.SortStableFunc(list, func(x, y *library.Album) int {
			return cmp.Or(cmp.Compare(x.ArtistKey, y.ArtistKey), cmp.Compare(x.Year, y.Year), byName(x, y))
		})
	case "Year":
		slices.SortStableFunc(list, func(x, y *library.Album) int {
			return cmp.Or(cmp.Compare(y.Year, x.Year), byName(x, y))
		})
	case "Recently Added":
		slices.SortStableFunc(list, func(x, y *library.Album) int {
			return cmp.Or(cmp.Compare(y.Added, x.Added), byName(x, y))
		})
	default:
		slices.SortStableFunc(list, byName)
	}
	s.key, s.list = key, list
	return list
}

// albumsPage shows every album.
func (a *App) albumsPage(c *ui.Context) {
	if len(a.lib.Albums) == 0 {
		a.loadingOr(c, "disc-3", "No albums", "This library has no albums yet.")
		return
	}
	if !slices.Contains(albumSorts, a.settings.AlbumSort) {
		a.settings.AlbumSort = albumSorts[0]
	}
	albums := a.albumsBy(a.settings.AlbumSort)
	cols := a.columns(c)
	sub := func(al *library.Album) string {
		if a.settings.AlbumSort == "Year" && al.Year > 0 {
			return fmt.Sprintf("%d · %s", al.Year, al.Artist)
		}
		return al.Artist
	}
	n := 1 + (len(albums)+cols-1)/cols
	ui.List(c.Key("albums"), &a.pages.albumList, n, func(i int) {
		if i == 0 {
			a.pageTitle(c, "Albums", count(len(albums), "album", "albums"), func() {
				if a.sortSelect(c, "album-sort", &a.settings.AlbumSort, albumSorts) {
					a.saveSettings()
				}
			})
			return
		}
		start := (i - 1) * cols
		tileRow(c, min(cols, len(albums)-start), cols, func(j int) {
			a.albumTile(c, albums[start+j], sub(albums[start+j]))
		})
	}).Grow(1).MinHeight(0).Padding(0, 0, 28)
}

// artistsPage shows every artist that albums are by.
func (a *App) artistsPage(c *ui.Context) {
	if len(a.lib.Artists) == 0 {
		a.loadingOr(c, "mic-vocal", "No artists", "This library has no artists yet.")
		return
	}
	s := &a.pages.artistList
	if key := fmt.Sprint(a.libGen); s.key != key {
		list := make([]*library.Artist, len(a.lib.Artists))
		for i := range a.lib.Artists {
			list[i] = &a.lib.Artists[i]
		}
		slices.SortStableFunc(list, func(x, y *library.Artist) int { return cmp.Compare(x.SortKey, y.SortKey) })
		s.key, s.list = key, list
	}
	artists := s.list
	cols := a.columns(c)
	n := 1 + (len(artists)+cols-1)/cols
	ui.List(c.Key("artists"), &a.pages.artistsList, n, func(i int) {
		if i == 0 {
			a.pageTitle(c, "Artists", count(len(artists), "artist", "artists"), nil)
			return
		}
		start := (i - 1) * cols
		tileRow(c, min(cols, len(artists)-start), cols, func(j int) {
			a.artistTile(c, artists[start+j])
		})
	}).Grow(1).MinHeight(0).Padding(0, 0, 28)
}

var songSorts = []string{"Title", "Artist", "Album", "Recently Added", "Most Played"}

// songsBy returns the library's songs in an order.
func (a *App) songsBy(order string) []*library.Song {
	key := fmt.Sprint(a.libGen, order)
	s := &a.pages.songs
	if order == "Title" {
		s = &a.pages.titles // a list of its own, which Favorites reads too
	}
	if s.key == key {
		return s.list
	}
	list := make([]*library.Song, len(a.lib.Songs))
	for i := range a.lib.Songs {
		list[i] = &a.lib.Songs[i]
	}
	byTitle := func(x, y *library.Song) int { return cmp.Compare(x.SortKey, y.SortKey) }
	inAlbum := func(x, y *library.Song) int {
		return cmp.Or(cmp.Compare(x.AlbumKey, y.AlbumKey), cmp.Compare(x.Disc, y.Disc), cmp.Compare(x.Track, y.Track))
	}
	switch order {
	case "Artist":
		slices.SortStableFunc(list, func(x, y *library.Song) int {
			return cmp.Or(cmp.Compare(x.ArtistKey, y.ArtistKey), inAlbum(x, y))
		})
	case "Album":
		slices.SortStableFunc(list, inAlbum)
	case "Recently Added":
		slices.SortStableFunc(list, func(x, y *library.Song) int {
			return cmp.Or(cmp.Compare(y.Added, x.Added), inAlbum(x, y))
		})
	case "Most Played":
		slices.SortStableFunc(list, func(x, y *library.Song) int {
			return cmp.Or(cmp.Compare(y.Plays, x.Plays), byTitle(x, y))
		})
	default:
		slices.SortStableFunc(list, byTitle)
	}
	s.key, s.list = key, list
	return list
}

// songsPage shows every song.
func (a *App) songsPage(c *ui.Context) {
	if len(a.lib.Songs) == 0 {
		if a.syncing {
			a.emptyState(c, "music", "Songs are on their way", "Aurelia is reading the library's songs. Albums and artists are ready meanwhile.")
		} else {
			a.emptyState(c, "music", "No songs", "This library has no songs yet.")
		}
		return
	}
	if !slices.Contains(songSorts, a.settings.SongSort) {
		a.settings.SongSort = songSorts[0]
	}
	songs := a.songsBy(a.settings.SongSort)
	ui.List(c.Key("songs"), &a.pages.songList, 2+len(songs), func(i int) {
		switch i {
		case 0:
			a.pageTitle(c, "Songs", count(len(songs), "song", "songs"), func() {
				if a.pillButton(c, "shuffle", "Shuffle", false).Clicked() {
					a.player.playShuffled(songs)
				}
				if a.sortSelect(c, "song-sort", &a.settings.SongSort, songSorts) {
					a.saveSettings()
				}
			})
		case 1:
			a.listHeader(c, "", true)
		default:
			a.songRow(c.Key(songs[i-2].ID), songRow{song: songs[i-2], showAlbum: true, play: func() { a.player.play(songs, i-2) }})
		}
	}).Grow(1).MinHeight(0).Padding(0, 0, 28)
}

// fetchAlbumSongs asks the server for the songs of an album the library
// does not hold yet, as during its first reading.
func (a *App) fetchAlbumSongs(id string, then func([]*library.Song)) {
	a.fetch("album:"+id, func(ctx context.Context) ([]*library.Song, error) {
		items, err := a.clientNow().AlbumSongs(ctx, id)
		if err != nil {
			return nil, err
		}
		songs := make([]*library.Song, len(items))
		for i := range items {
			s := library.SongOf(&items[i])
			library.PrepareSong(&s)
			songs[i] = &s
		}
		return songs, nil
	}, then)
}

// fetch asks the server for songs once, keeps them under key, and calls
// then with them.
func (a *App) fetch(key string, get func(ctx context.Context) ([]*library.Song, error), then func([]*library.Song)) {
	pg := &a.pages
	if pg.fetched == nil {
		pg.fetched, pg.fetching, pg.fetchErr = map[string][]*library.Song{}, map[string]bool{}, map[string]string{}
	}
	if songs, ok := pg.fetched[key]; ok {
		if then != nil {
			then(songs)
		}
		return
	}
	if pg.fetching[key] || a.clientNow() == nil {
		return
	}
	pg.fetching[key] = true
	delete(pg.fetchErr, key)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		songs, err := get(ctx)
		a.update(func() {
			delete(pg.fetching, key)
			if err != nil {
				pg.fetchErr[key] = friendly(err)
				return
			}
			pg.fetched[key] = songs
			a.favGen++ // the pages showing them build anew
			if then != nil {
				then(songs)
			}
		})
	}()
}

// hero is the head of the page of an album, an artist or a playlist: its
// picture, its name, a line about it, and its buttons.
type hero struct {
	kind, title    string
	id, tag, glyph string
	round          bool
	meta           func() // the line under the title
	play, shuffle  func()
	favorite       *bool
	favoriteID     string
	menu           func(m *ui.Menu)
}

func (a *App) hero(c *ui.Context, h hero) {
	p := a.pal
	ui.Row(c).Padding(10, pagePad, 22).Gap(26).AlignItems(ui.End).Children(func() {
		pic := a.art(c, h.id, h.tag, heroArt, h.glyph, nil).Size(heroArt, heroArt).Shadow(0, 10, 30, 0, p.shadow)
		if h.round {
			pic.Radius(heroArt / 2)
		} else {
			pic.Radius(p.radius + 2)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, strings.ToUpper(h.kind)).FontSize(11).FontWeight(600).LetterSpacing(1).TextColor(p.muted)
			size := float32(40)
			if len(h.title) > 34 {
				size = 28
			}
			ui.Text(c, h.title).FontSize(size).FontWeight(800).MaxLines(2).LineHeight(1.12)
			if h.meta != nil {
				ui.Row(c).Gap(6).Wrap().Children(h.meta)
			}
			ui.Row(c).Gap(10).Margin(10, 0, 0).Children(func() {
				if h.play != nil && a.pillButton(c, "play-fill", "Play", true).Clicked() {
					h.play()
				}
				if h.shuffle != nil && a.pillButton(c, "shuffle", "Shuffle", false).Clicked() {
					h.shuffle()
				}
				if h.favorite != nil {
					glyph, label := "heart", "Add to Favorites"
					if *h.favorite {
						glyph, label = "heart-fill", "Remove from Favorites"
					}
					if a.toggleIcon(c, glyph, label, *h.favorite, 36, 18).Clicked() {
						a.setFavorite(h.favoriteID, h.favorite, !*h.favorite)
					}
				}
				if h.menu != nil {
					a.iconButton(c, "ellipsis", "More", 36, 18).Menu(h.menu)
				}
			})
		})
	})
}

// metaText is a part of the line under a hero's title.
func (a *App) metaText(c *ui.Context, s string) {
	ui.Text(c, s).TextColor(a.pal.muted).SingleLine()
}

func (a *App) metaDot(c *ui.Context) { ui.Text(c, "·").TextColor(a.pal.faint) }

// metaLink is a name in that line that leads to a page.
func (a *App) metaLink(c *ui.Context, label, path string) {
	t := ui.Text(c, label).FontWeight(600).SingleLine().Cursor(ui.CursorPointer)
	if t.Hovered() {
		t.Underline()
	}
	if t.Clicked() {
		a.goTo(path)
	}
}

// albumPage shows an album and its songs.
func (a *App) albumPage(c *ui.Context, r *ui.Route) {
	id := r.Param("id")
	al := a.lib.Album(id)
	if al == nil {
		r.Title("Album")
		a.loadingOr(c, "disc-3", "Album not found", "It may have been removed from the library.")
		return
	}
	r.Title(al.Name)
	songs := a.lib.AlbumSongs(id)
	if len(songs) == 0 {
		a.fetchAlbumSongs(id, nil)
		songs = a.pages.fetched["album:"+id]
	}
	rows := a.rows(c, "/album/"+id, fmt.Sprint(len(songs), a.pages.fetchErr["album:"+id]), func(add func(func(c *ui.Context))) {
		artistID := a.artistOf(al.ArtistIDs)
		add(func(c *ui.Context) {
			a.hero(c, hero{
				kind: "Album", title: al.Name, id: al.ID, tag: al.ImageTag, glyph: "disc-3",
				meta: func() {
					if artistID != "" {
						a.metaLink(c, al.Artist, "/artist/"+artistID)
					} else if al.Artist != "" {
						ui.Text(c, al.Artist).FontWeight(600).SingleLine()
					}
					if al.Year > 0 {
						a.metaDot(c)
						a.metaText(c, fmt.Sprint(al.Year))
					}
					if len(songs) > 0 {
						var secs float64
						for _, s := range songs {
							secs += s.Seconds
						}
						a.metaDot(c)
						a.metaText(c, count(len(songs), "song", "songs")+", "+long(secs))
					}
				},
				play:     func() { a.player.play(songs, 0) },
				shuffle:  func() { a.player.playShuffled(songs) },
				favorite: &al.Favorite, favoriteID: al.ID,
				menu: func(m *ui.Menu) { a.albumMenu(m, al) },
			})
		})
		switch {
		case len(songs) > 0:
			add(func(c *ui.Context) { a.listHeader(c, "#", false) })
		case a.pages.fetchErr["album:"+id] != "":
			add(func(c *ui.Context) {
				a.emptyState(c, "circle-alert", "Could not read this album's songs", a.pages.fetchErr["album:"+id])
			})
		default:
			add(func(c *ui.Context) {
				ui.Text(c, "Reading the songs…").TextColor(a.pal.muted).Padding(20, pagePad)
			})
		}
		discs := 0
		for _, s := range songs {
			discs = max(discs, s.Disc)
		}
		// An album all by one artist names the artist once, above.
		sameArtist := true
		for _, s := range songs {
			if s.Artist != al.Artist {
				sameArtist = false
			}
		}
		lastDisc := -1
		for i, s := range songs {
			if discs > 1 && s.Disc != lastDisc {
				lastDisc = s.Disc
				add(func(c *ui.Context) {
					ui.Row(c).Gap(8).Padding(16, pagePad, 6).Children(func() {
						ui.Icon(c, icon("disc")).Size(14, 14).TextColor(a.pal.muted)
						ui.Textf(c, "Disc %d", s.Disc).FontWeight(600).TextColor(a.pal.muted)
					})
				})
			}
			number := s.Track
			if number == 0 {
				number = i + 1
			}
			add(func(c *ui.Context) {
				a.songRow(c.Key(s.ID), songRow{song: s, number: number, hideArtist: sameArtist, play: func() { a.player.play(songs, i) }})
			})
		}
		// More by the artist.
		if artistID != "" {
			var more []*library.Album
			for _, o := range a.lib.ArtistAlbums(artistID) {
				if o.ID != al.ID {
					more = append(more, o)
				}
			}
			cols := a.columns(c)
			if len(more) > 0 {
				a.section(add, "More by "+a.lib.Artist(artistID).Name, func(c *ui.Context) {
					if len(more) > cols && a.textButton(c, "Show all").Clicked() {
						a.goTo("/artist/" + artistID)
					}
				})
				more = more[:min(len(more), cols)]
				tileRows(add, len(more), cols, func(c *ui.Context, i int) {
					a.albumTile(c, more[i], yearOr(more[i]))
				})
			}
		}
	})
	a.list(c, rows)
}

// yearOr is the line under an album where its artist is known already.
func yearOr(al *library.Album) string {
	if al.Year > 0 {
		return fmt.Sprint(al.Year)
	}
	return al.Artist
}

// artistPage shows an artist: their most played songs, their albums, and
// the albums of others they are on.
func (a *App) artistPage(c *ui.Context, r *ui.Route) {
	id := r.Param("id")
	ar := a.lib.Artist(id)
	if ar == nil {
		r.Title("Artist")
		a.loadingOr(c, "mic-vocal", "Artist not found", "They may have been removed from the library.")
		return
	}
	r.Title(ar.Name)
	about := a.detail(id)
	rows := a.rows(c, "/artist/"+id, about, func(add func(func(c *ui.Context))) {
		albums := a.lib.ArtistAlbums(id)
		songs := a.artistSongs(id)
		cols := a.columns(c)
		add(func(c *ui.Context) {
			a.hero(c, hero{
				kind: "Artist", title: ar.Name, id: ar.ID, tag: ar.ImageTag, glyph: "mic-vocal", round: true,
				meta: func() {
					a.metaText(c, count(len(albums), "album", "albums"))
					a.metaDot(c)
					a.metaText(c, count(len(songs), "song", "songs"))
				},
				play:     func() { a.player.play(songs, 0) },
				shuffle:  func() { a.player.playShuffled(songs) },
				favorite: &ar.Favorite, favoriteID: ar.ID,
			})
		})
		// What was played most, where anything was.
		top := slices.Clone(songs)
		slices.SortStableFunc(top, func(x, y *library.Song) int { return cmp.Compare(y.Plays, x.Plays) })
		for i, s := range top {
			if s.Plays == 0 || i == 5 {
				top = top[:i]
				break
			}
		}
		if len(top) > 0 {
			a.section(add, "Most played", nil)
			for i, s := range top {
				add(func(c *ui.Context) {
					a.songRow(c.Key("top-"+s.ID), songRow{song: s, showAlbum: true, hideArtist: true, play: func() { a.player.play(top, i) }})
				})
			}
		}
		if len(albums) > 0 {
			a.section(add, "Albums", nil)
			tileRows(add, len(albums), cols, func(c *ui.Context, i int) { a.albumTile(c, albums[i], yearOr(albums[i])) })
		}
		if on := a.lib.AppearsOn(id); len(on) > 0 {
			a.section(add, "Appears on", nil)
			tileRows(add, len(on), cols, func(c *ui.Context, i int) { a.albumTile(c, on[i], "") })
		}
		if about != "" {
			a.section(add, "About", nil)
			add(func(c *ui.Context) {
				ui.Text(c, about).TextColor(a.pal.muted).LineHeight(1.5).MaxWidth(720).Padding(0, pagePad).Selectable()
			})
		}
	})
	a.list(c, rows)
}

// detail is what the server tells of an artist beyond the library.
type detail struct {
	overview string
	asked    bool
}

// detail returns the overview of an artist, "" until the server gave it.
func (a *App) detail(id string) string {
	d := a.details[id]
	if d == nil {
		d = &detail{}
		a.details[id] = d
	}
	if !d.asked && a.clientNow() != nil {
		d.asked = true
		cl := a.clientNow()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			it, err := cl.Item(ctx, id)
			if err != nil || it.Overview == "" {
				return
			}
			a.update(func() { d.overview = plain(it.Overview) })
		}()
	}
	return d.overview
}

// plain strips the tags of the HTML some overviews are written in.
func plain(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "</p>", "\n\n").Replace(s) {
		switch {
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// playlistsPage shows the user's playlists.
func (a *App) playlistsPage(c *ui.Context) {
	if len(a.lib.Playlists) == 0 {
		a.loadingOr(c, "list-music", "No playlists", "Playlists made in Jellyfin show here.")
		return
	}
	cols := a.columns(c)
	rows := a.rows(c, "/playlists", "", func(add func(func(c *ui.Context))) {
		add(func(c *ui.Context) {
			a.pageTitle(c, "Playlists", count(len(a.lib.Playlists), "playlist", "playlists"), nil)
		})
		tileRows(add, len(a.lib.Playlists), cols, func(c *ui.Context, i int) { a.playlistTile(c, &a.lib.Playlists[i]) })
	})
	a.list(c, rows)
}

// playlistPage shows a playlist and its songs, which the server lists
// when the page first shows.
func (a *App) playlistPage(c *ui.Context, r *ui.Route) {
	id := r.Param("id")
	pl := a.lib.Playlist(id)
	if pl == nil {
		r.Title("Playlist")
		a.loadingOr(c, "list-music", "Playlist not found", "It may have been removed.")
		return
	}
	r.Title(pl.Name)
	key := "playlist:" + id
	a.fetch(key, func(ctx context.Context) ([]*library.Song, error) {
		items, err := a.clientNow().PlaylistItems(ctx, id)
		if err != nil {
			return nil, err
		}
		var songs []*library.Song
		for i := range items {
			if items[i].Type != "Audio" {
				continue // playlists may hold videos too
			}
			s := library.SongOf(&items[i])
			library.PrepareSong(&s)
			songs = append(songs, &s)
		}
		return songs, nil
	}, nil)
	fetched, done := a.pages.fetched[key]
	// The library's own songs, where it has them: a favorite set here
	// shows everywhere.
	songs := make([]*library.Song, len(fetched))
	for i, s := range fetched {
		songs[i] = s
		if own := a.lib.Song(s.ID); own != nil {
			songs[i] = own
		}
	}
	rows := a.rows(c, "/playlist/"+id, fmt.Sprint(done, len(songs), a.pages.fetchErr[key]), func(add func(func(c *ui.Context))) {
		add(func(c *ui.Context) {
			h := hero{
				kind: "Playlist", title: pl.Name, id: pl.ID, tag: pl.ImageTag, glyph: "list-music",
				meta: func() {
					if done {
						var secs float64
						for _, s := range songs {
							secs += s.Seconds
						}
						a.metaText(c, count(len(songs), "song", "songs")+", "+long(secs))
					} else {
						a.metaText(c, count(pl.Songs, "song", "songs"))
					}
				},
			}
			if len(songs) > 0 {
				h.play = func() { a.player.play(songs, 0) }
				h.shuffle = func() { a.player.playShuffled(songs) }
			}
			a.hero(c, h)
		})
		switch {
		case a.pages.fetchErr[key] != "":
			add(func(c *ui.Context) {
				a.emptyState(c, "circle-alert", "Could not read this playlist", a.pages.fetchErr[key])
			})
		case !done:
			add(func(c *ui.Context) {
				ui.Text(c, "Reading the songs…").TextColor(a.pal.muted).Padding(20, pagePad)
			})
		case len(songs) == 0:
			add(func(c *ui.Context) {
				a.emptyState(c, "list-music", "This playlist is empty", "Add songs to it in Jellyfin.")
			})
		default:
			add(func(c *ui.Context) { a.listHeader(c, "", true) })
			for i, s := range songs {
				add(func(c *ui.Context) {
					a.songRow(c.Key(fmt.Sprint(i, s.ID)), songRow{song: s, showAlbum: true, play: func() { a.player.play(songs, i) }})
				})
			}
		}
	})
	a.list(c, rows)
}

// favoritesPage shows what the user marked: artists, albums and songs.
func (a *App) favoritesPage(c *ui.Context) {
	var artists []*library.Artist
	var albums []*library.Album
	var songs []*library.Song
	rows := a.rows(c, "/favorites", "", func(add func(func(c *ui.Context))) {
		for i := range a.lib.Artists {
			if a.lib.Artists[i].Favorite {
				artists = append(artists, &a.lib.Artists[i])
			}
		}
		for i := range a.lib.Albums {
			if a.lib.Albums[i].Favorite {
				albums = append(albums, &a.lib.Albums[i])
			}
		}
		for _, s := range a.songsBy("Title") {
			if s.Favorite {
				songs = append(songs, s)
			}
		}
		if len(artists)+len(albums)+len(songs) == 0 {
			return
		}
		cols := a.columns(c)
		add(func(c *ui.Context) {
			a.pageTitle(c, "Favorites", "", func() {
				if len(songs) > 0 && a.pillButton(c, "shuffle", "Shuffle songs", false).Clicked() {
					a.player.playShuffled(songs)
				}
			})
		})
		if len(artists) > 0 {
			a.section(add, "Artists", nil)
			tileRows(add, len(artists), cols, func(c *ui.Context, i int) { a.artistTile(c, artists[i]) })
		}
		if len(albums) > 0 {
			a.section(add, "Albums", nil)
			tileRows(add, len(albums), cols, func(c *ui.Context, i int) { a.albumTile(c, albums[i], "") })
		}
		if len(songs) > 0 {
			a.section(add, "Songs", nil)
			add(func(c *ui.Context) { a.listHeader(c, "", true) })
			for i, s := range songs {
				add(func(c *ui.Context) {
					a.songRow(c.Key(s.ID), songRow{song: s, showAlbum: true, play: func() { a.player.play(songs, i) }})
				})
			}
		}
	})
	if len(rows) == 0 {
		a.loadingOr(c, "heart", "No favorites yet", "The heart on a song, an album or an artist keeps it here.")
		return
	}
	a.list(c, rows)
}

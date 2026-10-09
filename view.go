package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// The sizes of the window's parts, in points.
const (
	sidebarW  = 228
	queueW    = 320
	topBarH   = 52
	playerH   = 78
	pagePad   = 28
	tilePad   = 8
	tileMin   = 148 // the least width of an album in a grid
	thumbArt  = 48  // pictures in rows
	tileArt   = 160 // pictures in grids
	heroArt   = 208 // the picture of the page
	bigArt    = 300 // the picture of the song playing, by its lyrics
	rowHeight = 52
)

// ownControls tells that the title bar draws the window's controls, as
// on Windows, whose window has no frame.
var ownControls = runtime.GOOS == "windows"

// frame prepares a frame: the theme, the toasts, and what follows the
// route.
func (a *App) frame(c *ui.Context) {
	a.images.tick()
	if a.editing != nil && !strings.HasPrefix(a.router.Path(), "/theme/") {
		a.editing = nil // left without saving
	}
	t := a.theme()
	key := fmt.Sprintf("%p %d", t, a.themeGen)
	if a.pal == nil || a.palFor != key {
		a.pal, a.palFor = newPalette(t), key
	}
	c.SetTheme(a.pal.widgets)
	root := c.Root().Background(a.pal.bg).TextColor(a.pal.text)
	// Theme files dropped on the window are imported.
	if files := root.DroppedFiles(); len(files) > 0 {
		a.importThemeFiles(files)
	}
	for _, t := range toasts {
		c.AddToast(t)
	}
	toasts = toasts[:0]
	a.search.follow(a)
	if a.downloads.busy() {
		c.After(300 * time.Millisecond) // the rings of what downloads fill
	}
}

// view builds the window.
func (a *App) view(c *ui.Context) {
	a.frame(c)
	if !a.signedIn() {
		a.loginPage(c)
		a.toastLayer(c, 16)
		return
	}
	a.cursor.begin(a.router.Location())
	root := ui.Column(c).Fill()
	a.shortcuts(c, root)
	if a.cursor.wake {
		a.cursor.wake = false
		c.After(time.Millisecond) // the rows scrolled to are built in the next frame
	}
	a.remember()
	a.tellSystem()
	root.Children(func() {
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.sidebar(c)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				a.topBar(c)
				a.router.View(c, func(r *ui.Route) { a.route(c, r) })
			})
			if a.settings.QueueOpen {
				a.queuePanel(c)
			}
		})
		a.playerBar(c)
	})
	a.toastLayer(c, playerH+14)
}

// sections are the parts of the library the sidebar leads to: the page
// each opens at, and what the pages inside it begin with.
var sections = []struct{ root, under string }{
	{"/home", ""}, {"/albums", "/album/"}, {"/artists", "/artist/"}, {"/songs", ""},
	{"/favorites", ""}, {"/playlists", "/playlist/"}, {"/downloads", ""},
}

// sectionOf returns the section a page is in, "" for the pages of none.
func sectionOf(path string) string {
	for _, s := range sections {
		if path == s.root || s.under != "" && strings.HasPrefix(path, s.under) {
			return s.root
		}
	}
	return ""
}

// remember keeps the page each section shows, for the sidebar to come
// back to.
func (a *App) remember() {
	if sec := sectionOf(a.router.Path()); sec != "" {
		if a.lastIn == nil {
			a.lastIn = map[string]string{}
		}
		a.lastIn[sec] = a.router.Location()
	}
}

// openSection shows a section where it was left; from inside it, its
// first page.
func (a *App) openSection(root string) {
	if sectionOf(a.router.Path()) == root {
		a.goTo(root)
		return
	}
	if at := a.lastIn[root]; at != "" {
		a.goTo(at)
		return
	}
	a.goTo(root)
}

// toastLayer shows the app's messages in its own look, at the right
// above bottom, clear of the player's buttons.
func (a *App) toastLayer(c *ui.Context, bottom float32) {
	p := a.pal
	ui.ToastViewportBase(c, func(viewport ui.Element, shown []ui.Toast) {
		viewport.Padding(16, 18, bottom).AlignItems(ui.End).Gap(8)
		for _, t := range shown {
			toast := ui.ToastBase(c.Key(t.ID), t)
			toast.Root.Row().AlignItems(ui.Start).Gap(10).Padding(11, 12, 11, 14).MaxWidth(380).Radius(p.radius+2).
				Background(p.surface).Border(1, p.border).Shadow(0, 8, 28, 0, p.shadow).
				Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 10}, Exit: &ui.Motion{}, Duration: 140 * time.Millisecond})
			toast.Root.Children(func() {
				if t.Type == "error" {
					ui.Icon(c, icon("circle-alert")).Size(16, 16).TextColor(p.danger).Margin(1, 0, 0)
				} else {
					ui.Icon(c, icon("check")).Size(16, 16).TextColor(p.accent).Margin(1, 0, 0)
				}
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, t.Title).FontWeight(600).TextColor(p.text)
					if t.Description != "" {
						ui.Text(c, t.Description).FontSize(12).TextColor(p.muted).MaxLines(4)
					}
				})
				if t.Action != "" {
					act := toast.ActionButton().Height(26).Padding(0, 12).Radius(13).Background(p.accent).Shrink(0)
					if act.Hovered() {
						act.Background(p.accentHover)
					}
					act.Children(func() { ui.Text(c, t.Action).FontSize(12).FontWeight(600).TextColor(p.onAcc) })
				}
				cl := toast.CloseButton().Size(22, 22).Radius(11).Label("Dismiss")
				if cl.Hovered() {
					cl.Background(p.hover)
				}
				cl.Children(func() { ui.Icon(c, icon("x")).Size(13, 13).TextColor(p.muted) })
			})
		}
	})
}

// route builds the page of the route.
func (a *App) route(c *ui.Context, r *ui.Route) {
	switch {
	case r.Match("/home"):
		r.Title("Home")
		a.homePage(c)
	case r.Match("/albums"):
		r.Title("Albums")
		a.albumsPage(c)
	case r.Match("/album/{id}"):
		a.albumPage(c, r)
	case r.Match("/artists"):
		r.Title("Artists")
		a.artistsPage(c)
	case r.Match("/artist/{id}"):
		a.artistPage(c, r)
	case r.Match("/songs"):
		r.Title("Songs")
		a.songsPage(c)
	case r.Match("/playlists"):
		r.Title("Playlists")
		a.playlistsPage(c)
	case r.Match("/playlist/{id}"):
		a.playlistPage(c, r)
	case r.Match("/favorites"):
		r.Title("Favorites")
		a.favoritesPage(c)
	case r.Match("/downloads"):
		r.Title("Downloads")
		a.downloadsPage(c)
	case r.Match("/search"):
		r.Title("Search")
		a.searchPage(c)
	case r.Match("/lyrics"):
		r.Title("Lyrics")
		a.lyricsPage(c)
	case r.Match("/settings"):
		r.Title("Settings")
		a.settingsPage(c)
	case r.Match("/shortcuts"):
		r.Title("Keyboard shortcuts")
		a.shortcutsPage(c)
	case r.Match("/theme/{id}"):
		r.Title("Edit theme")
		a.themeEditorPage(c, r.Param("id"))
	default:
		a.emptyState(c, "circle-alert", "Nothing here", "This page does not exist.")
	}
}

// goTo shows a page, unless it shows.
func (a *App) goTo(path string) {
	if a.router.Location() != path {
		a.router.Push(path)
	}
}

func (a *App) setVolume(v float64) {
	a.settings.Volume = max(0, min(1, v))
	a.settings.Muted = false
	a.player.applyVolume()
	a.saveSettings()
}

// contentWidth is the width pages have, for grids to count their columns.
func (a *App) contentWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	w -= sidebarW
	if a.settings.QueueOpen {
		w -= queueW
	}
	return max(w, 320)
}

// columns is how many tiles fit a row of a page.
func (a *App) columns(c *ui.Context) int {
	w := a.contentWidth(c) - 2*(pagePad-tilePad)
	return max(2, int(w/tileMin))
}

// pixels returns the size to ask the server for a picture shown at dip
// points: one of a few sizes, so that pictures are shared between pages.
func (a *App) pixels(dip float32) int {
	need := float64(dip) * a.scale
	for _, px := range []int{48, 96, 160, 320, 512, 768} {
		if float64(px) >= need*0.9 {
			return px
		}
	}
	return 1024
}

func imageKey(id, tag string, px int) string {
	return fmt.Sprintf("%s-%s-%d", id, tag, px)
}

// picture returns the picture of an item, or nil while it loads or when
// it has none.
func (a *App) picture(id, tag string, dip float32) *ui.Bitmap {
	if a.pictures != nil {
		return a.pictures(id)
	}
	if id == "" || tag == "" || a.client == nil {
		return nil
	}
	px := a.pixels(dip)
	return a.images.get(imageKey(id, tag, px), a.client.ImageURL(id, "Primary", tag, px))
}

// art builds the picture of an item in a box the caller sizes: the
// picture once it is there, an icon until then and for items without
// one. over builds what lies over it.
func (a *App) art(c *ui.Context, id, tag string, dip float32, glyph string, over func()) ui.Element {
	p := a.pal
	box := ui.Box(c).Background(p.artwork).Clip().Shrink(0)
	bmp := a.picture(id, tag, dip)
	box.Children(func() {
		if bmp != nil {
			ui.Image(c, bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover)
		} else {
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Children(func() {
				ui.Icon(c, icon(glyph)).FontSize(max(16, min(dip*0.3, 56))).TextColor(p.faint)
			})
		}
		if over != nil {
			over()
		}
	})
	return box
}

// sidebar builds the navigation: the history buttons at its top, by the
// window's controls, the parts of the library, and the playlists.
func (a *App) sidebar(c *ui.Context) {
	p := a.pal
	bar := c.TitleBar()
	ui.Column(c).Width(sidebarW).Shrink(0).Background(p.sidebar).BorderWidth(0, 1, 0, 0).BorderColor(p.border).Children(func() {
		ui.Row(c).Height(topBarH).Shrink(0).Padding(0, 12, 0, max(bar.Left+4, 12)).Gap(2).DragWindow().Children(func() {
			if bar.Left == 0 {
				ui.Icon(c, icon("logo")).Size(20, 20).TextColor(p.accent)
				ui.Text(c, "Aurelia").FontWeight(700).FontSize(15).Margin(0, 0, 0, 8)
			}
			ui.Spacer(c)
			a.historyButton(c, false)
			a.historyButton(c, true)
		})
		ui.Column(c).Padding(4, 10, 8).Gap(1).Shrink(0).Children(func() {
			a.navItem(c, "house", "Home", "/home", "")
			a.navItem(c, "disc-3", "Albums", "/albums", "/album/")
			a.navItem(c, "mic-vocal", "Artists", "/artists", "/artist/")
			a.navItem(c, "music", "Songs", "/songs", "")
			a.navItem(c, "heart", "Favorites", "/favorites", "")
			a.navItem(c, "list-music", "Playlists", "/playlists", "/playlist/")
			a.navItem(c, "circle-arrow-down", "Downloads", "/downloads", "")
		})
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(4, 10, 10).Gap(1).Children(func() {
			playlists := a.playlists()
			if len(playlists) > 0 {
				ui.Text(c, "PLAYLISTS").FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(p.faint).Padding(10, 10, 6)
			}
			for _, pl := range playlists {
				a.navItem(c, "", pl.Name, "/playlist/"+pl.ID, "")
			}
		})
		ui.Column(c).Padding(8, 10, 10).Gap(1).Shrink(0).BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
			a.syncStatus(c)
			a.navItem(c, "settings", "Settings", "/settings", "/theme/")
		})
	})
}

// historyButton goes back or forward through the pages shown; a right
// click lists them.
func (a *App) historyButton(c *ui.Context, forward bool) {
	p := a.pal
	var b ui.Element
	if forward {
		b = ui.ForwardButton(c, a.router)
	} else {
		b = ui.BackButton(c, a.router)
	}
	b.Size(30, 30).Padding(0).Radius(15).Background(ui.Transparent).Border(0, ui.Transparent).FontSize(17)
	switch {
	case b.IsDisabled():
		b.TextColor(p.faint)
	case b.Pressed():
		b.Background(p.active).TextColor(p.text)
	case b.Hovered():
		b.Background(p.hover).TextColor(p.text)
	default:
		b.TextColor(p.muted)
	}
}

// navItem is a row of the sidebar that shows a page.
func (a *App) navItem(c *ui.Context, glyph, label, path, under string) {
	p := a.pal
	at := a.router.Path()
	active := at == path || (under != "" && strings.HasPrefix(at, under))
	b := ui.ButtonBase(c.Key(path)).Height(32).Padding(0, 10).Gap(10).Radius(p.radius).Justify(ui.Start).Shrink(0).Label(label)
	fg := p.muted
	switch {
	case active:
		b.Background(p.surface)
		fg = p.text
	case b.Hovered():
		b.Background(p.hover.Alpha(0.6))
		fg = p.text
	}
	b.Children(func() {
		if glyph != "" {
			ig := fg
			if active {
				ig = p.accent
			}
			ui.Icon(c, icon(glyph)).Size(17, 17).TextColor(ig)
		}
		ui.Text(c, label).TextColor(fg).FontWeight(500).SingleLine().Grow(1).MinWidth(0)
	})
	if b.Clicked() {
		if sectionOf(path) == path {
			a.openSection(path) // where it was left; clicked again, its first page
		} else {
			a.goTo(path)
		}
	}
}

// syncStatus tells of the library being read, or of why it could not be.
func (a *App) syncStatus(c *ui.Context) {
	p := a.pal
	switch {
	case a.syncing:
		ui.Row(c).Height(30).Padding(0, 10).Gap(10).Children(func() {
			spin := ui.Icon(c, icon("loader-circle")).Size(15, 15).TextColor(p.muted)
			spin.Rotate(spin.Loop("spin", time.Second, ui.Linear) * 360)
			label := "Updating library…"
			if a.progress.Total > 0 {
				label = fmt.Sprintf("Updating library… %d%%", 100*a.progress.Done/a.progress.Total)
			}
			ui.Text(c, label).FontSize(12).TextColor(p.muted).SingleLine()
		})
	case a.syncErr != "":
		b := ui.ButtonBase(c).Height(30).Padding(0, 10).Gap(10).Radius(p.radius).Justify(ui.Start).Tooltip(a.syncErr + " Click to try again.")
		if b.Hovered() {
			b.Background(p.hover.Alpha(0.6))
		}
		b.Children(func() {
			ui.Icon(c, icon("circle-alert")).Size(15, 15).TextColor(p.warning)
			ui.Text(c, "Library not updated").FontSize(12).TextColor(p.muted).SingleLine()
		})
		if b.Clicked() {
			a.sync()
		}
	}
}

// topBar holds the search field, over the pages.
func (a *App) topBar(c *ui.Context) {
	p := a.pal
	ui.Row(c).Height(topBarH).Shrink(0).Padding(0, 16, 0, pagePad).Gap(8).DragWindow().Children(func() {
		a.search.field(a, c)
		ui.Spacer(c)
		if a.offline {
			// The server does not answer: what is downloaded plays.
			b := ui.ButtonBase(c).Height(28).Padding(0, 12, 0, 10).Gap(7).Radius(14).Background(p.surface).Shrink(0).
				Tooltip("The server is not answering. Downloaded songs still play. Click to try again.")
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Children(func() {
				ui.Icon(c, icon("wifi-off")).Size(14, 14).TextColor(p.warning)
				ui.Text(c, "Offline").FontSize(12).FontWeight(600).TextColor(p.muted)
			})
			if b.Clicked() {
				a.sync()
			}
		}
		a.windowControls(c)
	})
	_ = p
}

// windowControls draws the buttons that minimize, maximize and close the
// window where it has none of its own.
func (a *App) windowControls(c *ui.Context) {
	win := a.win
	if !ownControls || win == nil || win.IsFullScreen() {
		return
	}
	p := a.pal
	if a.iconButton(c, "minus", "Minimize", 30, 15).Clicked() {
		win.Minimize()
	}
	glyph, label := "square", "Maximize"
	if win.IsMaximized() {
		glyph, label = "copy", "Restore"
	}
	if a.iconButton(c, glyph, label, 30, 13).Clicked() {
		win.ToggleMaximize()
	}
	b := ui.ButtonBase(c).Size(30, 30).Radius(15).Label("Close").Tooltip("Close")
	fg := p.muted
	if b.Hovered() {
		b.Background(ui.Hex("#c42b1c"))
		fg = ui.Hex("#ffffff")
	}
	b.Children(func() { ui.Icon(c, icon("x")).Size(16, 16).TextColor(fg) })
	if b.Clicked() {
		win.Close()
	}
}

// emptyState fills a page that has nothing to show.
func (a *App) emptyState(c *ui.Context, glyph, title, detail string) {
	p := a.pal
	ui.Column(c).Grow(1).Center().Gap(10).Padding(40).Children(func() {
		ui.Icon(c, icon(glyph)).Size(40, 40).TextColor(p.faint)
		ui.Text(c, title).FontSize(17).FontWeight(600).Margin(8, 0, 0)
		if detail != "" {
			ui.Text(c, detail).TextColor(p.muted).TextAlign(ui.Center).MaxWidth(420)
		}
	})
}

// loading fills a page whose library is still coming.
func (a *App) loadingOr(c *ui.Context, glyph, title, detail string) {
	if a.syncing && a.lib.IsEmpty() {
		p := a.pal
		ui.Column(c).Grow(1).Center().Gap(14).Children(func() {
			spin := ui.Icon(c, icon("loader-circle")).Size(28, 28).TextColor(p.muted)
			spin.Rotate(spin.Loop("spin", time.Second, ui.Linear) * 360)
			ui.Text(c, "Reading your library…").TextColor(p.muted)
		})
		return
	}
	a.emptyState(c, glyph, title, detail)
}

// artistLink is the ID of the artist a song or an album leads to: one the
// library has a page for.
func (a *App) artistOf(ids ...[]string) string {
	for _, list := range ids {
		for _, id := range list {
			if a.lib.Artist(id) != nil {
				return id
			}
		}
	}
	return ""
}

// songMenu is the menu of a song, wherever it shows.
func (a *App) songMenu(m *ui.Menu, s *library.Song) {
	if m.Item("Play Next").Chosen() {
		a.player.playNext([]*library.Song{s})
		a.toast("Playing next")
	}
	if m.Item("Add to Queue").Chosen() {
		a.player.enqueue([]*library.Song{s})
		a.toast("Added to the queue")
	}
	m.Separator()
	if s.AlbumID != "" && m.Item("Go to Album").Chosen() {
		a.goTo("/album/" + s.AlbumID)
	}
	if id := a.artistOf(s.ArtistIDs, s.AlbumArtistIDs); id != "" && m.Item("Go to Artist").Chosen() {
		a.goTo("/artist/" + id)
	}
	m.Separator()
	label := "Add to Favorites"
	if s.Favorite {
		label = "Remove from Favorites"
	}
	if m.Item(label).Chosen() {
		a.setFavorite(s.ID, &s.Favorite, !s.Favorite)
	}
	if st, _ := a.downloads.state(s.ID); st == dlNone {
		if m.Item("Download").Chosen() {
			a.downloads.add(s)
		}
	} else if m.Item("Remove Download").Disabled(a.downloads.held(s.ID)).Chosen() {
		a.downloads.remove(s)
	}
}

// albumMenu is the menu of an album.
func (a *App) albumMenu(m *ui.Menu, al *library.Album) {
	songs := a.lib.AlbumSongs(al.ID)
	if m.Item("Play").Disabled(len(songs) == 0).Chosen() {
		a.player.play(songs, 0)
	}
	if m.Item("Shuffle").Disabled(len(songs) == 0).Chosen() {
		a.player.playShuffled(songs)
	}
	if m.Item("Play Next").Disabled(len(songs) == 0).Chosen() {
		a.player.playNext(songs)
		a.toast("Playing next")
	}
	if m.Item("Add to Queue").Disabled(len(songs) == 0).Chosen() {
		a.player.enqueue(songs)
		a.toast("Added to the queue")
	}
	m.Separator()
	if id := a.artistOf(al.ArtistIDs); id != "" && m.Item("Go to Artist").Chosen() {
		a.goTo("/artist/" + id)
	}
	label := "Add to Favorites"
	if al.Favorite {
		label = "Remove from Favorites"
	}
	if m.Item(label).Chosen() {
		a.setFavorite(al.ID, &al.Favorite, !al.Favorite)
	}
	if st, _ := a.downloads.group(songs); st == dlNone {
		if m.Item("Download").Disabled(len(songs) == 0).Chosen() {
			a.downloads.addAlbum(al, songs)
		}
	} else if m.Item("Remove Download").Chosen() {
		a.downloads.removeAlbum(al)
	}
}

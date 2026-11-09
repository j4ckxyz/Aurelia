package main

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
)

// Playlists made and changed here, on the server: new ones, songs added
// and taken out, a new name, and the queue kept as one.

// promptState is a question the user answers before something is done: a
// name to type, or a yes to a thing that cannot be undone.
type promptState struct {
	open        bool
	title, text string // what is asked, and a line under it
	field       bool   // a name is typed
	value       string
	confirm     string // the button's label
	danger      bool
	run         func(value string)
}

// ask shows a question.
func (a *App) ask(p promptState) {
	p.open = true
	a.prompt = p
}

// promptDialog is the dialog of the question.
func (a *App) promptDialog(c *ui.Context) {
	pr := &a.prompt
	if !pr.open {
		return
	}
	p := a.pal
	open := pr.open
	ui.Modal(c, &open, func() {
		ui.Column(c).Gap(14).Padding(22).Width(400).Children(func() {
			ui.Text(c, pr.title).FontSize(18).FontWeight(700).MaxLines(2)
			if pr.text != "" {
				ui.Text(c, pr.text).TextColor(p.muted).MaxLines(4)
			}
			submit := false
			if pr.field {
				in := ui.TextInput(c.Key("prompt"), &pr.value).Label(pr.title).Placeholder("Name").Height(36)
				if in.Submitted() {
					submit = true
				}
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if a.textButton(c, "Cancel").Clicked() {
					open = false
				}
				b := a.pillButton(c, "", pr.confirm, !pr.danger).Disabled(pr.field && strings.TrimSpace(pr.value) == "")
				if pr.danger {
					b.Background(p.danger)
				}
				if b.Clicked() {
					submit = true
				}
			})
			if submit && (!pr.field || strings.TrimSpace(pr.value) != "") {
				run, value := pr.run, strings.TrimSpace(pr.value)
				open = false
				if run != nil {
					run(value)
				}
			}
		})
	})
	if !open {
		*pr = promptState{}
	}
}

// ownPlaylists are the playlists the user may change: their own.
func (a *App) ownPlaylists() []*library.Playlist {
	var own []*library.Playlist
	for i := range a.lib.Playlists {
		if !a.lib.Playlists[i].Others {
			own = append(own, &a.lib.Playlists[i])
		}
	}
	return own
}

// playlistDo runs a change of the server in the background, and then, on
// the interface's goroutine, what follows it.
func (a *App) playlistDo(what string, call func(ctx context.Context, c *jellyfin.Client) error, then func()) {
	cl := a.clientNow()
	if cl == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := call(ctx, cl)
		a.update(func() {
			if err != nil {
				a.toastError(what, err)
				return
			}
			if then != nil {
				then()
			}
		})
	}()
}

// refreshPlaylists reads the user's playlists again, and shows them.
func (a *App) refreshPlaylists(then func()) { a.readPlaylists("", then) }

// readPlaylists reads the user's playlists again, and shows them. made is
// one the user just made here, which is theirs without asking.
func (a *App) readPlaylists(made string, then func()) {
	cl := a.clientNow()
	if cl == nil {
		return
	}
	// Whose a playlist is does not change: what was found stays, and the
	// server is asked of new playlists, and of those it did not tell of.
	known := map[string]library.Owner{}
	for i := range a.lib.Playlists {
		if o := a.lib.Playlists[i].Owner(); o != library.OwnerUnknown {
			known[a.lib.Playlists[i].ID] = o
		}
	}
	if made != "" {
		known[made] = library.OwnerSelf
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := cl.Playlists(ctx)
		if err == nil {
			var ask []string
			for _, it := range items {
				if _, ok := known[it.ID]; !ok {
					ask = append(ask, it.ID)
				}
			}
			for id, o := range library.PlaylistOwners(ctx, cl, ask) {
				known[id] = o
			}
		}
		a.update(func() {
			if err != nil {
				a.toastError("Could not read the playlists", err)
				return
			}
			var list []library.Playlist
			for i := range items {
				list = append(list, library.PlaylistOf(&items[i], known[items[i].ID]))
			}
			a.lib.SetPlaylists(list)
			a.libGen++
			// What was kept of playlists read before is out of date.
			for k := range a.pages.fetched {
				if strings.HasPrefix(k, "playlist:") {
					delete(a.pages.fetched, k)
				}
			}
			if then != nil {
				then()
			}
		})
	}()
}

func songIDs(songs []*library.Song) []string {
	ids := make([]string, len(songs))
	for i, s := range songs {
		ids[i] = s.ID
	}
	return ids
}

// addToPlaylist adds songs to a playlist. Of songs in it already, the
// user is asked when they are all there are, and otherwise they are left
// out.
func (a *App) addToPlaylist(pl *library.Playlist, songs []*library.Song) {
	cl := a.clientNow()
	if len(songs) == 0 || cl == nil {
		return
	}
	id := pl.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// When the playlist cannot be read, the songs are added as asked:
		// the server tells if it refuses them.
		items, err := cl.PlaylistItems(ctx, id)
		a.update(func() {
			in := map[string]bool{}
			if err == nil {
				for _, it := range items {
					in[it.ID] = true
				}
			}
			fresh, already := splitAdded(songs, in)
			switch {
			case len(already) == 0:
				a.addSongsToPlaylist(pl, songs, 0)
			case len(fresh) == 0:
				text := "“" + songs[0].Name + "” is in " + pl.Name + " already. Add it again?"
				if len(songs) > 1 {
					text = "All " + count(len(songs), "song", "songs") + " are in " + pl.Name + " already. Add them again?"
				}
				a.ask(promptState{
					title: "Already added — add anyway?", text: text, confirm: "Add Anyway",
					run: func(string) { a.addSongsToPlaylist(pl, songs, 0) },
				})
			default:
				a.addSongsToPlaylist(pl, fresh, len(already))
			}
		})
	}()
}

// splitAdded parts songs into those not in a playlist and those in it, by
// the IDs of its songs.
func splitAdded(songs []*library.Song, in map[string]bool) (fresh, already []*library.Song) {
	for _, s := range songs {
		if in[s.ID] {
			already = append(already, s)
		} else {
			fresh = append(fresh, s)
		}
	}
	return fresh, already
}

// addSongsToPlaylist adds songs to a playlist as they are. skipped is how
// many were left out, being in it already, for the toast to tell.
func (a *App) addSongsToPlaylist(pl *library.Playlist, songs []*library.Song, skipped int) {
	id, name, n := pl.ID, pl.Name, len(songs)
	a.playlistDo("Could not add to "+name, func(ctx context.Context, c *jellyfin.Client) error {
		return c.AddToPlaylist(ctx, id, songIDs(songs))
	}, func() {
		msg := "Added " + count(n, "song", "songs") + " to " + name
		if skipped > 0 {
			msg += ", leaving out " + count(skipped, "song", "songs") + " in it already"
		}
		a.toast(msg)
		a.refreshPlaylists(nil)
	})
}

// newPlaylist asks for a name, and makes a playlist of songs, which may be
// none.
func (a *App) newPlaylist(songs []*library.Song, suggest string) {
	a.ask(promptState{
		title: "New playlist", field: true, value: suggest, confirm: "Create",
		run: func(name string) {
			var made string
			a.playlistDo("Could not make the playlist", func(ctx context.Context, c *jellyfin.Client) (err error) {
				made, err = c.CreatePlaylist(ctx, name, songIDs(songs))
				return err
			}, func() {
				a.toast("Made the playlist " + name)
				a.readPlaylists(made, nil)
			})
		},
	})
}

// renamePlaylist asks for another name for a playlist.
func (a *App) renamePlaylist(pl *library.Playlist) {
	id := pl.ID
	a.ask(promptState{
		title: "Rename the playlist", field: true, value: pl.Name, confirm: "Rename",
		run: func(name string) {
			a.playlistDo("Could not rename the playlist", func(ctx context.Context, c *jellyfin.Client) error {
				return c.RenamePlaylist(ctx, id, name)
			}, func() {
				a.refreshPlaylists(nil)
			})
		},
	})
}

// deletePlaylist asks, and then removes a playlist. The songs stay.
func (a *App) deletePlaylist(pl *library.Playlist) {
	id, name := pl.ID, pl.Name
	a.ask(promptState{
		title: "Delete “" + name + "”?", text: "The playlist is removed from the server, for every device. The songs in it stay in the library.",
		confirm: "Delete", danger: true,
		run: func(string) {
			a.playlistDo("Could not delete the playlist", func(ctx context.Context, c *jellyfin.Client) error {
				return c.DeletePlaylist(ctx, id)
			}, func() {
				a.toast("Deleted " + name)
				if a.router.Path() == "/playlist/"+id {
					a.router.Back()
				}
				a.refreshPlaylists(nil)
			})
		},
	})
}

// removeFromPlaylist takes a song out of a playlist, from its place at,
// and offers to put it back.
func (a *App) removeFromPlaylist(pl *library.Playlist, s *library.Song, at int) {
	if s.Entry == "" {
		return
	}
	id, entry, name, song, title := pl.ID, s.Entry, pl.Name, s.ID, s.Name
	a.playlistDo("Could not take the song out of "+name, func(ctx context.Context, c *jellyfin.Client) error {
		return c.RemoveFromPlaylist(ctx, id, []string{entry})
	}, func() {
		toasts = append(toasts, ui.Toast{
			Title: "Removed “" + title + "” from " + name, Action: "Undo", Timeout: 8 * time.Second,
			OnAction: func() { a.putBackInPlaylist(id, name, song, at) },
		})
		a.refreshPlaylists(nil)
	})
}

// putBackInPlaylist adds a song taken out of a playlist again, and moves
// it to where it was, as the server adds at the end.
func (a *App) putBackInPlaylist(id, name, song string, at int) {
	a.playlistDo("Could not put the song back in "+name, func(ctx context.Context, c *jellyfin.Client) error {
		if err := c.AddToPlaylist(ctx, id, []string{song}); err != nil {
			return err
		}
		items, err := c.PlaylistItems(ctx, id)
		if err != nil {
			return err
		}
		// The song put back is the last of its ID.
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].ID == song {
				if i != at && items[i].PlaylistItemID != "" {
					return c.MoveInPlaylist(ctx, id, items[i].PlaylistItemID, at)
				}
				break
			}
		}
		return nil
	}, func() {
		a.refreshPlaylists(nil)
	})
}

// moveInPlaylist moves a song of a playlist to another place in it.
func (a *App) moveInPlaylist(pl *library.Playlist, s *library.Song, to int) {
	if s.Entry == "" {
		return
	}
	id, entry := pl.ID, s.Entry
	a.playlistDo("Could not move the song", func(ctx context.Context, c *jellyfin.Client) error {
		return c.MoveInPlaylist(ctx, id, entry, to)
	}, func() {
		a.refreshPlaylists(nil)
	})
}

// saveQueue asks for a name and keeps the queue as a playlist.
func (a *App) saveQueueAsPlaylist() {
	songs := a.player.queue
	if len(songs) == 0 {
		return
	}
	a.newPlaylist(append([]*library.Song(nil), songs...), "Queue of "+time.Now().Format("2 January"))
}

// menuPlaylists is how many playlists the Add to Playlist submenu lists:
// a menu of hundreds is no menu, and the picker finds the rest.
const menuPlaylists = 20

// addToPlaylistMenu is the submenu of a song or an album for playlists.
func (a *App) addToPlaylistMenu(m *ui.Menu, songs []*library.Song, suggest string, except string) {
	if len(songs) == 0 {
		return
	}
	m.Submenu("Add to Playlist", func(sm *ui.Menu) {
		if sm.Item("New Playlist…").Chosen() {
			a.newPlaylist(songs, suggest)
		}
		own := a.addablePlaylists(except)
		if len(own) > menuPlaylists && sm.Item("Find a Playlist…").Chosen() {
			a.picker = pickerState{open: true, songs: songs, suggest: suggest, except: except}
		}
		if len(own) > 0 {
			sm.Separator()
		}
		notes := playlistNotes(own)
		for i, pl := range own {
			if i == menuPlaylists {
				break
			}
			label := pl.Name
			if n := notes[pl.ID]; n != "" {
				label += " (" + n + ")"
			}
			if sm.Item(label).Chosen() {
				a.addToPlaylist(pl, songs)
			}
		}
	})
}

// addablePlaylists are the playlists songs can be added to: the user's
// own, but for the one they are of.
func (a *App) addablePlaylists(except string) []*library.Playlist {
	var out []*library.Playlist
	for _, pl := range a.ownPlaylists() {
		if pl.ID != except {
			out = append(out, pl)
		}
	}
	return out
}

// pickerState is the dialog that finds a playlist to add songs to, among
// more than the menu lists.
type pickerState struct {
	open    bool
	filter  string
	songs   []*library.Song
	suggest string // a name for a new playlist
	except  string // the playlist the songs are of
}

// pickerDialog is the dialog of the picker.
func (a *App) pickerDialog(c *ui.Context) {
	pk := &a.picker
	if !pk.open {
		return
	}
	p := a.pal
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Gap(12).Padding(22).Width(420).Children(func() {
			ui.Text(c, "Add to Playlist").FontSize(18).FontWeight(700)
			in := ui.TextInput(c.Key("picker"), &pk.filter).Label("Find a playlist").Placeholder("Find a playlist").Height(36).AutoFocus()
			own := a.addablePlaylists(pk.except)
			found, notes := filterPlaylists(own, pk.filter), playlistNotes(own)
			var chosen *library.Playlist
			if in.Submitted() && len(found) > 0 {
				chosen = found[0]
			}
			ui.Scroll(c).Height(300).Gap(1).Children(func() {
				if len(found) == 0 {
					ui.Text(c, "No playlist has that name.").TextColor(p.muted).Padding(10)
				}
				for _, pl := range found {
					b := ui.ButtonBase(c.Key(pl.ID)).Height(34).Padding(0, 10).Gap(10).Radius(p.radius).Justify(ui.Start).Shrink(0).Label(pl.Name)
					if b.Hovered() {
						b.Background(p.hover)
					}
					b.Children(func() {
						ui.Icon(c, icon("list-music")).Size(16, 16).TextColor(p.muted)
						ui.Text(c, pl.Name).FontWeight(500).SingleLine().Grow(1).MinWidth(0)
						note := notes[pl.ID]
						if note == "" {
							note = count(pl.Songs, "song", "songs")
						}
						ui.Text(c, note).FontSize(12).TextColor(p.faint).SingleLine().Shrink(0)
					})
					if b.Clicked() {
						chosen = pl
					}
				}
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if a.textButton(c, "Cancel").Clicked() {
					open = false
				}
				if a.pillButton(c, "plus", "New Playlist…", false).Clicked() {
					open = false
					a.newPlaylist(pk.songs, pk.suggest)
				}
			})
			if chosen != nil {
				open = false
				a.addToPlaylist(chosen, pk.songs)
			}
		})
	})
	if !open {
		a.picker = pickerState{}
	}
}

// filterPlaylists returns the playlists whose names hold every word of
// filter, as a search finds them.
func filterPlaylists(list []*library.Playlist, filter string) []*library.Playlist {
	words := strings.Fields(library.Fold(filter))
	if len(words) == 0 {
		return list
	}
	var out []*library.Playlist
	for _, pl := range list {
		name := library.Fold(pl.Name)
		match := true
		for _, w := range words {
			if !strings.Contains(name, w) {
				match = false
				break
			}
		}
		if match {
			out = append(out, pl)
		}
	}
	return out
}

// playlistNotes tells apart playlists that share a name: a few words for
// each of them to show after it, its songs and whether it is another's.
func playlistNotes(list []*library.Playlist) map[string]string {
	names := make(map[string]int, len(list))
	for _, pl := range list {
		names[library.Fold(pl.Name)]++
	}
	var notes map[string]string
	for _, pl := range list {
		if names[library.Fold(pl.Name)] < 2 {
			continue
		}
		if notes == nil {
			notes = map[string]string{}
		}
		note := count(pl.Songs, "song", "songs")
		if pl.Others {
			note += ", shared"
		}
		notes[pl.ID] = note
	}
	return notes
}

// How a playlist is played from its menu or its tile.
const (
	playNow = iota
	playShuffled
	playAfter // next, after the song playing
	playLast  // at the end of the queue
)

// playPlaylist plays a playlist, or queues it, once the server has told
// its songs.
func (a *App) playPlaylist(pl *library.Playlist, how int) {
	name := pl.Name
	a.fetchPlaylist(pl.ID, func(fetched []*library.Song) {
		songs := a.librarySongs(fetched)
		if len(songs) == 0 {
			a.toast(name + " is empty")
			return
		}
		switch how {
		case playNow:
			a.player.play(songs, 0)
		case playShuffled:
			a.player.playShuffled(songs)
		case playAfter:
			a.player.playNext(songs)
			a.toast("Playing next")
		case playLast:
			a.player.enqueue(songs)
			a.toast("Added to the queue")
		}
	})
}

// librarySongs returns the library's own songs for those the server
// listed, where it has them: a favorite set on one shows everywhere.
func (a *App) librarySongs(fetched []*library.Song) []*library.Song {
	songs := make([]*library.Song, len(fetched))
	for i, s := range fetched {
		songs[i] = s
		if own := a.lib.Song(s.ID); own != nil {
			songs[i] = own
		}
	}
	return songs
}

// sidebarPlaylist is a playlist's row in the sidebar, with its menu. One
// that shares its name with another says how they differ.
func (a *App) sidebarPlaylist(c *ui.Context, pl *library.Playlist) ui.Element {
	path := "/playlist/" + pl.ID
	return a.navItemNoted(c, path, "", pl.Name, a.pages.playlistNotes[pl.ID], path, "").
		ContextMenu(func(m *ui.Menu) { a.playlistMenu(m, pl) })
}

// playlistMenu is the menu of a playlist, in the sidebar and in a grid.
func (a *App) playlistMenu(m *ui.Menu, pl *library.Playlist) {
	if m.Item("Play").Chosen() {
		a.playPlaylist(pl, playNow)
	}
	if m.Item("Shuffle").Chosen() {
		a.playPlaylist(pl, playShuffled)
	}
	if m.Item("Play Next").Chosen() {
		a.playPlaylist(pl, playAfter)
	}
	if m.Item("Add to Queue").Chosen() {
		a.playPlaylist(pl, playLast)
	}
	m.Separator()
	a.playlistEditMenu(m, pl)
	a.pinMenu(m, "playlist", pl.ID, pl.Name)
}

// playlistEditMenu adds the items that change a playlist, for the user's
// own, and a separator after them. One the server did not say whose it is
// can be renamed, which the server refuses of another's, but not deleted,
// which the server allows an administrator of anyone's.
func (a *App) playlistEditMenu(m *ui.Menu, pl *library.Playlist) {
	if pl.Others {
		return
	}
	if m.Item("Rename…").Chosen() {
		a.renamePlaylist(pl)
	}
	if !pl.Unsure && m.Item("Delete Playlist…").Chosen() {
		a.deletePlaylist(pl)
	}
	m.Separator()
}

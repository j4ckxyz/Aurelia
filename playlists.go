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
func (a *App) refreshPlaylists(then func()) {
	cl := a.clientNow()
	if cl == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := cl.Playlists(ctx)
		a.update(func() {
			if err != nil {
				a.toastError("Could not read the playlists", err)
				return
			}
			// Those of others keep what was known of them: they do not change here.
			others := map[string]bool{}
			for _, pl := range a.lib.Playlists {
				if pl.Others {
					others[pl.ID] = true
				}
			}
			var list []library.Playlist
			for _, it := range items {
				list = append(list, library.Playlist{ID: it.ID, Name: it.Name, ImageTag: it.ImageTags["Primary"], Songs: it.ChildCount, Others: others[it.ID]})
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

// addToPlaylist adds songs to a playlist.
func (a *App) addToPlaylist(pl *library.Playlist, songs []*library.Song) {
	if len(songs) == 0 {
		return
	}
	id, name, n := pl.ID, pl.Name, len(songs)
	a.playlistDo("Could not add to "+name, func(ctx context.Context, c *jellyfin.Client) error {
		return c.AddToPlaylist(ctx, id, songIDs(songs))
	}, func() {
		a.toast("Added " + count(n, "song", "songs") + " to " + name)
		a.refreshPlaylists(nil)
	})
}

// newPlaylist asks for a name, and makes a playlist of songs, which may be
// none.
func (a *App) newPlaylist(songs []*library.Song, suggest string) {
	a.ask(promptState{
		title: "New playlist", field: true, value: suggest, confirm: "Create",
		run: func(name string) {
			a.playlistDo("Could not make the playlist", func(ctx context.Context, c *jellyfin.Client) error {
				_, err := c.CreatePlaylist(ctx, name, songIDs(songs))
				return err
			}, func() {
				a.toast("Made the playlist " + name)
				a.refreshPlaylists(nil)
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

// removeFromPlaylist takes a song out of a playlist.
func (a *App) removeFromPlaylist(pl *library.Playlist, s *library.Song) {
	if s.Entry == "" {
		return
	}
	id, entry, name := pl.ID, s.Entry, pl.Name
	a.playlistDo("Could not take the song out of "+name, func(ctx context.Context, c *jellyfin.Client) error {
		return c.RemoveFromPlaylist(ctx, id, []string{entry})
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

// addToPlaylistMenu is the submenu of a song or an album for playlists.
func (a *App) addToPlaylistMenu(m *ui.Menu, songs []*library.Song, suggest string, except string) {
	if len(songs) == 0 {
		return
	}
	m.Submenu("Add to Playlist", func(sm *ui.Menu) {
		if sm.Item("New Playlist…").Chosen() {
			a.newPlaylist(songs, suggest)
		}
		own := a.ownPlaylists()
		if len(own) > 0 {
			sm.Separator()
		}
		for i, pl := range own {
			if i == 30 {
				break // a menu of hundreds is no menu
			}
			if pl.ID != except && sm.Item(pl.Name).Chosen() {
				a.addToPlaylist(pl, songs)
			}
		}
	})
}

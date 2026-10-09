package main

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// Radio plays songs the server picks as being like a song, an album or an
// artist, as Jellyfin's instant mix does.

// radioSize is how many songs a radio asks for.
const radioSize = 50

// mixOf asks the server for songs like an item and gives those that the
// library has, on the interface's goroutine.
func (a *App) mixOf(kind, id string, limit int, done func(songs []*library.Song, err error)) {
	cl := a.clientNow()
	if cl == nil {
		done(nil, nil)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := cl.InstantMix(ctx, kind, id, limit)
		a.update(func() {
			var songs []*library.Song
			for _, it := range items {
				if s := a.lib.Song(it.ID); s != nil {
					songs = append(songs, s)
				}
			}
			done(songs, err)
		})
	}()
}

// startRadio plays songs like an item: kind is "Songs", "Albums" or
// "Artists". A song leads its own radio.
func (a *App) startRadio(kind, id, name string, seed *library.Song) {
	a.toast("Starting radio from " + name)
	a.mixOf(kind, id, radioSize, func(songs []*library.Song, err error) {
		if seed != nil && (len(songs) == 0 || songs[0] != seed) {
			rest := songs[:0:0]
			for _, s := range songs {
				if s != seed {
					rest = append(rest, s)
				}
			}
			songs = append([]*library.Song{seed}, rest...)
		}
		switch {
		case err != nil && len(songs) == 0:
			a.toastError("Could not start the radio", err)
		case len(songs) == 0:
			a.toast("The server found no songs like " + name)
		default:
			a.player.play(songs, 0)
		}
	})
}

// similarCheck, as the queue's last song plays, asks for songs like it and
// puts them after it, when the setting asks for it: the music goes on.
func (p *player) similarCheck() {
	a := p.app
	cur := p.current()
	switch {
	case !a.settings.Autoplay, cur == nil, p.far != nil, p.similarFor == cur.ID:
		return
	case p.followingIndex() >= 0:
		return // there is more, or it repeats
	}
	p.similarFor = cur.ID
	a.mixOf("Songs", cur.ID, 30, func(songs []*library.Song, err error) {
		if p.current() != cur || len(songs) == 0 {
			return
		}
		// Those that already are in the queue, or were played, are left out.
		seen := map[string]bool{}
		for _, s := range p.queue {
			seen[s.ID] = true
		}
		var fresh []*library.Song
		for _, s := range songs {
			if !seen[s.ID] {
				seen[s.ID] = true
				fresh = append(fresh, s)
			}
		}
		p.enqueue(fresh)
	})
}

// artistMenu is the menu of an artist.
func (a *App) artistMenu(m *ui.Menu, ar *library.Artist) {
	songs := a.artistSongs(ar.ID)
	if m.Item("Play").Disabled(len(songs) == 0).Chosen() {
		a.player.play(songs, 0)
	}
	if m.Item("Shuffle").Disabled(len(songs) == 0).Chosen() {
		a.player.playShuffled(songs)
	}
	if m.Item("Add to Queue").Disabled(len(songs) == 0).Chosen() {
		a.player.enqueue(songs)
		a.toast("Added to the queue")
	}
	m.Separator()
	if m.Item("Start Radio").Chosen() {
		a.startRadio("Artists", ar.ID, ar.Name, nil)
	}
}

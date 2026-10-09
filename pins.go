package main

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// Albums, artists and playlists can be kept in the sidebar, one click from
// anywhere.

// pinIndex is where an item is among the pins, -1 when it is not pinned.
func (a *App) pinIndex(kind, id string) int {
	return slices.IndexFunc(a.settings.Pinned, func(p Pin) bool { return p.Kind == kind && p.ID == id })
}

// togglePin pins an item, or unpins it when it is pinned, and keeps the pins.
func (a *App) togglePin(kind, id, name string) {
	if i := a.pinIndex(kind, id); i >= 0 {
		a.settings.Pinned = slices.Delete(a.settings.Pinned, i, i+1)
	} else {
		a.settings.Pinned = append(a.settings.Pinned, Pin{Kind: kind, ID: id, Name: name})
	}
	a.saveSettings()
}

// pinMenu adds the item that pins, or unpins, to a menu.
func (a *App) pinMenu(m *ui.Menu, kind, id, name string) {
	label := "Pin to Sidebar"
	if a.pinIndex(kind, id) >= 0 {
		label = "Unpin from Sidebar"
	}
	if m.Item(label).Chosen() {
		a.togglePin(kind, id, name)
	}
}

// pinned is a pin as the sidebar shows it: its name now, its page, and its icon.
func (a *App) pinned(p Pin) (name, path, glyph string, ok bool) {
	switch p.Kind {
	case "album":
		if al := a.lib.Album(p.ID); al != nil {
			return al.Name, "/album/" + p.ID, "disc-3", true
		}
	case "artist":
		if ar := a.lib.Artist(p.ID); ar != nil {
			return ar.Name, "/artist/" + p.ID, "mic-vocal", true
		}
	case "playlist":
		if pl := a.lib.Playlist(p.ID); pl != nil {
			return pl.Name, "/playlist/" + p.ID, "list-music", true
		}
	}
	// While the library is read, or when it lacks the item, what was kept.
	if a.lib.IsEmpty() || a.syncing {
		return p.Name, "/" + p.Kind + "/" + p.ID, map[string]string{"album": "disc-3", "artist": "mic-vocal", "playlist": "list-music"}[p.Kind], true
	}
	return "", "", "", false
}

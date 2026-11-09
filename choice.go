package main

import (
	"context"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// Songs are chosen in a list as in Finder: a click chooses a song,
// Cmd-click (Ctrl-click on Linux and Windows) adds one or takes it out,
// Shift-click chooses those from the song chosen last to it, and Escape
// lets them go. The menu of a song chosen is of them all, and dragging
// one drags them all: to a playlist of the sidebar, to the queue, or to
// another place in the playlist they are in.
//
// The pages' lists hold more than songs, a picture at the top, titles and
// albums, so the songs are chosen here rather than by the list, which
// would choose its every row.

// choice is the songs chosen in the list of a page, by their places in it
// and their IDs: the list made again in the same order keeps them, and a
// place that now holds another song is no longer chosen.
type choice struct {
	page  string         // the page, as the router has it
	at    map[int]string // the places chosen, and the songs there
	pivot int            // the place chosen last, where Shift-click chooses from
}

// has reports whether the song at place i of the list of page is chosen.
func (ch *choice) has(page string, i int, id string) bool {
	return ch.page == page && id != "" && ch.at[i] == id
}

// any reports whether songs of page are chosen.
func (ch *choice) any(page string) bool { return ch.page == page && len(ch.at) > 0 }

// clear lets the songs chosen go.
func (ch *choice) clear() { *ch = choice{} }

// click chooses as a click holding mods does on the song at place i of
// list.
func (ch *choice) click(page string, list []*library.Song, i int, mods ui.Modifiers) {
	if i < 0 || i >= len(list) {
		return
	}
	if ch.page != page || ch.at == nil {
		*ch = choice{page: page, at: map[int]string{}, pivot: -1}
	}
	switch {
	case mods&ui.Shift != 0 && ch.pivot >= 0 && ch.pivot < len(list):
		// From the place chosen last, which stays where the next
		// Shift-click chooses from; with Cmd as well, added to those.
		if mods&ui.Cmd == 0 {
			clear(ch.at)
		}
		for j := min(ch.pivot, i); j <= max(ch.pivot, i); j++ {
			ch.at[j] = list[j].ID
		}
	case mods&ui.Cmd != 0:
		if ch.at[i] == list[i].ID {
			delete(ch.at, i)
		} else {
			ch.at[i] = list[i].ID
			ch.pivot = i
		}
	default:
		clear(ch.at)
		ch.at[i] = list[i].ID
		ch.pivot = i
	}
}

// places returns the places of the songs of list a menu or a drag from
// place i is of: those chosen, in order, when the song there is one of
// them, else that song alone.
func (ch *choice) places(page string, list []*library.Song, i int) []int {
	if i < 0 || i >= len(list) {
		return nil
	}
	if !ch.has(page, i, list[i].ID) {
		return []int{i}
	}
	out := make([]int, 0, len(ch.at))
	for j, id := range ch.at {
		if j >= 0 && j < len(list) && list[j].ID == id {
			out = append(out, j)
		}
	}
	slices.Sort(out)
	return out
}

// songsAt returns the songs of list at places.
func songsAt(list []*library.Song, places []int) []*library.Song {
	out := make([]*library.Song, len(places))
	for k, j := range places {
		out[k] = list[j]
	}
	return out
}

// songDrag is songs being dragged from a list: the row dragged, which
// tells which songs go with it as they drop.
type songDrag struct {
	a    *App
	page string
	list []*library.Song
	at   int
}

// places returns the places in the list of the songs dragged.
func (d *songDrag) places() []int { return d.a.chosen.places(d.page, d.list, d.at) }

// songs returns the songs dragged.
func (d *songDrag) songs() []*library.Song { return songsAt(d.list, d.places()) }

// from reports whether the songs are dragged from list.
func (d *songDrag) from(list []*library.Song) bool {
	return len(d.list) == len(list) && len(list) > 0 && &d.list[0] == &list[0]
}

// songsMenu is the menu of several songs chosen together.
func (a *App) songsMenu(m *ui.Menu, songs []*library.Song) {
	if m.Item("Play").Chosen() {
		a.player.play(songs, 0)
	}
	if m.Item("Play Next").Chosen() {
		a.player.playNext(songs)
		a.toast("Playing next")
	}
	if m.Item("Add to Queue").Chosen() {
		a.player.enqueue(songs)
		a.toast("Added " + count(len(songs), "song", "songs") + " to the queue")
	}
	a.addToPlaylistMenu(m, songs, "", "")
	m.Separator()
	// Favorites all, unless they are already: then none.
	liked := true
	for _, s := range songs {
		liked = liked && s.Favorite
	}
	label := "Add to Favorites"
	if liked {
		label = "Remove from Favorites"
	}
	if m.Item(label).Chosen() {
		for _, s := range songs {
			if s.Favorite == liked {
				a.setFavorite(s.ID, &s.Favorite, !liked)
			}
		}
	}
	// Download those that are not, or when all are, remove them.
	all, held := true, true
	for _, s := range songs {
		st, _ := a.downloads.state(s.ID)
		all = all && st != dlNone
		held = held && a.downloads.held(s.ID)
	}
	if !all {
		if m.Item("Download").Chosen() {
			a.downloads.add(songs...)
		}
	} else if m.Item("Remove Downloads").Disabled(held).Chosen() {
		a.downloads.remove(songs...)
	}
}

// takeSongs makes a playlist of the sidebar take songs dragged to it.
func (a *App) takeSongs(b ui.Element, pl *library.Playlist) {
	if pl.Others {
		return // not the user's to change
	}
	if _, ok := ui.DragOver[*songDrag](b); ok {
		b.Background(a.pal.selection)
	}
	if d, ok := ui.Drop[*songDrag](b); ok {
		a.addToPlaylist(pl, d.songs())
	}
}

// queueTakes makes the queue's panel take songs dragged to it, which go
// at the end of the queue.
func (a *App) queueTakes(panel ui.Element) {
	if _, ok := ui.DragOver[*songDrag](panel); ok {
		accent := a.pal.accent
		panel.DrawOver(func(g *ui.Painter, r ui.Rect) {
			g.Stroke(ui.Rect{X: r.X + 2, Y: r.Y + 2, W: r.W - 4, H: r.H - 4}, accent, 6, 2)
		})
	}
	if d, ok := ui.Drop[*songDrag](panel); ok {
		songs := d.songs()
		a.player.enqueue(songs)
		a.toast("Added " + count(len(songs), "song", "songs") + " to the queue")
	}
}

// entryMove is a song of a playlist, by its entry, moved to a place: the
// place it has once it is moved.
type entryMove struct {
	entry string
	to    int
}

// moveEntries returns the entries of a playlist with those at places from
// moved, in their order, to before the one at place to (the number of
// entries for the end), and the moves that make that order on the server,
// which moves one entry at a time.
func moveEntries(entries []string, from []int, to int) ([]string, []entryMove) {
	moving := map[int]bool{}
	for _, i := range from {
		moving[i] = true
	}
	// They go before the first entry from place to on that stays.
	anchor := ""
	for i := to; i < len(entries); i++ {
		if !moving[i] {
			anchor = entries[i]
			break
		}
	}
	order := slices.Clone(entries)
	var moves []entryMove
	prev := ""
	for _, i := range from {
		e := entries[i]
		was := slices.Index(order, e)
		order = slices.Delete(order, was, was+1)
		at := len(order)
		switch {
		case prev != "":
			at = slices.Index(order, prev) + 1
		case anchor != "":
			at = slices.Index(order, anchor)
		}
		order = slices.Insert(order, at, e)
		if at != was {
			moves = append(moves, entryMove{e, at})
		}
		prev = e
	}
	return order, moves
}

// movePlaylistSongs moves the songs at places from of a playlist, as
// list has it, to before the one at place to: here at once, and then on
// the server. The songs moved are the ones chosen after it.
func (a *App) movePlaylistSongs(pl *library.Playlist, list []*library.Song, from []int, to int) {
	if pl.Others || len(from) == 0 {
		return
	}
	entries := make([]string, len(list))
	byEntry := make(map[string]*library.Song, len(list))
	for i, s := range list {
		if s.Entry == "" {
			return // read from the downloads: not the server's to move
		}
		entries[i] = s.Entry
		byEntry[s.Entry] = s
	}
	order, moves := moveEntries(entries, from, to)
	if len(moves) == 0 {
		return
	}
	moved := make([]*library.Song, len(order))
	for i, e := range order {
		moved[i] = byEntry[e]
	}
	key := "playlist:" + pl.ID
	a.pages.fetched[key] = moved
	a.pages.reordered++
	// They stay chosen, at their new places.
	ch := choice{page: a.router.Location(), at: map[int]string{}, pivot: -1}
	first := slices.Index(order, entries[from[0]])
	for k := range from {
		ch.at[first+k] = moved[first+k].ID
	}
	ch.pivot = first
	a.chosen = ch
	cl := a.clientNow()
	if cl == nil {
		return
	}
	id := pl.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var err error
		for _, mv := range moves {
			if err = cl.MoveInPlaylist(ctx, id, mv.entry, mv.to); err != nil {
				break
			}
		}
		a.update(func() {
			if err != nil {
				a.toastError("Could not move the songs", err)
			}
			// What the server holds, moved or not.
			a.refreshPlaylists(nil)
		})
	}()
}

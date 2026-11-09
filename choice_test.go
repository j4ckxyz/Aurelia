package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// find is where text shows, or the test ends.
func find(t *testing.T, tt *ui.Tester, text string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("%q does not show; texts: %q", text, tt.Texts())
	}
	return r
}

// dragTo presses at (x, y), moves the pointer in steps to (tx, ty), and
// lets go there.
func dragTo(tt *ui.Tester, x, y, tx, ty float32) {
	tt.Press(x, y)
	for k := float32(1); k <= 4; k++ {
		tt.Move(x+(tx-x)*k/4, y+(ty-y)*k/4)
	}
	tt.Release(tx, ty)
}

// chosenIDs are the songs chosen on the page showing, in the order of
// list.
func chosenIDs(a *App, list []*library.Song) string {
	var ids []string
	for i, s := range list {
		if a.chosen.has(a.router.Location(), i, s.ID) {
			ids = append(ids, s.ID)
		}
	}
	return strings.Join(ids, " ")
}

// A click chooses a song, Cmd-click adds one or takes it out, Shift-click
// chooses those between, and Escape lets them go; none of it plays, as a
// double click does.
func TestChooseSongs(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/songs")
	tt.Frame()
	songs := a.songsBy(a.settings.SongSort)
	if len(songs) != 4 {
		t.Fatalf("%d songs", len(songs))
	}
	click(t, tt, songs[0].Name)
	if got := chosenIDs(a, songs); got != songs[0].ID {
		t.Errorf("after a click, chosen: %q", got)
	}
	if a.player.current() != nil {
		t.Error("a click played the song")
	}
	if err := tt.ClickWith(ui.Cmd, songs[2].Name); err != nil {
		t.Fatal(err)
	}
	if got, want := chosenIDs(a, songs), songs[0].ID+" "+songs[2].ID; got != want {
		t.Errorf("after Cmd-click, chosen: %q, not %q", got, want)
	}
	if err := tt.ClickWith(ui.Cmd, songs[0].Name); err != nil {
		t.Fatal(err)
	}
	if got := chosenIDs(a, songs); got != songs[2].ID {
		t.Errorf("after Cmd-click on one chosen, chosen: %q", got)
	}
	// From the song chosen last.
	if err := tt.ClickWith(ui.Shift, songs[0].Name); err != nil {
		t.Fatal(err)
	}
	if got, want := chosenIDs(a, songs), songs[0].ID+" "+songs[1].ID+" "+songs[2].ID; got != want {
		t.Errorf("after Shift-click, chosen: %q, not %q", got, want)
	}
	// Chosen on another page is not chosen here.
	a.router.Push("/favorites")
	tt.Frame()
	if a.chosen.any(a.router.Location()) {
		t.Error("songs are chosen on another page")
	}
	a.router.Back()
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if got := chosenIDs(a, songs); got != "" {
		t.Errorf("after Escape, chosen: %q", got)
	}
	// The double click still plays.
	r := find(t, tt, songs[3].Name)
	tt.ClickAt(r.X+4, r.Y+4)
	tt.ClickAt(r.X+4, r.Y+4)
	if s := a.player.current(); s == nil || s.ID != songs[3].ID {
		t.Errorf("after a double click, playing %v", s)
	}
}

// The menu of a song chosen with others is of them all.
func TestMenuOfSongsChosen(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/album/al1")
	tt.Frame()
	songs := a.lib.AlbumSongs("al1")
	click(t, tt, songs[0].Name)
	if err := tt.ClickWith(ui.Shift, songs[1].Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick(songs[1].Name); err != nil {
		t.Fatal(err)
	}
	menu := tt.Menu()
	for _, want := range []string{"Play Next", "Add to Queue", "Add to Playlist", "Add to Favorites", "Download"} {
		if !slices.Contains(menu, want) {
			t.Errorf("%q is not in the menu: %q", want, menu)
		}
	}
	if slices.Contains(menu, "Go to Album") {
		t.Errorf("the menu is of one song: %q", menu)
	}
	if err := tt.ChooseMenuItem("Add to Playlist", "Road trip"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	// Both are in Road trip already: the app asks first.
	wait(t, a, "the question", func() bool { return a.prompt.open })
	tt.Frame()
	click(t, tt, "Add Anyway")
	if got := ps.waitFor(t, a, "POST /Playlists/p1/Items"); !strings.Contains(got, "ids="+songs[0].ID+"%2C"+songs[1].ID) {
		t.Errorf("the request was %q", got)
	}
	// Liked together.
	if err := tt.RightClick(songs[0].Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Add to Favorites"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	if !songs[0].Favorite || !songs[1].Favorite {
		t.Errorf("liked: %v %v", songs[0].Favorite, songs[1].Favorite)
	}
	// Queued together.
	a.player.play([]*library.Song{a.lib.Song("s3")}, 0)
	if err := tt.RightClick(songs[0].Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Add to Queue"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	if got, want := queueIDs(a.player), "s3 "+songs[0].ID+" "+songs[1].ID; got != want {
		t.Errorf("the queue is %s, not %s", got, want)
	}
	// A song not chosen has its own menu.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if err := tt.RightClick(songs[0].Name); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Contains(menu, "Go to Artist") {
		t.Errorf("the menu of one song: %q", menu)
	}
	tt.CloseMenu()
}

// Songs dragged to a playlist of the sidebar are added to it, and to the
// queue's panel, to the queue.
func TestDragSongs(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/album/al1")
	tt.Frame()
	songs := a.lib.AlbumSongs("al1")
	click(t, tt, songs[0].Name)
	if err := tt.ClickWith(ui.Cmd, songs[1].Name); err != nil {
		t.Fatal(err)
	}
	from := find(t, tt, songs[1].Name)
	to := find(t, tt, "Road trip") // the sidebar's
	dragTo(tt, from.X+4, from.Y+from.H/2, to.X+4, to.Y+to.H/2)
	wait(t, a, "the question", func() bool { return a.prompt.open }) // in it already
	tt.Frame()
	click(t, tt, "Add Anyway")
	if got := ps.waitFor(t, a, "POST /Playlists/p1/Items"); !strings.Contains(got, "ids="+songs[0].ID+"%2C"+songs[1].ID) {
		t.Errorf("the request was %q", got)
	}
	if a.player.current() != nil {
		t.Error("the drag played")
	}
	// A song not chosen goes alone, to the queue.
	a.player.play([]*library.Song{a.lib.Song("s3")}, 0)
	a.settings.QueueOpen = true
	tt.Frame()
	tt.Frame()
	from = find(t, tt, songs[0].Name)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	q := find(t, tt, "NOW PLAYING")
	dragTo(tt, from.X+4, from.Y+from.H/2, q.X+4, q.Y+q.H+80)
	tt.Frame()
	if got, want := queueIDs(a.player), "s3 "+songs[0].ID; got != want {
		t.Errorf("the queue is %s, not %s", got, want)
	}
}

// The songs of a playlist drag to another place in it, here at once and
// then on the server.
func TestDragInAPlaylist(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/playlist/p1")
	tt.Frame()
	wait(t, a, "the playlist's songs", func() bool { tt.Frame(); return tt.HasText("Perfect") })
	from := find(t, tt, "Perfect")
	to := find(t, tt, "Shape of You")
	// Onto the upper half of the first: before it.
	dragTo(tt, from.X+4, from.Y+from.H/2, to.X+4, to.Y+2)
	got := a.pages.fetched["playlist:p1"]
	if len(got) != 2 || got[0].ID != "s2" || got[1].ID != "s1" {
		t.Fatalf("the playlist here after the drag: %v", got)
	}
	ps.waitFor(t, a, "POST /Playlists/p1/Items/E2/Move/0")
	// What moved stays chosen.
	tt.Frame()
	if !a.chosen.has(a.router.Location(), 0, "s2") {
		t.Errorf("the song moved is not chosen: %+v", a.chosen)
	}
}

// The moves the server is asked for make the order the drag shows.
func TestMoveEntries(t *testing.T) {
	// The server moves an entry to the place it has once moved.
	serve := func(list []string, moves []entryMove) []string {
		list = slices.Clone(list)
		for _, mv := range moves {
			i := slices.Index(list, mv.entry)
			list = slices.Delete(list, i, i+1)
			list = slices.Insert(list, mv.to, mv.entry)
		}
		return list
	}
	entries := []string{"A", "B", "C", "D", "E", "F"}
	n := len(entries)
	// Every choice of places, to every place.
	for set := 1; set < 1<<n; set++ {
		var from []int
		for i := range n {
			if set&(1<<i) != 0 {
				from = append(from, i)
			}
		}
		for to := 0; to <= n; to++ {
			// What a list shows: the others in order, those moved
			// before the first other from place to.
			var want, moved []string
			for _, i := range from {
				moved = append(moved, entries[i])
			}
			placed := false
			for i, e := range entries {
				if i >= to && !placed && !slices.Contains(from, i) {
					want = append(want, moved...)
					placed = true
				}
				if !slices.Contains(from, i) {
					want = append(want, e)
				}
			}
			if !placed {
				want = append(want, moved...)
			}
			order, moves := moveEntries(entries, from, to)
			name := fmt.Sprint(from, " to ", to)
			if !slices.Equal(order, want) {
				t.Errorf("%s: %v, not %v", name, order, want)
			}
			if got := serve(entries, moves); !slices.Equal(got, want) {
				t.Errorf("%s: the server has %v, not %v (moves %v)", name, got, want, moves)
			}
			if len(moves) > len(from) {
				t.Errorf("%s: %d moves for %d songs", name, len(moves), len(from))
			}
		}
	}
}

package main

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// Favorites plays its songs in order, as well as shuffled, in the order
// the user chose for them.
func TestFavoritesPlayAndSort(t *testing.T) {
	a := testApp(t)
	a.lib.Song("s1").Favorite = true
	a.favGen++
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/favorites")
	tt.Frame()
	wantTexts(t, tt, "Play songs", "Shuffle songs", "Sort by", "Title")
	click(t, tt, "Play songs")
	p := a.player
	if got := queueIDs(p); got != "s3 s1" || p.index != 0 {
		t.Errorf("by title: %s at %d", got, p.index)
	}
	a.settings.FavoriteSort = "Album"
	tt.Frame()
	click(t, tt, "Play songs")
	if got := queueIDs(p); got != "s1 s3" || p.index != 0 {
		t.Errorf("by album: %s at %d", got, p.index)
	}
	// An order that is not one of the page's is the first.
	a.settings.FavoriteSort = "Most Played"
	tt.Frame()
	if a.settings.FavoriteSort != "Title" {
		t.Errorf("the order is %q", a.settings.FavoriteSort)
	}
}

// The heart tells what it did, in one toast however often it is pressed.
func TestFavoriteToast(t *testing.T) {
	a := testAppWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	tt := ui.NewTester(a.view, 1240, 800)
	s := a.lib.Song("s2")
	a.setFavorite(s.ID, &s.Favorite, true)
	tt.Frame()
	wantTexts(t, tt, "Added to Favorites")
	a.setFavorite(s.ID, &s.Favorite, false)
	tt.Frame()
	wantTexts(t, tt, "Removed from Favorites")
	if tt.HasText("Added to Favorites") {
		t.Error("the first toast stays beside the second")
	}
}

// Clearing the queue can be undone, while the same song plays.
func TestClearQueueUndo(t *testing.T) {
	a := testApp(t)
	a.settings.QueueOpen = true
	p := a.player
	p.play(a.lib.AlbumSongs("al1"), 0)
	p.enqueue([]*library.Song{a.lib.Song("s3"), a.lib.Song("s4")})
	tt := ui.NewTester(a.view, 1240, 800)
	click(t, tt, "Clear")
	if got := queueIDs(p); got != "s1" {
		t.Fatalf("after Clear: %s", got)
	}
	tt.Frame()
	wantTexts(t, tt, "Cleared 3 songs from the queue")
	click(t, tt, "Undo")
	if got := queueIDs(p); got != "s1 s2 s3 s4" || p.index != 0 {
		t.Errorf("after Undo: %s at %d", got, p.index)
	}
	// Once another song plays, the songs cleared stay out.
	tt.Frame()
	click(t, tt, "Clear")
	p.play([]*library.Song{a.lib.Song("s4")}, 0)
	tt.Frame()
	click(t, tt, "Undo")
	if got := queueIDs(p); got != "s4" {
		t.Errorf("undone after another song played: %s", got)
	}
}

// Home's "Show all" of what was added shows the albums in that order,
// and the order the user chose for Albums stays.
func TestShowAllKeepsAlbumSort(t *testing.T) {
	a := testApp(t)
	a.settings.AlbumSort = "Artist"
	tt := ui.NewTester(a.view, 1240, 800)
	before := func(first, second string) bool {
		x, ok1 := tt.Find(first)
		y, ok2 := tt.Find(second)
		if !ok1 || !ok2 {
			t.Fatalf("%q or %q does not show: %q", first, second, tt.Texts())
		}
		return x.X < y.X
	}
	a.router.Push("/albums")
	tt.Frame()
	if !before("Lemonade", "Divide") {
		t.Error("by artist, Divide comes before Lemonade")
	}
	a.router.Push("/albums?sort=Recently+Added")
	tt.Frame()
	if !before("Divide", "Lemonade") || !before("Lemonade", "Plus") {
		t.Error("the albums added last do not come first")
	}
	if a.settings.AlbumSort != "Artist" {
		t.Errorf("the albums' order became %q", a.settings.AlbumSort)
	}
	a.router.Back()
	tt.Frame()
	if !before("Lemonade", "Divide") {
		t.Error("back on Albums, the order is not the user's")
	}
	// An order chosen on the page shown in another becomes the user's.
	a.router.Forward()
	tt.Frame()
	click(t, tt, "Recently Added")
	click(t, tt, "Year")
	if a.settings.AlbumSort != "Year" || a.router.Location() != "/albums" {
		t.Errorf("after choosing Year: the order is %q, at %s", a.settings.AlbumSort, a.router.Location())
	}
}

// Past maxLists pages, only the page seen first forgets its place.
func TestPageListsForgetTheOldest(t *testing.T) {
	a := testApp(t)
	var kept *ui.ListState
	for i := range maxLists + 7 {
		a.router.Push(fmt.Sprint("/album/a", i))
		st := a.pageList()
		if i == 20 {
			kept = st
		}
	}
	if n := len(a.pages.lists); n != maxLists {
		t.Errorf("%d places kept", n)
	}
	for i := range 7 {
		if a.pages.lists[fmt.Sprint("/album/a", i)] != nil {
			t.Errorf("the place of page %d stays", i)
		}
	}
	if a.pages.lists["/album/a7"] == nil || a.pages.lists["/album/a20"] != kept {
		t.Error("a later page lost its place")
	}
}

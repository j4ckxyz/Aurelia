package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPinsInTheSidebar(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 900)
	a.router.Push("/albums")
	tt.Frame()
	if tt.HasText("PINNED") {
		t.Fatal("a sidebar with nothing pinned has a heading for it")
	}
	// Pin an album through its menu.
	al := a.lib.Album("al2")
	if err := tt.RightClick(al.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Pin to Sidebar"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	tt.Frame()
	wantTexts(t, tt, "PINNED", al.Name)
	if len(a.settings.Pinned) != 1 || a.settings.Pinned[0] != (Pin{"album", "al2", al.Name}) {
		t.Fatalf("the pins are %+v", a.settings.Pinned)
	}
	if saved := loadSettings(a.dirs); len(saved.Pinned) != 1 {
		t.Errorf("the pin was not kept: %+v", saved.Pinned)
	}
	// An artist and a playlist the same way.
	a.togglePin("artist", "ar1", a.lib.Artist("ar1").Name)
	a.togglePin("playlist", "p1", a.lib.Playlist("p1").Name)
	if len(a.settings.Pinned) != 3 {
		t.Fatalf("the pins are %+v", a.settings.Pinned)
	}
	// The playlist shows in both places, without a clash of keys.
	a.router.Push("/home")
	tt.Frame()
	pl := a.lib.Playlist("p1")
	wantTexts(t, tt, "PINNED", "PLAYLISTS", pl.Name, a.lib.Artist("ar1").Name)
	// Unpinning from the sidebar's own menu.
	if err := tt.RightClick(al.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Unpin from Sidebar"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	tt.Frame()
	if len(a.settings.Pinned) != 2 || a.pinIndex("album", "al2") >= 0 {
		t.Errorf("the album was not unpinned: %+v", a.settings.Pinned)
	}
	// A pin of something the library no longer has is not shown.
	a.settings.Pinned = append(a.settings.Pinned, Pin{"album", "gone", "Gone Album"})
	tt.Frame()
	if tt.HasText("Gone Album") {
		t.Error("a pin of a missing album is shown")
	}
}

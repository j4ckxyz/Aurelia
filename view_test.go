package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
	"aurelia/internal/theme"
)

// testApp is the app signed in to a server that answers nothing, with a
// small library, and no sound card.
func testApp(t *testing.T) *App {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	a := newApp(dirs{data: dir, cache: filepath.Join(dir, "cache")}, true)
	a.settings.Session = &jellyfin.Session{Server: srv.URL, ServerID: "srv", ServerName: "Test", UserID: "u", UserName: "Ada", Token: "t", DeviceID: "d"}
	a.settings.Theme = theme.DefaultDark
	a.setClient(jellyfin.New(*a.settings.Session))
	a.setLibrary(library.Build(library.Data{
		Version: 1,
		Artists: []library.Artist{{ID: "ar1", Name: "Ed Sheeran"}, {ID: "ar2", Name: "Beyoncé"}},
		Albums: []library.Album{
			{ID: "al1", Name: "Divide", Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}, Year: 2017, Added: 30},
			{ID: "al2", Name: "Lemonade", Artist: "Beyoncé", ArtistIDs: []string{"ar2"}, Year: 2016, Added: 20},
			{ID: "al3", Name: "Plus", Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}, Year: 2011, Added: 10},
		},
		Songs: []library.Song{
			{ID: "s1", Name: "Shape of You", Album: "Divide", AlbumID: "al1", Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}, AlbumArtistIDs: []string{"ar1"}, Track: 1, Disc: 1, Seconds: 233, Plays: 9},
			{ID: "s2", Name: "Perfect", Album: "Divide", AlbumID: "al1", Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}, AlbumArtistIDs: []string{"ar1"}, Track: 2, Disc: 1, Seconds: 263},
			{ID: "s3", Name: "Formation", Album: "Lemonade", AlbumID: "al2", Artist: "Beyoncé", ArtistIDs: []string{"ar2"}, AlbumArtistIDs: []string{"ar2"}, Track: 1, Disc: 1, Seconds: 206, Favorite: true},
			{ID: "s4", Name: "The A Team", Album: "Plus", AlbumID: "al3", Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}, AlbumArtistIDs: []string{"ar1"}, Track: 1, Disc: 1, Seconds: 258},
		},
		Playlists: []library.Playlist{{ID: "p1", Name: "Road trip", Songs: 2}},
	}))
	return a
}

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("clicking %q: %v\ntexts: %q", label, err, tt.Texts())
	}
}

func wantTexts(t *testing.T, tt *ui.Tester, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if !tt.HasText(s) {
			t.Errorf("%q does not show; texts: %q", s, tt.Texts())
		}
	}
}

// The pages open on a click, and the buttons at the top left go back and
// forward through them.
func TestNavigation(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 800)
	wantTexts(t, tt, "Home", "Albums", "Recently added", "Divide", "Road trip")
	click(t, tt, "Albums")
	if a.router.Path() != "/albums" {
		t.Fatalf("at %s after clicking Albums", a.router.Path())
	}
	wantTexts(t, tt, "3 albums", "Divide", "Lemonade", "Plus")
	click(t, tt, "Lemonade")
	if a.router.Path() != "/album/al2" {
		t.Fatalf("at %s after clicking an album", a.router.Path())
	}
	wantTexts(t, tt, "ALBUM", "Formation", "2016", "1 song, 3 min")
	// To the artist, by the name under the title.
	click(t, tt, "Beyoncé")
	if a.router.Path() != "/artist/ar2" {
		t.Fatalf("at %s after clicking the artist", a.router.Path())
	}
	wantTexts(t, tt, "ARTIST", "1 album")
	click(t, tt, "Back")
	if a.router.Path() != "/album/al2" {
		t.Errorf("at %s after Back", a.router.Path())
	}
	click(t, tt, "Back")
	click(t, tt, "Back")
	if a.router.Path() != "/home" {
		t.Errorf("at %s after three Backs", a.router.Path())
	}
	click(t, tt, "Forward")
	click(t, tt, "Forward")
	if a.router.Path() != "/album/al2" || !tt.HasText("Formation") {
		t.Errorf("at %s after two Forwards", a.router.Path())
	}
	for path, texts := range map[string][]string{
		"/artists":      {"2 artists", "Ed Sheeran", "2 albums"},
		"/songs":        {"4 songs", "Shape of You", "The A Team", "3:53"},
		"/favorites":    {"Favorites", "Formation"},
		"/playlists":    {"1 playlist", "Road trip"},
		"/playlist/p1":  {"PLAYLIST", "Road trip"},
		"/settings":     {"Appearance", "Tokyo Night", "Normalize volume", "Sign out", "Ada"},
		"/lyrics":       {"Nothing is playing"},
		"/nowhere":      {"Nothing here"},
		"/album/gone":   {"Album not found"},
		"/artist/gone":  {"Artist not found"},
		"/artist/ar1":   {"Most played", "Shape of You", "Albums", "Divide", "Plus"},
		"/search?q=zzz": {"Nothing found for “zzz”"},
	} {
		a.router.Push(path)
		tt.Frame()
		wantTexts(t, tt, texts...)
	}
}

// Typing in the search field searches as the letters come, on one page
// of the history.
func TestSearch(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 800)
	click(t, tt, "Search")
	tt.Type("perf")
	if a.router.Location() != "/search?q=perf" {
		t.Fatalf("at %s after typing", a.router.Location())
	}
	wantTexts(t, tt, "Songs", "Perfect")
	if tt.HasText("Formation") {
		t.Error("a song that does not match shows")
	}
	tt.Type("ect ed")
	wantTexts(t, tt, "Perfect")
	click(t, tt, "Back")
	if a.router.Path() != "/home" || a.search.query != "" {
		t.Errorf("after Back: at %s, the field holds %q", a.router.Path(), a.search.query)
	}
	click(t, tt, "Forward")
	if a.search.query != "perfect ed" || !tt.HasText("Perfect") {
		t.Errorf("after Forward the field holds %q", a.search.query)
	}
	// Accents and case do not matter.
	a.router.Push("/search?q=BEYONCE")
	tt.Frame()
	wantTexts(t, tt, "Artists", "Beyoncé", "Lemonade", "Formation")
}

// Playing fills the queue from the song chosen, and the queue's buttons
// move through it.
func TestQueue(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/album/al1")
	tt.Frame()
	click(t, tt, "Play")
	p := a.player
	if len(p.queue) != 2 || p.current() == nil || p.current().ID != "s1" {
		t.Fatalf("after Play: %d songs, at %d", len(p.queue), p.index)
	}
	tt.Frame()
	wantTexts(t, tt, "3:53", "4:23") // the songs' lengths, and the bar's
	click(t, tt, "Next")
	if p.current().ID != "s2" {
		t.Errorf("after Next: %v", p.current())
	}
	click(t, tt, "Next") // the end of the queue: it stays
	if p.current().ID != "s2" {
		t.Errorf("past the end: %v", p.current())
	}
	p.enqueue([]*library.Song{a.lib.Song("s3")})
	p.playNext([]*library.Song{a.lib.Song("s4")})
	if got := queueIDs(p); got != "s1 s2 s4 s3" {
		t.Errorf("the queue is %s", got)
	}
	click(t, tt, "Queue")
	wantTexts(t, tt, "NOW PLAYING", "NEXT UP", "The A Team", "Formation")
	p.remove(2)
	if got := queueIDs(p); got != "s1 s2 s3" {
		t.Errorf("after removing: %s", got)
	}
	// Shuffle keeps the song playing first, and gives the order back.
	a.router.Push("/home") // where the player's Shuffle is the only one
	tt.Frame()
	click(t, tt, "Shuffle")
	if !a.settings.Shuffle || p.current().ID != "s2" || p.index != 0 || len(p.queue) != 3 {
		t.Errorf("shuffled: %s at %d", queueIDs(p), p.index)
	}
	click(t, tt, "Shuffle")
	if got := queueIDs(p); got != "s1 s2 s3" || p.index != 1 {
		t.Errorf("unshuffled: %s at %d", got, p.index)
	}
	click(t, tt, "Repeat")
	if a.settings.Repeat != repeatAll || p.following() != 2 {
		t.Errorf("repeat %d, then %d", a.settings.Repeat, p.following())
	}
	p.playIndex(2)
	if p.following() != 0 {
		t.Errorf("repeating all, after the last comes %d", p.following())
	}
	// A new library keeps the queue, on its own songs.
	a.setLibrary(library.Build(a.lib.Data))
	if p.current() != a.lib.Song("s3") {
		t.Error("the queue holds songs of the library before")
	}
}

func queueIDs(p *player) string {
	var ids []string
	for _, s := range p.queue {
		ids = append(ids, s.ID)
	}
	return strings.Join(ids, " ")
}

// Signed out, the window asks to sign in; signing out empties it.
func TestSignedOut(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 2000) // tall enough for all the settings
	a.router.Push("/settings")
	tt.Frame()
	click(t, tt, "Sign out")
	tt.Frame()
	wantTexts(t, tt, "Sign in to your Jellyfin server", "Server", "Username", "Password")
	if a.signedIn() || !a.lib.IsEmpty() || a.clientNow() != nil {
		t.Error("signed out, the session or the library stays")
	}
	saved := loadSettings(a.dirs)
	if saved.Session != nil {
		t.Error("the settings on disk keep the session")
	}
}

// Every theme draws every page; a theme made in the editor is a file,
// and the one shown.
func TestThemes(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 800)
	for _, th := range a.themes.Themes() {
		a.settings.Theme = th.ID
		for _, path := range []string{"/home", "/album/al1", "/settings"} {
			a.router.Push(path)
			tt.Frame()
		}
		if got := a.pal.t.ID; got != th.ID {
			t.Errorf("showing %s, the palette is of %s", th.ID, got)
		}
	}
	a.settings.Theme = theme.DefaultDark
	a.router.Push("/settings")
	tt.Frame()
	click(t, tt, "New theme")
	if a.router.Path() != "/theme/"+theme.DefaultDark {
		t.Fatalf("at %s", a.router.Path())
	}
	tt.Frame()
	wantTexts(t, tt, "New theme", "Accent", "Background")
	if a.editing == nil {
		t.Fatal("no theme is being edited")
	}
	// The window shows a color as it changes.
	a.editing.colors["accent"] = ui.RGB(0, 255, 0)
	a.editing.changed()
	tt.Frame()
	if got := a.pal.accent; got.R != 0 || got.G != 255 {
		t.Errorf("the accent shown is %v", got)
	}
	click(t, tt, "Save")
	tt.Frame()
	saved := a.themes.Get(a.settings.Theme)
	if saved == nil || saved.BuiltIn || saved.Name != "Aurelia Dark Copy" || saved.Accent != (theme.Color{R: 0, G: 255, B: 0, A: 255}) {
		t.Fatalf("the theme saved: %+v (settings name %q)", saved, a.settings.Theme)
	}
	if _, err := os.Stat(saved.Path); err != nil {
		t.Error(err)
	}
	if a.editing != nil || a.router.Path() != "/settings" {
		t.Errorf("after saving: editing %v, at %s", a.editing != nil, a.router.Path())
	}
	// Themes of other programs come in as files.
	a.importThemeFiles([]string{filepath.Join("internal", "theme", "testdata", "vscode-color-theme.json")})
	tt.Frame()
	if got := a.themes.Get(a.settings.Theme); got == nil || got.Name != "Night Owlish" || a.pal.t != got {
		t.Errorf("after importing: %+v", got)
	}
	wantTexts(t, tt, "Night Owlish", "Edit", "Delete")
	click(t, tt, "Delete")
	tt.Frame()
	for _, th := range a.themes.Themes() {
		if th.Name == "Night Owlish" {
			t.Error("the theme deleted is still a theme")
		}
	}
	if a.settings.Theme != "auto" {
		t.Errorf("after deleting, the theme is %q", a.settings.Theme)
	}
	// A file written by hand into the folder shows without a restart.
	os.WriteFile(filepath.Join(a.dirs.themes(), "mine.json"), []byte(`{"name": "Mine", "colors": {"background": "#102030", "accent": "#ff8800"}}`), 0o644)
	if !a.themes.Changed() || a.themes.Get("user-mine") == nil {
		t.Error("a theme written into the folder does not show")
	}
}

// A narrow window and a wide one both lay out.
func TestSizes(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 800)
	for _, size := range [][2]int{{860, 540}, {1920, 1200}, {1000, 700}} {
		tt.SetSize(size[0], size[1])
		a.settings.QueueOpen = size[0] > 900
		for _, path := range []string{"/home", "/albums", "/album/al1", "/songs", "/settings"} {
			a.router.Push(path)
			tt.Frame()
		}
	}
}

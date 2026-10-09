package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
	"aurelia/internal/theme"
)

// testApp is the app signed in to a server that answers nothing, with a
// small library, and no sound card.
func testApp(t *testing.T) *App {
	t.Helper()
	return testAppWith(t, http.NotFoundHandler())
}

// testAppWith is testApp with a server that answers as handler does.
func testAppWith(t *testing.T, handler http.Handler) *App {
	t.Helper()
	srv := httptest.NewServer(handler)
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
	// Songs of the queue move to another place, and the one playing
	// stays the one playing.
	p.setShuffle(false)
	p.play([]*library.Song{a.lib.Song("s1"), a.lib.Song("s2"), a.lib.Song("s3"), a.lib.Song("s4")}, 1)
	p.move([]int{3}, 2)
	if got := queueIDs(p); got != "s1 s2 s4 s3" || p.current().ID != "s2" {
		t.Errorf("after moving the last up: %s, playing %s", got, p.current().ID)
	}
	p.move([]int{2}, 4)
	if got := queueIDs(p); got != "s1 s2 s3 s4" || p.index != 1 {
		t.Errorf("after moving it to the end: %s at %d", got, p.index)
	}
	// A double click in the queue plays from there.
	tt.Frame()
	box, _ := tt.Find("The A Team")
	tt.ClickAt(box.X+4, box.Y+4)
	tt.ClickAt(box.X+4, box.Y+4)
	if p.current().ID != "s4" || p.index != 3 {
		t.Errorf("after a double click on a song of the queue: %s at %d", p.current().ID, p.index)
	}
	p.playIndex(1)
	// Dragged in the queue's panel.
	tt.Frame()
	from, ok1 := tt.Find("The A Team")
	to, ok2 := tt.Find("Formation")
	if !ok1 || !ok2 {
		t.Fatalf("the queue does not show its songs: %q", tt.Texts())
	}
	tt.Press(from.X+4, from.Y+from.H/2)
	for k := float32(1); k <= 4; k++ {
		tt.Move(from.X+4, from.Y+from.H/2+(to.Y+2-from.Y-from.H/2)*k/4)
	}
	tt.Release(to.X+4, to.Y+2)
	if got := queueIDs(p); got != "s1 s2 s4 s3" || p.current().ID != "s2" {
		t.Errorf("after dragging: %s, playing %s", got, p.current().ID)
	}
	a.player.playIndex(2)

	// A new library keeps the queue, on its own songs.
	a.setLibrary(library.Build(a.lib.Data))
	if p.current() != a.lib.Song("s4") {
		t.Error("the queue holds songs of the library before")
	}
}

// The queue comes back in the next run, paused where it was.
func TestQueueComesBack(t *testing.T) {
	a := testApp(t)
	a.player.play(a.lib.AlbumSongs("al1"), 1)
	a.player.enqueue([]*library.Song{a.lib.Song("s4")})
	a.saveQueue()
	a.queueWrites.Wait() // written on another goroutine

	b := newApp(a.dirs, true)
	b.settings.Session = a.settings.Session
	b.setClient(a.clientNow())
	b.setLibrary(library.Build(a.lib.Data))
	p := b.player
	if got := queueIDs(p); got != "s1 s2 s4" || p.current() == nil || p.current().ID != "s2" {
		t.Fatalf("the queue that came back: %s at %d", got, p.index)
	}
	if st := p.state(); !st.Paused || !p.cold {
		t.Errorf("it came back playing: %+v", st)
	}
	tt := ui.NewTester(b.view, 1240, 800)
	wantTexts(t, tt, "Perfect")
	p.seek(42 * time.Second)
	if got := p.state().Position; got != 42*time.Second {
		t.Errorf("a seek before it plays: at %v", got)
	}
}

// The keys of the window leave typing alone, and the button over an
// album's picture plays it without opening it.
func TestKeysAndHover(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 800)
	a.player.play(a.lib.AlbumSongs("al1"), 0)
	tt.Frame()
	paused := func() bool { return a.player.paused }
	// Space plays and pauses.
	tt.Key(0, ui.KeySpace)
	if !paused() {
		t.Error("Space did not pause")
	}
	tt.Key(0, ui.KeySpace)
	if paused() {
		t.Error("Space did not play on")
	}
	tt.Key(primary, ui.KeyRight)
	if a.player.current().ID != "s2" {
		t.Errorf("the next-song key: at %s", a.player.current().ID)
	}
	// But in the search field a space is a space, and the arrows move in
	// the text.
	click(t, tt, "Search")
	tt.Type("shape of")
	tt.Key(primary, ui.KeyLeft)
	if paused() || a.search.query != "shape of" || a.player.current().ID != "s2" {
		t.Errorf("typing: paused %v, the field holds %q, playing %s", paused(), a.search.query, a.player.current().ID)
	}
	// The pointer over an album shows its play button.
	a.router.Push("/albums")
	tt.Frame()
	if tt.HasText("Play Lemonade") {
		t.Fatal("the play button shows without the pointer")
	}
	box, ok := tt.Find("Lemonade")
	if !ok {
		t.Fatal("no album Lemonade")
	}
	// Over its picture, whether the box found is the tile's or its title's.
	if box.H > 100 {
		tt.Move(box.X+box.W/2, box.Y+box.W/2)
	} else {
		tt.Move(box.X+box.W/2, box.Y-60)
	}
	click(t, tt, "Play Lemonade")
	if a.router.Path() != "/albums" || a.player.current().ID != "s3" {
		t.Errorf("after the play button: at %s, playing %s", a.router.Path(), a.player.current().ID)
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

// wait runs what the app's goroutines hand back until cond holds.
func wait(t *testing.T, a *App, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(2 * time.Millisecond) {
		a.drain()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited in vain for %s", what)
		}
	}
}

// Songs, albums and playlists download for offline: their files stay,
// play without the server, and go when removed.
func TestDownloads(t *testing.T) {
	served := map[string]int{}
	var mu sync.Mutex
	down := false
	a := testAppWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if down || !strings.HasPrefix(r.URL.Path, "/Audio/") {
			http.Error(w, "away", http.StatusServiceUnavailable)
			return
		}
		id := strings.Split(r.URL.Path, "/")[2]
		served[id]++
		w.Write([]byte("fLaC sound of " + id))
	}))
	d := a.downloads
	tt := ui.NewTester(a.view, 1240, 800)

	// An album, from its page.
	a.router.Push("/album/al1")
	tt.Frame()
	click(t, tt, "Download")
	wait(t, a, "the album's songs", func() bool { return d.done["s1"] && d.done["s2"] })
	al := a.lib.Album("al1")
	if st, _ := d.group(a.lib.AlbumSongs("al1")); st != dlDone || !d.albums["al1"] {
		t.Errorf("the album's state is %d", st)
	}
	if b, err := os.ReadFile(d.store.Path("s1")); err != nil || string(b) != "fLaC sound of s1" {
		t.Errorf("the file of s1: %q, %v", b, err)
	}
	tt.Frame()
	wantTexts(t, tt, "Downloaded: click to remove")
	// One song on its own, and one of a playlist.
	d.add(a.lib.Song("s3"))
	d.addPlaylist(a.lib.Playlist("p1"), []*library.Song{a.lib.Song("s4"), a.lib.Song("s1")})
	wait(t, a, "the other songs", func() bool { return d.done["s3"] && d.done["s4"] })
	if served["s1"] != 1 {
		t.Errorf("s1 was fetched %d times", served["s1"])
	}
	a.router.Push("/downloads")
	tt.Frame()
	wantTexts(t, tt, "Downloads", "Albums", "Divide", "Playlists", "Road trip", "Songs", "Formation", "The A Team")

	// The server goes away: what is downloaded still opens, and the
	// app says it is offline.
	mu.Lock()
	down = true
	mu.Unlock()
	f, err := a.player.trackOf(a.lib.Song("s3")).Open()
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(b) != "fLaC sound of s3" {
		t.Errorf("offline, s3 reads %q, %v", b, err)
	}
	a.sync()
	wait(t, a, "the sync to fail", func() bool { return !a.syncing })
	tt.Frame()
	if !a.offline || !tt.HasText("Offline") {
		t.Error("the app does not say it is offline")
	}

	// It all comes back in the next run.
	b2 := newApp(a.dirs, true)
	if n := len(b2.downloads.done); n != 4 || !b2.downloads.albums["al1"] || b2.downloads.playlists["p1"] == nil {
		t.Errorf("the next run has %d songs downloaded", n)
	}

	// Removing: a song an album holds stays until the album goes.
	d.remove(a.lib.Song("s2"))
	if !d.done["s2"] {
		t.Error("a song of a downloaded album was removed on its own")
	}
	d.removeAlbum(al)
	if d.done["s2"] || d.store.Has("s2") || d.albums["al1"] {
		t.Error("the album's songs stay after it was removed")
	}
	if !d.done["s1"] {
		t.Error("a song the playlist holds went with the album")
	}
	d.removeAll()
	if len(d.songs) != 0 || len(d.store.Keys()) != 0 {
		t.Errorf("after removing all: %d songs, files %v", len(d.songs), d.store.Keys())
	}
	tt.Frame()
	wantTexts(t, tt, "Nothing downloaded")
}

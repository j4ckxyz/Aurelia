package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

// playlistServer answers like a Jellyfin server that holds one playlist,
// and remembers what it was asked.
type playlistServer struct {
	mu   sync.Mutex
	seen []string // "METHOD /path?query body"
}

func (ps *playlistServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	line := r.Method + " " + r.URL.Path
	if r.URL.RawQuery != "" && r.Method != "GET" {
		line += "?" + r.URL.RawQuery
	}
	if len(body) > 0 {
		line += " " + string(body)
	}
	ps.mu.Lock()
	ps.seen = append(ps.seen, line)
	ps.mu.Unlock()
	switch {
	case r.Method == "POST" && r.URL.Path == "/Playlists":
		w.Write([]byte(`{"Id":"p9"}`))
	case r.Method == "GET" && r.URL.Path == "/Playlists/p1/Items":
		w.Write([]byte(`{"Items":[{"Id":"s1","Name":"Formation","Type":"Audio","PlaylistItemId":"E1","Artists":["Beyoncé"]},
			{"Id":"s2","Name":"Perfect","Type":"Audio","PlaylistItemId":"E2","Artists":["Ed Sheeran"]}],"TotalRecordCount":2}`))
	case r.Method == "GET" && r.URL.Path == "/Items" && strings.Contains(r.URL.RawQuery, "Playlist"):
		w.Write([]byte(`{"Items":[{"Id":"p1","Name":"Road trip","Type":"Playlist","ChildCount":3},{"Id":"p9","Name":"Mix","Type":"Playlist","ChildCount":1}],"TotalRecordCount":2}`))
	case r.Method == "GET":
		http.NotFound(w, r)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (ps *playlistServer) waitFor(t *testing.T, a *App, prefix string) string {
	t.Helper()
	var got string
	wait(t, a, prefix, func() bool {
		ps.mu.Lock()
		defer ps.mu.Unlock()
		for _, l := range ps.seen {
			if strings.HasPrefix(l, prefix) {
				got = l
				return true
			}
		}
		return false
	})
	return got
}

func TestAddToAPlaylistFromTheMenu(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/album/al1")
	tt.Frame()
	s := a.lib.AlbumSongs("al1")[0]
	if err := tt.RightClick(s.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Add to Playlist", "Road trip"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	got := ps.waitFor(t, a, "POST /Playlists/p1/Items")
	if !strings.Contains(got, "ids="+s.ID) || !strings.Contains(got, "userId=") {
		t.Errorf("the request was %q", got)
	}
	// The playlists are read again after it.
	ps.waitFor(t, a, "GET /Items")
}

func TestNewPlaylistFromASong(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/album/al1")
	tt.Frame()
	s := a.lib.AlbumSongs("al1")[0]
	if err := tt.RightClick(s.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Add to Playlist", "New Playlist…"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	tt.Frame()
	wantTexts(t, tt, "New playlist", "Create", "Cancel")
	if a.prompt.value != s.Name {
		t.Errorf("the name suggested is %q, want the song's %q", a.prompt.value, s.Name)
	}
	a.prompt.value = "  Mix  "
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	got := ps.waitFor(t, a, "POST /Playlists ")
	var body struct {
		Name      string
		Ids       []string
		MediaType string
		UserId    string
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got, "POST /Playlists ")), &body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "Mix" || len(body.Ids) != 1 || body.Ids[0] != s.ID || body.MediaType != "Audio" || body.UserId == "" {
		t.Errorf("the request was %+v", body)
	}
	wait(t, a, "the playlists read again", func() bool { return a.lib.Playlist("p9") != nil })
	if a.lib.Playlist("p9").Name != "Mix" || a.lib.Playlist("p1") == nil {
		t.Errorf("the playlists are %+v", a.lib.Playlists)
	}
	if a.prompt.open {
		t.Error("the question is still open")
	}
}

func TestPromptNeedsAName(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	tt := ui.NewTester(a.view, 1240, 800)
	a.newPlaylist(nil, "")
	tt.Frame()
	_ = tt.Click("Create") // disabled with no name
	tt.Frame()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for _, l := range ps.seen {
		if strings.HasPrefix(l, "POST") {
			t.Errorf("a playlist without a name was made: %s", l)
		}
	}
	if !a.prompt.open {
		t.Error("the question closed")
	}
}

func TestDeleteRenameAndRemove(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	tt := ui.NewTester(a.view, 1240, 800)
	pl := a.lib.Playlist("p1")
	// Removing a song: through the menu of its row on the playlist's page.
	a.router.Push("/playlist/p1")
	tt.Frame()
	wait(t, a, "the playlist's songs", func() bool { tt.Frame(); return tt.HasText("Perfect") })
	if err := tt.RightClick("Perfect"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Remove from This Playlist"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	if got := ps.waitFor(t, a, "DELETE /Playlists/p1/Items"); !strings.Contains(got, "entryIds=E2") {
		t.Errorf("the request was %q", got)
	}
	// Moving: the second of two songs can go up, and not down.
	a.refreshPlaylists(nil)
	wait(t, a, "the playlist read again", func() bool { tt.Frame(); return tt.HasText("Perfect") })
	if err := tt.RightClick("Perfect"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Move Down"); err == nil {
		t.Error("the last song could be moved down")
		tt.CloseMenu()
	}
	if err := tt.RightClick("Perfect"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Move Up"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	if got := ps.waitFor(t, a, "POST /Playlists/p1/Items/E2/Move/0"); got == "" {
		t.Error("no move was asked")
	}
	// Renaming.
	a.renamePlaylist(pl)
	tt.Frame()
	if a.prompt.value != "Road trip" {
		t.Errorf("the name suggested is %q", a.prompt.value)
	}
	a.prompt.value = "Long drive"
	if err := tt.Click("Rename"); err != nil {
		t.Fatal(err)
	}
	if got := ps.waitFor(t, a, "POST /Playlists/p1 "); !strings.Contains(got, `"Name":"Long drive"`) {
		t.Errorf("the request was %q", got)
	}
	// Deleting asks first.
	a.deletePlaylist(pl)
	tt.Frame()
	wantTexts(t, tt, "Delete “Road trip”?", "Delete")
	ps.mu.Lock()
	for _, l := range ps.seen {
		if strings.HasPrefix(l, "DELETE /Items/") {
			t.Errorf("deleted without a yes: %s", l)
		}
	}
	ps.mu.Unlock()
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	ps.waitFor(t, a, "DELETE /Items/p1")
}

func TestSaveTheQueue(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	tt := ui.NewTester(a.view, 1240, 800)
	a.player.queue = songsOf(a, "s1", "s2")
	a.player.unshuffled = a.player.queue
	a.player.index = 0
	a.settings.QueueOpen = true
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.HasPrefix(a.prompt.value, "Queue of ") {
		t.Errorf("the name suggested is %q", a.prompt.value)
	}
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	got := ps.waitFor(t, a, "POST /Playlists ")
	if !strings.Contains(got, `"Ids":["s1","s2"]`) {
		t.Errorf("the request was %q", got)
	}
}

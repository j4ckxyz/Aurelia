package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
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
	a.router.Push("/album/al3")
	tt.Frame()
	s := a.lib.AlbumSongs("al3")[0]
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
	// The playlists are read again after it. The new one the server lists
	// did not say whose it is: it shows, but is not taken for the user's.
	wait(t, a, "the playlists read again", func() bool { return a.lib.Playlist("p9") != nil })
	if o := a.lib.Playlist("p9").Owner(); o != library.OwnerUnknown {
		t.Errorf("a playlist the server said nothing of is %v", o)
	}
	if o := a.lib.Playlist("p1").Owner(); o != library.OwnerSelf {
		t.Errorf("the user's playlist became %v", o)
	}
}

// A song in the playlist already is asked about before it is added again.
func TestAddingASongTwiceAsks(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	tt := ui.NewTester(a.view, 1240, 800)
	a.addToPlaylist(a.lib.Playlist("p1"), songsOf(a, "s2"))
	wait(t, a, "the question", func() bool { return a.prompt.open })
	tt.Frame()
	wantTexts(t, tt, "Already added — add anyway?", "“Perfect” is in Road trip already. Add it again?")
	ps.mu.Lock()
	for _, l := range ps.seen {
		if strings.HasPrefix(l, "POST /Playlists/p1/Items") {
			t.Errorf("added before the answer: %s", l)
		}
	}
	ps.mu.Unlock()
	click(t, tt, "Add Anyway")
	if got := ps.waitFor(t, a, "POST /Playlists/p1/Items"); !strings.Contains(got, "ids=s2") {
		t.Errorf("the request was %q", got)
	}
	// Of an album, those in it already are left out, and the rest added
	// without asking.
	ps.mu.Lock()
	ps.seen = nil
	ps.mu.Unlock()
	a.addToPlaylist(a.lib.Playlist("p1"), songsOf(a, "s2", "s4"))
	got := ps.waitFor(t, a, "POST /Playlists/p1/Items")
	if !strings.Contains(got, "ids=s4") || strings.Contains(got, "s2") {
		t.Errorf("the request was %q", got)
	}
}

// With many playlists, the menu lists some, and a picker finds any.
func TestPickAPlaylistAmongMany(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	var lists []library.Playlist
	for i := range 30 {
		lists = append(lists, library.Playlist{ID: fmt.Sprint("m", i), Name: fmt.Sprintf("Mix %02d", i)})
	}
	a.lib.SetPlaylists(lists)
	a.libGen++
	a.settings.SidebarHidden = true // which lists them too
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/album/al3")
	tt.Frame()
	if err := tt.RightClick("The A Team"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Add to Playlist", "Mix 25"); err == nil {
		t.Error("the menu lists every playlist")
	} else if err := tt.ChooseMenuItem("Add to Playlist", "Find a Playlist…"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	tt.Frame()
	wantTexts(t, tt, "Add to Playlist", "Mix 00")
	a.picker.filter = "mix 2 5"
	tt.Frame()
	if tt.HasText("Mix 00") {
		t.Error("the filter leaves other playlists")
	}
	click(t, tt, "Mix 25")
	if got := ps.waitFor(t, a, "POST /Playlists/m25/Items"); !strings.Contains(got, "ids=s4") {
		t.Errorf("the request was %q", got)
	}
	if a.picker.open {
		t.Error("the picker is still open")
	}
}

func TestFilterPlaylists(t *testing.T) {
	list := []*library.Playlist{{ID: "a", Name: "Driving Mix"}, {ID: "b", Name: "2000s Mix"}, {ID: "c", Name: "Café"}}
	for filter, want := range map[string]string{"": "a b c", "mix": "a b", "MIX dri": "a", "cafe": "c", "jazz": ""} {
		var got []string
		for _, pl := range filterPlaylists(list, filter) {
			got = append(got, pl.ID)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%q finds %q, want %q", filter, got, want)
		}
	}
}

// Playlists that share a name say how they differ, in the sidebar.
func TestPlaylistsOfOneName(t *testing.T) {
	a := testApp(t)
	a.settings.OthersPlaylists = true
	a.lib.SetPlaylists([]library.Playlist{
		{ID: "p1", Name: "Road trip", Songs: 2},
		{ID: "x1", Name: "2000s Mix", Songs: 50},
		{ID: "x2", Name: "2000s mix", Songs: 48, Others: true},
	})
	a.libGen++
	notes := playlistNotes(a.playlists())
	if notes["p1"] != "" || notes["x1"] != "50 songs" || notes["x2"] != "48 songs, shared" {
		t.Errorf("the notes are %q", notes)
	}
	tt := ui.NewTester(a.view, 1240, 800)
	wantTexts(t, tt, "50 songs", "48 songs, shared")
}

// A playlist's menu, in the sidebar and on its tile, plays it, queues it
// and changes it; the hover button on its tile plays it.
func TestPlaylistMenus(t *testing.T) {
	ps := &playlistServer{}
	a := testAppWith(t, ps)
	a.lib.SetPlaylists([]library.Playlist{
		{ID: "p1", Name: "Road trip", Songs: 2},
		{ID: "p2", Name: "Not sure", Songs: 1, Unsure: true},
		{ID: "p3", Name: "Somebody's mix", Songs: 3, Others: true},
	})
	a.libGen++
	a.settings.OthersPlaylists = true
	tt := ui.NewTester(a.view, 1240, 800)
	menu := func(name string) []string {
		t.Helper()
		if err := tt.RightClick(name); err != nil {
			t.Fatal(err)
		}
		m := tt.Menu()
		tt.CloseMenu()
		return m
	}
	has := func(m []string, item string) bool { return slices.Contains(m, item) }
	if m := menu("Road trip"); !has(m, "Play") || !has(m, "Shuffle") || !has(m, "Add to Queue") || !has(m, "Rename…") || !has(m, "Delete Playlist…") || !has(m, "Pin to Sidebar") {
		t.Errorf("the menu of the user's playlist is %q", m)
	}
	if m := menu("Not sure"); !has(m, "Rename…") || has(m, "Delete Playlist…") {
		t.Errorf("the menu of a playlist of an owner not known is %q", m)
	}
	if m := menu("Somebody's mix"); has(m, "Rename…") || has(m, "Delete Playlist…") || !has(m, "Play") {
		t.Errorf("the menu of another's playlist is %q", m)
	}
	if err := tt.RightClick("Road trip"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Add to Queue"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	wait(t, a, "the songs queued", func() bool { return len(a.player.queue) == 2 })
	if a.player.queue[0] != a.lib.Song("s1") {
		t.Error("the queue does not hold the library's own songs")
	}
	// The tile's play button shows under the pointer.
	a.player.queue = nil
	a.router.Push("/playlists")
	tt.Frame()
	if tt.HasText("Play Road trip") {
		t.Error("the play button shows without the pointer")
	}
	r, ok := tt.Find("2 songs") // the tile's line, under its picture
	if !ok {
		t.Fatalf("no tile; texts: %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y-40)
	tt.Frame()
	click(t, tt, "Play Road trip")
	wait(t, a, "the playlist playing", func() bool { return len(a.player.queue) == 2 })
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
	// A toast tells of it, and puts the song back.
	wait(t, a, "the toast", func() bool { tt.Frame(); return tt.HasText("Removed “Perfect” from Road trip") })
	click(t, tt, "Undo")
	if got := ps.waitFor(t, a, "POST /Playlists/p1/Items?"); !strings.Contains(got, "ids=s2") {
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

// An empty playlist tells where songs are added from.
func TestEmptyPlaylist(t *testing.T) {
	a := testAppWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Items":[],"TotalRecordCount":0}`))
	}))
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/playlist/p1")
	wait(t, a, "the playlist read", func() bool { tt.Frame(); return tt.HasText("This playlist is empty") })
	wantTexts(t, tt, "choose Add to Playlist")
	if tt.HasText("Jellyfin") {
		t.Error("the empty playlist sends the user to Jellyfin")
	}
}

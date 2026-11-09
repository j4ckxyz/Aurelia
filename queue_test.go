package main

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// albumApp is testApp with an album of fourteen songs, a1 to a14, beside
// the songs s1 to s4.
func albumApp(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	data := a.lib.Data
	data.Albums = append(slices.Clone(data.Albums), library.Album{ID: "al9", Name: "Fourteen", Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}})
	data.Songs = slices.Clone(data.Songs)
	for i := 1; i <= 14; i++ {
		data.Songs = append(data.Songs, library.Song{
			ID: "a" + strconv.Itoa(i), Name: "Song " + strconv.Itoa(i), Album: "Fourteen", AlbumID: "al9",
			Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}, AlbumArtistIDs: []string{"ar1"}, Track: i, Disc: 1, Seconds: 200,
		})
	}
	a.setLibrary(library.Build(data))
	return a
}

// albumIDs are the IDs of the album's songs from a to b.
func albumIDs(a, b int) string {
	var ids []string
	for i := a; i <= b; i++ {
		ids = append(ids, "a"+strconv.Itoa(i))
	}
	return strings.Join(ids, " ")
}

// Songs added play right after the song playing, ahead of the rest of
// the album: Play Next's first, oldest first, then Add to Queue's.
func TestAddedSongsPlayNext(t *testing.T) {
	a := albumApp(t)
	p := a.player
	p.play(a.lib.AlbumSongs("al9"), 1)
	p.enqueue(songsOf(a, "s1"))
	if got, want := queueIDs(p), "a1 a2 s1 "+albumIDs(3, 14); got != want {
		t.Fatalf("after Add to Queue: %s", got)
	}
	p.enqueue(songsOf(a, "s2"))
	p.playNext(songsOf(a, "s3"))
	p.playNext(songsOf(a, "s4"))
	if got, want := queueIDs(p), "a1 a2 s3 s4 s1 s2 "+albumIDs(3, 14); got != want {
		t.Fatalf("after Play Next twice: %s", got)
	}
	// One played, the next Play Next goes after the other's.
	p.skip()
	p.playNext(songsOf(a, "a14"))
	if got, want := queueIDs(p), "a1 a2 s3 s4 a14 s1 s2 "+albumIDs(3, 14); got != want || p.current().ID != "s3" || p.added != 4 {
		t.Errorf("after one played and a Play Next: %s, playing %s, %d added", got, p.current().ID, p.added)
	}
	// A song of the album chosen further on plays, and the songs added
	// still come next.
	p.playIndex(slices.IndexFunc(p.queue, func(s *library.Song) bool { return s.ID == "a5" }))
	if got, want := queueIDs(p), "a1 a2 s3 a3 a4 a5 s4 a14 s1 s2 "+albumIDs(6, 14); got != want || p.current().ID != "a5" {
		t.Errorf("after playing a5: %s, playing %s", got, p.current().ID)
	}
	// They show under a heading of their own in the queue's panel.
	tt := ui.NewTester(a.view, 1240, 800)
	tt.Frame()
	click(t, tt, "Queue")
	wantTexts(t, tt, "NOW PLAYING", "NEXT IN QUEUE", "NEXT UP")
}

// The rows of the queue's panel find their songs past the headings.
func TestQueueRows(t *testing.T) {
	r := queueRows{start: 3, added: 2, n: 5}
	if r.count() != 7 || !r.header(0) || !r.header(3) || r.header(1) || r.header(4) {
		t.Fatalf("rows %d, headings at 0 %v and 3 %v", r.count(), r.header(0), r.header(3))
	}
	for row, want := range []int{3, 3, 4, 5, 5, 6, 7, 8} {
		if got := r.at(row); got != want {
			t.Errorf("row %d is at %d of the queue, not %d", row, got, want)
		}
	}
	if r := (queueRows{start: 3, added: 2, n: 2}); r.count() != 3 || r.header(3) || r.at(3) != 5 {
		t.Errorf("only songs added: %d rows, the end at %d", r.count(), r.at(3))
	}
	if r := (queueRows{start: 3, n: 2}); r.count() != 2 || r.header(0) || r.at(1) != 4 {
		t.Errorf("none added: %d rows", r.count())
	}
}

// Shuffling keeps the songs played before the one playing, and the songs
// added after it; turning it off brings back the order as it was moved.
func TestShuffleKeepsTheQueue(t *testing.T) {
	a := albumApp(t)
	p := a.player
	p.play(a.lib.AlbumSongs("al9"), 1)
	p.playNext(songsOf(a, "s1"))
	p.move([]int{14}, 4) // a14 after a3
	want := "a1 a2 s1 a3 a14 " + albumIDs(4, 13)
	if got := queueIDs(p); got != want || p.added != 1 {
		t.Fatalf("after moving a14 up: %s, %d added", got, p.added)
	}
	p.setShuffle(true)
	if got := queueIDs(p); !strings.HasPrefix(got, "a1 a2 s1 ") || p.index != 1 || len(p.queue) != 15 {
		t.Errorf("shuffled: %s at %d", got, p.index)
	}
	p.playNext(songsOf(a, "s2"))
	p.enqueue(songsOf(a, "s3"))
	if got := queueIDs(p); !strings.HasPrefix(got, "a1 a2 s1 s2 s3 ") {
		t.Errorf("added while shuffled: %s", got)
	}
	p.setShuffle(false)
	if got, want := queueIDs(p), "a1 a2 s1 s2 s3 a3 a14 "+albumIDs(4, 13); got != want || p.index != 1 || p.added != 3 {
		t.Errorf("unshuffled: %s at %d, %d added", got, p.index, p.added)
	}
	// Further on, shuffling leaves the songs played, which Previous goes
	// back to.
	p.playIndex(8) // a5, which the songs added follow
	p.setShuffle(true)
	if got, want := queueIDs(p), "a1 a2 a3 a14 a4 a5 s1 s2 s3 "; !strings.HasPrefix(got, want) || p.index != 5 {
		t.Errorf("shuffled from a5: %s at %d", got, p.index)
	}
	p.previous()
	if p.current().ID != "a4" {
		t.Errorf("Previous went to %s", p.current().ID)
	}
	// An added song played while shuffled stays where it played.
	p.setShuffle(false)
	p.play(a.lib.AlbumSongs("al9"), 0)
	p.setShuffle(true)
	p.enqueue(songsOf(a, "s4"))
	p.skip()
	p.setShuffle(false)
	if got, want := queueIDs(p), "a1 s4 "+albumIDs(2, 14); got != want || p.current().ID != "s4" {
		t.Errorf("unshuffled with an added song playing: %s, playing %s", got, p.current().ID)
	}
}

// A shuffled queue comes back in the next run with the order it was
// shuffled from, and the songs added.
func TestShuffledQueueComesBack(t *testing.T) {
	a := albumApp(t)
	a.player.play(a.lib.AlbumSongs("al9"), 1)
	a.player.enqueue(songsOf(a, "s1"))
	a.player.setShuffle(true)
	shuffled := queueIDs(a.player)
	a.saveQueue()
	a.queueWrites.Wait()

	b := newApp(a.dirs, true)
	t.Cleanup(b.queueWrites.Wait)
	b.settings.Session, b.settings.Shuffle = a.settings.Session, true
	b.setClient(a.clientNow())
	b.setLibrary(library.Build(a.lib.Data))
	p := b.player
	if got := queueIDs(p); got != shuffled || p.index != 1 || p.added != 1 {
		t.Fatalf("came back as %s at %d, %d added; was %s", got, p.index, p.added, shuffled)
	}
	p.setShuffle(false)
	if got, want := queueIDs(p), "a1 a2 s1 "+albumIDs(3, 14); got != want {
		t.Errorf("unshuffled after coming back: %s", got)
	}
}

// writeQueue keeps q as the queue of the last run of a, and returns the
// app of the next run.
func writeQueue(t *testing.T, a *App, q savedQueue, lib library.Data) *App {
	t.Helper()
	q.Server = a.settings.Session.ServerID + "/" + a.settings.Session.UserID
	b, _ := json.Marshal(q)
	if err := os.WriteFile(a.dirs.queueFile(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	next := newApp(a.dirs, true)
	t.Cleanup(next.queueWrites.Wait)
	next.settings.Session = a.settings.Session
	next.setClient(a.clientNow())
	next.setLibrary(library.Build(lib))
	return next
}

// A queue kept by an earlier version, without the songs added or the
// order shuffled from, still comes back.
func TestOldQueueComesBack(t *testing.T) {
	a := testApp(t)
	b := writeQueue(t, a, savedQueue{Songs: []string{"s1", "s2", "s3"}, Index: 1, Position: 42}, a.lib.Data)
	p := b.player
	if got := queueIDs(p); got != "s1 s2 s3" || p.current().ID != "s2" || p.state().Position != 42*time.Second || p.added != 0 {
		t.Errorf("came back as %s at %d, from %v", got, p.index, p.state().Position)
	}
}

// When the song that played is gone from the library, the next that is
// not comes back in its place, from its start.
func TestQueueComesBackPastAGoneSong(t *testing.T) {
	a := testApp(t)
	lib := a.lib.Data
	lib.Songs = slices.DeleteFunc(slices.Clone(lib.Songs), func(s library.Song) bool { return s.ID == "s2" })
	b := writeQueue(t, a, savedQueue{Songs: []string{"s1", "s2", "s3", "s4"}, Index: 1, Added: 2, Position: 42}, lib)
	p := b.player
	if got := queueIDs(p); got != "s1 s3 s4" || p.current().ID != "s3" || p.added != 1 {
		t.Fatalf("came back as %s at %d, %d added", got, p.index, p.added)
	}
	if got := p.state().Position; got != 0 {
		t.Errorf("s3 comes back at %v", got)
	}
	// None after it left: the last before it.
	c := writeQueue(t, a, savedQueue{Songs: []string{"s1", "s3", "s2"}, Index: 2, Position: 42}, lib)
	if p := c.player; p.current() == nil || p.current().ID != "s3" || p.state().Position != 0 {
		t.Errorf("came back at %d, from %v", p.index, p.state().Position)
	}
}

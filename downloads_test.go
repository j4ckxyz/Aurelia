package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// countText returns how many texts or labels of the last frame are s.
func countText(tt *ui.Tester, s string) int {
	n := 0
	for _, t := range tt.Texts() {
		if t == s {
			n++
		}
	}
	return n
}

// A song downloaded has one quiet mark, the same on its row and on the
// album's head; a download on its way and one that failed show as such,
// and the page of downloads, where all are, leaves the mark out.
func TestDownloadMarks(t *testing.T) {
	a := testApp(t)
	d := a.downloads
	tt := ui.NewTester(a.view, 1240, 800)
	songs := a.lib.AlbumSongs("al1")
	d.albums["al1"] = true
	for _, s := range songs {
		d.songs[s.ID], d.done[s.ID] = s, true
	}
	a.router.Push("/album/al1")
	tt.Frame()
	if n := countText(tt, "Downloaded"); n != len(songs) {
		t.Errorf("%d songs downloaded show %d marks", len(songs), n)
	}
	wantTexts(t, tt, "Downloaded: click to remove")

	// On the page of downloads, the songs here have no mark.
	a.router.Push("/downloads")
	tt.Frame()
	if n := countText(tt, "Downloaded"); n != 0 {
		t.Errorf("the page of downloads shows %d marks", n)
	}

	// One waits its turn, then fails: both show, there and on the album.
	s := songs[1]
	delete(d.done, s.ID)
	d.queued[s.ID] = true
	a.favGen++
	tt.Frame()
	if countText(tt, "Downloading") != 1 {
		t.Errorf("a song waiting shows no ring; texts: %q", tt.Texts())
	}
	delete(d.queued, s.ID)
	d.failed[s.ID] = "The server is away"
	a.favGen++
	tt.Frame()
	if countText(tt, "Could not download") != 1 {
		t.Errorf("a song that failed shows no warning; texts: %q", tt.Texts())
	}
	if st, _ := d.group(songs); st != dlFailed {
		t.Errorf("the album with a song that failed is in state %d", st)
	}
	a.router.Push("/album/al1")
	tt.Frame()
	wantTexts(t, tt, "Could not download", "Some songs could not be downloaded: click to try again")
	if n := countText(tt, "Downloaded"); n != len(songs)-1 {
		t.Errorf("%d marks of songs downloaded, want %d", n, len(songs)-1)
	}
}

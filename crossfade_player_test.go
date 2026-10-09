package main

import (
	"testing"

	"aurelia/internal/library"
)

func TestFollowsOnAlbum(t *testing.T) {
	song := func(id, album string, disc, track int) *library.Song {
		return &library.Song{ID: id, AlbumID: album, Disc: disc, Track: track}
	}
	a1, a2, a3 := song("1", "A", 1, 1), song("2", "A", 1, 2), song("3", "A", 1, 4)
	b1 := song("4", "B", 1, 1)
	d2 := song("5", "A", 2, 1)
	for name, tc := range map[string]struct {
		cur, next *library.Song
		want      bool
	}{
		"the next track of the album":      {a1, a2, true},
		"a track skipped over":             {a2, a3, false},
		"another album":                    {a1, b1, false},
		"the same song again (repeat one)": {a1, a1, true},
		"the first of the next disc":       {a2, d2, true}, // whether it was the disc's last is not known
		"nothing playing":                  {nil, a1, false},
		"songs with no album":              {song("6", "", 0, 0), song("7", "", 0, 1), false},
	} {
		if got := followsOnAlbum(tc.cur, tc.next); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
	last := song("8", "A", 1, 12)
	if !followsOnAlbum(last, d2) {
		t.Error("the last track of a disc to the first of the next is a continuation")
	}
}

func TestCrossfadeNames(t *testing.T) {
	for _, n := range crossfadeChoices {
		if got := crossfadeSecs(crossfadeName(n)); got != n {
			t.Errorf("%d seconds went through its name as %d", n, got)
		}
	}
	if crossfadeSecs("nonsense") != 0 {
		t.Error("an unknown name is not off")
	}
}

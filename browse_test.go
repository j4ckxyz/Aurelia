package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFacets(t *testing.T) {
	a := testApp(t)
	for i := range a.lib.Albums {
		al := &a.lib.Albums[i]
		switch i % 3 {
		case 0:
			al.Genres, al.Year = []string{"Pop", "R&B/Soul"}, 2016
		case 1:
			al.Genres, al.Year = []string{"pop"}, 1994
		default:
			al.Genres, al.Year = []string{"Hip Hop"}, 2019
		}
	}
	a.libGen++
	f := a.facetsOf()
	names := func(list []facet) (out []string) {
		for _, x := range list {
			out = append(out, x.name)
		}
		return
	}
	for in, want := range map[string]string{"R&B/Soul": "r-b-soul", "Hip Hop": "hip-hop", "  Rock ": "rock", "!!!": "other", "Électro": "électro"} {
		if got := slugOf(in); got != want {
			t.Errorf("slug of %q is %q, want %q", in, got, want)
		}
	}
	// Pop and pop are one genre; the one with the most albums leads.
	if len(f.genres) == 0 || f.genres[0].name != "Pop" {
		t.Fatalf("the genres are %v", names(f.genres))
	}
	for _, g := range f.genres {
		if g.name == "pop" {
			t.Errorf("one genre in two cases: %v", names(f.genres))
		}
	}
	if d := names(f.decades); len(d) != 2 || d[0] != "2010s" || d[1] != "1990s" {
		t.Errorf("the decades are %v", d)
	}
}

func TestBrowsePages(t *testing.T) {
	a := testApp(t)
	for i := range a.lib.Albums {
		a.lib.Albums[i].Genres = []string{"R&B/Soul", "Hip Hop"}
		a.lib.Albums[i].Year = 2016
	}
	a.libGen++
	tt := ui.NewTester(a.view, 1240, 800)
	a.router.Push("/genres")
	tt.Frame()
	wantTexts(t, tt, "Browse", "Genres", "R&B/Soul", "Hip Hop", "Decades", "2010s")
	if err := tt.Click("R&B/Soul"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.router.Path(); got != "/genre/r-b-soul" {
		t.Errorf("the path is %q", got)
	}
	al := a.lib.Albums[0]
	wantTexts(t, tt, "R&B/Soul", al.Name)
	// A decade.
	a.router.Push("/genres")
	tt.Frame()
	if err := tt.Click("2010s"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	wantTexts(t, tt, "2010s", al.Name)
	// A genre that is not there.
	a.router.Push("/genre/Polka")
	tt.Frame()
	wantTexts(t, tt, "Nothing here")
	// The sidebar leads there, and keeps the section lit inside it.
	if sectionOf("/genre/hip-hop") != "/genres" || sectionOf("/decade/1990") != "/genres" {
		t.Error("the pages of genres are not of the Browse section")
	}
}

func TestPlayedSorts(t *testing.T) {
	a := testApp(t)
	songs := a.lib.Songs
	if len(songs) < 3 {
		t.Skip("too few songs")
	}
	songs[0].Plays, songs[0].LastPlayed = 3, 1000
	songs[1].Plays, songs[1].LastPlayed = 1, 2000
	for i := 2; i < len(songs); i++ {
		songs[i].Plays, songs[i].LastPlayed = 0, 0
	}
	a.libGen++
	recent := a.songsBy("Recently Played")
	if len(recent) != 2 || recent[0].ID != songs[1].ID || recent[1].ID != songs[0].ID {
		t.Errorf("recently played: %v", recent)
	}
	never := a.songsBy("Never Played")
	if len(never) != len(songs)-2 {
		t.Errorf("never played: %d songs, want %d", len(never), len(songs)-2)
	}
	for _, s := range never {
		if s.Plays > 0 {
			t.Errorf("%q was played", s.Name)
		}
	}
	// A song that plays moves to the head of the list as it ends.
	songs[2].Plays, songs[2].LastPlayed = 1, 3000
	a.favGen++
	if got := a.songsBy("Recently Played"); len(got) != 3 || got[0].ID != songs[2].ID {
		t.Errorf("after a play: %v", got)
	}
}

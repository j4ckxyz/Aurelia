package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestListeningPage(t *testing.T) {
	a := testApp(t)
	for i := range a.lib.Songs {
		a.lib.Songs[i].Plays = 0
	}
	tt := ui.NewTester(a.view, 1240, 900)
	a.router.Push("/listening")
	tt.Frame()
	wantTexts(t, tt, "Nothing played yet")
	songs := a.lib.Songs
	songs[0].Plays, songs[0].Seconds = 5, 100
	songs[1].Plays, songs[1].Seconds = 2, 200
	a.libGen++
	l := a.listeningOf()
	if l.plays != 7 || l.time.Seconds() != 900 {
		t.Errorf("%d plays for %v, want 7 for 900 s", l.plays, l.time)
	}
	if len(l.songs) != 2 || l.songs[0] != &songs[0] {
		t.Errorf("the songs are %v", l.songs)
	}
	tt.Frame()
	wantTexts(t, tt, "Your listening", "7 plays · 15 min", "Songs you play most", songs[0].Name, "Albums", "Artists")
}

func TestSimilarArtistsOnTheirPage(t *testing.T) {
	a := testAppWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Similar") {
			w.Write([]byte(`{"Items":[{"Id":"ar2"},{"Id":"ghost"}],"TotalRecordCount":2}`))
			return
		}
		http.NotFound(w, r)
	}))
	tt := ui.NewTester(a.view, 1240, 1200)
	a.router.Push("/artist/ar1")
	tt.Frame()
	wait(t, a, "similar artists", func() bool { return len(a.similarArtists("ar1")) > 0 })
	tt.Frame()
	wantTexts(t, tt, "Fans also like", a.lib.Artist("ar2").Name)
	if got := a.similarArtists("ar1"); len(got) != 1 {
		t.Errorf("artists the library lacks were shown: %v", got)
	}
}

package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// The frames of every page of a real library, when AURELIA_TEST_LIBRARY
// names the file of one (a library-*.json.gz of the app's data
// directory): the first frame of a page, which sorts and lays out, and
// the frames after it, as scrolling builds them. A display of 120 Hz
// leaves a frame 8 ms. The frames here are drawn on the CPU, which the
// app leaves to the GPU, so only the first frame of a page is held to a
// limit; "scroll albums 200" of the debug hook times the app's own
// frames, with MYGO_FRAME_STATS=all. Run it alone (go test -p 1, or
// -run FrameTimes): other packages' tests on the same cores slow it.
func TestFrameTimes(t *testing.T) {
	path := os.Getenv("AURELIA_TEST_LIBRARY")
	if path == "" {
		t.Skip("AURELIA_TEST_LIBRARY names no library")
	}
	a := testApp(t)
	l, err := library.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a.setLibrary(l)
	tt := ui.NewTester(a.view, 1240, 800)
	album, artist := l.Albums[len(l.Albums)/2].ID, ""
	for i := range l.Artists {
		if n := len(l.ArtistAlbums(l.Artists[i].ID)); n > 15 {
			artist = l.Artists[i].ID
		}
	}
	t.Logf("%d albums, %d artists, %d songs", len(l.Albums), len(l.Artists), len(l.Songs))
	for _, path := range []string{"/home", "/albums", "/album/" + album, "/artists", "/artist/" + artist, "/songs", "/favorites", "/playlists", "/search?q=love", "/search?q=the+a", "/settings", "/home", "/albums", "/songs"} {
		start := time.Now()
		a.router.Push(path)
		tt.Frame()
		first := time.Since(start)
		// Scrolling: a frame for each step of the wheel.
		const steps = 60
		var worst time.Duration
		start = time.Now()
		for i := 0; i < steps; i++ {
			s := time.Now()
			tt.Scroll(700, 400, 0, 90)
			worst = max(worst, time.Since(s))
		}
		avg := time.Since(start) / steps
		line := fmt.Sprintf("%-18s first frame %6.2f ms; scrolling %5.2f ms a frame, the worst %5.2f ms", path[:min(len(path), 18)], ms(first), ms(avg), ms(worst))
		t.Log(line)
		if first > 16*time.Millisecond {
			t.Errorf("%s is slow: %s", path, line)
		}
	}
	// Searching as letters come.
	a.router.Push("/search?q=")
	tt.Frame()
	for _, q := range []string{"l", "lo", "lov", "love", "love y", "love yo", "love you"} {
		start := time.Now()
		a.search.query = q
		a.router.Replace("/search?q=" + q)
		tt.Frame()
		t.Logf("search %-10q %5.2f ms", q, ms(time.Since(start)))
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

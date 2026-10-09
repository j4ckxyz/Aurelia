package main

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A server that answers instant mixes with songs it picks.
func mixServer(t *testing.T, mix string, asked *[]string, mu *sync.Mutex) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/InstantMix") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		*asked = append(*asked, r.URL.Path)
		mu.Unlock()
		w.Write([]byte(`{"Items":[` + mix + `],"TotalRecordCount":3}`))
	})
}

func TestStartRadio(t *testing.T) {
	var asked []string
	var mu sync.Mutex
	// The server leaves the seed out of the middle, and names a song the
	// library does not have.
	a := testAppWith(t, mixServer(t, `{"Id":"s3"},{"Id":"nope"},{"Id":"s1"},{"Id":"s4"}`, &asked, &mu))
	tt := ui.NewTester(a.view, 1240, 800)
	tt.Frame()
	seed := a.lib.Song("s1")
	a.startRadio("Songs", seed.ID, seed.Name, seed)
	wait(t, a, "the radio", func() bool { return len(a.player.queue) > 0 })
	var ids []string
	for _, s := range a.player.queue {
		ids = append(ids, s.ID)
	}
	if got := strings.Join(ids, ","); got != "s1,s3,s4" {
		t.Errorf("the radio plays %s, want the seed first and the library's own: s1,s3,s4", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0] != "/Songs/s1/InstantMix" {
		t.Errorf("the server was asked %v", asked)
	}
}

func TestSimilarWhenTheQueueEnds(t *testing.T) {
	var asked []string
	var mu sync.Mutex
	a := testAppWith(t, mixServer(t, `{"Id":"s2"},{"Id":"s3"},{"Id":"s4"}`, &asked, &mu))
	p := a.player
	p.queue = songsOf(a, "s1")
	p.unshuffled = p.queue
	p.index = 0
	// Off: nothing is asked.
	p.armNext()
	mu.Lock()
	n := len(asked)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("the server was asked with the setting off: %v", asked)
	}
	a.settings.Autoplay = true
	p.armNext()
	wait(t, a, "songs like the last", func() bool { return len(p.queue) > 1 })
	if got := len(p.queue); got != 4 {
		t.Errorf("the queue has %d songs, want the song and three like it", got)
	}
	// Asked once for that song, however often the queue is looked at.
	p.armNext()
	p.armNext()
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 {
		t.Errorf("asked %d times: %v", len(asked), asked)
	}
	// The sleep timer's stop is not the queue's end.
	a.settings.Autoplay = true
}

// The sleep timer wins: the queue may grow with songs like the last, but
// the music stops at the end of the song all the same.
func TestSimilarDoesNotGetRoundTheSleepTimer(t *testing.T) {
	var asked []string
	var mu sync.Mutex
	a := testAppWith(t, mixServer(t, `{"Id":"s2"},{"Id":"s3"}`, &asked, &mu))
	p := a.player
	a.settings.Autoplay = true
	p.queue = songsOf(a, "s1")
	p.unshuffled = p.queue
	p.index = 0
	p.setSleep(sleepSong, 0)
	wait(t, a, "songs like the last", func() bool { return len(p.queue) > 1 })
	if got := p.following(); got != -1 {
		t.Errorf("the music would go on to song %d in spite of the timer", got)
	}
	p.sleepEnded()
	if got := p.following(); got != 1 {
		t.Errorf("after the timer, the song to follow is %d, want 1", got)
	}
}

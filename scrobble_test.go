package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newServer starts a server for the test.
func newServer(t *testing.T, h http.Handler) *httptest.Server {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// feed plays a song for secs seconds of the scrobbler's clock, a second at
// a time, and returns what it asked for.
func feed(sc *scrobbler, at *time.Time, id string, dur time.Duration, from time.Duration, secs int, playing bool) (acts []scrobbleAct) {
	for i := 0; i < secs; i++ {
		*at = at.Add(time.Second)
		pos := from + time.Duration(i+1)*time.Second
		if !playing {
			pos = from
		}
		if act := sc.step(*at, id, dur, pos, playing); act != scrobbleNothing {
			acts = append(acts, act)
		}
	}
	return
}

func TestScrobbleCountsHalfTheSong(t *testing.T) {
	var sc scrobbler
	now := time.Now()
	acts := feed(&sc, &now, "a", 200*time.Second, 0, 99, true)
	if len(acts) != 1 || acts[0] != scrobbleStarted {
		t.Fatalf("in the first half the acts were %v, want the start only", acts)
	}
	acts = feed(&sc, &now, "a", 200*time.Second, 99*time.Second, 5, true)
	if len(acts) != 1 || acts[0] != scrobbleHeard {
		t.Errorf("past half the acts were %v, want it heard once", acts)
	}
	if acts = feed(&sc, &now, "a", 200*time.Second, 104*time.Second, 50, true); len(acts) != 0 {
		t.Errorf("sent again: %v", acts)
	}
}

func TestScrobbleFourMinutesOfALongSong(t *testing.T) {
	var sc scrobbler
	now := time.Now()
	feed(&sc, &now, "long", 20*time.Minute, 0, 239, true)
	if acts := feed(&sc, &now, "long", 20*time.Minute, 239*time.Second, 2, true); len(acts) != 1 || acts[0] != scrobbleHeard {
		t.Errorf("at four minutes the acts were %v", acts)
	}
}

func TestScrobbleSkippedAndShortSongs(t *testing.T) {
	var sc scrobbler
	now := time.Now()
	// Skipped after twenty seconds: the start was told, the play is not.
	acts := feed(&sc, &now, "a", 200*time.Second, 0, 20, true)
	acts = append(acts, feed(&sc, &now, "b", 200*time.Second, 0, 20, true)...)
	for _, a := range acts {
		if a == scrobbleHeard {
			t.Error("a song skipped early was counted")
		}
	}
	if len(acts) != 2 {
		t.Errorf("each song's start is told: %v", acts)
	}
	// Under thirty seconds long never counts.
	sc = scrobbler{}
	for _, a := range feed(&sc, &now, "jingle", 20*time.Second, 0, 20, true) {
		if a == scrobbleHeard {
			t.Error("a song under thirty seconds was counted")
		}
	}
}

func TestScrobbleCountsOnlyWhatPlays(t *testing.T) {
	var sc scrobbler
	now := time.Now()
	feed(&sc, &now, "a", 100*time.Second, 0, 30, true)
	// Paused for ten minutes: nothing is heard.
	if acts := feed(&sc, &now, "a", 100*time.Second, 30*time.Second, 600, false); len(acts) != 0 {
		t.Errorf("paused: %v", acts)
	}
	if sc.played > 31*time.Second {
		t.Errorf("heard %v while it was paused", sc.played)
	}
	// A window that stopped for a minute adds no more than three seconds.
	now = now.Add(time.Minute)
	sc.step(now, "a", 100*time.Second, 31*time.Second, true)
	if sc.played > 35*time.Second {
		t.Errorf("a stall counted for %v", sc.played)
	}
}

func TestScrobbleRepeatCountsAgain(t *testing.T) {
	var sc scrobbler
	now := time.Now()
	feed(&sc, &now, "a", 60*time.Second, 0, 40, true) // heard, and sent
	acts := feed(&sc, &now, "a", 60*time.Second, 0, 40, true)
	var started, heard int
	for _, a := range acts {
		if a == scrobbleStarted {
			started++
		} else if a == scrobbleHeard {
			heard++
		}
	}
	if started != 1 || heard != 1 {
		t.Errorf("a song played again: started %d, heard %d", started, heard)
	}
}

// listenBrainz is a server that keeps what it is sent.
type listenBrainz struct {
	mu   sync.Mutex
	got  []map[string]any
	auth []string
}

func (lb *listenBrainz) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.auth = append(lb.auth, r.Header.Get("Authorization"))
	switch r.URL.Path {
	case "/1/validate-token":
		if r.Header.Get("Authorization") != "Token good" {
			w.Write([]byte(`{"valid":false,"message":"Invalid token."}`))
			return
		}
		w.Write([]byte(`{"valid":true,"user_name":"robin"}`))
	case "/1/submit-listens":
		if r.Header.Get("Authorization") != "Token good" {
			http.Error(w, `{"code":401,"error":"Invalid authorization token."}`, 401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		lb.got = append(lb.got, m)
		w.Write([]byte(`{"status":"ok"}`))
	default:
		http.NotFound(w, r)
	}
}

func (lb *listenBrainz) count() int {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return len(lb.got)
}

func TestSendingListens(t *testing.T) {
	lb := &listenBrainz{}
	srv := newServer(t, lb)
	a := testApp(t)
	a.settings.Scrobble, a.settings.ListenBrainzToken, a.settings.ListenBrainzURL = true, "good", srv.URL
	s := a.lib.Song("s1")
	at := int64(1700000000)
	a.sendListen("playing_now", listenOf(s, 0))
	wait(t, a, "the song playing now", func() bool { return lb.count() == 1 })
	a.sendListen("single", listenOf(s, at))
	wait(t, a, "the listen", func() bool { return lb.count() == 2 })
	wait(t, a, "the listen to be forgotten", func() bool { return len(a.scrobble.pending) == 0 })

	lb.mu.Lock()
	defer lb.mu.Unlock()
	now, single := lb.got[0], lb.got[1]
	if now["listen_type"] != "playing_now" {
		t.Errorf("the first is %v", now["listen_type"])
	}
	if _, has := now["payload"].([]any)[0].(map[string]any)["listened_at"]; has {
		t.Error("a song playing now has a time it was listened at")
	}
	if single["listen_type"] != "single" {
		t.Errorf("the second is %v", single["listen_type"])
	}
	p := single["payload"].([]any)[0].(map[string]any)
	meta := p["track_metadata"].(map[string]any)
	if p["listened_at"].(float64) != float64(at) || meta["track_name"] != s.Name || meta["artist_name"] != s.Artist || meta["release_name"] != s.Album {
		t.Errorf("the listen is %v", p)
	}
	info := meta["additional_info"].(map[string]any)
	if info["media_player"] != "Aurelia" || info["duration_ms"].(float64) <= 0 {
		t.Errorf("its additional info is %v", info)
	}
	for _, auth := range lb.auth {
		if auth != "Token good" {
			t.Errorf("the authorization was %q", auth)
		}
	}
}

func TestListensAreKeptWhenTheServerFails(t *testing.T) {
	lb := &listenBrainz{}
	srv := newServer(t, lb)
	a := testApp(t)
	a.settings.Scrobble, a.settings.ListenBrainzToken, a.settings.ListenBrainzURL = true, "wrong", srv.URL
	s := a.lib.Song("s1")
	a.sendListen("single", listenOf(s, 1700000000))
	wait(t, a, "the failure", func() bool { return a.scrobble.err != "" })
	if len(a.scrobble.pending) != 1 {
		t.Fatalf("the listen was not kept: %d pending", len(a.scrobble.pending))
	}
	if !strings.Contains(a.scrobble.err, "Invalid authorization token") {
		t.Errorf("the error is %q", a.scrobble.err)
	}
	// With the right token the kept listen goes, together with a new one.
	a.settings.ListenBrainzToken = "good"
	a.sendListen("single", listenOf(a.lib.Song("s2"), 1700000100))
	wait(t, a, "both listens", func() bool { return lb.count() == 1 && len(a.scrobble.pending) == 0 })
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.got[0]["listen_type"] != "import" || len(lb.got[0]["payload"].([]any)) != 2 {
		t.Errorf("the batch was %v", lb.got[0])
	}
}

func TestCheckingTheToken(t *testing.T) {
	lb := &listenBrainz{}
	srv := newServer(t, lb)
	a := testApp(t)
	a.settings.ListenBrainzURL = srv.URL
	a.settings.ListenBrainzToken = "bad"
	a.checkListenBrainz()
	wait(t, a, "the check", func() bool { return !a.scrobble.checking })
	if a.scrobble.user != "" || a.scrobble.checkErr == "" {
		t.Errorf("a bad token: user %q, error %q", a.scrobble.user, a.scrobble.checkErr)
	}
	a.settings.ListenBrainzToken = "good"
	a.checkListenBrainz()
	wait(t, a, "the check", func() bool { return !a.scrobble.checking })
	if a.scrobble.user != "robin" {
		t.Errorf("the user is %q", a.scrobble.user)
	}
}

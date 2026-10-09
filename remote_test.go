package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
)

// farServer is a server with another device signed in: it lists the
// device, takes the orders sent to it, and lets a test change what the
// device plays.
type farServer struct {
	mu      sync.Mutex
	orders  []string
	playing map[string]any // the device's session, as the server tells it
}

func (s *farServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Method == "GET" && r.URL.Path == "/Sessions":
		session := map[string]any{
			"Id": "sess2", "Client": "Jellyfin Web", "DeviceName": "Kitchen", "DeviceId": "other",
			"SupportsRemoteControl": true, "SupportsMediaControl": true, "PlayableMediaTypes": []string{"Audio", "Video"},
		}
		for k, v := range s.playing {
			session[k] = v
		}
		json.NewEncoder(w).Encode([]any{
			session,
			// Not listed: this device itself, and one that takes no orders.
			map[string]any{"Id": "me", "Client": "Aurelia", "DeviceName": "Here", "DeviceId": "d", "SupportsRemoteControl": true, "SupportsMediaControl": true},
			map[string]any{"Id": "tv", "Client": "Old TV", "DeviceName": "TV", "DeviceId": "tv", "SupportsRemoteControl": false},
		})
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/Sessions/sess2/"):
		body, _ := io.ReadAll(r.Body)
		order := strings.TrimPrefix(r.URL.Path, "/Sessions/sess2/")
		if q := r.URL.RawQuery; q != "" {
			order += "?" + q
		}
		if len(body) > 0 {
			order += " " + string(body)
		}
		s.orders = append(s.orders, order)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *farServer) took() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.orders
	s.orders = nil
	return out
}

func (s *farServer) plays(v map[string]any) {
	s.mu.Lock()
	s.playing = v
	s.mu.Unlock()
}

// Another device plays in this computer's place: what plays here goes
// there, the player shows what the device does, and the buttons reach it.
func TestPlayOnAnotherDevice(t *testing.T) {
	srv := &farServer{}
	a := testAppWith(t, srv)
	tt := ui.NewTester(a.view, 1240, 800)
	tt.Frame()
	a.refreshDevices()
	wait(t, a, "the devices", func() bool { return len(a.remote.devices) > 0 })
	if len(a.remote.devices) != 1 || a.remote.devices[0].Title() != "Kitchen · Jellyfin Web" {
		t.Fatalf("the devices: %+v", a.remote.devices)
	}
	orders := func(what string, want ...string) {
		t.Helper()
		var got []string
		wait(t, a, what, func() bool {
			got = append(got, srv.took()...)
			return len(got) >= len(want)
		})
		for i, w := range want {
			if i >= len(got) || !strings.Contains(got[i], w) {
				t.Errorf("%s: the device was sent %q, want %q", what, got, want)
				return
			}
		}
	}

	// What plays here goes on there.
	a.player.play(a.lib.AlbumSongs("al1"), 1)
	a.playOn(a.remote.devices[0])
	orders("handing over", "Playing?itemIds=s1%2Cs2&playCommand=PlayNow&startIndex=1")
	tt.Frame()
	wantTexts(t, tt, "Playing on Kitchen · Jellyfin Web", "Play here", "Perfect")
	if !a.player.playing() || a.player.far == nil {
		t.Fatalf("after handing over: playing %v, far %v", a.player.playing(), a.player.far)
	}

	// The device tells what it does: another song, paused, at its place.
	a.player.far.quiet = time.Time{}
	srv.plays(map[string]any{
		"NowPlayingItem":  map[string]any{"Id": "s1", "Name": "Shape of You", "Type": "Audio"},
		"PlayState":       map[string]any{"PositionTicks": 420_000_000, "IsPaused": true, "VolumeLevel": 35},
		"NowPlayingQueue": []map[string]string{{"Id": "s1", "PlaylistItemId": "a"}, {"Id": "s2", "PlaylistItemId": "b"}},
		"PlaylistItemId":  "a",
	})
	a.pollFar()
	wait(t, a, "the device's song", func() bool { s := a.player.current(); return s != nil && s.ID == "s1" })
	st := a.player.state()
	if !st.Paused || st.Position != 42*time.Second || a.player.far.volume != 35 {
		t.Errorf("mirrored: paused %v at %v, volume %d", st.Paused, st.Position, a.player.far.volume)
	}

	// The buttons are the device's.
	a.player.toggle()
	orders("play", "Playing/Unpause")
	a.player.skip()
	orders("next", "Playing/NextTrack")
	a.player.seek(90 * time.Second)
	orders("seek", "Playing/Seek?seekPositionTicks=900000000")
	if got := a.player.state().Position; got < 90*time.Second || got > 92*time.Second {
		t.Errorf("after seeking: at %v", got)
	}
	a.setVolume(0.6)
	orders("volume", `Command {"Arguments":{"Volume":"60"},"Name":"SetVolume"}`)
	a.player.cycleRepeat()
	orders("repeat", `"RepeatMode":"RepeatAll"`)
	a.player.enqueue(a.lib.AlbumSongs("al2"))
	orders("add to the queue", "Playing?itemIds=s3&playCommand=PlayLast")
	if a.settings.Volume == 0.6 {
		t.Error("the device's volume became this computer's")
	}

	// Back here: the device stops, and its queue waits at its place.
	a.playHere(false)
	orders("coming back", "Playing/Stop")
	if a.player.far != nil || a.player.current().ID != "s1" || !a.player.cold {
		t.Errorf("back here: far %v, song %v, cold %v", a.player.far, a.player.current(), a.player.cold)
	}
	tt.Frame()
	if tt.HasText("Play here") {
		t.Error("the strip of the device still shows")
	}
}

// This computer does what another device asks of it.
func TestOrdersFromAnotherDevice(t *testing.T) {
	a := testApp(t)
	p := a.player
	a.obey(jellyfin.Order{Kind: "Play", ItemIDs: []string{"s1", "s2", "s3"}, StartIndex: 1, PlayCommand: "PlayNow"})
	if s := p.current(); s == nil || s.ID != "s2" || len(p.queue) != 3 || !p.playing() {
		t.Fatalf("after Play: %v, %d in the queue", s, len(p.queue))
	}
	a.obey(jellyfin.Order{Kind: "Playstate", Command: "Pause"})
	a.obey(jellyfin.Order{Kind: "Playstate", Command: "Pause"}) // twice is still paused
	if p.playing() {
		t.Error("Pause did not pause")
	}
	a.obey(jellyfin.Order{Kind: "Playstate", Command: "Unpause"})
	a.obey(jellyfin.Order{Kind: "Playstate", Command: "NextTrack"})
	if !p.playing() || p.current().ID != "s3" {
		t.Errorf("after Unpause and NextTrack: playing %v, %v", p.playing(), p.current())
	}
	a.obey(jellyfin.Order{Kind: "GeneralCommand", Command: "SetVolume", Arguments: map[string]string{"Volume": "25"}})
	a.obey(jellyfin.Order{Kind: "GeneralCommand", Command: "SetRepeatMode", Arguments: map[string]string{"RepeatMode": "RepeatOne"}})
	a.obey(jellyfin.Order{Kind: "GeneralCommand", Command: "ToggleMute"})
	if a.settings.Volume != 0.25 || a.settings.Repeat != repeatOne || !a.settings.Muted {
		t.Errorf("volume %v, repeat %d, muted %v", a.settings.Volume, a.settings.Repeat, a.settings.Muted)
	}
	// An album stands for its songs, and PlayLast adds them.
	a.obey(jellyfin.Order{Kind: "Play", ItemIDs: []string{"al3"}, PlayCommand: "PlayLast"})
	if len(p.queue) != 4 || p.queue[3].ID != "s4" {
		t.Errorf("after PlayLast of an album: %s", queueIDs(p))
	}
	a.obey(jellyfin.Order{Kind: "Playstate", Command: "Stop"})
	if p.playing() {
		t.Error("Stop did not stop")
	}
}

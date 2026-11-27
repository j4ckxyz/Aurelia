package main

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/audio"
	"aurelia/internal/jellyfin"
)

// streams is a server that has every song, whose file says which it is.
var streams = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if id, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/Audio/"), "/Lyrics"); ok {
		io.WriteString(w, `{"Lyrics":[{"Text":"the words of `+id+`","Start":10000000}]}`)
		return
	}
	if id, ok := strings.CutPrefix(r.URL.Path, "/Audio/"); ok {
		id = strings.TrimSuffix(id, "/universal")
		io.WriteString(w, "the file of "+id+strings.Repeat(".", 100_000))
		return
	}
	http.NotFound(w, r)
})

// both waits for cond, running what the apps were asked to.
func both(t *testing.T, apps []*App, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(2 * time.Millisecond) {
		for _, a := range apps {
			a.drain()
		}
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited in vain for %s", what)
		}
	}
}

// guestOf is an app of a server of its own, named name, in host's Jam.
func guestOf(t *testing.T, host *App, name, server string) *App {
	t.Helper()
	g := testAppWith(t, streams)
	g.settings.Session.ServerID, g.settings.Session.UserName = server, name
	g.settings.Jam = true
	h := host.jam.host
	g.joinJam("127.0.0.1:"+strconv.Itoa(h.Port()), h.Code())
	both(t, []*App{host, g}, name+" in the Jam", func() bool { return g.jam.guest != nil && len(host.jam.members) > 0 })
	t.Cleanup(func() { g.stopJam() })
	return g
}

func readAll(t *testing.T, open func() (*audio.File, error)) string {
	t.Helper()
	f, err := open()
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	defer f.Close()
	done := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(f)
		done <- b
	}()
	select {
	case b := <-done:
		return string(b)
	case <-time.After(10 * time.Second):
		t.Fatal("the file did not come")
	}
	return ""
}

// A Jam between two servers: the guest plays the host's songs through
// the host, adds its own, which the host gets from it, and asks the host
// to pause; its own queue comes back when it leaves.
func TestJamAcrossServers(t *testing.T) {
	host := testAppWith(t, streams)
	host.settings.Jam = true
	host.settings.Session.UserName = "Ada"
	host.startJam()
	if host.jam.host == nil {
		t.Fatal("the Jam did not start")
	}
	t.Cleanup(host.stopJam)

	guest := testAppWith(t, streams)
	guest.settings.Session.ServerID, guest.settings.Session.UserName = "srv2", "Bo"
	guest.settings.Jam = true
	// What the guest had: it comes back.
	guest.player.play(guest.lib.AlbumSongs("al2"), 0)
	h := host.jam.host
	guest.joinJam("127.0.0.1:"+strconv.Itoa(h.Port()), h.Code())
	apps := []*App{host, guest}
	both(t, apps, "the guest in", func() bool { return guest.jam.guest != nil && len(host.jam.members) == 1 })
	t.Cleanup(guest.stopJam)

	// The host plays: the guest plays the same, a song of another server.
	host.player.play(host.lib.AlbumSongs("al1"), 1)
	host.jamPublish()
	both(t, apps, "the host's song", func() bool { s := guest.player.current(); return s != nil && s.Name == "Perfect" })
	gp := guest.player
	cur := gp.current()
	if !guest.foreign(cur) || guest.songKey(cur) != "srv/s2" || len(gp.queue) != 2 || gp.index != 1 || !gp.playing() {
		t.Fatalf("the guest's queue: %d songs, at %d, key %s, playing %v", len(gp.queue), gp.index, guest.songKey(cur), gp.playing())
	}
	if guest.lib.Song("s2") == cur {
		t.Error("a song of another server is taken for the library's own")
	}
	// It plays through the host, from the host's server.
	if got := readAll(t, gp.trackOf(cur).Open); !strings.HasPrefix(got, "the file of s2") {
		t.Errorf("the host's song, through the host: %.20q", got)
	}

	// Its lyrics come the same way.
	guest.lyricLines(cur)
	var lines []jellyfin.LyricLine
	both(t, apps, "the lyrics", func() bool {
		var known bool
		lines, known = guest.lyricLines(cur)
		return known
	})
	if len(lines) != 1 || lines[0].Text != "the words of s2" || lines[0].Start != time.Second {
		t.Errorf("the lyrics through the host: %+v", lines)
	}

	// The guest adds a song of its own: the host plays it from the guest.
	gp.enqueue(songsOf(guest, "s4"))
	both(t, apps, "the guest's song in the host's queue", func() bool { return len(host.player.queue) == 3 })
	added := host.player.queue[2]
	if !host.foreign(added) || host.songKey(added) != "srv2/s4" || host.jam.by[added] != "Bo" || host.player.added != 1 {
		t.Fatalf("added: %s by %q, foreign %v, %d added", host.songKey(added), host.jam.by[added], host.foreign(added), host.player.added)
	}
	if got := readAll(t, host.player.trackOf(added).Open); !strings.HasPrefix(got, "the file of s4") {
		t.Errorf("the guest's song, from the guest: %.20q", got)
	}
	both(t, apps, "the guest told of its song", func() bool { return len(gp.queue) == 3 })
	if s := gp.queue[2]; guest.foreign(s) || s != guest.lib.Song("s4") || guest.jam.by[s] != "Bo" {
		t.Errorf("the guest's own song in the Jam: foreign %v, by %q", guest.foreign(s), guest.jam.by[s])
	}

	// The guest pauses everyone.
	gp.toggle()
	both(t, apps, "paused", func() bool { return host.player.paused && gp.paused })

	// A Jam whose host keeps control: the guest's skip is refused.
	host.settings.JamLocked = true
	host.jamPublish()
	both(t, apps, "control kept", func() bool { return !guest.jamControls() })
	gp.skip()
	gp.remove(2)
	time.Sleep(50 * time.Millisecond)
	both(t, apps, "nothing changed", func() bool { return true })
	if host.player.index != 1 || len(host.player.queue) != 3 {
		t.Errorf("a guest without control changed the Jam: at %d of %d", host.player.index, len(host.player.queue))
	}
	tt := ui.NewTester(guest.view, 1240, 800)
	guest.settings.QueueOpen = true
	tt.Frame()
	wantTexts(t, tt, "In Ada's Jam", "Perfect", "Only Ada moves and removes songs in this Jam.", "The A Team")
	if !tt.HasText("Ed Sheeran · added by Bo") {
		t.Errorf("who added the song does not show; texts: %q", tt.Texts())
	}

	// Leaving: the guest's own queue comes back; the host hears of it.
	guest.leaveJam(true)
	both(t, apps, "the guest gone", func() bool { return len(host.jam.members) == 0 })
	if s := gp.current(); s == nil || s.Name != "Formation" || !gp.paused {
		t.Errorf("after leaving: %v", s)
	}
	// Its song goes from the host's queue, having nobody to play from.
	if len(host.player.queue) != 2 {
		t.Errorf("the host's queue after the guest left: %d songs", len(host.player.queue))
	}
}

// A guest of the same server plays the songs from it, as its own.
func TestJamSameServer(t *testing.T) {
	host := testAppWith(t, streams)
	host.startJam()
	t.Cleanup(host.stopJam)
	guest := guestOf(t, host, "Cy", "srv")
	host.player.play(host.lib.AlbumSongs("al1"), 0)
	host.jamPublish()
	both(t, []*App{host, guest}, "the song", func() bool { return guest.player.current() != nil })
	if s := guest.player.current(); s != guest.lib.Song("s1") || guest.foreign(s) {
		t.Errorf("a song of the same server: %+v, foreign %v", s, guest.foreign(s))
	}
}

// The host ends the Jam: the guest is told, and plays its own again.
func TestJamEnds(t *testing.T) {
	host := testAppWith(t, streams)
	host.startJam()
	guest := guestOf(t, host, "Di", "srv3")
	host.player.play(host.lib.AlbumSongs("al1"), 0)
	host.jamPublish()
	both(t, []*App{host, guest}, "the song", func() bool { return guest.player.current() != nil })
	host.endJam()
	both(t, []*App{host, guest}, "the end", func() bool { return guest.jam.guest == nil })
	if guest.player.current() != nil || len(guest.jam.keys) != 0 {
		t.Errorf("after the end: %v", guest.player.current())
	}
	if host.player.current() == nil || host.jam.host != nil {
		t.Errorf("the host after the end: %v", host.player.current())
	}
}

// The settings turn Jams on, and the player's bar then has its button.
func TestJamSetting(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 800)
	a.goTo("/settings")
	tt.Frame()
	wantTexts(t, tt, "Beta", "Jams")
	if tt.Click("Jam") == nil {
		t.Error("the Jam's button shows while Jams are off")
	}
	a.settings.Jam = true
	tt.Frame()
	click(t, tt, "Jam")
	wantTexts(t, tt, "Start a Jam", "JOIN A JAM")
	click(t, tt, "Start a Jam")
	if a.jam.host == nil {
		t.Fatal("no Jam")
	}
	defer a.stopJam()
	tt.Frame()
	wantTexts(t, tt, "Your Jam", "Nobody has joined yet.", "End Jam", "Guests control playback")
	click(t, tt, "End Jam")
	if a.jam.host != nil {
		t.Error("the Jam did not end")
	}
}

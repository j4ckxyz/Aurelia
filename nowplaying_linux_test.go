package main

import (
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"aurelia/internal/library"
)

// What a desktop sees of Aurelia over D-Bus, and what its buttons do:
// read and pressed here as a desktop would, from another connection.
// Without a session bus there is nobody to tell, and the test is skipped,
// unless AURELIA_REQUIRE_DBUS says one must be there, as in CI.
func TestMPRIS(t *testing.T) {
	a := testApp(t)
	var n nowPlaying
	n.init(a)
	if !n.ok {
		if os.Getenv("AURELIA_REQUIRE_DBUS") != "" {
			t.Fatal("no session bus, or the name is taken")
		}
		t.Skip("no session bus")
	}
	defer n.conn.Close()
	if !n.handlesKeys() {
		t.Error("the desktop's media keys are not taken through MPRIS")
	}
	client, err := dbus.SessionBusPrivate()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Auth(nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Hello(); err != nil {
		t.Fatal(err)
	}
	obj := client.Object(mprisName, mprisPath)
	get := func(iface, name string) any {
		t.Helper()
		v, err := obj.GetProperty(iface + "." + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return v.Value()
	}
	if got := get(mprisRoot, "Identity"); got != "Aurelia" {
		t.Errorf("Identity is %v", got)
	}
	if got := get(mprisPlayer, "PlaybackStatus"); got != "Stopped" {
		t.Errorf("with nothing playing, the status is %v", got)
	}

	// A song plays: its name, its artist, its length and its picture.
	cover := t.TempDir() + "/cover art.jpg"
	os.WriteFile(cover, []byte("jpeg"), 0o644)
	n.set(playing{id: "s1", title: "Shape of You", artist: "Ed Sheeran", album: "Divide",
		duration: 233 * time.Second, position: 12 * time.Second, art: cover})
	if got := get(mprisPlayer, "PlaybackStatus"); got != "Playing" {
		t.Errorf("the status is %v", got)
	}
	meta, ok := get(mprisPlayer, "Metadata").(map[string]dbus.Variant)
	if !ok {
		t.Fatalf("Metadata is %T", get(mprisPlayer, "Metadata"))
	}
	want := map[string]any{
		"xesam:title": "Shape of You", "xesam:album": "Divide", "mpris:length": int64(233_000_000),
		"mpris:artUrl": "file://" + t.TempDir()[:0] + (&testURL{cover}).String(),
	}
	for k, v := range want {
		if got := meta[k].Value(); got != v {
			t.Errorf("%s is %v, want %v", k, got, v)
		}
	}
	if artists, _ := meta["xesam:artist"].Value().([]string); len(artists) != 1 || artists[0] != "Ed Sheeran" {
		t.Errorf("xesam:artist is %v", meta["xesam:artist"].Value())
	}
	if got := get(mprisPlayer, "Position"); got != int64(12_000_000) {
		t.Errorf("Position is %v", got)
	}
	n.progress(15 * time.Second)
	if got := get(mprisPlayer, "Position"); got != int64(15_000_000) {
		t.Errorf("Position, later, is %v", got)
	}
	// Without the file, the server's address.
	n.set(playing{id: "s1", title: "Shape of You", artist: "Ed Sheeran", paused: true, artURL: "https://music.example/cover"})
	meta = get(mprisPlayer, "Metadata").(map[string]dbus.Variant)
	if got := meta["mpris:artUrl"].Value(); got != "https://music.example/cover" {
		t.Errorf("mpris:artUrl is %v", got)
	}
	if got := get(mprisPlayer, "PlaybackStatus"); got != "Paused" {
		t.Errorf("paused, the status is %v", got)
	}

	// The desktop's buttons, and the keyboard's keys it passes on.
	p := a.player
	p.play([]*library.Song{a.lib.Song("s1"), a.lib.Song("s2"), a.lib.Song("s3")}, 0)
	call := func(method string, args ...any) {
		t.Helper()
		if err := obj.Call(mprisPlayer+"."+method, 0, args...).Err; err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		a.drain()
	}
	call("Next")
	if p.current().ID != "s2" {
		t.Errorf("after Next: %s", p.current().ID)
	}
	call("PlayPause")
	if !p.paused {
		t.Error("PlayPause did not pause")
	}
	call("Play")
	call("Play")
	if p.paused {
		t.Error("Play did not play on")
	}
	call("Pause")
	if !p.paused {
		t.Error("Pause did not pause")
	}
	call("Previous")
	if p.current().ID != "s1" {
		t.Errorf("after Previous: %s", p.current().ID)
	}
	call("Seek", int64(5_000_000))
	call("SetPosition", trackPath("s1"), int64(30_000_000))
	// Settings the desktop may change.
	if err := obj.SetProperty(mprisPlayer+".Shuffle", dbus.MakeVariant(true)); err != nil {
		t.Fatal(err)
	}
	a.drain()
	if !a.settings.Shuffle {
		t.Error("setting Shuffle did nothing")
	}
	if err := obj.SetProperty(mprisPlayer+".LoopStatus", dbus.MakeVariant("Track")); err != nil {
		t.Fatal(err)
	}
	a.drain()
	if a.settings.Repeat != repeatOne {
		t.Errorf("setting LoopStatus: repeat is %d", a.settings.Repeat)
	}
	if err := obj.SetProperty(mprisPlayer+".Volume", dbus.MakeVariant(0.25)); err != nil {
		t.Fatal(err)
	}
	a.drain()
	if a.settings.Volume != 0.25 {
		t.Errorf("setting Volume: %v", a.settings.Volume)
	}
	// And what lists the service's methods finds them.
	var xml string
	if err := obj.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&xml); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`name="PlayPause"`, `name="Seek"`, `name="Metadata"`, `name="Seeked"`} {
		if !contains(xml, s) {
			t.Errorf("the introspection lacks %s", s)
		}
	}
}

// testURL writes a file's path as the URL MPRIS takes.
type testURL struct{ path string }

func (u *testURL) String() string {
	out := ""
	for _, r := range u.path {
		if r == ' ' {
			out += "%20"
		} else {
			out += string(r)
		}
	}
	return out
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

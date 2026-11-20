package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/audio"
)

// Automatic plays spatial audio on headphones alone: as they are put on
// and taken off, and not with mono audio.
func TestSpatialFollowsHeadphones(t *testing.T) {
	a := testApp(t)
	speakers := audio.Device{ID: "spk", Name: "MacBook Pro Speakers", Default: true}
	airpods := audio.Device{ID: "pods", Name: "Robin's AirPods Pro", Headphones: true}
	a.output.devices = []audio.Device{speakers, airpods}

	if a.spatialOn() {
		t.Fatal("spatial audio plays while off")
	}
	a.settings.Spatial = spatialAlways
	if !a.spatialOn() {
		t.Fatal("Always On does not play it on speakers")
	}
	a.settings.Spatial = spatialAuto
	if a.spatialOn() {
		t.Fatal("Automatic plays it on speakers")
	}
	// Chosen in the settings.
	a.output.on = "pods"
	if !a.spatialOn() {
		t.Fatal("Automatic does not play it on AirPods chosen")
	}
	// The system's default, put on.
	a.output.on = ""
	speakers.Default, airpods.Default = false, true
	a.output.devices = []audio.Device{speakers, airpods}
	if !a.spatialOn() {
		t.Fatal("Automatic does not play it on AirPods that are the default")
	}
	a.settings.Mono = true
	if a.spatialOn() {
		t.Fatal("spatial audio plays in mono")
	}
}

// The setting is in Settings and in the player's "…".
func TestSpatialSetting(t *testing.T) {
	a := testApp(t)
	a.player.play(a.lib.AlbumSongs("al1"), 0)
	tt := ui.NewTester(a.view, 1240, 900)
	a.goTo("/settings")
	tt.Frame()
	if !tt.HasText("Spatial audio") {
		t.Fatal("Settings has no spatial audio")
	}
	if err := tt.Click(playerMoreLabel); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("Spatial Audio", "Always On"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	tt.Frame()
	if a.settings.Spatial != spatialAlways {
		t.Errorf("the menu set %q", a.settings.Spatial)
	}
}

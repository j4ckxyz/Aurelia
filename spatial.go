package main

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/audio"
)

// Spatial audio: on headphones, the music as from speakers around the
// listener in a room, rather than from inside the head, in the manner of
// Apple Music's spatial audio on AirPods (internal/audio/spatial.go).
// Automatic plays it on headphones alone, as Apple Music's Automatic
// plays Dolby Atmos: AirPods, and other headphones the system knows as
// such.

const (
	spatialOff    = ""
	spatialAuto   = "auto"
	spatialAlways = "on"
)

var spatialChoices = []struct{ id, name string }{
	{spatialOff, "Off"},
	{spatialAuto, "Automatic"},
	{spatialAlways, "Always On"},
}

// spatialName is the setting as it is shown.
func spatialName(id string) string {
	for _, ch := range spatialChoices {
		if ch.id == id {
			return ch.name
		}
	}
	return spatialChoices[0].name
}

// playingOn is the output the sound goes to: the one chosen, or the
// system's default.
func (a *App) playingOn() (audio.Device, bool) {
	devs := a.output.devices
	if i := slices.IndexFunc(devs, func(d audio.Device) bool { return a.output.on != "" && d.ID == a.output.on }); i >= 0 {
		return devs[i], true
	}
	if i := slices.IndexFunc(devs, func(d audio.Device) bool { return d.Default }); i >= 0 {
		return devs[i], true
	}
	return audio.Device{}, false
}

// onHeadphones reports whether the sound plays on headphones.
func (a *App) onHeadphones() bool {
	d, ok := a.playingOn()
	return ok && d.Headphones
}

// spatialOn reports whether the music plays as spatial audio now. Mono,
// which is for one earphone, leaves it out.
func (a *App) spatialOn() bool {
	if a.settings.Mono {
		return false
	}
	switch a.settings.Spatial {
	case spatialAlways:
		return true
	case spatialAuto:
		return a.onHeadphones()
	}
	return false
}

// setSpatial chooses when spatial audio plays.
func (a *App) setSpatial(id string) {
	a.settings.Spatial = id
	a.applyEffects()
	a.saveSettings()
	if id == spatialAuto {
		a.readOutputs(a.followHeadphones) // headphones put on since
	}
}

// followHeadphones turns spatial audio on or off as headphones are put on
// or taken off, in Automatic.
func (a *App) followHeadphones() {
	if a.spatialOn() != a.spatialHeard {
		a.applyEffects()
	}
}

// spatialHelp says what spatial audio does, and whether it plays.
func (a *App) spatialHelp() string {
	help := "Music on headphones sounds as if from speakers around you in a room, rather than from inside your head."
	switch {
	case a.settings.Mono:
		return help + " Off while mono audio is on."
	case a.settings.Spatial == spatialAuto:
		d, ok := a.playingOn()
		switch {
		case !ok:
			return help + " Automatic plays it on AirPods and other headphones."
		case d.Headphones:
			return help + " On now, on " + d.Name + "."
		default:
			return help + " Off now: " + d.Name + " are not headphones."
		}
	}
	return help
}

// spatialMenu is the setting in the player's "…".
func (a *App) spatialMenu(m *ui.Menu) {
	for _, ch := range spatialChoices {
		if m.Item(ch.name).Checked(a.settings.Spatial == ch.id).Chosen() {
			a.setSpatial(ch.id)
		}
	}
}

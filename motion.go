package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The app's animations are short, a tenth of a second or a little more,
// and nothing waits for them: a page is there, and moves into place.

// Motion is the setting of Settings ▸ Appearance: "" follows the system's
// own "Reduce motion", "on" animates always, "reduced" never.
const (
	motionSystem  = ""
	motionOn      = "on"
	motionReduced = "reduced"
)

// motionChoices are the names the setting is shown by, in order.
var motionChoices = []struct{ id, name string }{
	{motionSystem, "Follow the system"},
	{motionOn, "On"},
	{motionReduced, "Reduced"},
}

var (
	// animate tells whether the app moves things; it is read as the views
	// are built, on the interface's goroutine.
	animate = true
	// fade is how hovers come and go.
	fade = ui.ElementTransition{Colors: true, Duration: 90 * time.Millisecond}
)

// tween is the transition t where animations are on, and an instant change
// of colors where they are reduced.
func tween(t ui.ElementTransition) ui.ElementTransition {
	if animate {
		return t
	}
	return ui.ElementTransition{Colors: true, Duration: time.Millisecond}
}

// The transitions of the app, by what they are for.
var (
	// pageIn is a page shown: a little way up, and in.
	pageIn = ui.ElementTransition{Enter: &ui.Motion{Y: 8, Opacity: 0.35}, Duration: 150 * time.Millisecond}
	// panelIn is the queue sliding in from its edge, and out again.
	panelIn = ui.ElementTransition{Enter: &ui.Motion{X: 36, Opacity: 0.3}, Exit: &ui.Motion{X: 36}, Duration: 160 * time.Millisecond}
	// sidebarIn is the sidebar opening and closing along its width.
	sidebarIn = ui.ElementTransition{Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}, Duration: 170 * time.Millisecond}
	// risesIn is a button coming up under the pointer.
	risesIn = ui.ElementTransition{Enter: &ui.Motion{Y: 8}, Exit: &ui.Motion{Y: 6}, Duration: 130 * time.Millisecond}
	// picturesIn is a picture arriving, or being changed for another.
	picturesIn = ui.ElementTransition{Enter: &ui.Motion{}, Exit: &ui.Motion{}, Duration: 180 * time.Millisecond}
)

// applyMotion makes the animations follow the setting and the system.
func (a *App) applyMotion() {
	reduced := a.settings.Motion == motionReduced || a.settings.Motion == motionSystem && a.systemReduces
	animate = !reduced
	if animate {
		fade = ui.ElementTransition{Colors: true, Duration: 90 * time.Millisecond}
	} else {
		fade = ui.ElementTransition{Colors: true, Duration: time.Millisecond}
	}
}

// followSystem takes the desktop's wish to reduce motion, which the
// window reads (macOS's Reduce Motion, Windows's animation effects,
// GNOME's animations), as a frame is built.
func (a *App) followSystem(c *ui.Context) {
	if r := c.Preferences().ReduceMotion; r != a.systemReduces {
		a.systemReduces = r
		a.applyMotion()
	}
}

// motionName is the setting as it is shown.
func motionName(id string) string {
	for _, c := range motionChoices {
		if c.id == id {
			return c.name
		}
	}
	return motionChoices[0].name
}

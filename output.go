package main

import (
	"log"
	"slices"
	"time"

	"aurelia/internal/audio"
)

// outputState is what is known of the computer's outputs: the list, when
// it was read, and whether the sound is where the user chose.
type outputState struct {
	devices []audio.Device
	readAt  time.Time
	reading bool
	err     error
	// on is the output the engine was told to play on, "" for the default.
	on string
}

// outputMaxAge is how long the list of outputs is trusted: a device
// plugged in shows in the settings within it.
const outputMaxAge = 4 * time.Second

// readOutputs lists the outputs on another goroutine, which the system
// takes its time over, and then runs fn on the interface's.
func (a *App) readOutputs(fn func()) {
	if a.output.reading {
		return
	}
	a.output.reading = true
	go func() {
		devs, err := audio.Devices()
		a.update(func() {
			a.output.reading = false
			a.output.readAt = time.Now()
			a.output.err = err
			if err == nil {
				a.output.devices = devs
			}
			if fn != nil {
				fn()
			}
		})
	}()
}

// outputThere reports whether the output of the settings is plugged in.
func (a *App) outputThere() bool {
	return slices.ContainsFunc(a.output.devices, func(d audio.Device) bool { return d.ID == a.settings.OutputDevice })
}

// chooseOutput makes an output the one to play on, for now and from now
// on; "" is the system's default.
func (a *App) chooseOutput(id, name string) {
	a.settings.OutputDevice, a.settings.OutputName = id, name
	a.saveSettings()
	a.settleOutput()
}

// settleOutput moves the sound to the device of the settings if it is
// there, and to the system's default if it is not; the engine is told only
// of changes.
func (a *App) settleOutput() {
	e := a.player.engine
	if e == nil {
		return
	}
	target := ""
	if want := a.settings.OutputDevice; want != "" && a.outputThere() {
		target = want
	}
	if target == a.output.on {
		return
	}
	if err := e.SetDevice(target); err != nil {
		log.Print("the output device: ", err)
		a.toastError("Could not play on "+a.settings.OutputName, err)
		return
	}
	a.output.on = target
}

// outputMissing reports that an output is chosen and is not the one
// playing.
func (a *App) outputMissing() bool {
	return a.settings.OutputDevice != "" && a.player.engine != nil && a.output.on != a.settings.OutputDevice
}

// watchOutput, every few seconds, goes back to the chosen output once it
// is plugged in again, and plays on the default once it is not.
func (a *App) watchOutput() {
	if a.settings.OutputDevice == "" || a.player.engine == nil {
		return
	}
	if time.Since(a.output.readAt) < 5*time.Second {
		return
	}
	a.readOutputs(a.settleOutput)
}

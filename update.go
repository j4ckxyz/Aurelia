package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
)

// updates is the state of the app's own updates: new versions come from
// the releases of its GitHub repository, signed, and replace the app
// where it is installed. The version running goes on until a restart.
type updates struct {
	checking   bool
	checkedAt  time.Time
	upToDate   bool
	available  *mygo.Update // found, and not installed yet
	installing bool
	progress   float64 // of the download, from 0 to 1
	ready      string  // the version installed, which a restart runs
	err        string
	started    bool
}

// appVersion is the version of the app running.
func appVersion() string {
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return jellyfin.Version
}

// startUpdates checks for a new version a little after the app opens,
// and a few times a day, while the setting is on.
func (a *App) startUpdates() {
	if a.updates.started || !mygo.Updater.Enabled() {
		return
	}
	a.updates.started = true
	go func() {
		time.Sleep(15 * time.Second)
		for {
			a.update(func() {
				if a.settings.AutoUpdate {
					a.checkForUpdates(false)
				}
			})
			time.Sleep(6 * time.Hour)
		}
	}()
}

// checkForUpdates asks whether a newer version is released, and with
// install downloads it at once: as the setting does on its own, and as
// the button in Settings does when asked.
func (a *App) checkForUpdates(manual bool) {
	u := &a.updates
	if u.checking || u.installing || u.ready != "" {
		return
	}
	if !mygo.Updater.Enabled() {
		u.err = "This copy of Aurelia does not update itself: it is a development build, or it was installed where it cannot write, as by a package manager."
		return
	}
	u.checking, u.err, u.upToDate = true, "", false
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		up, err := mygo.Updater.Check(ctx)
		a.update(func() {
			u.checking, u.checkedAt = false, time.Now()
			switch {
			case err != nil:
				// Checking in the background fails quietly: the next
				// check may not.
				if manual {
					u.err = "Could not check for updates: " + friendly(err)
				}
			case up == nil:
				u.upToDate = true
			default:
				u.available = up
				if a.settings.AutoUpdate || manual {
					a.installUpdate()
				}
			}
		})
	}()
}

// installUpdate downloads the version found and puts it in place of the
// app, then offers to restart.
func (a *App) installUpdate() {
	u := &a.updates
	up := u.available
	if up == nil || u.installing {
		return
	}
	u.installing, u.progress, u.err = true, 0, ""
	go func() {
		var last time.Time
		err := up.Install(context.Background(), func(downloaded, total int64) {
			if now := time.Now(); total > 0 && now.Sub(last) > 100*time.Millisecond {
				last = now
				a.update(func() { u.progress = float64(downloaded) / float64(total) })
			}
		})
		a.update(func() {
			u.installing = false
			if err != nil {
				u.err = "Could not install version " + up.Version + ": " + friendly(err)
				return
			}
			u.available, u.ready = nil, up.Version
			toasts = append(toasts, ui.Toast{
				ID:          "update-ready",
				Title:       "Aurelia " + up.Version + " is ready",
				Description: "Restart to use the new version. The music stops for a moment.",
				Action:      "Restart",
				OnAction:    func() { a.restart() },
				Timeout:     -1, // until answered
			})
		})
	}()
}

// restart quits the app and starts it again, as the new version when one
// was installed.
func (a *App) restart() {
	a.saveQueue()
	mygo.App.Relaunch()
}

// updatesCard is the part of Settings about the app's version.
func (a *App) updatesCard(c *ui.Context) {
	u := &a.updates
	if u.checking || u.installing {
		c.After(200 * time.Millisecond)
	}
	a.card(c, func() {
		status := ""
		switch {
		case u.ready != "":
			status = "Version " + u.ready + " is installed. Restart to use it."
		case u.installing:
			status = fmt.Sprintf("Downloading version %s… %d%%", u.available.Version, int(u.progress*100))
		case u.available != nil:
			status = "Version " + u.available.Version + " is available."
		case u.checking:
			status = "Checking for a new version…"
		case u.err != "":
			status = u.err
		case u.upToDate:
			status = "This is the latest version. Checked " + ago(u.checkedAt) + "."
		case !mygo.Updater.Enabled():
			status = "Updates come with the released app; this build does not update itself."
		default:
			status = "New versions come from the releases on GitHub."
		}
		a.setting(c, "Aurelia "+appVersion(), status, func() {
			switch {
			case u.ready != "":
				if a.pillButton(c, "refresh-cw", "Restart", true).Clicked() {
					a.restart()
				}
			case u.available != nil && !u.installing:
				if a.pillButton(c, "download", "Install", true).Clicked() {
					a.installUpdate()
				}
			default:
				if a.pillButton(c, "refresh-cw", "Check for updates", false).Disabled(u.checking || u.installing).Clicked() {
					a.checkForUpdates(true)
				}
			}
		})
		a.setting(c, "Update automatically", "Download new versions as they are released, and ask to restart. Turned off, Aurelia checks only when you ask.", func() {
			if ui.Switch(c.Key("auto-update"), &a.settings.AutoUpdate).Label("Update automatically").Changed() {
				a.saveSettings()
			}
		})
	})
}

// commandLine handles the arguments that make Aurelia do one thing and
// exit, without a window. It reports whether it did.
//
//	aurelia --version                 prints the version
//	aurelia --write-version FILE      writes it to a file, for scripts
//	                                  where the app has no terminal
//	aurelia --self-update FILE        installs the newest release over the
//	                                  app, and writes what it did
func commandLine(args []string) bool {
	if len(args) == 0 {
		return false
	}
	out := ""
	if len(args) > 1 {
		out = args[1]
	}
	say := func(s string) {
		fmt.Println(s)
		if out != "" {
			os.WriteFile(out, []byte(s+"\n"), 0o644)
		}
	}
	switch args[0] {
	case "--version", "-version", "-v":
		out = ""
		say("Aurelia " + appVersion())
	case "--write-version":
		say(appVersion())
	case "--self-update":
		say(selfUpdate())
	default:
		return false
	}
	return true
}

// selfUpdate installs the newest release and tells what happened.
func selfUpdate() string {
	from := appVersion()
	if !mygo.Updater.Enabled() {
		return "error: this copy of Aurelia " + from + " does not update itself"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	up, err := mygo.Updater.Check(ctx)
	switch {
	case errors.Is(err, mygo.ErrUpdatesDisabled):
		return "error: updates are not enabled in this build"
	case err != nil:
		return "error: checking: " + err.Error()
	case up == nil:
		return "up to date: " + from
	}
	if err := up.Install(ctx, nil); err != nil {
		return "error: installing " + up.Version + ": " + err.Error()
	}
	return "updated: " + from + " -> " + up.Version
}

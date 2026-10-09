package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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
	// offer shows the page that offers the version found as the app
	// opened; reopen has the app start again once it is installed, and
	// declined is the version that was not wanted in this run.
	offer    bool
	reopen   bool
	declined string
}

// Why a check for updates is made.
const (
	checkAtOpening    = iota // the app just opened: a version found is offered on a page of its own
	checkInBackground        // the app runs: a version found is installed quietly
	checkAsked               // the user asked in Settings
)

// appVersion is the version of the app running: the released app's, or
// the one the code names for a build that was not packaged.
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
		// As the app opens, before the user is far into anything; then
		// now and then.
		time.Sleep(time.Second)
		for why := checkAtOpening; ; why = checkInBackground {
			a.update(func() {
				if a.settings.AutoUpdate {
					a.checkForUpdates(why)
				}
			})
			time.Sleep(6 * time.Hour)
		}
	}()
}

// checkForUpdates asks whether a newer version is released. One found
// as the app opens is offered on a page of its own; one found while it
// runs, or asked for in Settings, is downloaded at once, and a restart
// offered.
func (a *App) checkForUpdates(why int) {
	manual := why == checkAsked
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
			case why == checkAtOpening:
				u.available, u.offer = up, true
			case !manual && up.Version == u.declined:
				// Not now, they said.
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
				u.reopen = false
				return
			}
			u.available, u.ready = nil, up.Version
			if u.reopen {
				// Asked for on the page of the update: the new version
				// opens in this one's place, signed in as this one is.
				a.restart()
				return
			}
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
// was installed. The session and the queue are in the app's files: the
// app that opens is signed in, with the same songs waiting.
func (a *App) restart() {
	a.saveQueue()
	a.saveSettings()
	mygo.App.Relaunch()
}

// acceptUpdate installs the version offered and reopens the app as it.
func (a *App) acceptUpdate() {
	u := &a.updates
	if u.available == nil || u.installing {
		return
	}
	u.reopen = true
	a.installUpdate()
}

// declineUpdate leaves the version offered for another time.
func (a *App) declineUpdate() {
	u := &a.updates
	if u.installing {
		return
	}
	if u.available != nil {
		u.declined = u.available.Version
	}
	u.offer, u.err = false, ""
}

// updatePage offers a version found as the app opened, in place of the
// app: what is new in it, and a button that installs it and reopens.
func (a *App) updatePage(c *ui.Context) {
	p := a.pal
	u := &a.updates
	up := u.available
	if up == nil && u.ready == "" {
		u.offer = false
		return
	}
	version := u.ready
	if up != nil {
		version = up.Version
	}
	if u.installing {
		c.After(100 * time.Millisecond)
	}
	bar := c.TitleBar()
	ui.Column(c).Fill().LinearGradient(ui.LinearGradient{From: p.accent.Alpha(0.2), To: p.accent.Alpha(0), Angle: 180, End: 0.6, Oklab: true}).Children(func() {
		ui.Row(c).Height(max(bar.Height, topBarH)).DragWindow().Justify(ui.End).Padding(0, 12).Children(func() {
			a.windowControls(c)
		})
		ui.Column(c).Grow(1).MinHeight(0).Center().Padding(8, 24, 40).Children(func() {
			ui.Column(c).Width(480).MaxHeightPercent(100).Gap(18).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c).AlignItems(ui.Center).Gap(8).Shrink(0).Children(func() {
					ui.Box(c).Size(64, 64).Radius(18).Center().Background(p.accent).Shadow(0, 10, 30, 0, p.shadow).Children(func() {
						ui.Icon(c, icon("logo")).Size(38, 38).TextColor(p.onAcc)
					})
					ui.Text(c, "Aurelia "+version+" is here").FontSize(26).FontWeight(800).Margin(10, 0, 0).TextAlign(ui.Center)
					ui.Text(c, "You have "+appVersion()+". Updating takes a moment: Aurelia reopens by itself, signed in, with your queue as it is.").
						TextColor(p.muted).TextAlign(ui.Center)
				})
				if up != nil {
					if notes := releaseNotes(up.Notes); len(notes) > 0 {
						ui.Scroll(c.Key("notes")).MinHeight(60).Shrink(1).Padding(14, 16).Gap(9).Radius(p.radius+4).Background(p.surface.Alpha(0.8)).Border(1, p.border).Children(func() {
							ui.Text(c, "WHAT IS NEW").FontSize(11).FontWeight(700).LetterSpacing(1).TextColor(p.accent)
							for i, n := range notes {
								ui.Row(c.Key(i)).Gap(9).AlignItems(ui.Start).Children(func() {
									ui.Box(c).Size(5, 5).Radius(3).Background(p.faint).Margin(7, 0, 0).Shrink(0)
									ui.Text(c, n).Grow(1).MinWidth(0).LineHeight(1.4)
								})
							}
						})
					}
				}
				ui.Column(c).Gap(10).Shrink(0).AlignItems(ui.Stretch).Children(func() {
					switch {
					case u.installing || u.ready != "":
						status := "Reopening…"
						if u.installing {
							status = fmt.Sprintf("Downloading… %d%%", int(u.progress*100))
						}
						ui.Box(c).Height(6).Radius(3).Background(p.hover).Children(func() {
							ui.Box(c).WidthPercent(100 * float32(max(u.progress, 0.02))).Height(6).Radius(3).Background(p.accent)
						})
						ui.Text(c, status).FontSize(12).TextColor(p.muted).TextAlign(ui.Center).FontFeatures("tnum")
					default:
						if u.err != "" {
							ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
								ui.Icon(c, icon("circle-alert")).Size(15, 15).TextColor(p.danger).Margin(1, 0, 0)
								ui.Text(c, u.err).FontSize(12).TextColor(p.muted).Grow(1)
							})
						}
						ui.Row(c).Gap(10).Justify(ui.Center).Children(func() {
							label := "Update and reopen"
							if u.err != "" {
								label = "Try again"
							}
							if a.pillButton(c, "download", label, true).Clicked() {
								a.acceptUpdate()
							}
							if a.pillButton(c, "", "Not now", false).Clicked() {
								a.declineUpdate()
							}
						})
					}
				})
			})
		})
	})
}

// releaseNotes are the points of a version's notes, which are the
// Markdown of its part of CHANGELOG.md: a list, its lines wrapped.
func releaseNotes(md string) []string {
	var notes []string
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimRight(line, " \t\r")
		text := strings.TrimSpace(line)
		switch {
		case text == "" || strings.HasPrefix(text, "#"):
			continue
		case strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "* "):
			notes = append(notes, text[2:])
		case len(notes) > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
			notes[len(notes)-1] += " " + text // a wrapped line of the point
		default:
			notes = append(notes, text)
		}
	}
	for i, n := range notes {
		notes[i] = strings.NewReplacer("`", "", "**", "").Replace(n)
	}
	return notes
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
					a.checkForUpdates(checkAsked)
				}
			}
		})
		a.setting(c, "Update automatically", "Offer a new version as Aurelia opens, and download the ones released while it runs. Turned off, Aurelia checks only when you ask.", func() {
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
//	aurelia --write-data-dir FILE     writes where the app keeps its settings
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
	case "--write-data-dir":
		say(appDirs().data)
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

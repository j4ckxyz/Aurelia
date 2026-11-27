package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"aurelia/internal/jam"
)

// What a Jam shows: its dialog, the strip above the player's bar while
// one is on, its button there, and its settings.

const jamStripH = 28

// openJam shows the Jam's dialog.
func (a *App) openJam() {
	a.jam.open = true
	a.jam.joinErr = ""
	a.lookForJams()
	// The devices of the tailnet are read, and asked whether they run
	// Aurelia, while the dialog shows.
	a.tail.panel.Store(true)
	a.tailKick()
}

// jamButton is the Jam's button in the player's bar, while Jams are on.
func (a *App) jamButton(c *ui.Context) {
	j := &a.jam
	if !a.settings.Jam && !j.in() {
		return
	}
	label := "Jam"
	switch {
	case j.host != nil:
		label = "Your Jam"
	case j.guest != nil:
		label = j.hostName + "'s Jam"
	}
	if a.toggleIcon(c.Key("jam"), "users-round", label, j.in(), 32, 16).Clicked() {
		a.openJam()
	}
}

// people words the guests of a Jam but me: "Bo", "Bo and Cy", "Bo, Cy
// and 2 others".
func people(ms []jam.Member, me string) string {
	var names []string
	for _, m := range ms {
		if !m.Host && m.ID != me {
			names = append(names, m.Name)
		}
	}
	switch n := len(names); {
	case n == 0:
		return ""
	case n == 1:
		return names[0]
	case n == 2:
		return names[0] + " and " + names[1]
	case n == 3:
		return names[0] + ", " + names[1] + " and " + names[2]
	default:
		return names[0] + ", " + names[1] + " and " + count(n-2, "other", "others")
	}
}

// jamStrip tells, above the player's bar, that a Jam is on.
func (a *App) jamStrip(c *ui.Context) {
	j := &a.jam
	if !j.in() {
		return
	}
	p := a.pal
	var text, action string
	switch {
	case j.host != nil:
		text = "Your Jam · code " + jam.ShowCode(j.host.Code())
		if who := people(j.members, ""); who != "" {
			text = "Jamming with " + who + " · code " + jam.ShowCode(j.host.Code())
		}
		if n := len(j.requests); n > 0 {
			text += " · " + count(n, "person asks", "people ask") + " to join"
		}
		action = "Open"
	default:
		text = "In " + j.hostName + "'s Jam"
		if j.away {
			text += " · reconnecting…"
		} else if who := people(j.members, j.guest.ID()); who != "" {
			text += " with " + who
		}
		action = "Leave"
	}
	ui.Row(c).Height(jamStripH).Shrink(0).Padding(0, 16).Gap(8).Justify(ui.Center).Background(p.accent).Children(func() {
		ui.Icon(c, icon("users-round")).Size(14, 14).TextColor(p.onAcc)
		t := ui.Text(c, text).FontSize(12).FontWeight(600).TextColor(p.onAcc).SingleLine().MinWidth(0).Shrink(1).Cursor(ui.CursorPointer)
		if t.Clicked() {
			a.openJam()
		}
		b := ui.ButtonBase(c.Key("jam-strip")).Height(20).Padding(0, 9).Radius(10).Margin(0, 0, 0, 6).Background(p.onAcc.Alpha(0.16)).Cursor(ui.CursorPointer).Label(action).Shrink(0)
		if b.Hovered() {
			b.Background(p.onAcc.Alpha(0.28))
		}
		b.Children(func() { ui.Text(c, action).FontSize(11).FontWeight(600).TextColor(p.onAcc).SingleLine() })
		if b.Clicked() {
			if j.host != nil {
				a.openJam()
			} else {
				a.leaveJam(true)
			}
		}
	})
}

// betaTag marks what is in beta.
func (a *App) betaTag(c *ui.Context) {
	p := a.pal
	ui.Box(c).Padding(2, 7).Radius(8).Background(p.accent.Alpha(0.16)).Shrink(0).Children(func() {
		ui.Text(c, "BETA").FontSize(10).FontWeight(700).TextColor(p.accent)
	})
}

// jamDialog is the Jam's dialog: to start or join one, and while one is
// on, who is in it and how it goes.
func (a *App) jamDialog(c *ui.Context) {
	j := &a.jam
	a.tail.panel.Store(j.open && a.jam.guest == nil)
	if !j.open {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		// The title stays; what is under it scrolls, in a window too low
		// for all of it, as with many people or devices.
		ui.Column(c).Width(440).MinHeight(0).Shrink(1).Children(func() {
			title := "Jam"
			switch {
			case j.host != nil:
				title = "Your Jam"
			case j.guest != nil:
				title = j.hostName + "'s Jam"
			}
			ui.Row(c).Gap(10).Padding(22, 22, 0, 22).Shrink(0).Children(func() {
				ui.Text(c, title).FontSize(18).FontWeight(700).SingleLine().MinWidth(0).Shrink(1)
				a.betaTag(c)
				ui.Spacer(c)
				if a.iconButton(c, "x", "Close", 28, 15).Clicked() {
					open = false
				}
			})
			ui.Scroll(c.Key("jam-dialog")).MinHeight(0).Shrink(1).Children(func() {
				ui.Column(c).Gap(14).Padding(14, 22, 22, 22).Children(func() {
					a.tailInviteBanner(c)
					switch {
					case j.host != nil:
						a.jamHostView(c)
					case j.guest != nil:
						a.jamGuestView(c)
					default:
						a.jamStartView(c)
					}
				})
			})
		})
	})
	if !open {
		j.open = false
		a.stopLooking()
		if j.joining && !j.waiting {
			a.cancelJoin()
		}
	}
}

// jamStartView starts a Jam, or joins one.
func (a *App) jamStartView(c *ui.Context) {
	j := &a.jam
	p := a.pal
	ui.Text(c, "Listen together with people on this network. Everyone hears the same queue, each on their own computer and with their own sound: their volume, equalizer, vinyl and the rest.").TextColor(p.muted).MaxLines(5)
	if a.signedIn() {
		ui.Row(c).Children(func() {
			if a.pillButton(c, "users-round", "Start a Jam", true).Disabled(j.joining).Clicked() {
				a.startJam()
			}
		})
	}
	label := func(text string) {
		ui.Text(c, text).FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(p.faint).Margin(6, 0, 0)
	}
	label("JOIN A JAM")
	if j.looking == nil && !j.joining && !a.silent {
		a.lookForJams()
	}
	if len(j.nearby) == 0 {
		msg := "Looking for Jams on this network…"
		if j.looking == nil || time.Since(j.lookingAt) > 6*time.Second {
			msg = "No Jams found nearby. Ask the host for their code."
		} else {
			c.After(time.Second)
		}
		ui.Text(c, msg).FontSize(12).TextColor(p.muted)
	}
	for _, f := range j.nearby {
		ui.Row(c.Key("near-"+f.ID)).Gap(10).Padding(8, 10).Radius(p.radius).Background(p.surface.Alpha(0.6)).Children(func() {
			ui.Icon(c, icon("users-round")).Size(16, 16).TextColor(p.accent)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, f.Name+"'s Jam").FontWeight(600).SingleLine()
				if f.Proto != jam.Proto {
					ui.Text(c, "Needs the same version of Aurelia").FontSize(12).TextColor(p.muted).SingleLine()
				} else {
					ui.Text(c, "Asks "+f.Name+" to let you in").FontSize(12).TextColor(p.muted).SingleLine()
				}
			})
			if a.pillButton(c, "", "Join", false).Disabled(j.joining || f.Proto != jam.Proto).Clicked() {
				a.joinJam(f.Addr, "")
			}
		})
	}
	submit := false
	ui.Row(c).Gap(8).Children(func() {
		in := ui.TextInput(c.Key("jam-code"), &j.input).Placeholder("Code, or invite").Label("Code or invite").Height(36).Grow(1).Disabled(j.joining)
		if in.Submitted() {
			submit = true
		}
		_, ok := jam.ParseTarget(j.input)
		if a.pillButton(c, "", "Join", true).Disabled(!ok || j.joining).Clicked() {
			submit = true
		}
	})
	if submit && !j.joining {
		if t, ok := jam.ParseTarget(j.input); ok {
			a.joinTarget(t)
		} else {
			j.joinErr = "A code has six letters and numbers, as K7Q X4R."
		}
	}
	switch {
	case j.joining:
		ui.Row(c).Gap(8).Children(func() {
			spin := ui.Icon(c, icon("loader-circle")).Size(14, 14).TextColor(p.muted)
			spin.Rotate(spin.Loop("jam-spin", 900*time.Millisecond, ui.Linear) * 360)
			msg := "Joining…"
			if j.waiting {
				msg = "Waiting for the host to let you in…"
			}
			ui.Text(c, msg).FontSize(12).TextColor(p.muted).Grow(1)
			if a.textButton(c, "Cancel").Clicked() {
				a.cancelJoin()
			}
		})
	case j.joinErr != "":
		ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, icon("circle-alert")).Size(14, 14).TextColor(p.danger).Margin(1, 0, 0)
			ui.Text(c, j.joinErr).FontSize(12).TextColor(p.muted).MaxLines(4).Grow(1).MinWidth(0)
		})
	}
	a.tailSection(c)
	note := "Songs your server does not have play from the person who added them."
	if !a.signedIn() {
		note = "Without a server of your own, every song plays from the people in the Jam."
	}
	where := " Jams work between computers on the same network, whose firewalls let Aurelia in."
	if a.tailUp() {
		where = " Jams work between computers on the same network or on your tailnet."
	}
	ui.Text(c, note+where).FontSize(12).TextColor(p.faint).MaxLines(4).Margin(4, 0, 0)
}

// jamHostView is the host's: the code, who is in, and the Jam's rules.
func (a *App) jamHostView(c *ui.Context) {
	j := &a.jam
	h := j.host
	p := a.pal
	intro := "Others join from Aurelia's Jam button with this code, or by finding your Jam nearby."
	if j.relayAddr != "" {
		intro = "Others join from Aurelia's Jam button with this code, from anywhere with the invite."
	}
	ui.Text(c, intro).TextColor(p.muted).MaxLines(3)
	invite := a.jamInvite()
	ui.Row(c).Gap(12).Padding(12, 16).Radius(p.radius + 2).Background(p.surface.Alpha(0.6)).Children(func() {
		ui.Text(c, jam.ShowCode(h.Code())).FontSize(28).FontWeight(800).LetterSpacing(2).FontFeatures("tnum").Selectable().Grow(1)
		if a.pillButton(c, "copy", "Copy invite", false).Tooltip(invite).Clicked() {
			if a.native {
				mygo.Clipboard.WriteText(invite)
			}
			a.toast("Invite copied: paste it into the Jam's Join box")
		}
	})
	switch {
	case j.relayAddr != "":
		a.jamLine(c, "circle-check", p.success, "Reachable through your relay: the invite works on any network.")
	case j.relayErr != "":
		a.jamLine(c, "circle-alert", p.warning, "Your relay: "+j.relayErr+". On this network, the Jam works as ever.")
	}
	ui.Text(c, "PEOPLE").FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(p.faint).Margin(4, 0, 0)
	ui.Column(c).Gap(2).Children(func() {
		a.memberRow(c, "host", a.jamName()+" (you)", "Host", false)
		for _, m := range j.members {
			note := ""
			if m.Peer != nil {
				note = m.Peer.Device
			}
			if m.Away {
				note = "Reconnecting…"
			}
			if a.memberRow(c, m.ID, m.Name, note, true) {
				j.host.Remove(m.ID)
			}
		}
		if len(j.members) == 0 {
			ui.Text(c, "Nobody has joined yet.").FontSize(12).TextColor(p.muted).Padding(4, 10)
		}
	})
	for _, r := range j.requests {
		ui.Row(c.Key("ask-"+r.ID)).Gap(8).Padding(8, 10).Radius(p.radius).Background(p.accent.Alpha(0.1)).Children(func() {
			ask := r.Name + " would like to join"
			if r.Peer != nil {
				ask = r.Peer.Who() + " wants to join"
			}
			ui.Text(c, ask).FontWeight(500).Grow(1).MinWidth(0).SingleLine()
			if a.textButton(c, "Turn away").Clicked() {
				a.answerJam(r.ID, false)
			}
			if a.pillButton(c, "", "Let in", true).Clicked() {
				a.answerJam(r.ID, true)
			}
		})
	}
	a.jamActivity(c)
	a.tailSection(c)
	a.card(c, func() {
		a.setting(c, "Guests control playback", "They play, pause, skip and change the queue. Off, they only add songs.", func() {
			on := !a.settings.JamLocked
			if ui.Switch(c.Key("jam-control"), &on).Label("Guests control playback").Changed() {
				a.settings.JamLocked = !on
				a.saveSettings()
				a.jamPublish()
			}
		})
		a.setting(c, "Show to people nearby", "They find your Jam without the code, and ask you to let them in.", func() {
			on := !a.settings.JamHidden
			if ui.Switch(c.Key("jam-shown"), &on).Label("Show to people nearby").Changed() {
				a.settings.JamHidden = !on
				a.saveSettings()
				h.SetDiscoverable(on)
			}
		})
	})
	ui.Row(c).Justify(ui.End).Children(func() {
		if a.pillButton(c, "log-out", "End Jam", false).Background(p.danger).Clicked() {
			a.endJam()
		}
	})
}

// memberRow is someone in the Jam; it reports whether its button took
// them out.
func (a *App) memberRow(c *ui.Context, key, name, note string, removable bool) (removed bool) {
	p := a.pal
	ui.Row(c.Key("member-"+key)).Gap(10).Height(36).Padding(0, 10).Radius(p.radius).Children(func() {
		ui.Box(c).Size(24, 24).Radius(12).Center().Background(p.accent.Alpha(0.16)).Shrink(0).Children(func() {
			initial := strings.ToUpper(string([]rune(name + " ")[0]))
			ui.Text(c, initial).FontSize(11).FontWeight(700).TextColor(p.accent)
		})
		ui.Text(c, name).FontWeight(500).SingleLine().Grow(1).MinWidth(0)
		if note != "" {
			ui.Text(c, note).FontSize(12).TextColor(p.muted).Shrink(0)
		}
		if removable && a.iconButton(c, "x", "Remove "+name+" from the Jam", 26, 14).Clicked() {
			removed = true
		}
	})
	return removed
}

// jamGuestView is a guest's: who is in, and leaving.
func (a *App) jamGuestView(c *ui.Context) {
	j := &a.jam
	p := a.pal
	if j.away {
		ui.Text(c, "The connection to "+j.hostName+" was lost. Reconnecting…").TextColor(p.warning)
	}
	if j.selfPaused {
		ui.Row(c.Key("self-paused")).Gap(8).Padding(8, 10).Radius(p.radius).Background(p.accent.Alpha(0.1)).Children(func() {
			ui.Text(c, "Paused here only. The Jam goes on for everyone else.").FontWeight(500).Grow(1).MinWidth(0).MaxLines(2)
			if a.pillButton(c, "play", "Catch up", true).Clicked() {
				a.jamCatchUp()
			}
		})
	}
	rule := "Only " + j.hostName + " controls playback. You can add songs to the queue."
	if a.jamControls() {
		rule = "Everyone can play, pause, skip and change the queue."
	}
	ui.Text(c, rule).TextColor(p.muted).MaxLines(3)
	ui.Text(c, "PEOPLE").FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(p.faint).Margin(4, 0, 0)
	ui.Column(c).Gap(2).Children(func() {
		for _, m := range j.members {
			name, note := m.Name, ""
			switch {
			case m.Host:
				note = "Host"
			case m.ID == j.guest.ID():
				name += " (you)"
			case m.Away:
				note = "Reconnecting…"
			}
			a.memberRow(c, m.ID, name, note, false)
		}
	})
	a.jamActivity(c)
	ui.Text(c, "Your volume, equalizer and other sound settings stay your own. Your queue comes back when you leave.").FontSize(12).TextColor(p.faint).MaxLines(3)
	ui.Row(c).Justify(ui.End).Children(func() {
		if a.pillButton(c, "log-out", "Leave Jam", false).Clicked() {
			a.leaveJam(true)
			a.jam.open = false
		}
	})
}

// jamSettings is the Jam's switch, among the settings in beta.
func (a *App) jamSettings(c *ui.Context) {
	a.setting(c, "Jams", "Listen together with people on the same network: everyone hears the same queue on their own computer, with their own sound settings, and anyone can add songs. Songs one person's server lacks play from the person who added them.", func() {
		if ui.Switch(c.Key("jam-beta"), &a.settings.Jam).Label("Jams").Changed() {
			if !a.settings.Jam {
				a.stopJam()
			}
			a.saveSettings()
			a.tailAbout()
		}
	})
	a.tailSettings(c)
	a.jamRelaySettings(c)
}

// jamLine is a line of how the Jam is reached.
func (a *App) jamLine(c *ui.Context, glyph string, color ui.Color, text string) {
	ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, icon(glyph)).Size(14, 14).TextColor(color).Margin(2, 0, 0).Shrink(0)
		ui.Text(c, text).FontSize(12).TextColor(a.pal.muted).Grow(1).MinWidth(0).MaxLines(3)
	})
}

// jamRelaySettings is the user's relay, which Jams are reached through
// from any network.
func (a *App) jamRelaySettings(c *ui.Context) {
	j := &a.jam
	if !j.relayEdited {
		j.relayEdit, j.relayKeyEdit = a.settings.JamRelay, a.settings.JamRelayKey
	}
	a.setting(c, "Jam relay", "Your own relay (relay/ in Aurelia's source), on a server of yours: Jams you host are reached through it from any network, and codes not found on this one are looked for there. Its address and key.", func() {
		ui.Column(c).Gap(6).Width(250).Children(func() {
			if ui.TextInput(c.Key("jam-relay"), &j.relayEdit).Placeholder("https://relay.example.com").Label("Relay address").Changed() {
				j.relayEdited = true
			}
			if ui.TextInput(c.Key("jam-relay-key"), &j.relayKeyEdit).Password().Placeholder("Key").Label("Relay key").Changed() {
				j.relayEdited = true
			}
		})
		changed := strings.TrimSpace(j.relayEdit) != a.settings.JamRelay || strings.TrimSpace(j.relayKeyEdit) != a.settings.JamRelayKey
		if a.pillButton(c, "", "Save", changed).Disabled(!changed).Clicked() {
			base := strings.TrimSuffix(strings.TrimSpace(j.relayEdit), "/")
			if base != "" && !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://") {
				base = "https://" + base
			}
			a.settings.JamRelay, a.settings.JamRelayKey = base, strings.TrimSpace(j.relayKeyEdit)
			j.relayEdit, j.relayEdited = base, false
			a.saveSettings()
			a.jamRelay()
		}
	})
}

// jamOnlyView is the window of someone not signed in, in a Jam: what
// plays and what follows, without a library of their own.
func (a *App) jamOnlyView(c *ui.Context) {
	p := a.pal
	bar := c.TitleBar()
	root := ui.Column(c).Fill()
	a.shortcuts(c, root)
	a.tellSystem()
	root.Children(func() {
		ui.Row(c).Height(max(bar.Height, topBarH)).Shrink(0).Padding(0, max(bar.Right+8, 16), 0, max(bar.Left+8, 16)).Gap(10).DragWindow().Children(func() {
			ui.Text(c, a.jam.hostName+"'s Jam").FontSize(15).FontWeight(700).SingleLine()
			a.betaTag(c)
		})
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Center().Gap(8).Padding(24).Children(func() {
				song := a.player.current()
				if song == nil {
					ui.Text(c, "Nothing is playing yet").FontWeight(600).FontSize(16)
					ui.Text(c, "Songs added to the Jam play here.").TextColor(p.muted)
					return
				}
				a.art(c, song.ImageItem, song.ImageTag, bigArt, "music", nil).Size(bigArt, bigArt).Radius(p.radius+4).Shadow(0, 16, 40, 0, p.shadow)
				ui.Text(c, song.Name).FontSize(20).FontWeight(700).MaxLines(2).TextAlign(ui.Center).Margin(14, 0, 0)
				ui.Text(c, song.Artist).TextColor(p.muted).SingleLine()
			})
			a.queuePanel(c)
		})
		a.jamStrip(c)
		a.playerBar(c)
	})
	a.jamDialog(c)
	a.tailInviteDialog(c)
	a.toastLayer(c, float32(playerH+14+jamStripH))
}

// jamEvents is the Jam's log as this app has it, the latest last, and
// the time now by the host's clock.
func (a *App) jamEvents() ([]jam.Event, time.Time) {
	j := &a.jam
	switch {
	case j.host != nil:
		return j.log, time.Now()
	case j.guest != nil && j.state != nil:
		return j.state.Log, j.guest.HostNow()
	}
	return nil, time.Now()
}

// jamActivity is what was done in the Jam, and by whom, the latest first:
// a few, or all of them.
func (a *App) jamActivity(c *ui.Context) {
	events, now := a.jamEvents()
	if len(events) == 0 {
		return
	}
	j := &a.jam
	p := a.pal
	shown := 5
	if j.logOpen {
		shown = len(events)
	}
	ui.Row(c).Margin(4, 0, 0).Children(func() {
		ui.Text(c, "ACTIVITY").FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(p.faint).Grow(1)
		if len(events) > 5 {
			label := "Show all"
			if j.logOpen {
				label = "Show less"
			}
			if a.textButton(c, label).Clicked() {
				j.logOpen = !j.logOpen
			}
		}
	})
	ui.Column(c).Gap(2).Children(func() {
		for i := len(events) - 1; i >= 0 && i >= len(events)-shown; i-- {
			a.jamEventRow(c, events[i], now)
		}
	})
	// "now" becomes "1m".
	c.After(20 * time.Second)
}

// jamEventRow is one thing done: its sign, who, what and to which song,
// and how long ago.
func (a *App) jamEventRow(c *ui.Context, ev jam.Event, now time.Time) {
	p := a.pal
	verb, song, more := jamEventWords(ev)
	ui.Row(c.Key("event-"+strconv.FormatInt(ev.ID, 10))).Gap(10).Height(32).Padding(0, 10).Radius(p.radius).Children(func() {
		ui.Box(c).Size(24, 24).Radius(12).Center().Background(p.surface).Shrink(0).Children(func() {
			ui.Icon(c, icon(jamEventIcon(ev))).Size(13, 13).TextColor(p.muted)
		})
		ui.Row(c).Gap(4).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, ev.Who).FontWeight(600).SingleLine().Shrink(0).MaxWidth(150)
			ui.Text(c, verb).TextColor(p.muted).SingleLine().Shrink(0)
			if song != "" {
				ui.Text(c, song).FontWeight(500).SingleLine().MinWidth(0).Shrink(1)
			}
			if more != "" {
				ui.Text(c, strings.TrimSpace(more)).TextColor(p.muted).SingleLine().Shrink(0)
			}
		})
		ui.Text(c, agoShort(now.Sub(time.UnixMilli(ev.At)))).FontSize(12).TextColor(p.faint).Shrink(0).FontFeatures("tnum")
	}).Tooltip(jamEventText(ev))
}

// jamEventIcon is the sign of an event.
func jamEventIcon(ev jam.Event) string {
	switch ev.Op {
	case jam.OpPause:
		return "pause"
	case jam.OpResume:
		return "play"
	case jam.OpSkip:
		return "skip-forward"
	case jam.OpPrevious:
		return "skip-back"
	case jam.OpSeek:
		return "clock"
	case jam.OpPlay, jam.EvStart:
		return "square-play"
	case jam.OpAdd:
		return "list-plus"
	case jam.OpNext:
		return "list-start"
	case jam.OpRemove, jam.OpClear:
		return "trash"
	case jam.OpMove:
		return "list-music"
	case jam.OpShuffle:
		return "shuffle"
	case jam.OpRepeat:
		if ev.Mode == repeatOne {
			return "repeat-1"
		}
		return "repeat"
	case jam.EvJoined, jam.EvBack:
		return "users-round"
	case jam.EvLeft:
		return "log-out"
	case jam.EvAway:
		return "wifi-off"
	}
	return "music"
}

// agoShort says how long ago, shortly: "now", "4m", "2h".
func agoShort(d time.Duration) string {
	switch {
	case d < 45*time.Second:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Round(time.Minute)/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

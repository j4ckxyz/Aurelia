package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/jam"
)

// What Tailscale shows of Jams: the devices of the tailnet, in the Jam's
// dialog; an invite come from one; and the door's setting.

// tailSection lists the devices of the tailnet that run Aurelia, to
// invite to the Jam held here or join the one they hold. It is not there
// without a tailnet, nor for a guest.
func (a *App) tailSection(c *ui.Context) {
	t := &a.tail
	if !a.tailUp() || a.jam.guest != nil {
		return
	}
	p := a.pal
	canHost := a.jam.host != nil || a.signedIn()
	devices := a.tailDevices()
	ui.Text(c, "TAILSCALE").FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(p.faint).Margin(6, 0, 0)
	if len(devices) == 0 {
		msg := "Looking for Aurelia on your tailnet…"
		if !t.probing && !t.probedAt.IsZero() {
			msg = "No other device on your tailnet has Aurelia open."
		} else {
			c.After(time.Second)
		}
		ui.Text(c, msg).FontSize(12).TextColor(p.muted)
	} else {
		// Four rows, and the rest scrolled to, on a big tailnet.
		list := func() {
			for _, d := range devices {
				a.tailRow(c, d, canHost)
			}
		}
		if len(devices) > 4 {
			ui.Scroll(c.Key("tail-devices")).Height(4*61 - 6 + 30).Gap(6).Children(list)
		} else {
			ui.Column(c).Gap(6).Children(list)
		}
	}
	if canHost {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, "Anyone who can reach this computer on Tailscale can join with its invite.").FontSize(12).TextColor(p.faint).Grow(1).MinWidth(0).MaxLines(2)
			if a.textButton(c, "Copy tailnet invite").Clicked() {
				a.tailCopyInvite()
			}
		})
	}
}

// tailRow is a device of the tailnet: whose it is, how Aurelia is there,
// and what may be done.
func (a *App) tailRow(c *ui.Context, d tailDevice, canHost bool) {
	t := &a.tail
	p := a.pal
	name, status := d.Name, ""
	glyph := "monitor"
	about := d.about
	switch {
	case d.Sharee:
		name, glyph = cmpOr(d.Owner.Name, d.Owner.Login, "Someone"), "user-round"
		status = "Can join you"
	case d.member:
		status = "In your Jam"
	case about != nil && about.Proto != jam.Proto:
		status = "Needs the same version of Aurelia"
	case about != nil && about.Live != nil:
		status = "Hosting " + about.Live.Host + "'s Jam · " + count(about.Live.People, "person", "people")
	case about != nil && !about.Jam:
		status = "Aurelia is open, Jams off"
	case about != nil:
		status = "Aurelia is open"
	case !d.Online:
		status = "Offline"
	default:
		status = "Aurelia isn't open"
	}
	switch {
	case d.member || d.Sharee:
	case t.sent[d.ID] == inviteSent:
		status = "Invited"
	case t.sent[d.ID] == inviteFailed:
		status = "Did not answer the invite"
	}
	owner := d.Owner.Label()
	if d.Sharee {
		owner = cmpOr(d.Owner.Login, "Shared with")
	}
	ui.Row(c.Key("tail-"+d.ID)).Gap(10).Padding(8, 10).Radius(p.radius).Background(p.surface.Alpha(0.6)).Children(func() {
		ui.Icon(c, icon(glyph)).Size(16, 16).TextColor(p.accent).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, name).FontWeight(600).SingleLine()
			ui.Text(c, owner+" · "+status).FontSize(12).TextColor(p.muted).MaxLines(2)
		})
		switch {
		case d.Sharee || d.member:
			// They join with the invite link: this computer may not
			// reach them, nor need it.
		case about != nil && about.Proto != jam.Proto:
		case about != nil && about.Live != nil && !a.jam.in():
			if a.pillButton(c, "", "Join", false).Disabled(a.jam.joining).Tooltip(joinTip(d)).Clicked() {
				a.tailJoin(d)
			}
		case !canHost:
		case about != nil && t.sent[d.ID] != inviteFailed:
			label := "Invite"
			switch t.sent[d.ID] {
			case inviteSending:
				label = "Inviting…"
			case inviteSent:
				label = "Invited"
			}
			if a.pillButton(c, "", label, false).Disabled(t.sent[d.ID] != "").Clicked() {
				a.tailInvite(d.Device)
			}
		default:
			// Aurelia is closed there, or did not answer: a link to send.
			if a.pillButton(c, "copy", "Copy invite link", false).Clicked() {
				a.tailCopyInvite()
			}
		}
	})
}

func joinTip(d tailDevice) string {
	if d.Owner.Mine {
		return "Your device: you join at once"
	}
	return "Asks " + d.about.Live.Host + " to let you in"
}

// tailInviteDialog is an invite come from a device of the tailnet,
// waiting for an answer: from whom Tailscale says it is.
func (a *App) tailInviteDialog(c *ui.Context) {
	t := &a.tail
	for len(t.invites) > 0 && time.Since(t.invites[0].at) > 10*time.Minute {
		t.invites = t.invites[1:]
	}
	if len(t.invites) == 0 || a.jam.open {
		return
	}
	inv := t.invites[0]
	p := a.pal
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Gap(14).Padding(22).Width(420).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
				ui.Icon(c, icon("users-round")).Size(18, 18).TextColor(p.accent).Margin(3, 0, 0).Shrink(0)
				ui.Text(c, inviter(inv.from)+" invited you to a Jam (beta)").FontSize(17).FontWeight(700).MaxLines(3).Grow(1).MinWidth(0)
			})
			ui.Text(c, "Listen together: everyone hears the same queue, each on their own device with their own sound.").TextColor(p.muted).MaxLines(4)
			who := "Tailscale says it's from your device " + inv.from.Device + "."
			if !inv.from.Mine {
				who = "Tailscale says it's from " + inv.from.Device + ", " + cmpOr(inv.from.User, inv.from.Login) + "'s device"
				if inv.from.Login != "" && inv.from.Login != inv.from.User {
					who += " (" + inv.from.Login + ")"
				}
				who += "."
			}
			ui.Text(c, who).FontSize(12).TextColor(p.faint).MaxLines(3)
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if a.textButton(c, "Not now").Clicked() {
					a.answerInvite(inv.id, false)
				}
				label := "Turn on and join"
				switch {
				case a.jam.in():
					label = "Leave this Jam and join"
				case a.settings.Jam:
					label = "Join"
				}
				if a.pillButton(c, "users-round", label, true).Clicked() {
					a.answerInvite(inv.id, true)
				}
			})
		})
	})
	if !open {
		a.answerInvite(inv.id, false)
	}
}

// tailSettings is the door's switch, among the settings in beta, for a
// computer with Tailscale.
func (a *App) tailSettings(c *ui.Context) {
	if !a.tail.known {
		return
	}
	a.setting(c, "Let devices on my tailnet invite me to Jams", "Devices on your Tailscale network can see that Aurelia is open here and invite you, even while Jams are off. Nothing outside your tailnet can reach it.", func() {
		on := !a.settings.TailnetInvitesOff
		if ui.Switch(c.Key("tail-invites"), &on).Label("Let devices on my tailnet invite me to Jams").Changed() {
			a.settings.TailnetInvitesOff = !on
			a.saveSettings()
			a.tailDoor()
		}
	})
}

// tailInviteBanner shows, in the Jam's dialog, an invite that came while
// it was open, which the invite's own dialog waits behind.
func (a *App) tailInviteBanner(c *ui.Context) {
	t := &a.tail
	if len(t.invites) == 0 || a.jam.joining {
		return
	}
	inv := t.invites[0]
	p := a.pal
	ui.Row(c.Key("invite-"+inv.id)).Gap(8).Padding(8, 10).Radius(p.radius).Background(p.accent.Alpha(0.1)).Children(func() {
		ui.Text(c, inviter(inv.from)+" invited you to their Jam").FontWeight(500).Grow(1).MinWidth(0).SingleLine()
		if a.textButton(c, "Not now").Clicked() {
			a.answerInvite(inv.id, false)
		}
		if a.pillButton(c, "", "Join", true).Clicked() {
			a.answerInvite(inv.id, true)
		}
	})
}

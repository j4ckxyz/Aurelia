package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// loginForm is what the sign-in page holds.
type loginForm struct {
	server, user, password string
	// proxy is the proxy to sign in through, and proxyShown whether its
	// field shows: most people need none.
	proxy      string
	proxyShown bool
	busy       bool
	err        string
}

// loginPage asks for a server and who to sign in as.
func (a *App) loginPage(c *ui.Context) {
	p := a.pal
	f := &a.login
	bar := c.TitleBar()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(max(bar.Height, topBarH)).DragWindow().Justify(ui.End).Padding(0, 12).Children(func() {
			a.windowControls(c)
		})
		ui.Column(c).Grow(1).Center().Padding(24).Children(func() {
			ui.Column(c).Width(360).Gap(14).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c).AlignItems(ui.Center).Gap(8).Margin(0, 0, 10).Children(func() {
					ui.Box(c).Size(64, 64).Radius(18).Center().Background(p.accent).Children(func() {
						ui.Icon(c, icon("logo")).Size(38, 38).TextColor(p.onAcc)
					})
					ui.Text(c, "Aurelia").FontSize(26).FontWeight(800).Margin(10, 0, 0)
					ui.Text(c, "Sign in to your Jellyfin server").TextColor(p.muted)
				})
				field := func(label string, value *string, placeholder string) ui.Element {
					var in ui.Element
					ui.Column(c).Gap(6).Children(func() {
						ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(p.muted)
						in = ui.TextInput(c.Key(label), value).Placeholder(placeholder).Label(label).Height(36).Disabled(f.busy)
					})
					return in
				}
				server := field("Server", &f.server, "https://jellyfin.example.com")
				if f.server == "" && f.user == "" {
					server.AutoFocus()
				}
				user := field("Username", &f.user, "")
				pw := field("Password", &f.password, "").Password()
				ready := f.server != "" && f.user != "" && !f.busy
				submit := server.Submitted() || user.Submitted() || pw.Submitted()
				if f.proxyShown {
					if field("Proxy", &f.proxy, "socks5://host:1080 or http://host:8080").Submitted() {
						submit = true
					}
					ui.Text(c, "For networks that block your server: Aurelia reaches it through this HTTP or SOCKS5 proxy instead. Leave empty for none.").
						FontSize(12).TextColor(p.muted)
					if risk := proxyRisk(f.server); risk != "" && strings.TrimSpace(f.proxy) != "" {
						ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
							ui.Icon(c, icon("circle-alert")).Size(14, 14).TextColor(p.warning).Margin(1, 0, 0)
							ui.Text(c, risk).FontSize(12).TextColor(p.muted).Grow(1)
						})
					}
				}
				b := ui.ButtonBase(c).Height(38).Radius(p.radius).Margin(6, 0, 0).Disabled(!ready).Label("Sign in")
				switch {
				case !ready:
					b.Background(p.surface)
				case b.Hovered():
					b.Background(p.accentHover)
				default:
					b.Background(p.accent)
				}
				b.Children(func() {
					fg := p.onAcc
					if !ready {
						fg = p.muted
					}
					if f.busy {
						spin := ui.Icon(c, icon("loader-circle")).Size(16, 16).TextColor(fg)
						spin.Rotate(spin.Loop("spin", time.Second, ui.Linear) * 360)
						ui.Text(c, "Signing in…").TextColor(fg).FontWeight(600).Margin(0, 0, 0, 8)
						return
					}
					ui.Text(c, "Sign in").TextColor(fg).FontWeight(600)
				})
				if ready && (b.Clicked() || submit) {
					a.signIn()
				}
				if f.err != "" {
					ui.Row(c).Gap(8).AlignItems(ui.Start).Padding(10, 12).Radius(p.radius).Background(p.danger.Alpha(0.12)).Children(func() {
						ui.Icon(c, icon("circle-alert")).Size(15, 15).TextColor(p.danger).Margin(1, 0, 0)
						ui.Text(c, f.err).TextColor(p.text).Grow(1)
					})
				}
				if !f.proxyShown {
					ui.Row(c).Justify(ui.Center).Children(func() {
						if a.textButton(c, "Connect through a proxy").Clicked() {
							f.proxyShown = true
						}
					})
				}
			})
		})
	})
}

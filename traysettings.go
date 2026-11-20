package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

// traySettings are the rows of Settings ▸ Desktop that choose how the icon
// in the menu bar or the tray looks: Aurelia's icon in a color, or the
// cover of the song playing, and the song beside it; with a picture of
// how it looks.
func (a *App) traySettings(c *ui.Context) {
	p := a.pal
	set := &a.settings
	a.setting(c, "Looks like", a.trayLookHelp(), func() { a.trayPreview(c) })
	a.setting(c, "Icon", "Album artwork shows the cover of the song playing, and Aurelia's icon while nothing plays.", func() {
		names := []string{"Aurelia's icon", "Album artwork"}
		at := names[0]
		if set.TrayIcon == "art" {
			at = names[1]
		}
		if ui.Select(c.Key("tray-icon"), &at, names).Width(170).Label("Icon").Changed() {
			set.TrayIcon = ""
			if at == names[1] {
				set.TrayIcon = "art"
			}
			a.trayChanged()
		}
	})
	if set.TrayIcon != "art" {
		help := "Automatic is the menu bar's own black or white."
		if runtime.GOOS != "darwin" {
			help = "Automatic is the theme's color. White or black stand out on a dark or a light panel."
		}
		a.setting(c, "Icon color", help, func() {
			ui.Row(c).Gap(6).Children(func() {
				for _, tc := range trayColors {
					on := set.TrayColor == tc.id
					b := ui.ButtonBase(c.Key("tray-color-"+tc.id)).Size(26, 26).Radius(13).Center().Label(tc.name).Tooltip(tc.name).Cursor(ui.CursorPointer)
					if on {
						b.Border(2, p.accent)
					} else if b.Hovered() {
						b.Border(2, p.border)
					}
					b.Children(func() {
						dot := ui.Box(c).Size(18, 18).Radius(9).Border(1, p.border)
						switch {
						case tc.hex != "":
							dot.Background(ui.Hex(tc.hex))
						case runtime.GOOS == "darwin":
							// The menu bar's: half black, half white.
							dot.LinearGradient(ui.LinearGradient{From: ui.RGB(28, 28, 30), To: ui.RGB(255, 255, 255), Angle: 135, Start: 0.5, End: 0.5})
						default:
							// The theme's color, and the theme's around it.
							dot.LinearGradient(ui.LinearGradient{From: p.accent, To: p.bg, Angle: 135, Start: 0.5, End: 0.5})
						}
					})
					if b.Clicked() && !on {
						set.TrayColor = tc.id
						a.trayChanged()
					}
				}
			})
		})
	}
	if trayTextShows() {
		a.setting(c, "Beside the icon", a.trayTextHelp(), func() {
			names := make([]string, len(trayTexts))
			at := names[0]
			for i, t := range trayTexts {
				names[i] = t.name
				if t.id == set.TrayText {
					at = t.name
				}
			}
			if at == "" {
				at = names[0]
			}
			if ui.Select(c.Key("tray-text"), &at, names).Width(210).Label("Beside the icon").Changed() {
				for _, t := range trayTexts {
					if t.name == at {
						set.TrayText = t.id
					}
				}
				a.trayChanged()
			}
		})
	}
}

// trayLookHelp says where the icon is, and what shows of the song on
// this system.
func (a *App) trayLookHelp() string {
	switch runtime.GOOS {
	case "darwin":
		return "In the menu bar, at the top right of the screen."
	case "windows":
		return "In the taskbar's corner, by the clock; it may be under the ^. Windows shows the song when you point at it."
	}
	return "In your panel's tray or indicators."
}

// trayTextHelp says where text beside the icon shows.
func (a *App) trayTextHelp() string {
	if runtime.GOOS == "darwin" {
		return "The menu bar shows it next to the icon; a long name is cut short."
	}
	return "Shown by GNOME (with its AppIndicator extension), Ubuntu, Xfce, Cinnamon, MATE and Budgie. KDE Plasma shows the icon alone, and the song when you point at it."
}

// trayPreview is a picture of the icon as the settings make it, with the
// song playing, on a strip like the menu bar or the panel.
func (a *App) trayPreview(c *ui.Context) {
	p := a.pal
	s := a.player.current()
	pic, template, key, title := a.trayLook(s)
	if s == nil && trayTextShows() && a.settings.TrayText != "" {
		// Nothing plays: what would show, of a song.
		title = map[string]string{"song": "Song", "artist": "Song — Artist"}[a.settings.TrayText]
	}
	bmp := a.desk.previewBitmap(key, pic)
	ui.Row(c.Key("tray-preview")).Height(30).Padding(0, 10).Gap(6).Radius(8).Background(p.surface).Border(1, p.border).
		AlignItems(ui.Center).MaxWidth(300).Children(func() {
		switch {
		case template:
			// The menu bar tints it: as the text is.
			ui.Icon(c, icon("logo")).Size(18, 18).TextColor(p.text).Shrink(0)
		case bmp != nil:
			ui.Image(c, bmp).Size(18, 18).Shrink(0)
		}
		if title != "" {
			ui.Text(c, title).FontSize(13).FontWeight(500).SingleLine().Shrink(1).MinWidth(0)
		}
	})
}

// previewBitmap is the icon's picture for the preview, made again only as
// it changes.
func (d *desktopState) previewBitmap(key string, pic []byte) *ui.Bitmap {
	if d.previewKey == key && d.preview != nil {
		return d.preview
	}
	d.previewKey, d.preview = key, nil
	if img, err := decodePNG(pic); err == nil {
		d.preview = ui.NewBitmap(img)
	}
	return d.preview
}

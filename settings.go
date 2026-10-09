package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
	"aurelia/internal/theme"
)

// card groups settings in a box.
func (a *App) card(c *ui.Context, fn func()) {
	p := a.pal
	ui.Column(c).Radius(p.radius+2).Background(p.surface.Alpha(0.5)).Border(1, p.border).Dividers(1, p.border).Children(fn)
}

// setting is a row of a card: a name, a line about it, and its control.
func (a *App) setting(c *ui.Context, label, help string, control func()) {
	p := a.pal
	ui.Row(c).Padding(12, 16).Gap(16).MinHeight(52).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, label).FontWeight(500)
			if help != "" {
				ui.Text(c, help).FontSize(12).TextColor(p.muted)
			}
		})
		ui.Row(c).Gap(8).Shrink(0).Children(control)
	})
}

// cacheSizes is what the caches hold on disk, measured on another
// goroutine: thousands of files take a while to count.
type cacheSizes struct {
	at            time.Time
	measuring     bool
	audio, images int64
}

// measure counts the caches again, unless they were counted lately.
func (a *App) measureCaches() {
	if a.sizes.measuring || time.Since(a.sizes.at) < 5*time.Second {
		return
	}
	a.sizes.measuring = true
	go func() {
		audio, images := a.player.cache.Size(), a.images.diskSize()
		a.update(func() {
			a.sizes = cacheSizes{at: time.Now(), audio: audio, images: images}
		})
	}()
}

var qualities = []string{"Original", "320 kbps", "192 kbps", "128 kbps"}
var qualityRates = []int{0, 320, 192, 128}
var cacheLimits = []string{"100 MB", "300 MB", "1 GB", "2 GB", "5 GB"}
var cacheLimitMB = []int{100, 300, 1024, 2048, 5120}
var pictureLimits = []string{"50 MB", "150 MB", "500 MB"}
var pictureLimitMB = []int{50, 150, 500}

func indexOf(list []int, v int) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return 1 // the default of each list
}

// settingsPage shows the settings: the theme, how songs play, the
// library, and the account.
func (a *App) settingsPage(c *ui.Context) {
	p := a.pal
	// Themes written by hand show as their files are saved.
	c.After(time.Second)
	if a.themes.Changed() {
		a.themeGen++
	}
	a.measureCaches()
	ui.Scroll(c.Key("settings")).Grow(1).MinHeight(0).Padding(0, 0, 40).Children(func() {
		a.pageTitle(c, "Settings", "", nil)
		ui.Column(c).Padding(0, pagePad).Gap(10).MaxWidth(860).Children(func() {
			a.settingsHead(c, "Appearance")
			a.themeGrid(c)
			ui.Row(c).Gap(8).Margin(4, 0, 0).Wrap().Children(func() {
				if a.pillButton(c, "plus", "New theme", false).Clicked() {
					a.goTo("/theme/" + a.theme().ID)
				}
				if a.pillButton(c, "import", "Import…", false).Tooltip("A Visual Studio Code theme (.json, .vsix), a Firefox or Chrome theme (manifest.json, .xpi, .crx, .zip), or one of Aurelia's").Clicked() {
					a.importTheme()
				}
				if a.pillButton(c, "folder-open", "Themes folder", false).Clicked() {
					os.MkdirAll(a.dirs.themes(), 0o755)
					c.OpenURL((&url.URL{Scheme: "file", Path: a.dirs.themes()}).String())
				}
				if t := a.themes.Get(a.settings.Theme); t != nil && !t.BuiltIn {
					if a.pillButton(c, "pencil", "Edit", false).Clicked() {
						a.goTo("/theme/" + t.ID)
					}
					if a.pillButton(c, "trash", "Delete", false).Clicked() {
						if err := a.themes.Delete(t.ID); err != nil {
							a.toastError("Could not delete the theme", err)
						}
						a.themeGen++
						a.settings.Theme = "auto"
						a.saveSettings()
					}
				}
			})
			for name, why := range a.themes.Errors {
				ui.Row(c).Gap(8).Children(func() {
					ui.Icon(c, icon("circle-alert")).Size(14, 14).TextColor(p.warning)
					ui.Text(c, name+" in the themes folder is not a theme: "+why).FontSize(12).TextColor(p.muted)
				})
			}

			a.settingsHead(c, "Playback")
			a.card(c, func() {
				a.setting(c, "Normalize volume", "Play every song about as loud, with the gain the server measured.", func() {
					if ui.Switch(c.Key("normalize"), &a.settings.Normalize).Label("Normalize volume").Changed() {
						if a.player.engine != nil {
							a.player.engine.SetNormalize(a.settings.Normalize)
						}
						a.saveSettings()
					}
				})
				a.setting(c, "Streaming quality", "Original plays files as they are. Lower rates have the server convert to MP3, for slow or metered connections.", func() {
					q := qualities[indexOf(qualityRates, a.settings.MaxBitrate)]
					if ui.Select(c.Key("quality"), &q, qualities).Width(150).Changed() {
						for i, name := range qualities {
							if name == q {
								a.settings.MaxBitrate = qualityRates[i]
							}
						}
						a.saveSettings()
					}
				})
				if a.player.err != nil {
					a.setting(c, "No sound", "The sound card could not be opened: "+a.player.err.Error(), func() {
						ui.Icon(c, icon("circle-alert")).Size(18, 18).TextColor(p.danger)
					})
				}
			})

			a.settingsHead(c, "Storage")
			a.card(c, func() {
				d := a.downloads
				a.setting(c, "Downloads", fmt.Sprintf("%s kept to play without the server, using %s. They stay until you remove them.", count(len(d.done), "song", "songs"), bytesText(d.size())), func() {
					if a.pillButton(c, "", "Manage", false).Clicked() {
						a.goTo("/downloads")
					}
				})
				a.setting(c, "Songs played lately", fmt.Sprintf("Kept for a while so that they start at once and play again without the network. Using %s; the oldest make room.", bytesText(a.sizes.audio)), func() {
					lim := cacheLimits[indexOf(cacheLimitMB, a.settings.AudioCacheMB)]
					if ui.Select(c.Key("cache"), &lim, cacheLimits).Width(110).Changed() {
						for i, name := range cacheLimits {
							if name == lim {
								a.settings.AudioCacheMB = cacheLimitMB[i]
							}
						}
						a.player.cache.SetMaxBytes(int64(a.settings.AudioCacheMB) << 20)
						a.saveSettings()
						a.sizes.at = time.Time{}
					}
					if a.pillButton(c, "", "Clear", false).Clicked() {
						a.player.cache.Clear()
						a.sizes.at = time.Time{}
					}
				})
				a.setting(c, "Pictures", fmt.Sprintf("Album and artist pictures, kept so that they show at once. Using %s; those shown longest ago make room.", bytesText(a.sizes.images)), func() {
					lim := pictureLimits[indexOf(pictureLimitMB, a.settings.PictureCacheMB)]
					if ui.Select(c.Key("pictures"), &lim, pictureLimits).Width(110).Changed() {
						for i, name := range pictureLimits {
							if name == lim {
								a.settings.PictureCacheMB = pictureLimitMB[i]
							}
						}
						a.images.maxDisk.Store(int64(a.settings.PictureCacheMB) << 20)
						go a.images.trim()
						a.saveSettings()
						a.sizes.at = time.Time{}
					}
					if a.pillButton(c, "", "Clear", false).Clicked() {
						a.images.clear()
						a.sizes.at = time.Time{}
					}
				})
			})

			a.settingsHead(c, "Library")
			a.card(c, func() {
				l := a.lib
				info := fmt.Sprintf("%s, %s, %s", count(len(l.Albums), "album", "albums"), count(len(l.Artists), "artist", "artists"), count(len(l.Songs), "song", "songs"))
				switch {
				case a.syncing && a.progress.Total > 0:
					info += fmt.Sprintf(" · updating, %d%%", 100*a.progress.Done/a.progress.Total)
				case a.syncing:
					info += " · updating"
				case a.syncErr != "":
					info += " · not updated: " + a.syncErr
				case !l.SyncedAt.IsZero():
					info += " · updated " + ago(l.SyncedAt)
				}
				a.setting(c, "Music library", info, func() {
					if a.pillButton(c, "refresh-cw", "Update now", false).Disabled(a.syncing).Clicked() {
						a.sync()
					}
				})
			})

			a.settingsHead(c, "Account")
			a.card(c, func() {
				if s := a.settings.Session; s != nil {
					a.setting(c, s.UserName, s.ServerName+" · "+s.Server, func() {
						if a.pillButton(c, "log-out", "Sign out", false).Clicked() {
							a.signOut()
						}
					})
				}
			})

			a.settingsHead(c, "About")
			a.card(c, func() {
				a.setting(c, "Aurelia "+jellyfin.Version, "A native music player for Jellyfin, built with MyGo "+mygo.Version+". Icons by Lucide, under the ISC license.", nil2)
			})
		})
	})
}

func nil2() {}

func (a *App) settingsHead(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(15).FontWeight(700).Margin(18, 0, 2)
}

// ago words a time in the past.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return count(int(d.Minutes()), "minute", "minutes") + " ago"
	case d < 24*time.Hour:
		return count(int(d.Hours()), "hour", "hours") + " ago"
	}
	return count(int(d.Hours()/24), "day", "days") + " ago"
}

// themeGrid shows every theme as a small picture of the window in its
// colors; a click chooses one.
func (a *App) themeGrid(c *ui.Context) {
	w := a.contentWidth(c) - 2*pagePad
	cols := max(2, int(min(w, 860)/150))
	themes := a.themes.Themes()
	n := len(themes) + 1 // and the one that follows the system
	for start := 0; start < n; start += cols {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			for i := start; i < start+cols; i++ {
				switch {
				case i == 0:
					dark, light := a.themes.Get(theme.DefaultDark), a.themes.Get(theme.DefaultLight)
					a.themeCard(c, "auto", "Match system", dark, light)
				case i <= len(themes):
					t := themes[i-1]
					a.themeCard(c, t.ID, t.Name, t, nil)
				default:
					ui.Box(c).Grow(1).Basis(0)
				}
			}
		})
	}
}

// themeCard is a theme of the grid: a picture of the window in its
// colors, or in those of two themes, half each.
func (a *App) themeCard(c *ui.Context, id, name string, t, second *theme.Theme) {
	p := a.pal
	chosen := a.settings.Theme == id
	b := ui.ButtonBase(c.Key("theme-" + id)).Column().AlignItems(ui.Stretch).Justify(ui.Start).Grow(1).Basis(0).MinWidth(0).Gap(8).Padding(6).Radius(p.radius + 4).Label(name).Checked(chosen)
	if b.Hovered() {
		b.Background(p.surface)
	}
	b.Children(func() {
		pic := ui.Box(c).AspectRatio(1.6).Radius(p.radius).Clip()
		if chosen {
			pic.Border(2, p.accent)
		} else {
			pic.Border(1, p.border)
		}
		pic.Draw(func(g *ui.Painter, r ui.Rect) {
			drawThemePreview(g, r, t)
			if second != nil {
				g.Clip(ui.Rect{X: r.X + r.W/2, Y: r.Y, W: r.W / 2, H: r.H}, 0, func() {
					drawThemePreview(g, r, second)
				})
			}
		})
		ui.Row(c).Gap(6).Padding(0, 2, 2).Children(func() {
			label := ui.Text(c, name).FontSize(12).FontWeight(500).SingleLine().Grow(1).MinWidth(0)
			if chosen {
				label.TextColor(p.text)
				ui.Icon(c, icon("check")).Size(13, 13).TextColor(p.accent)
			} else {
				label.TextColor(p.muted)
			}
		})
	})
	if b.Clicked() {
		a.settings.Theme = id
		a.saveSettings()
	}
	if t != nil && second == nil {
		b.ContextMenu(func(m *ui.Menu) {
			if m.Item("Duplicate and Edit").Chosen() {
				a.goTo("/theme/" + t.ID)
			}
			if !t.BuiltIn {
				if m.Item("Edit").Chosen() {
					a.settings.Theme = t.ID
					a.goTo("/theme/" + t.ID)
				}
				if m.Item("Delete").Chosen() {
					a.themes.Delete(t.ID)
					a.themeGen++
					if a.settings.Theme == t.ID {
						a.settings.Theme = "auto"
					}
					a.saveSettings()
				}
			}
		})
	}
}

// drawThemePreview paints the window in small: its sidebar, a few rows,
// and the player's bar with the accent.
func drawThemePreview(g *ui.Painter, r ui.Rect, t *theme.Theme) {
	if t == nil {
		return
	}
	g.Fill(r, col(t.Background), 0)
	side := r.W * 0.28
	bar := r.H * 0.2
	g.Fill(ui.Rect{X: r.X, Y: r.Y, W: side, H: r.H - bar}, col(t.Sidebar), 0)
	g.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - bar, W: r.W, H: bar}, col(t.Bar), 0)
	g.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - bar, W: r.W, H: 1}, col(t.Border), 0)
	u := r.H / 16
	for i := 0; i < 3; i++ {
		c := t.TextMuted
		if i == 0 {
			c = t.Text
		}
		g.Fill(ui.Rect{X: r.X + u, Y: r.Y + u*(2+2*float32(i)), W: side - 2*u - float32(i)*u, H: u * 0.9}, col(c), u/2)
	}
	x := r.X + side + u*1.2
	g.Fill(ui.Rect{X: x, Y: r.Y + u*1.6, W: r.W * 0.3, H: u * 1.4}, col(t.Text), u/2)
	for i := 0; i < 3; i++ {
		y := r.Y + u*(4.6+2.2*float32(i))
		g.Fill(ui.Rect{X: x, Y: y, W: u * 1.6, H: u * 1.6}, col(t.Surface), u/3)
		c := t.TextMuted
		if i == 1 {
			c = t.Accent
		}
		g.Fill(ui.Rect{X: x + u*2.4, Y: y + u*0.4, W: r.W*0.36 - float32(i)*u*1.5, H: u * 0.8}, col(c), u/2)
	}
	cy := r.Y + r.H - bar/2
	g.Fill(ui.Rect{X: r.X + r.W/2 - u, Y: cy - u, W: 2 * u, H: 2 * u}, col(t.Accent), u)
	g.Fill(ui.Rect{X: r.X + u, Y: cy - u*0.4, W: r.W * 0.18, H: u * 0.8}, col(t.Text), u/2)
}

// importTheme asks for a file and brings in the themes it holds.
func (a *App) importTheme() {
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:   win,
			Title:    "Import a theme",
			Multiple: true,
			Filters: []mygo.FileFilter{
				{Name: "Themes", Extensions: []string{"json", "jsonc", "vsix", "xpi", "crx", "zip"}},
			},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.update(func() { a.importThemeFiles(paths) })
	}()
}

// importThemeFiles brings in the themes of files, and shows the last.
func (a *App) importThemeFiles(paths []string) {
	var last string
	n := 0
	for _, path := range paths {
		ids, err := a.themes.Import(path)
		if err != nil {
			a.toastError("Could not import "+filepath.Base(path), err)
			continue
		}
		n += len(ids)
		if len(ids) > 0 {
			last = ids[0]
		}
	}
	if n == 0 {
		return
	}
	a.themeGen++
	a.settings.Theme = last
	a.saveSettings()
	if n == 1 {
		a.toast("Imported " + a.themes.Get(last).Name)
	} else {
		a.toast(fmt.Sprintf("Imported %d themes", n))
	}
}

package main

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/theme"
)

// themeEditor is a theme being edited. The whole window shows it as it
// changes: the page is its own preview.
type themeEditor struct {
	// id is the page's: the theme edited, or the one a new theme starts
	// from.
	id string
	// path is the file of the theme edited, "" for a new one.
	path   string
	name   string
	dark   bool
	radius float64
	font   string
	colors map[string]ui.Color
	hex    map[string]string // the hex fields, as typed

	cached *theme.Theme
}

// newThemeEditor starts editing a theme of the user's, or a new one from
// a copy of another.
func newThemeEditor(id string, t *theme.Theme) *themeEditor {
	e := &themeEditor{id: id, name: t.Name, dark: t.Dark, radius: t.Radius, font: t.Font, colors: map[string]ui.Color{}, hex: map[string]string{}}
	if t.BuiltIn || t.Path == "" {
		e.name = t.Name + " Copy"
	} else {
		e.path = t.Path
	}
	for _, k := range theme.Keys {
		c := t.Color(k.Name)
		e.colors[k.Name] = col(c)
		e.hex[k.Name] = c.Hex()
	}
	return e
}

func (e *themeEditor) spec() theme.Spec {
	s := theme.Spec{Name: strings.TrimSpace(e.name), Type: "light", Colors: map[string]string{}, Radius: e.radius, Font: strings.TrimSpace(e.font)}
	if e.dark {
		s.Type = "dark"
	}
	if s.Name == "" {
		s.Name = "My theme"
	}
	for k, c := range e.colors {
		s.Colors[k] = theme.Color{R: c.R, G: c.G, B: c.B, A: 255}.Hex()
	}
	return s
}

// resolved returns the theme as it stands, the same one until it
// changes.
func (e *themeEditor) resolved() *theme.Theme {
	if e.cached == nil {
		e.cached = theme.Resolve(e.spec())
	}
	return e.cached
}

func (e *themeEditor) changed() { e.cached = nil }

// themeEditorPage edits the colors of a theme.
func (a *App) themeEditorPage(c *ui.Context, id string) {
	p := a.pal
	if a.editing == nil || a.editing.id != id {
		from := a.themes.Get(id)
		if from == nil {
			from = a.theme()
		}
		a.editing = newThemeEditor(id, from)
		c.Invalidate() // in its colors from the next frame on
		return
	}
	e := a.editing
	done := func() {
		a.editing = nil
		if a.router.CanGoBack() {
			a.router.Back()
		} else {
			a.router.Replace("/settings")
		}
	}
	ui.Scroll(c.Key("theme-editor")).Grow(1).MinHeight(0).Padding(0, 0, 40).Children(func() {
		title := "New theme"
		if e.path != "" {
			title = "Edit theme"
		}
		a.pageTitle(c, title, "The window shows the theme as you change it.", func() {
			if a.pillButton(c, "", "Cancel", false).Clicked() {
				done()
			}
			if a.pillButton(c, "check", "Save", true).Clicked() {
				id, err := a.themes.Save(e.spec(), e.path)
				if err != nil {
					a.toastError("Could not save the theme", err)
					return
				}
				a.themeGen++
				a.settings.Theme = id
				a.saveSettings()
				a.toast("Theme saved")
				done()
			}
		})
		ui.Column(c).Padding(0, pagePad).Gap(18).MaxWidth(760).Children(func() {
			a.card(c, func() {
				a.setting(c, "Name", "", func() {
					if ui.TextInput(c.Key("name"), &e.name).Width(260).Label("Name").Changed() {
						e.changed()
					}
				})
				a.setting(c, "Appearance", "Whether the system's controls and scroll bars are for a dark or a light window.", func() {
					sel := 0
					if !e.dark {
						sel = 1
					}
					if ui.Segmented(c.Key("kind"), &sel, "Dark", "Light").Changed() {
						e.dark = sel == 0
						e.changed()
					}
				})
				a.setting(c, "Corners", "How round the corners of pictures, rows and fields are.", func() {
					if ui.Slider(c.Key("radius"), &e.radius, 0, 18).Width(180).Label("Corners").Changed() {
						e.changed()
					}
				})
				a.setting(c, "Font", "A font family installed on this computer; empty is the system's.", func() {
					if ui.TextInput(c.Key("font"), &e.font).Width(260).Placeholder("System").Label("Font").Changed() {
						e.changed()
					}
				})
			})
			a.card(c, func() {
				for _, k := range theme.Keys {
					a.setting(c, k.Label, k.Help, func() {
						col := e.colors[k.Name]
						hex := e.hex[k.Name]
						in := ui.TextInput(c.Key("hex-"+k.Name), &hex).Width(96).Font("monospace").Label(k.Label + " in hex")
						if in.Changed() {
							e.hex[k.Name] = hex
							if v, ok := theme.ParseColor(hex); ok {
								e.colors[k.Name] = ui.RGB(v.R, v.G, v.B)
								e.changed()
							}
						}
						if ui.ColorWell(c.Key("well-"+k.Name), &col).Size(44, 28).Label(k.Label).Changed() {
							e.colors[k.Name] = col
							e.hex[k.Name] = theme.Color{R: col.R, G: col.G, B: col.B, A: 255}.Hex()
							e.changed()
						}
					})
				}
			})
			ui.Text(c, "Themes are JSON files in the themes folder, which you can edit in any editor: Aurelia shows them again as they are saved.").
				TextColor(p.muted).FontSize(12)
		})
	})
}

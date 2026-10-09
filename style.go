package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/theme"
)

// palette is the theme as the view draws with it.
type palette struct {
	t *theme.Theme

	bg, sidebar, bar            ui.Color
	surface, hover, active      ui.Color
	border                      ui.Color
	text, muted, faint          ui.Color
	accent, accentHover, onAcc  ui.Color
	selection                   ui.Color
	danger, success, warning    ui.Color
	shadow, scrim, artwork, dim ui.Color
	radius                      float32

	// widgets is the theme of MyGo's own widgets: fields, menus, toasts.
	widgets *ui.Theme
}

func col(c theme.Color) ui.Color { return ui.RGBA(c.R, c.G, c.B, float32(c.A)/255) }

func newPalette(t *theme.Theme) *palette {
	p := &palette{
		t:  t,
		bg: col(t.Background), sidebar: col(t.Sidebar), bar: col(t.Bar),
		surface: col(t.Surface), hover: col(t.SurfaceHover), active: col(t.SurfaceActive),
		border: col(t.Border),
		text:   col(t.Text), muted: col(t.TextMuted), faint: col(t.TextFaint),
		accent: col(t.Accent), accentHover: col(t.AccentHover), onAcc: col(t.AccentText),
		selection: col(t.Selection),
		danger:    col(t.Danger), success: col(t.Success), warning: col(t.Warning),
		radius: float32(t.Radius),
	}
	// Where a picture will be, and what dims a picture under a button.
	p.artwork = col(theme.Mix(t.Surface, t.Text, 0.04))
	p.scrim = ui.RGBA(0, 0, 0, 0.45)
	p.dim = ui.RGBA(0, 0, 0, 0.32)
	p.shadow = ui.RGBA(0, 0, 0, 0.28)
	if !t.Dark {
		p.shadow = ui.RGBA(20, 20, 40, 0.14)
	}
	base := ui.LightTheme()
	if t.Dark {
		base = ui.DarkTheme()
	}
	w := *base
	w.Dark = t.Dark
	w.Background, w.Surface, w.SurfaceHover, w.SurfacePressed = p.bg, p.surface, p.hover, p.active
	w.Border, w.Text, w.TextMuted = p.border, p.text, p.muted
	w.Accent, w.AccentHover, w.AccentText = p.accent, p.accentHover, p.onAcc
	w.AccentPressed = col(theme.Mix(t.Accent, t.Background, 0.2))
	w.Danger, w.Warning, w.Success = p.danger, p.warning, p.success
	w.Selection = p.accent.Alpha(0.35)
	w.Focus = p.accent.Alpha(0.6)
	w.Scrollbar = p.text.Alpha(0.26)
	w.ScrollbarWidth = 7
	w.Radius = p.radius
	w.Font = t.Font
	// Toasts and tooltips stand out from the page without glaring.
	w.Inverse, w.InverseText = col(theme.Mix(t.Text, t.Background, 0.1)), p.bg
	p.widgets = &w
	return p
}

// primary is the modifier of shortcuts: Command on macOS, Control
// elsewhere.
var primary = func() ui.Modifiers {
	if runtime.GOOS == "darwin" {
		return ui.Super
	}
	return ui.Ctrl
}()

// fade is how hovers come and go.
var fade = ui.ElementTransition{Colors: true, Duration: 90 * time.Millisecond}

// iconButton is a button showing an icon alone, named for the tooltip and
// for assistive technology.
func (a *App) iconButton(c *ui.Context, name, label string, size, glyph float32) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c).Size(size, size).Radius(size / 2).Shrink(0).Label(label).Tooltip(label).Cursor(ui.CursorPointer)
	fg := p.muted
	switch {
	case b.IsDisabled():
		fg = p.faint
	case b.Pressed():
		b.Background(p.active)
		fg = p.text
	case b.Hovered():
		b.Background(p.hover)
		fg = p.text
	}
	b.Transition(fade)
	b.Children(func() {
		ui.Icon(c, icon(name)).Size(glyph, glyph).TextColor(fg)
	})
	return b
}

// toggleIcon is an icon button that shows a state: in the accent color
// while on.
func (a *App) toggleIcon(c *ui.Context, name, label string, on bool, size, glyph float32) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c).Size(size, size).Radius(size / 2).Shrink(0).Label(label).Tooltip(label).Cursor(ui.CursorPointer).Checked(on)
	fg := p.muted
	if on {
		fg = p.accent
	}
	switch {
	case b.Pressed():
		b.Background(p.active)
	case b.Hovered():
		b.Background(p.hover)
		if !on {
			fg = p.text
		}
	}
	b.Transition(fade)
	b.Children(func() {
		ui.Icon(c, icon(name)).Size(glyph, glyph).TextColor(fg)
	})
	return b
}

// pillButton is a button with an icon and a label: filled with the accent
// when primary, else with the surface.
func (a *App) pillButton(c *ui.Context, name, label string, primary bool) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c).Height(36).Padding(0, 18, 0, 14).Gap(8).Radius(18).Shrink(0).Cursor(ui.CursorPointer)
	fg := p.text
	if primary {
		fg = p.onAcc
		b.Background(p.accent)
		if b.Hovered() {
			b.Background(p.accentHover)
		}
	} else {
		b.Background(p.surface)
		if b.Hovered() {
			b.Background(p.hover)
		}
	}
	if name == "" {
		b.Padding(0, 18)
	}
	b.Transition(fade)
	b.Children(func() {
		if name != "" {
			ui.Icon(c, icon(name)).Size(16, 16).TextColor(fg)
		}
		ui.Text(c, label).FontWeight(600).TextColor(fg).SingleLine()
	})
	return b
}

// textButton is a quiet button: a label that lights up under the pointer.
func (a *App) textButton(c *ui.Context, label string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c).Height(28).Padding(0, 10).Radius(p.radius).Shrink(0).Cursor(ui.CursorPointer)
	fg := p.muted
	if b.Hovered() {
		b.Background(p.hover)
		fg = p.text
	}
	b.Transition(fade)
	b.Children(func() { ui.Text(c, label).TextColor(fg).FontWeight(500).SingleLine() })
	return b
}

// sectionTitle heads a part of a page.
func (a *App) sectionTitle(c *ui.Context, title string) ui.Element {
	return ui.Text(c, title).FontSize(19).FontWeight(700).TextColor(a.pal.text).SingleLine()
}

// clock writes a duration as 3:07 or 1:02:45.
func clock(d time.Duration) string {
	s := int(d.Seconds() + 0.5)
	if s < 0 {
		s = 0
	}
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// long writes a duration as "1 hr 12 min" or "47 min".
func long(seconds float64) string {
	m := int(seconds+30) / 60
	switch {
	case m >= 60 && m%60 == 0:
		return fmt.Sprintf("%d hr", m/60)
	case m >= 60:
		return fmt.Sprintf("%d hr %d min", m/60, m%60)
	case m < 1:
		return fmt.Sprintf("%d sec", int(seconds))
	}
	return fmt.Sprintf("%d min", m)
}

// count writes "1 song" or "12 songs".
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%s %s", grouped(n), many)
}

// grouped writes a number with thin spaces between thousands: 9,937.
func grouped(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// bytesText writes a size as "1.4 GB" or "312 MB".
func bytesText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

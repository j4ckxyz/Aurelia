package main

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/audio"
)

// EQSettings is the equalizer as it is kept: on or off, the preamp, and
// what each band boosts or cuts in dB.
type EQSettings struct {
	On     bool        `json:"on,omitempty"`
	Preamp float64     `json:"preamp,omitempty"`
	Bands  [10]float64 `json:"bands"`
}

// EQPreset is a curve with a name.
type EQPreset struct {
	Name   string      `json:"name"`
	Preamp float64     `json:"preamp,omitempty"`
	Bands  [10]float64 `json:"bands"`
}

// eqPresets are the curves that come with the app, in dB for the bands
// from 31 Hz to 16 kHz.
var eqPresets = []EQPreset{
	{Name: "Flat"},
	{Name: "Bass Boost", Bands: [10]float64{6, 5, 4, 2, 1, 0, 0, 0, 0, 0}},
	{Name: "Bass Reducer", Bands: [10]float64{-6, -5, -4, -2, -1, 0, 0, 0, 0, 0}},
	{Name: "Treble Boost", Bands: [10]float64{0, 0, 0, 0, 0, 1, 2, 4, 5, 6}},
	{Name: "Treble Reducer", Bands: [10]float64{0, 0, 0, 0, 0, -1, -2, -4, -5, -6}},
	{Name: "Vocal", Bands: [10]float64{-2, -3, -3, 1, 3, 3, 3, 2, 0, -1}},
	{Name: "Loudness", Bands: [10]float64{5, 3, 0, -1, -2, -2, 0, 2, 4, 5}},
	{Name: "Acoustic", Bands: [10]float64{4, 4, 3, 1, 1, 1, 3, 3, 3, 2}},
	{Name: "Classical", Bands: [10]float64{0, 0, 0, 0, 0, 0, -3, -3, -3, -5}},
	{Name: "Electronic", Bands: [10]float64{5, 4, 1, 0, -2, 2, 1, 1, 4, 5}},
	{Name: "Hip-Hop", Bands: [10]float64{5, 4, 2, 3, -1, -1, 2, 0, 2, 3}},
	{Name: "Jazz", Bands: [10]float64{3, 2, 1, 2, -2, -2, 0, 1, 2, 3}},
	{Name: "Pop", Bands: [10]float64{-1, 1, 3, 4, 3, 0, -1, -1, -1, -1}},
	{Name: "Rock", Bands: [10]float64{5, 4, 3, 1, -1, -1, 1, 3, 4, 5}},
}

// eqCustom is the name of the curve that is none of the presets.
const eqCustom = "Custom"

// eqState is what the equalizer's page holds besides the settings.
type eqState struct {
	// dirty is a change not saved yet: it is saved when the slider is let
	// go, not for every pixel on the way.
	dirty bool
	name  string // the name typed for a preset to save
}

// effects is what the engine is told: the settings as filters.
func (s *Settings) effects() audio.Effects {
	return audio.Effects{
		EQ: s.EQ.On, Gains: s.EQ.Bands, Preamp: s.EQ.Preamp,
		Mono: s.Mono, Balance: s.Balance,
	}
}

// applyEffects tells the engine what the settings ask of the sound.
func (a *App) applyEffects() {
	if a.player.engine != nil {
		a.player.engine.SetEffects(a.settings.effects())
	}
}

// presetNamed finds a preset by name, the user's own after those that come
// with the app.
func (s *Settings) presetNamed(name string) (EQPreset, bool) {
	for _, list := range [][]EQPreset{eqPresets, s.EQPresets} {
		for _, p := range list {
			if p.Name == name {
				return p, true
			}
		}
	}
	return EQPreset{}, false
}

// presetNow names the preset that the equalizer's curve is, eqCustom for
// none of them.
func (s *Settings) presetNow() string {
	for _, list := range [][]EQPreset{eqPresets, s.EQPresets} {
		for _, p := range list {
			if p.Bands == s.EQ.Bands && p.Preamp == s.EQ.Preamp {
				return p.Name
			}
		}
	}
	return eqCustom
}

// userPreset reports that name is a preset of the user's.
func (s *Settings) userPreset(name string) bool {
	return slices.ContainsFunc(s.EQPresets, func(p EQPreset) bool { return p.Name == name })
}

// bandNames are the bands as the sliders name them.
var bandNames = [10]string{"31", "62", "125", "250", "500", "1k", "2k", "4k", "8k", "16k"}

// dbText writes a gain: +3.5, -6, 0.
func dbText(v float64) string {
	switch r := math.Round(v*10) / 10; {
	case r == 0:
		return "0"
	case r == math.Trunc(r):
		return fmt.Sprintf("%+.0f", r)
	default:
		return fmt.Sprintf("%+.1f", r)
	}
}

// eqChanged tells the engine and keeps the change for saving.
func (a *App) eqChanged() {
	a.applyEffects()
	a.eq.dirty = true
}

// equalizerPage is the equalizer: a curve over ten sliders and a preamp,
// presets, and a way to keep one's own.
func (a *App) equalizerPage(c *ui.Context) {
	p := a.pal
	eq := &a.settings.EQ
	pressed := false
	ui.Scroll(c.Key("equalizer")).Grow(1).MinHeight(0).Padding(0, 0, 40).Children(func() {
		a.pageTitle(c, "Equalizer", "Shape the sound; it changes as you move a slider.", nil)
		ui.Column(c).Padding(0, pagePad).Gap(14).MaxWidth(860).Children(func() {
			// The presets.
			now := a.settings.presetNow()
			ui.Row(c).Gap(10).Wrap().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Preset").TextColor(p.muted)
				names := make([]string, 0, len(eqPresets)+len(a.settings.EQPresets)+1)
				for _, pr := range eqPresets {
					names = append(names, pr.Name)
				}
				for _, pr := range a.settings.EQPresets {
					names = append(names, pr.Name)
				}
				if now == eqCustom {
					names = append(names, eqCustom)
				}
				pick := now
				if ui.Select(c.Key("eq-preset"), &pick, names).Width(190).Label("Preset").Changed() && pick != eqCustom {
					if pr, ok := a.settings.presetNamed(pick); ok {
						eq.Bands, eq.Preamp = pr.Bands, pr.Preamp
						a.eqChanged()
					}
				}
				if a.settings.userPreset(now) && a.textButton(c, "Delete preset").Clicked() {
					a.settings.EQPresets = slices.DeleteFunc(a.settings.EQPresets, func(pr EQPreset) bool { return pr.Name == now })
					a.eq.dirty = true
				}
				if a.textButton(c, "Reset").Clicked() {
					eq.Bands, eq.Preamp = [10]float64{}, 0
					a.eqChanged()
				}
				ui.Spacer(c)
				ui.Text(c, "On").TextColor(p.muted)
				if ui.Switch(c.Key("eq-on"), &eq.On).Label("Equalizer").Changed() {
					a.eqChanged()
				}
			})

			// The curve and the sliders under it.
			fx := a.settings.effects()
			fx.EQ = true // the curve shows even while the equalizer is off
			ui.Column(c).Radius(p.radius+2).Background(p.surface.Alpha(0.5)).Border(1, p.border).Padding(14, 10, 10).Gap(6).Children(func() {
				curve := ui.Box(c).Height(86).Margin(0, 38, 0, 38)
				curve.Draw(func(g *ui.Painter, r ui.Rect) { a.drawCurve(g, r, fx) })
				ui.Row(c).Gap(0).AlignItems(ui.Stretch).Children(func() {
					a.eqSlider(c, "Preamp", "eq-preamp", &eq.Preamp, &pressed)
					ui.Box(c).Width(1).Margin(30, 6, 30).Background(p.border).Shrink(0)
					for i := range eq.Bands {
						a.eqSlider(c, bandNames[i], bandNames[i], &eq.Bands[i], &pressed)
					}
				})
			})
			if !eq.On {
				ui.Text(c, "The equalizer is off: the sound is played as it is.").FontSize(12).TextColor(p.muted)
			}

			// Keeping a curve.
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c.Key("eq-name"), &a.eq.name).Width(220).Placeholder("Name this curve").Label("Name of the preset")
				name := strings.TrimSpace(a.eq.name)
				_, builtin := presetIn(eqPresets, name)
				save := a.pillButton(c, "plus", "Save preset", false).Disabled(name == "" || builtin)
				if save.Clicked() {
					a.savePreset(name)
				}
			})
		})
	})
	if a.eq.dirty && !pressed {
		a.eq.dirty = false
		a.saveSettings()
	}
}

func presetIn(list []EQPreset, name string) (EQPreset, bool) {
	for _, p := range list {
		if p.Name == name {
			return p, true
		}
	}
	return EQPreset{}, false
}

// savePreset keeps the curve as a preset of the user's, over the one of
// that name.
func (a *App) savePreset(name string) {
	pr := EQPreset{Name: name, Preamp: a.settings.EQ.Preamp, Bands: a.settings.EQ.Bands}
	if _, ok := presetIn(a.settings.EQPresets, name); ok {
		for i := range a.settings.EQPresets {
			if a.settings.EQPresets[i].Name == name {
				a.settings.EQPresets[i] = pr
			}
		}
	} else {
		a.settings.EQPresets = append(a.settings.EQPresets, pr)
	}
	a.eq.name = ""
	a.eq.dirty = true
	a.toast("Saved the preset " + name)
}

// eqSlider is one vertical slider of the equalizer, with its gain over it
// and its name under it.
func (a *App) eqSlider(c *ui.Context, name, key string, v *float64, pressed *bool) {
	p := a.pal
	on := a.settings.EQ.On
	ui.Column(c.Key("col-" + key)).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(4).Children(func() {
		fg := p.muted
		if !on {
			fg = p.faint
		}
		ui.Text(c, dbText(*v)).FontSize(11).FontWeight(600).TextColor(fg).SingleLine()
		const pad = 9
		s := ui.SliderBase(c.Key(key), v, -audio.EQRange, audio.EQRange).Vertical().Step(0.5).Width(34).Height(190).Padding(pad, 0).Label(name + " " + dbText(*v) + " dB")
		if s.Changed() {
			*v = math.Round(*v*2) / 2 // half a dB at a time
			a.eqChanged()
		}
		if s.Pressed() {
			*pressed = true
		}
		active := s.Hovered() || s.Pressed()
		val := *v
		s.Draw(func(g *ui.Painter, r ui.Rect) {
			x := r.X + r.W/2
			top, h := r.Y+pad, r.H-2*pad
			zero := top + h/2
			y := top + h*float32(1-(val+audio.EQRange)/(2*audio.EQRange))
			g.Fill(ui.Rect{X: x - 2, Y: top, W: 4, H: h}, p.text.Alpha(0.12), 2)
			g.Fill(ui.Rect{X: x - 7, Y: zero - 0.5, W: 14, H: 1}, p.text.Alpha(0.3), 0)
			col := p.muted
			if on {
				col = p.accent
			}
			if active {
				col = p.accentHover
			}
			lo, hi := min(y, zero), max(y, zero)
			g.Fill(ui.Rect{X: x - 2, Y: lo, W: 4, H: max(hi-lo, 0)}, col, 2)
			knob := float32(13)
			if active {
				knob = 15
			}
			g.Fill(ui.Rect{X: x - knob/2, Y: y - knob/2, W: knob, H: knob}, p.text, knob/2)
		})
		ui.Text(c, name).FontSize(11).TextColor(p.muted).SingleLine()
	})
}

// drawCurve draws the equalizer's response from 20 Hz to 20 kHz, with
// zero in the middle and a line for each octave of the bands.
func (a *App) drawCurve(g *ui.Painter, r ui.Rect, fx audio.Effects) {
	p := a.pal
	on := a.settings.EQ.On
	const lo, hi = 20.0, 20000.0
	at := func(f float64) float32 { return r.X + r.W*float32(math.Log(f/lo)/math.Log(hi/lo)) }
	mid := r.Y + r.H/2
	g.Fill(ui.Rect{X: r.X, Y: mid - 0.5, W: r.W, H: 1}, p.text.Alpha(0.18), 0)
	for _, f := range audio.EQBands {
		g.Fill(ui.Rect{X: at(f) - 0.5, Y: r.Y, W: 1, H: r.H}, p.text.Alpha(0.06), 0)
	}
	scale := r.H / 2 / (audio.EQRange * 0.8)
	var line, fill ui.Path
	const steps = 96
	for i := 0; i <= steps; i++ {
		f := lo * math.Pow(hi/lo, float64(i)/steps)
		x := at(f)
		y := mid - float32(fx.Response(f, 44100))*scale
		y = max(r.Y, min(r.Y+r.H, y))
		if i == 0 {
			line.MoveTo(x, y)
			fill.MoveTo(x, mid)
			fill.LineTo(x, y)
		} else {
			line.LineTo(x, y)
			fill.LineTo(x, y)
		}
	}
	fill.LineTo(r.X+r.W, mid)
	fill.Close()
	col := p.muted
	if on {
		col = p.accent
	}
	g.FillPath(&fill, col.Alpha(0.14))
	g.StrokePath(&line, 2, col)
}

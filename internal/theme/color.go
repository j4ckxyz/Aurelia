// Package theme holds Aurelia's themes: their JSON format, the themes
// built in, and the reading of themes made for Visual Studio Code and for
// browsers.
package theme

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Color is an sRGB color with an alpha.
type Color struct{ R, G, B, A uint8 }

// Hex returns the color as #rrggbb, or #rrggbbaa when it is translucent.
func (c Color) Hex() string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

var named = map[string]Color{
	"white": {255, 255, 255, 255}, "black": {0, 0, 0, 255}, "transparent": {},
	"red": {255, 0, 0, 255}, "green": {0, 128, 0, 255}, "blue": {0, 0, 255, 255},
	"gray": {128, 128, 128, 255}, "grey": {128, 128, 128, 255},
}

// ParseColor reads a color as themes write them: #rgb, #rgba, #rrggbb,
// #rrggbbaa, rgb(), rgba(), hsl(), hsla() or a few names.
func ParseColor(s string) (Color, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if c, ok := named[s]; ok {
		return c, true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 || len(h) == 4 {
			var b strings.Builder
			for _, r := range h {
				b.WriteRune(r)
				b.WriteRune(r)
			}
			h = b.String()
		}
		if len(h) != 6 && len(h) != 8 {
			return Color{}, false
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return Color{}, false
		}
		if len(h) == 6 {
			return Color{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
		}
		return Color{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
	}
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return Color{}, false
	}
	fn := strings.TrimSpace(s[:open])
	parts := strings.FieldsFunc(s[open+1:end], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	if len(parts) < 3 {
		return Color{}, false
	}
	num := func(p string, scale float64) (float64, bool) {
		pct := strings.HasSuffix(p, "%")
		p = strings.TrimSuffix(strings.TrimSuffix(p, "%"), "deg")
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0, false
		}
		if pct {
			v = v / 100 * scale
		}
		return v, true
	}
	alpha := 1.0
	if len(parts) >= 4 {
		a, ok := num(parts[3], 1)
		if !ok {
			return Color{}, false
		}
		alpha = a
	}
	switch fn {
	case "rgb", "rgba":
		var v [3]float64
		for i := range v {
			x, ok := num(parts[i], 255)
			if !ok {
				return Color{}, false
			}
			v[i] = x
		}
		return Color{clamp8(v[0]), clamp8(v[1]), clamp8(v[2]), clamp8(alpha * 255)}, true
	case "hsl", "hsla":
		h, ok1 := num(parts[0], 360)
		sat, ok2 := num(parts[1], 1)
		l, ok3 := num(parts[2], 1)
		if !ok1 || !ok2 || !ok3 {
			return Color{}, false
		}
		if !strings.HasSuffix(parts[1], "%") {
			sat /= 100
		}
		if !strings.HasSuffix(parts[2], "%") {
			l /= 100
		}
		r, g, b := hslToRGB(h, sat, l)
		return Color{clamp8(r * 255), clamp8(g * 255), clamp8(b * 255), clamp8(alpha * 255)}, true
	}
	return Color{}, false
}

func hslToRGB(h, s, l float64) (r, g, b float64) {
	h = math.Mod(math.Mod(h, 360)+360, 360) / 360
	if s == 0 {
		return l, l, l
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	f := func(t float64) float64 {
		t = math.Mod(t+1, 1)
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return f(h + 1.0/3), f(h), f(h - 1.0/3)
}

func clamp8(v float64) uint8 {
	return uint8(math.Max(0, math.Min(255, math.Round(v))))
}

// parseAny reads a color of a theme's JSON: a string, or the [r, g, b]
// and [r, g, b, a] arrays of browser themes.
func parseAny(raw json.RawMessage) (Color, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return ParseColor(s)
	}
	var arr []float64
	if json.Unmarshal(raw, &arr) == nil && len(arr) >= 3 {
		c := Color{clamp8(arr[0]), clamp8(arr[1]), clamp8(arr[2]), 255}
		if len(arr) >= 4 {
			c.A = clamp8(arr[3] * 255)
		}
		return c, true
	}
	return Color{}, false
}

// Mix returns a with t of b mixed in, from 0 to 1.
func Mix(a, b Color, t float64) Color {
	m := func(x, y uint8) uint8 { return clamp8(float64(x) + (float64(y)-float64(x))*t) }
	return Color{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), m(a.A, b.A)}
}

// Over returns c drawn over an opaque background.
func Over(c, bg Color) Color {
	if c.A == 255 {
		return c
	}
	out := Mix(bg, Color{c.R, c.G, c.B, 255}, float64(c.A)/255)
	out.A = 255
	return out
}

// Luminance returns the relative luminance of a color, from 0 to 1, as
// WCAG defines it.
func Luminance(c Color) float64 {
	f := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.R) + 0.7152*f(c.G) + 0.0722*f(c.B)
}

// Contrast returns the contrast ratio of two colors, from 1 to 21.
func Contrast(a, b Color) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// On returns black or white, whichever reads better on c.
func On(c Color) Color {
	white, black := Color{255, 255, 255, 255}, Color{16, 16, 20, 255}
	if Contrast(c, white) >= Contrast(c, black) {
		return white
	}
	return black
}

package theme

import (
	"encoding/json"
	"strings"
)

// Spec is a theme as its JSON file writes it:
//
//	{
//	  "name": "Tokyo Night",
//	  "type": "dark",
//	  "colors": {
//	    "background": "#1a1b26",
//	    "text": "#c0caf5",
//	    "accent": "#7aa2f7"
//	  }
//	}
//
// Every color is optional: those left out are made from the others.
type Spec struct {
	Name string `json:"name"`
	// Type is "dark" or "light"; left out, the background tells.
	Type   string            `json:"type,omitempty"`
	Colors map[string]string `json:"colors"`
	// Radius rounds corners, in points; 0 is the default.
	Radius float64 `json:"radius,omitempty"`
	// Font is a font family, or several separated by commas; "" is the
	// system's.
	Font string `json:"font,omitempty"`
}

// Key is a color of a theme.
type Key struct {
	Name  string // in the JSON
	Label string
	Help  string
}

// Keys are the colors of a theme, in the order an editor shows them.
var Keys = []Key{
	{"background", "Background", "Behind the pages"},
	{"sidebar", "Sidebar", "Behind the sidebar"},
	{"bar", "Bars", "Behind the title bar and the player"},
	{"surface", "Surface", "Fields, buttons and cards"},
	{"surfaceHover", "Surface, hovered", "Rows and buttons under the pointer"},
	{"border", "Border", "Lines between and around things"},
	{"text", "Text", "Titles and names"},
	{"textMuted", "Secondary text", "Artists, durations and labels"},
	{"accent", "Accent", "The play button, what plays, sliders and links"},
	{"accentText", "Text on accent", "What is drawn on the accent color"},
	{"selection", "Selection", "The selected row and selected text"},
	{"danger", "Danger", "Errors"},
	{"success", "Success", "What went well"},
	{"warning", "Warning", "What calls for attention"},
}

// Theme is a theme with every color settled.
type Theme struct {
	ID   string
	Name string
	Dark bool
	// BuiltIn themes come with Aurelia; the others are files, at Path.
	BuiltIn bool
	Path    string
	Spec    Spec

	Background, Sidebar, Bar             Color
	Surface, SurfaceHover, SurfaceActive Color
	Border                               Color
	Text, TextMuted, TextFaint           Color
	Accent, AccentHover, AccentText      Color
	Selection                            Color
	Danger, Success, Warning             Color
	Radius                               float64
	Font                                 string
}

// Resolve settles every color of a spec: those it names, and the others
// from them, readable whatever the spec says.
func Resolve(s Spec) *Theme {
	t := &Theme{Name: s.Name, Spec: s, Radius: s.Radius, Font: s.Font}
	get := func(key string) (Color, bool) {
		v, ok := s.Colors[key]
		if !ok {
			return Color{}, false
		}
		return ParseColor(v)
	}
	bg, hasBg := get("background")
	text, hasText := get("text")
	switch strings.ToLower(s.Type) {
	case "dark", "hc", "hc-black", "vs-dark":
		t.Dark = true
	case "light", "hc-light", "vs":
		t.Dark = false
	default:
		switch {
		case hasBg:
			t.Dark = Luminance(bg) < 0.4
		case hasText:
			t.Dark = Luminance(text) > 0.5
		default:
			t.Dark = true
		}
	}
	if !hasBg {
		bg = Color{15, 15, 18, 255}
		if !t.Dark {
			bg = Color{255, 255, 255, 255}
		}
	}
	bg = Over(bg, On(bg))
	bg.A = 255
	if !hasText || Contrast(Over(text, bg), bg) < 3 {
		text = Color{236, 236, 241, 255}
		if Luminance(bg) > 0.4 {
			text = Color{23, 23, 28, 255}
		}
	}
	text = Over(text, bg)
	t.Background, t.Text = bg, text

	or := func(key string, fallback Color) Color {
		if c, ok := get(key); ok {
			return Over(c, bg)
		}
		return fallback
	}
	t.Sidebar = or("sidebar", Mix(bg, text, 0.035))
	t.Bar = or("bar", t.Sidebar)
	t.Surface = or("surface", Mix(bg, text, 0.07))
	t.SurfaceHover = or("surfaceHover", Mix(t.Surface, text, 0.07))
	t.SurfaceActive = Mix(t.SurfaceHover, text, 0.07)
	t.Border = or("border", Mix(bg, text, 0.12))
	t.TextMuted = or("textMuted", Mix(text, bg, 0.38))
	// Secondary text reads, and reads as secondary.
	if Contrast(t.TextMuted, bg) < 2.6 || Contrast(t.TextMuted, text) < 1.25 {
		t.TextMuted = Mix(text, bg, 0.38)
	}
	t.TextFaint = Mix(t.TextMuted, bg, 0.4)

	accent := Color{164, 140, 255, 255}
	if !t.Dark {
		accent = Color{109, 79, 224, 255}
	}
	if c, ok := get("accent"); ok {
		// An accent that does not show on the background is none.
		if c = Over(c, bg); Contrast(c, bg) >= 1.6 {
			accent = c
		}
	}
	t.Accent = accent
	t.AccentText = On(accent)
	if c, ok := get("accentText"); ok {
		if c = Over(c, accent); Contrast(c, accent) >= 3 {
			t.AccentText = c
		}
	}
	if t.Dark {
		t.AccentHover = Mix(accent, Color{255, 255, 255, 255}, 0.14)
	} else {
		t.AccentHover = Mix(accent, Color{0, 0, 0, 255}, 0.12)
	}
	t.Selection = or("selection", Mix(bg, accent, 0.22))
	if Contrast(text, t.Selection) < 3 {
		t.Selection = Mix(bg, accent, 0.22)
	}
	if t.Dark {
		t.Danger, t.Success, t.Warning = Color{248, 113, 113, 255}, Color{74, 222, 128, 255}, Color{251, 191, 36, 255}
	} else {
		t.Danger, t.Success, t.Warning = Color{220, 38, 38, 255}, Color{22, 163, 74, 255}, Color{217, 119, 6, 255}
	}
	t.Danger, t.Success, t.Warning = or("danger", t.Danger), or("success", t.Success), or("warning", t.Warning)
	if t.Radius <= 0 {
		t.Radius = 8
	}
	return t
}

// Color returns the settled color of a key.
func (t *Theme) Color(key string) Color {
	switch key {
	case "background":
		return t.Background
	case "sidebar":
		return t.Sidebar
	case "bar":
		return t.Bar
	case "surface":
		return t.Surface
	case "surfaceHover":
		return t.SurfaceHover
	case "border":
		return t.Border
	case "text":
		return t.Text
	case "textMuted":
		return t.TextMuted
	case "accent":
		return t.Accent
	case "accentText":
		return t.AccentText
	case "selection":
		return t.Selection
	case "danger":
		return t.Danger
	case "success":
		return t.Success
	case "warning":
		return t.Warning
	}
	return Color{}
}

// Full returns the spec with every color written out, as an editor
// starts from and as an export writes.
func (t *Theme) Full() Spec {
	s := Spec{Name: t.Name, Type: "light", Colors: map[string]string{}, Font: t.Font}
	if t.Dark {
		s.Type = "dark"
	}
	if t.Spec.Radius > 0 {
		s.Radius = t.Spec.Radius
	}
	for _, k := range Keys {
		s.Colors[k.Name] = t.Color(k.Name).Hex()
	}
	return s
}

// Marshal writes a spec as the JSON of a theme file, its colors in the
// order of Keys.
func Marshal(s Spec) []byte {
	var b strings.Builder
	str := func(v string) string { j, _ := json.Marshal(v); return string(j) }
	b.WriteString("{\n  \"name\": " + str(s.Name) + ",\n")
	if s.Type != "" {
		b.WriteString("  \"type\": " + str(s.Type) + ",\n")
	}
	if s.Radius > 0 {
		j, _ := json.Marshal(s.Radius)
		b.WriteString("  \"radius\": " + string(j) + ",\n")
	}
	if s.Font != "" {
		b.WriteString("  \"font\": " + str(s.Font) + ",\n")
	}
	b.WriteString("  \"colors\": {")
	first := true
	write := func(k, v string) {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString("\n    " + str(k) + ": " + str(v))
	}
	seen := map[string]bool{}
	for _, k := range Keys {
		if v, ok := s.Colors[k.Name]; ok {
			write(k.Name, v)
			seen[k.Name] = true
		}
	}
	for k, v := range s.Colors {
		if !seen[k] {
			write(k, v)
		}
	}
	b.WriteString("\n  }\n}\n")
	return []byte(b.String())
}

// Slug makes a file name of a theme's name.
func Slug(name string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if s == "" {
		s = "theme"
	}
	return s
}

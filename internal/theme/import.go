package theme

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// ErrNotATheme is returned for files that hold no theme Aurelia reads.
var ErrNotATheme = errors.New("theme: not a theme Aurelia can read")

// Parse reads the themes of a file, whatever made it: a theme of
// Aurelia's, a color theme of Visual Studio Code (.json, with comments or
// not), the manifest.json of a Firefox or Chrome theme, or an extension
// holding those: a .vsix, .xpi, .crx or .zip. name is the file's, which
// names a theme that does not name itself.
func Parse(data []byte, name string) ([]Spec, error) {
	if i := bytes.Index(data, []byte("PK\x03\x04")); i >= 0 && i < 4096 && !looksLikeJSON(data) {
		return parseArchive(data[i:], name)
	}
	spec, err := parseJSON(data, fallbackName(name))
	if err != nil {
		return nil, err
	}
	return []Spec{spec}, nil
}

func looksLikeJSON(data []byte) bool {
	t := bytes.TrimLeft(data, " \t\r\n\xef\xbb\xbf")
	return len(t) > 0 && (t[0] == '{' || t[0] == '/')
}

func fallbackName(file string) string {
	base := filepath.Base(file)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.TrimSuffix(base, "-color-theme")
	if base == "" || base == "." || base == "manifest" {
		return "Imported theme"
	}
	return base
}

func parseJSON(data []byte, name string) (Spec, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(stripJSONC(data), &doc); err != nil {
		return Spec{}, fmt.Errorf("theme: %s: %w", name, err)
	}
	// A browser theme: a manifest with a "theme" of colors.
	if raw, ok := doc["theme"]; ok {
		var th struct {
			Colors map[string]json.RawMessage `json:"colors"`
		}
		if json.Unmarshal(raw, &th) == nil && len(th.Colors) > 0 {
			var n string
			json.Unmarshal(doc["name"], &n)
			if n == "" || strings.HasPrefix(n, "__MSG_") {
				n = name
			}
			return fromBrowser(n, th.Colors), nil
		}
	}
	var colors map[string]json.RawMessage
	if raw, ok := doc["colors"]; ok {
		json.Unmarshal(raw, &colors)
	}
	if len(colors) == 0 {
		return Spec{}, ErrNotATheme
	}
	var n, kind string
	json.Unmarshal(doc["name"], &n)
	json.Unmarshal(doc["type"], &kind)
	if n == "" {
		n = name
	}
	for k := range colors {
		if strings.Contains(k, ".") {
			return fromVSCode(n, kind, colors), nil
		}
	}
	// One of Aurelia's own.
	spec := Spec{Name: n, Type: kind, Colors: map[string]string{}}
	for k, raw := range colors {
		if c, ok := parseAny(raw); ok {
			spec.Colors[k] = c.Hex()
		}
	}
	json.Unmarshal(doc["radius"], &spec.Radius)
	json.Unmarshal(doc["font"], &spec.Font)
	if len(spec.Colors) == 0 {
		return Spec{}, ErrNotATheme
	}
	return spec, nil
}

// picker reads the colors of a foreign theme, the first of several keys
// that it sets.
type picker struct {
	colors map[string]json.RawMessage
	out    map[string]string
}

func (p *picker) get(keys ...string) (Color, bool) {
	for _, k := range keys {
		if raw, ok := p.colors[k]; ok {
			if c, ok := parseAny(raw); ok && c.A > 0 {
				return c, true
			}
		}
	}
	return Color{}, false
}

// set gives a color of Aurelia's the first of keys the theme sets, drawn
// over bg when it is translucent, as themes write hovers and selections.
func (p *picker) set(name string, bg Color, keys ...string) (Color, bool) {
	c, ok := p.get(keys...)
	if !ok {
		return Color{}, false
	}
	c = Over(c, bg)
	p.out[name] = c.Hex()
	return c, true
}

// accent gives the accent the first of keys whose color stands out on bg,
// as what plays and links must: themes dim many of their buttons. With
// none that does, it is the one standing out most. It returns the key
// taken, "" for none.
func (p *picker) accent(bg Color, keys ...string) (Color, string) {
	var best Color
	bestKey, bestContrast := "", 0.0
	for _, k := range keys {
		c, ok := p.get(k)
		if !ok {
			continue
		}
		c = Over(c, bg)
		contrast := Contrast(c, bg)
		if contrast >= 3 {
			best, bestKey = c, k
			break
		}
		if contrast > bestContrast {
			best, bestKey, bestContrast = c, k, contrast
		}
	}
	if bestKey != "" {
		p.out["accent"] = best.Hex()
	}
	return best, bestKey
}

// fromVSCode maps the workbench colors of a Visual Studio Code theme.
func fromVSCode(name, kind string, colors map[string]json.RawMessage) Spec {
	p := &picker{colors: colors, out: map[string]string{}}
	spec := Spec{Name: name, Colors: p.out}
	switch strings.ToLower(kind) {
	case "dark", "hc", "hcdark", "hc-black", "vs-dark":
		spec.Type = "dark"
	case "light", "hclight", "hc-light", "vs":
		spec.Type = "light"
	}
	base := Color{30, 30, 30, 255}
	if spec.Type == "light" {
		base = Color{255, 255, 255, 255}
	}
	bg, ok := p.set("background", base, "editor.background", "panel.background", "sideBar.background")
	if !ok {
		bg = base
	}
	if spec.Type == "" {
		spec.Type = "light"
		if Luminance(bg) < 0.4 {
			spec.Type = "dark"
		}
	}
	p.set("sidebar", bg, "sideBar.background", "activityBar.background", "editorGroupHeader.tabsBackground")
	p.set("bar", bg, "titleBar.activeBackground", "statusBar.background", "activityBar.background", "sideBar.background")
	p.set("surface", bg, "input.background", "dropdown.background", "editorWidget.background", "tab.inactiveBackground")
	p.set("surfaceHover", bg, "list.hoverBackground", "toolbar.hoverBackground", "list.inactiveSelectionBackground")
	p.set("border", bg, "panel.border", "sideBar.border", "editorGroup.border", "input.border", "contrastBorder", "widget.border")
	p.set("text", bg, "editor.foreground", "foreground", "sideBar.foreground")
	p.set("textMuted", bg, "descriptionForeground", "sideBarTitle.foreground", "tab.inactiveForeground", "editorLineNumber.activeForeground", "disabledForeground")
	switch accent, key := p.accent(bg, "button.background", "activityBarBadge.background", "textLink.foreground", "progressBar.background", "focusBorder", "textLink.activeForeground", "terminal.ansiBlue"); key {
	case "button.background":
		if fg, ok := p.get("button.foreground"); ok {
			p.out["accentText"] = Over(fg, accent).Hex()
		}
	case "activityBarBadge.background":
		if fg, ok := p.get("activityBarBadge.foreground"); ok {
			p.out["accentText"] = Over(fg, accent).Hex()
		}
	}
	p.set("selection", bg, "list.activeSelectionBackground", "list.focusBackground", "editor.selectionBackground")
	p.set("danger", bg, "errorForeground", "editorError.foreground", "terminal.ansiRed")
	p.set("warning", bg, "editorWarning.foreground", "terminal.ansiYellow")
	p.set("success", bg, "gitDecoration.addedResourceForeground", "terminal.ansiGreen")
	return spec
}

// fromBrowser maps the colors of a Firefox or Chrome theme, which name
// the parts of a browser's window: Chrome's keys, and those Firefox added.
func fromBrowser(name string, colors map[string]json.RawMessage) Spec {
	p := &picker{colors: colors, out: map[string]string{}}
	spec := Spec{Name: name, Colors: p.out}
	white := Color{255, 255, 255, 255}
	frame, hasFrame := p.get("frame", "accentcolor")
	if !hasFrame {
		frame = white
	}
	frame = Over(frame, white)
	// The page behind everything: the new tab page's, else the toolbar's,
	// each with the text written on it.
	var bg Color
	switch {
	case p.has("ntp_background"):
		bg, _ = p.set("background", frame, "ntp_background")
		p.set("text", bg, "ntp_text", "toolbar_text", "bookmark_text")
	case p.has("toolbar"):
		bg, _ = p.set("background", frame, "toolbar")
		p.set("text", bg, "toolbar_text", "bookmark_text", "tab_text", "textcolor")
	default:
		bg = frame
		p.out["background"] = frame.Hex()
		p.set("text", bg, "tab_background_text", "textcolor", "tab_text")
	}
	if hasFrame {
		p.out["bar"] = frame.Hex()
	}
	if _, ok := p.set("sidebar", bg, "sidebar"); !ok && hasFrame {
		p.out["sidebar"] = frame.Hex()
	}
	p.set("surface", bg, "toolbar_field", "omnibox_background", "popup", "button_background")
	p.set("surfaceHover", bg, "button_background_hover", "toolbar_field_focus")
	p.set("border", bg, "toolbar_field_border", "sidebar_border", "popup_border", "toolbar_bottom_separator", "toolbar_top_separator")
	if accent, key := p.accent(bg, "tab_line", "icons_attention", "toolbar_field_border_focus", "ntp_link", "popup_highlight", "button_background_active", "toolbar_button_icon", "icons"); key == "popup_highlight" {
		if fg, ok := p.get("popup_highlight_text"); ok {
			p.out["accentText"] = Over(fg, accent).Hex()
		}
	}
	p.set("selection", bg, "sidebar_highlight", "toolbar_field_highlight")
	if Luminance(bg) < 0.4 {
		spec.Type = "dark"
	} else {
		spec.Type = "light"
	}
	return spec
}

func (p *picker) has(key string) bool {
	_, ok := p.get(key)
	return ok
}

// parseArchive reads the themes of an extension: those a Visual Studio
// Code extension contributes, or the theme of a browser's.
func parseArchive(data []byte, name string) ([]Spec, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("theme: %s: %w", filepath.Base(name), err)
	}
	read := func(want string) []byte {
		for _, f := range zr.File {
			if f.Name != want || f.UncompressedSize64 > 8<<20 {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil
			}
			defer rc.Close()
			var b bytes.Buffer
			if _, err := b.ReadFrom(rc); err != nil {
				return nil
			}
			return b.Bytes()
		}
		return nil
	}
	var specs []Spec
	// A Visual Studio Code extension lists its themes in package.json.
	for _, root := range []string{"extension/", ""} {
		pkg := read(root + "package.json")
		if pkg == nil {
			continue
		}
		var manifest struct {
			Contributes struct {
				Themes []struct {
					Label   string `json:"label"`
					UITheme string `json:"uiTheme"`
					Path    string `json:"path"`
				} `json:"themes"`
			} `json:"contributes"`
		}
		if json.Unmarshal(stripJSONC(pkg), &manifest) != nil {
			continue
		}
		for _, th := range manifest.Contributes.Themes {
			body := read(path.Join(root, th.Path))
			if body == nil {
				continue
			}
			label := th.Label
			if label == "" {
				label = fallbackName(th.Path)
			}
			spec, err := parseJSON(body, label)
			if err != nil {
				continue
			}
			spec.Name = label
			if spec.Type == "" {
				spec.Type = "light"
				if strings.Contains(th.UITheme, "dark") || th.UITheme == "hc-black" {
					spec.Type = "dark"
				}
			}
			specs = append(specs, spec)
		}
		if len(specs) > 0 {
			return specs, nil
		}
	}
	if m := read("manifest.json"); m != nil {
		if spec, err := parseJSON(m, fallbackName(name)); err == nil {
			return []Spec{spec}, nil
		}
	}
	// Else any theme file inside.
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".json") || strings.HasSuffix(f.Name, "package.json") {
			continue
		}
		if body := read(f.Name); body != nil {
			if spec, err := parseJSON(body, fallbackName(f.Name)); err == nil {
				specs = append(specs, spec)
			}
		}
	}
	if len(specs) == 0 {
		return nil, ErrNotATheme
	}
	return specs, nil
}

// stripJSONC removes what Visual Studio Code allows in its JSON files and
// JSON does not: comments, and commas before a closing bracket.
func stripJSONC(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	out := make([]byte, 0, len(data))
	inString := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out = append(out, c)
			if c == '\\' && i+1 < len(data) {
				i++
				out = append(out, data[i])
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++
		case c == '}' || c == ']':
			// Drop a comma left before it.
			j := len(out) - 1
			for j >= 0 && (out[j] == ' ' || out[j] == '\n' || out[j] == '\t' || out[j] == '\r') {
				j--
			}
			if j >= 0 && out[j] == ',' {
				out = append(out[:j], out[j+1:]...)
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

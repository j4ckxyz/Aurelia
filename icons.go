package main

import (
	"bytes"
	"embed"
	"strings"

	"github.com/egoist/mygo/ui"
)

// The icons are Lucide's (assets/icons/LICENSE); the logo is Aurelia's
// own.
//
//go:embed assets/icons/*.svg assets/icons/color/*.svg assets/logo.svg
var iconFiles embed.FS

var icons = map[string]*ui.SVG{}

// icon returns an icon by its Lucide name. A name ending in "-fill" is
// the icon with its shapes filled, as the play button's.
func icon(name string) *ui.SVG {
	if s, ok := icons[name]; ok {
		return s
	}
	file, fill := strings.CutSuffix(name, "-fill")
	path := "assets/icons/" + file + ".svg"
	if file == "logo" {
		path = "assets/logo.svg"
	}
	data, err := iconFiles.ReadFile(path)
	if err != nil {
		panic("no icon named " + name)
	}
	if fill {
		data = bytes.Replace(data, []byte(`fill="none"`), []byte(`fill="currentColor"`), 1)
	}
	s := ui.MustParseSVG(data)
	icons[name] = s
	return s
}

// colorIcon returns the picture of an icon in the colors of the thing it
// shows, as the sidebar has them, or nil when it has none: they are drawn
// after Lucide's shapes (assets/icons/color).
func colorIcon(name string) *ui.SVG {
	key := "color/" + name
	if s, ok := icons[key]; ok {
		return s
	}
	var s *ui.SVG
	if data, err := iconFiles.ReadFile("assets/icons/" + key + ".svg"); err == nil {
		s = ui.MustParseSVG(data)
	}
	icons[key] = s
	return s
}

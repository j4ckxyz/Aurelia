package main

import (
	"bytes"
	"embed"
	"strings"

	"github.com/egoist/mygo/ui"
)

// The icons are Lucide's (assets/icons/LICENSE).
//
//go:embed assets/icons/*.svg
var iconFiles embed.FS

var icons = map[string]*ui.SVG{}

// icon returns an icon by its Lucide name. A name ending in "-fill" is
// the icon with its shapes filled, as the play button's.
func icon(name string) *ui.SVG {
	if s, ok := icons[name]; ok {
		return s
	}
	file, fill := strings.CutSuffix(name, "-fill")
	data, err := iconFiles.ReadFile("assets/icons/" + file + ".svg")
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

package theme

import (
	"os"
	"path/filepath"
	"testing"
)

// Themes as published, in a directory that AURELIA_TEST_THEMES names.
func TestPublishedThemes(t *testing.T) {
	dir := os.Getenv("AURELIA_TEST_THEMES")
	if dir == "" {
		t.Skip("AURELIA_TEST_THEMES names no directory")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		specs, err := Parse(data, f)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		for _, s := range specs {
			th := Resolve(s)
			t.Logf("%s: %q dark=%v bg=%s sidebar=%s bar=%s surface=%s text=%s muted=%s accent=%s on=%s sel=%s",
				filepath.Base(f), th.Name, th.Dark, th.Background.Hex(), th.Sidebar.Hex(), th.Bar.Hex(), th.Surface.Hex(),
				th.Text.Hex(), th.TextMuted.Hex(), th.Accent.Hex(), th.AccentText.Hex(), th.Selection.Hex())
			if Contrast(th.Text, th.Background) < 4.5 || Contrast(th.AccentText, th.Accent) < 3 {
				t.Errorf("%s: %q does not read", filepath.Base(f), th.Name)
			}
		}
	}
}

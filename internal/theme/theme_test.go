package theme

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseColor(t *testing.T) {
	for in, want := range map[string]Color{
		"#fff":                     {255, 255, 255, 255},
		"#1a1b26":                  {26, 27, 38, 255},
		"#7e57c233":                {126, 87, 194, 0x33},
		"rgb(54, 32, 44)":          {54, 32, 44, 255},
		"rgba(255, 159, 90, 0.5)":  {255, 159, 90, 128},
		"rgb(100% 0% 0% / 50%)":    {255, 0, 0, 128},
		"hsl(0, 100%, 50%)":        {255, 0, 0, 255},
		"hsl(120deg 100% 25%)":     {0, 128, 0, 255},
		"hsla(240, 100%, 50%, .5)": {0, 0, 255, 128},
		"White":                    {255, 255, 255, 255},
	} {
		got, ok := ParseColor(in)
		if !ok || got != want {
			t.Errorf("ParseColor(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "#12", "#gggggg", "rgb(1,2)", "nope", "rgb(a,b,c)"} {
		if _, ok := ParseColor(bad); ok {
			t.Errorf("ParseColor(%q) succeeded", bad)
		}
	}
}

// Every theme built in reads well: text on its backgrounds, the accent
// on the page, and what is written on the accent.
func TestBuiltInThemesAreReadable(t *testing.T) {
	ids := map[string]bool{}
	for _, th := range BuiltIn() {
		if ids[th.ID] {
			t.Errorf("two themes with the ID %q", th.ID)
		}
		ids[th.ID] = true
		for _, bg := range []Color{th.Background, th.Sidebar, th.Bar, th.Surface, th.SurfaceHover, th.Selection} {
			if c := Contrast(th.Text, bg); c < 4.5 {
				t.Errorf("%s: text on %s has a contrast of %.1f", th.Name, bg.Hex(), c)
			}
			if c := Contrast(th.TextMuted, bg); c < 3 {
				t.Errorf("%s: secondary text on %s has a contrast of %.1f", th.Name, bg.Hex(), c)
			}
		}
		if c := Contrast(th.Accent, th.Background); c < 3 {
			t.Errorf("%s: the accent on the background has a contrast of %.1f", th.Name, c)
		}
		if c := Contrast(th.AccentText, th.Accent); c < 4.5 {
			t.Errorf("%s: text on the accent has a contrast of %.1f", th.Name, c)
		}
	}
	if !ids[DefaultDark] || !ids[DefaultLight] {
		t.Error("the default themes are missing")
	}
}

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestVSCodeTheme(t *testing.T) {
	specs, err := Parse(read(t, "vscode-color-theme.json"), "night-owlish-color-theme.json")
	if err != nil {
		t.Fatal(err)
	}
	th := Resolve(specs[0])
	if th.Name != "Night Owlish" || !th.Dark {
		t.Errorf("name %q, dark %v", th.Name, th.Dark)
	}
	want := map[string]string{
		"background": "#011627", "sidebar": "#010e1a", "bar": "#010e1a", "surface": "#0b253a",
		"text": "#d6deeb", "accent": "#7e57c2", "accentText": "#ffffff", "border": "#5f7e97", "danger": "#ef5350",
	}
	for k, v := range want {
		if got := th.Color(k).Hex(); got != v {
			t.Errorf("%s = %s, want %s", k, got, v)
		}
	}
	// Translucent colors are drawn over the background.
	if h := th.SurfaceHover; h.A != 255 || h == th.Background {
		t.Errorf("the hover color is %v", h)
	}
	if th.TextMuted.A != 255 || Contrast(th.TextMuted, th.Background) < 4 {
		t.Errorf("secondary text is %v", th.TextMuted)
	}
}

func TestBrowserThemes(t *testing.T) {
	specs, err := Parse(read(t, "firefox-manifest.json"), "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	fx := Resolve(specs[0])
	if fx.Name != "Sunset Dunes" || !fx.Dark {
		t.Errorf("Firefox: name %q, dark %v", fx.Name, fx.Dark)
	}
	for k, v := range map[string]string{"background": "#4a2c3a", "bar": "#36202c", "sidebar": "#2d1a25", "text": "#faebe1", "accent": "#ff9f5a"} {
		if got := fx.Color(k).Hex(); got != v {
			t.Errorf("Firefox: %s = %s, want %s", k, got, v)
		}
	}
	if Contrast(fx.AccentText, fx.Accent) < 4.5 {
		t.Errorf("Firefox: text on the accent is %v on %v", fx.AccentText, fx.Accent)
	}

	specs, err = Parse(read(t, "chrome-manifest.json"), "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	cr := Resolve(specs[0])
	if cr.Name != "Paper" || cr.Dark {
		t.Errorf("Chrome: name %q, dark %v", cr.Name, cr.Dark)
	}
	for k, v := range map[string]string{"background": "#fafaf8", "bar": "#dee1e6", "text": "#202124", "accent": "#1a73e8", "surface": "#f1f3f4"} {
		if got := cr.Color(k).Hex(); got != v {
			t.Errorf("Chrome: %s = %s, want %s", k, got, v)
		}
	}
}

// A theme that says nearly nothing, or nonsense, still reads.
func TestResolveKeepsThemesReadable(t *testing.T) {
	for _, s := range []Spec{
		{Name: "empty"},
		{Name: "only a background", Colors: map[string]string{"background": "#fdf6e3"}},
		{Name: "text like the background", Colors: map[string]string{"background": "#202020", "text": "#222222", "accent": "#212121", "textMuted": "#202020"}},
		{Name: "light said dark", Type: "dark", Colors: map[string]string{"background": "#ffffff"}},
	} {
		th := Resolve(s)
		if Contrast(th.Text, th.Background) < 4.5 || Contrast(th.TextMuted, th.Background) < 2.6 ||
			Contrast(th.Accent, th.Background) < 1.6 || Contrast(th.AccentText, th.Accent) < 3 {
			t.Errorf("%s: text %s, secondary %s, accent %s on %s", s.Name, th.Text.Hex(), th.TextMuted.Hex(), th.Accent.Hex(), th.Background.Hex())
		}
	}
}

func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range files {
		w, _ := zw.Create(name)
		w.Write(data)
	}
	zw.Close()
	return b.Bytes()
}

func TestExtensionArchives(t *testing.T) {
	theme := read(t, "vscode-color-theme.json")
	pkg := []byte(`{"name":"owl","contributes":{"themes":[
		{"label":"Owl Dark","uiTheme":"vs-dark","path":"./themes/dark.json"},
		{"label":"Owl Also","uiTheme":"vs-dark","path":"themes/dark.json"}]}}`)
	specs, err := Parse(zipOf(t, map[string][]byte{"extension/package.json": pkg, "extension/themes/dark.json": theme, "[Content_Types].xml": []byte("<x/>")}), "owl.vsix")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || specs[0].Name != "Owl Dark" || specs[0].Colors["background"] != "#011627" {
		t.Errorf("vsix: %+v", specs)
	}
	// A Chrome extension: a zip after a header.
	crx := append([]byte("Cr24\x03\x00\x00\x00\x10\x00\x00\x00headerheaderhead"), zipOf(t, map[string][]byte{"manifest.json": read(t, "chrome-manifest.json")})...)
	specs, err = Parse(crx, "paper.crx")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].Name != "Paper" {
		t.Errorf("crx: %+v", specs)
	}
	if _, err := Parse(zipOf(t, map[string][]byte{"readme.txt": []byte("hi")}), "x.zip"); err == nil {
		t.Error("a zip without a theme was read as one")
	}
	if _, err := Parse([]byte(`{"hello": 1}`), "x.json"); err != ErrNotATheme {
		t.Errorf("a JSON file without colors: %v", err)
	}
}

// The directory of themes: files of any kind dropped in it are themes,
// read again as they change.
func TestStore(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	n := len(s.Themes())
	if n == 0 || s.Get(DefaultDark) == nil {
		t.Fatal("no built-in themes")
	}
	os.WriteFile(filepath.Join(dir, "owl.json"), read(t, "vscode-color-theme.json"), 0o644)
	os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a theme"), 0o644)
	if !s.Changed() || len(s.Themes()) != n+1 {
		t.Fatalf("%d themes after adding one", len(s.Themes()))
	}
	if s.Changed() {
		t.Error("changed without a change")
	}
	if s.Errors["broken.json"] == "" || len(s.Errors) != 1 {
		t.Errorf("errors: %v", s.Errors)
	}
	owl := s.Get("user-owl")
	if owl == nil || owl.Name != "Night Owlish" || owl.BuiltIn {
		t.Fatalf("the theme of owl.json: %+v", owl)
	}
	// Importing writes one of Aurelia's own, whole, to edit.
	ids, err := s.Import(filepath.Join("testdata", "firefox-manifest.json"))
	if err != nil || len(ids) != 1 {
		t.Fatalf("import: %v, %v", ids, err)
	}
	imported := s.Get(ids[0])
	var written Spec
	data, _ := os.ReadFile(imported.Path)
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("the imported file is not JSON: %v\n%s", err, data)
	}
	if written.Name != "Sunset Dunes" || len(written.Colors) != len(Keys) || written.Colors["accent"] != "#ff9f5a" {
		t.Errorf("imported: %+v", written)
	}
	// Edited, saved over its file, and deleted.
	written.Colors["accent"] = "#00ff00"
	id, err := s.Save(written, imported.Path)
	if err != nil || id != ids[0] || s.Get(id).Accent != (Color{0, 255, 0, 255}) {
		t.Errorf("saved over: %q, %v", id, err)
	}
	if id2, _ := s.Save(written, ""); id2 == id || s.Get(id2) == nil {
		t.Errorf("a second theme of the same name took the ID %q", id2)
	}
	if err := s.Delete(id); err != nil || s.Get(id) != nil {
		t.Errorf("delete: %v", err)
	}
	if s.Delete(DefaultDark); s.Get(DefaultDark) == nil {
		t.Error("a built-in theme was deleted")
	}
}

package theme

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func spec(name, kind string, kv ...string) Spec {
	s := Spec{Name: name, Type: kind, Colors: map[string]string{}}
	for i := 0; i+1 < len(kv); i += 2 {
		s.Colors[kv[i]] = kv[i+1]
	}
	return s
}

// builtIn are the themes that come with Aurelia. The first dark one and
// the first light one are what "Match the system" shows.
var builtIn = []Spec{
	spec("Aurelia Dark", "dark",
		"background", "#0f0f12", "sidebar", "#0a0a0c", "bar", "#131317", "surface", "#1b1b21", "surfaceHover", "#25252d",
		"border", "#24242b", "text", "#ececf1", "textMuted", "#9a9aa8", "accent", "#a48cff", "accentText", "#100e1a",
		"selection", "#2a2545"),
	spec("Aurelia Light", "light",
		"background", "#ffffff", "sidebar", "#f6f6f8", "bar", "#fbfbfc", "surface", "#f1f1f4", "surfaceHover", "#e7e7ec",
		"border", "#e4e4ea", "text", "#17171c", "textMuted", "#6b6b78", "accent", "#6d4fe0", "accentText", "#ffffff",
		"selection", "#e6e0fb"),
	spec("Midnight", "dark",
		"background", "#000000", "sidebar", "#000000", "bar", "#080808", "surface", "#141414", "surfaceHover", "#1e1e1e",
		"border", "#1c1c1c", "text", "#f2f2f2", "textMuted", "#8e8e93", "accent", "#5eead4", "accentText", "#04201c",
		"selection", "#0f2b28"),
	spec("Catppuccin Mocha", "dark",
		"background", "#1e1e2e", "sidebar", "#181825", "bar", "#11111b", "surface", "#313244", "surfaceHover", "#45475a",
		"border", "#313244", "text", "#cdd6f4", "textMuted", "#a6adc8", "accent", "#cba6f7", "accentText", "#11111b",
		"selection", "#3b3654", "danger", "#f38ba8", "success", "#a6e3a1", "warning", "#f9e2af"),
	spec("Catppuccin Latte", "light",
		"background", "#eff1f5", "sidebar", "#e6e9ef", "bar", "#dce0e8", "surface", "#dfe2ea", "surfaceHover", "#ccd0da",
		"border", "#ccd0da", "text", "#4c4f69", "textMuted", "#6c6f85", "accent", "#8839ef", "accentText", "#eff1f5",
		"selection", "#d9c9f5", "danger", "#d20f39", "success", "#40a02b", "warning", "#df8e1d"),
	spec("Tokyo Night", "dark",
		"background", "#1a1b26", "sidebar", "#16161e", "bar", "#16161e", "surface", "#24283b", "surfaceHover", "#2f3549",
		"border", "#292e42", "text", "#c0caf5", "textMuted", "#7f89b5", "accent", "#7aa2f7", "accentText", "#16161e",
		"selection", "#283457", "danger", "#f7768e", "success", "#9ece6a", "warning", "#e0af68"),
	spec("Nord", "dark",
		"background", "#2e3440", "sidebar", "#292e39", "bar", "#272c36", "surface", "#3b4252", "surfaceHover", "#434c5e",
		"border", "#3b4252", "text", "#eceff4", "textMuted", "#a9b3c7", "accent", "#88c0d0", "accentText", "#2e3440",
		"selection", "#3f4c5f", "danger", "#bf616a", "success", "#a3be8c", "warning", "#ebcb8b"),
	spec("Dracula", "dark",
		"background", "#282a36", "sidebar", "#21222c", "bar", "#191a21", "surface", "#343746", "surfaceHover", "#44475a",
		"border", "#3a3d4d", "text", "#f8f8f2", "textMuted", "#a4abcb", "accent", "#bd93f9", "accentText", "#21222c",
		"selection", "#44475a", "danger", "#ff5555", "success", "#50fa7b", "warning", "#f1fa8c"),
	spec("Gruvbox Dark", "dark",
		"background", "#282828", "sidebar", "#1d2021", "bar", "#1d2021", "surface", "#3c3836", "surfaceHover", "#504945",
		"border", "#3c3836", "text", "#ebdbb2", "textMuted", "#a89984", "accent", "#fabd2f", "accentText", "#282828",
		"selection", "#4a4130", "danger", "#fb4934", "success", "#b8bb26", "warning", "#fe8019"),
	spec("Rosé Pine", "dark",
		"background", "#191724", "sidebar", "#1f1d2e", "bar", "#1f1d2e", "surface", "#26233a", "surfaceHover", "#312e4a",
		"border", "#2a273f", "text", "#e0def4", "textMuted", "#908caa", "accent", "#c4a7e7", "accentText", "#191724",
		"selection", "#352f52", "danger", "#eb6f92", "success", "#9ccfd8", "warning", "#f6c177"),
	spec("One Dark", "dark",
		"background", "#282c34", "sidebar", "#21252b", "bar", "#21252b", "surface", "#31363f", "surfaceHover", "#3a3f4b",
		"border", "#3b4048", "text", "#d7dae0", "textMuted", "#8b93a1", "accent", "#61afef", "accentText", "#1b1f27",
		"selection", "#2f4660", "danger", "#e06c75", "success", "#98c379", "warning", "#e5c07b"),
	spec("GitHub Dark", "dark",
		"background", "#0d1117", "sidebar", "#010409", "bar", "#010409", "surface", "#161b22", "surfaceHover", "#21262d",
		"border", "#30363d", "text", "#e6edf3", "textMuted", "#8d96a0", "accent", "#4493f8", "accentText", "#0d1117",
		"selection", "#16304f", "danger", "#f85149", "success", "#3fb950", "warning", "#d29922"),
	spec("GitHub Light", "light",
		"background", "#ffffff", "sidebar", "#f6f8fa", "bar", "#f6f8fa", "surface", "#f0f3f6", "surfaceHover", "#e6eaef",
		"border", "#d1d9e0", "text", "#1f2328", "textMuted", "#59636e", "accent", "#0969da", "accentText", "#ffffff",
		"selection", "#ddf4ff", "danger", "#d1242f", "success", "#1a7f37", "warning", "#9a6700"),
	spec("Solarized Light", "light",
		"background", "#fdf6e3", "sidebar", "#eee8d5", "bar", "#eee8d5", "surface", "#eee8d5", "surfaceHover", "#e3dcc6",
		"border", "#ddd6c1", "text", "#3d5158", "textMuted", "#5f7379", "accent", "#1f6fb0", "accentText", "#fdf6e3",
		"selection", "#dfe6d6", "danger", "#dc322f", "success", "#859900", "warning", "#b58900"),
}

// DefaultDark and DefaultLight are the IDs of the themes that follow the
// system's appearance.
const (
	DefaultDark  = "aurelia-dark"
	DefaultLight = "aurelia-light"
)

// BuiltIn returns the themes that come with Aurelia.
func BuiltIn() []*Theme {
	out := make([]*Theme, len(builtIn))
	for i, s := range builtIn {
		t := Resolve(s)
		t.ID, t.BuiltIn = Slug(s.Name), true
		out[i] = t
	}
	return out
}

// Store holds the themes: those built in, and the files of a directory,
// which it reads again as they change.
type Store struct {
	Dir string

	themes []*Theme
	stamp  string
	// Errors are the files of the directory that hold no theme, with
	// why, by name.
	Errors map[string]string
}

// NewStore returns the themes of dir, with those built in.
func NewStore(dir string) *Store {
	s := &Store{Dir: dir}
	s.Reload()
	return s
}

// Themes returns every theme: those built in first, then the user's by
// name.
func (s *Store) Themes() []*Theme { return s.themes }

// Get returns the theme of an ID, or nil.
func (s *Store) Get(id string) *Theme {
	for _, t := range s.themes {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// dirStamp changes when a file of the directory does.
func (s *Store) dirStamp() string {
	des, err := os.ReadDir(s.Dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, de := range des {
		if info, err := de.Info(); err == nil && !de.IsDir() {
			b.WriteString(de.Name())
			b.WriteString(info.ModTime().Format(time.RFC3339Nano))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Changed reads the directory again if a file of it changed, and reports
// whether it did: a theme edited in a text editor shows as it is saved.
func (s *Store) Changed() bool {
	if s.dirStamp() == s.stamp {
		return false
	}
	s.Reload()
	return true
}

// Reload reads the directory.
func (s *Store) Reload() {
	s.themes = BuiltIn()
	s.Errors = map[string]string{}
	s.stamp = s.dirStamp()
	des, _ := os.ReadDir(s.Dir)
	var user []*Theme
	taken := map[string]bool{}
	for _, t := range s.themes {
		taken[t.ID] = true
	}
	for _, de := range des {
		name := de.Name()
		if de.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".json", ".jsonc", ".vsix", ".xpi", ".crx", ".zip":
		default:
			continue
		}
		p := filepath.Join(s.Dir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			s.Errors[name] = err.Error()
			continue
		}
		specs, err := Parse(data, name)
		if err != nil {
			s.Errors[name] = err.Error()
			continue
		}
		base := Slug(strings.TrimSuffix(name, filepath.Ext(name)))
		for _, sp := range specs {
			t := Resolve(sp)
			t.ID, t.Path = "user-"+base, p
			if len(specs) > 1 {
				t.ID += "-" + Slug(sp.Name)
			}
			if t.Name == "" {
				t.Name = base
			}
			for id, n := t.ID, 2; taken[t.ID]; n++ {
				t.ID = id + "-" + itoa(n)
			}
			taken[t.ID] = true
			user = append(user, t)
		}
	}
	slices.SortStableFunc(user, func(a, b *Theme) int {
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	s.themes = append(s.themes, user...)
}

// Save writes a theme of Aurelia's into the directory, under a file name
// made of its name, or over file when it names one, and returns the ID
// the theme has from then on.
func (s *Store) Save(sp Spec, file string) (id string, err error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", err
	}
	if file == "" {
		base := Slug(sp.Name)
		file = filepath.Join(s.Dir, base+".json")
		for n := 2; ; n++ {
			if _, err := os.Stat(file); err != nil {
				break
			}
			file = filepath.Join(s.Dir, base+"-"+itoa(n)+".json")
		}
	}
	if err := os.WriteFile(file, Marshal(sp), 0o644); err != nil {
		return "", err
	}
	s.Reload()
	for _, t := range s.themes {
		if t.Path == file {
			return t.ID, nil
		}
	}
	return "", nil
}

// Import reads the themes of a file anywhere, of any kind Parse reads,
// and saves each as a theme of Aurelia's. It returns their IDs.
func (s *Store) Import(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	specs, err := Parse(data, path)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, sp := range specs {
		// Written out whole, so that the file is one to edit.
		full := Resolve(sp).Full()
		id, err := s.Save(full, "")
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Delete removes a theme of the user's.
func (s *Store) Delete(id string) error {
	t := s.Get(id)
	if t == nil || t.BuiltIn || t.Path == "" {
		return nil
	}
	err := os.Remove(t.Path)
	s.Reload()
	return err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

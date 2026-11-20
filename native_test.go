package main

import (
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The system's own buttons over the window: nothing of the app's is under
// them, whichever side they are on, with the queue open or not.
func TestSystemButtonsKeepClear(t *testing.T) {
	const width, height = 1240, 780
	bars := []struct {
		name string
		bar  ui.TitleBar
	}{
		{"windows", ui.TitleBar{Height: topBarH, Right: 3 * 46}},
		{"gnome", ui.TitleBar{Height: topBarH, Right: 46}},
		{"elementary", ui.TitleBar{Height: topBarH, Left: 46, Right: 46}},
		{"macos", ui.TitleBar{Height: 28, Left: 78}},
	}
	for _, b := range bars {
		for _, queue := range []bool{false, true} {
			name := b.name
			if queue {
				name += "-queue"
			}
			t.Run(name, func(t *testing.T) {
				a := testApp(t)
				a.settings.QueueOpen = queue
				a.player.play(a.lib.AlbumSongs("al1"), 0)
				a.player.enqueue(a.lib.AlbumSongs("al2"))
				tt := ui.NewTester(a.view, width, height)
				tt.SetTitleBar(b.bar)
				tt.Frame()
				tt.Frame()
				if dir := os.Getenv("AURELIA_CHROME_SHOTS"); dir != "" {
					f, err := os.Create(filepath.Join(dir, name+".png"))
					if err != nil {
						t.Fatal(err)
					}
					png.Encode(f, tt.Image())
					f.Close()
				}
				left := ui.Rect{X: 0, Y: 0, W: b.bar.Left, H: b.bar.Height}
				right := ui.Rect{X: width - b.bar.Right, Y: 0, W: b.bar.Right, H: b.bar.Height}
				labels := []string{"Search", "Save", "Clear", "Queue", "Close the queue", "Aurelia's menu", "Back", "Forward", "Hide the sidebar"}
				for _, l := range labels {
					r, ok := tt.Find(l)
					if !ok {
						continue
					}
					for _, under := range []ui.Rect{left, right} {
						if under.W > 0 && overlaps(r, under) {
							t.Errorf("%q at %v is under the system's buttons at %v", l, r, under)
						}
					}
				}
				if queue && b.bar.Right > 0 {
					if _, ok := tt.Find("Close the queue"); ok {
						t.Error("the queue has a close button of its own beside the window's")
					}
				}
			})
		}
	}
}

func overlaps(a, b ui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// The app's menu button opens the menus of the menu bar, made by the
// system; on macOS the menu bar has them.
func TestAppMenu(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 780)
	tt.Frame()
	if err := tt.Click("Aurelia's menu"); err != nil {
		if runtime.GOOS == "darwin" {
			return
		}
		t.Fatal(err)
	}
	tt.Frame()
	items := tt.Menu()
	for _, want := range []string{"Check for Updates…", "View", "Playback", "Library", "Quit Aurelia"} {
		found := false
		for _, it := range items {
			if it == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the menu has no %q: %q", want, items)
		}
	}
	if err := tt.ChooseMenuItem("View", "Albums"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.router.Path() != "/albums" {
		t.Errorf("View ▸ Albums went to %q", a.router.Path())
	}
}

// Aurelia is a music player for Jellyfin, in MyGo's native UI: no
// webview, one small binary, and a library kept on disk so that every
// page shows at once.
//
//	go tool mygo dev      the app, rebuilt as the code changes
//	go test ./...         the packages, and the view without a window
//	go tool mygo build    the app for this platform, in build/
package main

import (
	"context"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
)

func main() {
	if commandLine(os.Args[1:]) {
		return
	}
	// A music player's heap is small and steady: collect it eagerly, so
	// that the app stays near the memory it needs.
	debug.SetGCPercent(15)
	debug.SetMemoryLimit(56 << 20)

	// One Aurelia at a time: opening it again shows the one running.
	// Development runs several, each with a directory of its own.
	single := os.Getenv("AURELIA_DIR") == ""
	if single && !mygo.App.RequestSingleInstanceLock() {
		return
	}
	jellyfin.Version = appVersion()
	a := newApp(appDirs(), false)
	if single {
		mygo.App.OnSecondInstance(func([]string, string) { a.show() })
	}
	mygo.App.SetMenu(a.menu())
	mygo.App.WhenReady(a.open)
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			a.show()
		}
	})
	mygo.App.OnWindowAllClosed(func() {
		// On macOS the music plays on with the window closed, as in
		// the system's own player.
		if runtime.GOOS != "darwin" {
			mygo.App.Quit()
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.quitting = true
		a.quit()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// open opens the window.
func (a *App) open() {
	a.systemDark = mygo.Theme.IsDark()
	opts := mygo.WindowOptions{
		Title:           "Aurelia",
		Width:           1240,
		Height:          800,
		MinWidth:        860,
		MinHeight:       540,
		StateKey:        "main",
		BackgroundColor: a.theme().Background.Hex(),
		Content:         ui.View(a.view),
	}
	switch {
	case runtime.GOOS == "darwin":
		// The view draws under the window's buttons.
		opts.TitleBarStyle = mygo.TitleBarHidden
		opts.TrafficLightPosition = &mygo.Point{X: 18, Y: 18}
	case ownControls:
		opts.Frameless = true
	}
	win := mygo.NewWindow(opts)
	a.win = win
	a.rescale()
	win.OnMove(a.rescale)
	if runtime.GOOS == "darwin" {
		// Closing the window hides it, and the music plays on, as in
		// the system's own player; the Dock's icon shows it again.
		win.OnClose(func(e *mygo.CloseEvent) {
			if !a.quitting {
				e.PreventDefault()
				win.Hide()
			}
		})
	}
	win.OnClosed(func() { a.win = nil })
	mygo.Theme.OnUpdated(func() {
		dark := mygo.Theme.IsDark()
		a.update(func() { a.systemDark = dark })
	})
	win.Update(a.drain) // what happened before there was a window
	if !a.started {
		a.started = true
		a.start()
		a.startUpdates()
		// What the system offers beyond the window is a courtesy: where
		// it fails, the player still plays.
		func() {
			defer func() {
				if err := recover(); err != nil {
					log.Printf("telling the system what plays: %v\n%s", err, debug.Stack())
				}
			}()
			a.system.init(a)
			a.mediaKeys()
		}()
		go a.keepHouse()
	}
	a.debugHook()
}

// keepHouse does, every second, what must go on whether or not the
// window draws: a window that is hidden, covered or behind a locked
// screen builds no frames, and the music plays on. The system is told of
// the song that began meanwhile, and the server of how far it is.
func (a *App) keepHouse() {
	for range time.Tick(time.Second) {
		mygo.RunOnMain(func() {
			a.tellSystem()
			if a.player.playing() {
				a.player.reportProgress(false)
			}
		})
	}
}

// show brings the window back, or opens one.
func (a *App) show() {
	if a.win == nil {
		a.open()
		return
	}
	a.win.Show()
	a.win.Focus()
}

// rescale notes the pixels per point of the display the window is on,
// for the size of the pictures to ask for.
func (a *App) rescale() {
	win := a.win
	if win == nil {
		return
	}
	b := win.Bounds()
	if s := mygo.Screen.DisplayMatching(b).ScaleFactor; s > 0 && s != a.scale {
		win.Update(func() { a.scale = s })
	}
}

// mediaKeys makes the keyboard's play, next and previous keys work while
// other apps are in front, where the system lets an app take them.
func (a *App) mediaKeys() {
	if a.system.handlesKeys() {
		return // the system sends them to the app playing, with what it shows of it
	}
	for key, fn := range map[string]func(){
		"MediaPlayPause":     func() { a.player.toggle() },
		"MediaNextTrack":     func() { a.player.skip() },
		"MediaPreviousTrack": func() { a.player.previous() },
	} {
		if err := mygo.GlobalShortcut.Register(key, func() { a.update(fn) }); err != nil {
			log.Printf("the %s key is not available: %v", key, err)
		}
	}
}

// quit tells the server the song stopped, and keeps the settings.
func (a *App) quit() {
	p := a.player
	if s, c := p.current(), a.clientNow(); s != nil && c != nil && p.session != "" && !p.finished {
		pb := jellyfin.Playback{SongID: s.ID, SessionID: p.session, Position: p.state().Position}
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		c.ReportStop(ctx, pb)
		cancel()
	}
	a.saveQueue()
	if p.engine != nil {
		p.engine.Close()
	}
	a.saveSettings()
	a.queueWrites.Wait() // the queue's file is written on another goroutine
}

// menu is the app's menu bar.
func (a *App) menu() *mygo.Menu {
	item := func(label, accel string, fn func()) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Accelerator: accel, Click: func(*mygo.MenuItem, *mygo.Window) {
			a.update(fn)
		}}
	}
	// The commands of a menu, with their keys.
	of := func(title string) []*mygo.MenuItem {
		var items []*mygo.MenuItem
		for i := range commands {
			if cmd := &commands[i]; cmd.menu == title {
				accel := cmd.keys[0].accelerator()
				switch cmd.id {
				case "toggle":
					accel = "" // Space is the window's: a menu would take it from what is typed
				case "back", "onward":
					// The brackets: the arrows with Command move in a
					// field's text, which a menu would take them from.
					accel = cmd.keys[1].accelerator()
				}
				items = append(items, item(cmd.label, accel, func() { a.do(cmd) }))
			}
		}
		return items
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			item("Check for Updates…", "", func() {
				if a.signedIn() {
					a.goTo("/settings")
				}
				a.checkForUpdates(checkAsked)
			}),
		}},
		{Label: "Edit", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo},
			{Role: mygo.RoleRedo},
			mygo.Separator(),
			{Role: mygo.RoleCut},
			{Role: mygo.RoleCopy},
			{Role: mygo.RolePaste},
			{Role: mygo.RoleSelectAll},
		}},
		{Label: "View", Submenu: append(of("View"), mygo.Separator(), &mygo.MenuItem{Role: mygo.RoleToggleFullScreen})},
		{Label: "Playback", Submenu: of("Playback")},
		{Label: "Library", Submenu: of("Library")},
		{Role: mygo.RoleWindowMenu},
	})
}

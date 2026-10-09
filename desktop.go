package main

import (
	"bytes"
	"fmt"
	"image/png"
	"log"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// What Aurelia shows of itself outside its window: an icon in the menu bar
// or the tray, with the song and its buttons, and a notification as the
// song changes.

// desktopState is what was last shown outside the window.
type desktopState struct {
	tray      *mygo.Tray
	trayKey   string // what the menu says, so that it is made again only when it changes
	announced string // the song the notification was last of
}

// trayName is what the icon is called on this system.
func trayName() string {
	if runtime.GOOS == "darwin" {
		return "menu bar"
	}
	return "tray"
}

// trayIcon draws the icon: the jellyfish of the logo, black for the menu
// bar to tint, in the accent elsewhere.
func (a *App) trayIcon() []byte {
	tint := ui.RGB(0, 0, 0)
	if runtime.GOOS != "darwin" {
		tint = a.pal.accent
	}
	const size = 22
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Icon(c, icon("logo")).Size(size, size).TextColor(tint)
	}, size, size)
	tt.SetScale(2)
	tt.Frame()
	var buf bytes.Buffer
	if err := png.Encode(&buf, tt.Image()); err != nil {
		return nil
	}
	return buf.Bytes()
}

// desktopSync keeps the tray and the notification as the song and the
// settings are; it is called as the system is told what plays.
func (a *App) desktopSync(s *library.Song, paused bool) {
	d := &a.desk
	func() {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("the tray: %v", err)
			}
		}()
		a.syncTray(s, paused)
	}()
	switch {
	case s == nil:
		d.announced = ""
	case s.ID != d.announced && !paused:
		d.announced = s.ID
		if a.settings.Notify && (a.win == nil || !a.win.IsFocused()) {
			n := mygo.NewNotification(mygo.NotificationOptions{
				ID: "song", Title: s.Name, Subtitle: s.Artist, Body: s.Album, Silent: true, Group: "Aurelia",
			})
			n.OnClick(func() { a.update(a.show) })
			if err := n.Show(); err != nil {
				log.Print("the notification: ", err)
			}
		}
	}
}

// syncTray shows or removes the icon, and makes its menu say what plays.
func (a *App) syncTray(s *library.Song, paused bool) {
	d := &a.desk
	if !a.settings.Tray {
		if d.tray != nil {
			d.tray.Destroy()
			d.tray, d.trayKey = nil, ""
		}
		return
	}
	title, tip := "Nothing is playing", "Aurelia"
	if s != nil {
		title = s.Name
		if s.Artist != "" {
			title += " — " + s.Artist
		}
		tip = title
	}
	key := fmt.Sprint(title, paused, s != nil)
	if d.tray != nil && d.trayKey == key {
		return
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: title, Disabled: true},
		mygo.Separator(),
		{Label: map[bool]string{true: "Play", false: "Pause"}[paused || s == nil], Disabled: s == nil,
			Click: func(*mygo.MenuItem, *mygo.Window) { a.update(func() { a.player.toggle() }) }},
		{Label: "Next", Disabled: s == nil, Click: func(*mygo.MenuItem, *mygo.Window) { a.update(func() { a.player.skip() }) }},
		{Label: "Previous", Disabled: s == nil, Click: func(*mygo.MenuItem, *mygo.Window) { a.update(func() { a.player.previous() }) }},
		mygo.Separator(),
		{Label: "Show Aurelia", Click: func(*mygo.MenuItem, *mygo.Window) { a.update(a.show) }},
		{Label: "Quit Aurelia", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
	if d.tray == nil {
		t, err := mygo.NewTray(mygo.TrayOptions{
			Icon: a.trayIcon(), IconIsTemplate: runtime.GOOS == "darwin", ToolTip: tip, Menu: menu,
		})
		if err != nil {
			log.Print("the ", trayName(), " icon: ", err)
			a.settings.Tray = false
			return
		}
		d.tray = t
	} else {
		d.tray.SetMenu(menu)
		d.tray.SetToolTip(tip)
	}
	d.trayKey = key
}

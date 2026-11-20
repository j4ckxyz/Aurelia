package main

import (
	"log"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// What the system draws for Aurelia, rather than Aurelia itself: the
// window's buttons, the menus and the questions that ask before something
// is undone. They look as the theme does, light or dark.

// wantAppearance is how the system's own parts of the window should look:
// as the theme chosen, or as the system when the theme follows it.
func (a *App) wantAppearance() mygo.ThemeSource {
	t := a.themes.Get(a.settings.Theme)
	if a.editing != nil {
		t = a.editing.resolved()
	}
	switch {
	case t == nil:
		return mygo.ThemeSystem
	case t.Dark:
		return mygo.ThemeDark
	}
	return mygo.ThemeLight
}

// applyAppearance tells the system how its parts of the window should
// look, when that changed: the caption buttons of Windows and GTK's title
// buttons take their colors from it, as menus and dialogs do everywhere.
func (a *App) applyAppearance() {
	if !a.native {
		return
	}
	want := a.wantAppearance()
	if want == a.appearance {
		return
	}
	a.appearance = want
	mygo.Theme.SetSource(want)
	if want == mygo.ThemeSystem {
		// The system's own appearance may have changed while it was
		// overridden, which no event told.
		go func() {
			dark := mygo.Theme.IsDark()
			a.update(func() {
				if a.appearance == mygo.ThemeSystem {
					a.systemDark = dark
				}
			})
		}()
	}
}

// askNatively asks a question that is answered yes or no in the system's
// own dialog, and reports whether it could.
func (a *App) askNatively(p promptState) bool {
	if !a.native || p.field {
		return false
	}
	buttons := []string{p.confirm, "Cancel"}
	typ, def := mygo.MessageQuestion, 0
	if p.danger {
		// What cannot be undone is not what Return does.
		typ, def = mygo.MessageWarning, 1
	}
	opts := mygo.MessageOptions{
		Parent: a.win, Type: typ, Message: p.title, Detail: p.text,
		Buttons: buttons, DefaultButton: def, CancelButton: 1,
	}
	if runtime.GOOS != "darwin" {
		// Windows and GTK show a title over the message; macOS none.
		opts.Title = "Aurelia"
	}
	go func() {
		r, err := mygo.Dialog.Message(opts)
		if err != nil {
			log.Print("the question: ", err)
			a.update(func() { p.open = true; a.prompt = p }) // asked in the app's own
			return
		}
		if r.Button == 0 && p.run != nil {
			a.update(func() { p.run(p.value) })
		}
	}()
	return true
}

// appMenu fills the menu that the app's menu button opens where windows
// have no menu bar of their own in sight (Windows, Linux): the menus of
// the menu bar, made by the system as the menu bar's are.
func (a *App) appMenu(m *ui.Menu) {
	group := func(title string) {
		m.Submenu(title, func(m *ui.Menu) {
			for i := range commands {
				cmd := &commands[i]
				if cmd.menu != title {
					continue
				}
				it := m.Item(cmd.label)
				k := cmd.keys[0]
				switch cmd.id {
				case "back", "onward":
					k = cmd.keys[1]
				}
				it.Shortcut(k.mods, k.key)
				if it.Chosen() {
					a.do(cmd)
				}
			}
		})
	}
	if m.Item("Check for Updates…").Chosen() {
		if a.signedIn() {
			a.goTo("/settings")
		}
		a.checkForUpdates(checkAsked)
	}
	m.Separator()
	group("View")
	group("Playback")
	group("Library")
	m.Separator()
	if m.Item("Quit Aurelia").Chosen() && a.native {
		mygo.App.Quit()
	}
}

// appMenuButton is the app's name at the top of the sidebar. Where windows
// have no menu bar in sight (Windows, Linux) it opens the app's menus, as
// a window's icon opens its menu there; on macOS the menu bar has them.
func (a *App) appMenuButton(c *ui.Context) {
	p := a.pal
	name := func() {
		ui.Icon(c, icon("logo")).Size(20, 20).TextColor(p.accent)
		ui.Text(c, "Aurelia").FontWeight(700).FontSize(15).SingleLine().Shrink(1).MinWidth(0)
	}
	if runtime.GOOS == "darwin" {
		ui.Row(c).Gap(8).Shrink(1).MinWidth(0).Children(name)
		return
	}
	b := ui.ButtonBase(c.Key("app-menu")).Height(30).Padding(0, 5).Margin(0, 0, 0, -5).Gap(4).Radius(p.radius).
		Shrink(1).MinWidth(0).Label("Aurelia's menu").Tooltip("Menu (Alt)")
	if b.Hovered() || b.Pressed() {
		b.Background(p.hover)
	}
	b.Children(func() {
		ui.Icon(c, icon("logo")).Size(20, 20).TextColor(p.accent).Margin(0, 4, 0, 0)
		ui.Text(c, "Aurelia").FontWeight(700).FontSize(15).SingleLine().Shrink(1).MinWidth(0)
		ui.Icon(c, icon("chevron-down")).Size(11, 11).TextColor(p.muted).Shrink(0)
	})
	b.Menu(a.appMenu)
}

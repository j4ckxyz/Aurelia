package main

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
)

// chord is a key with the modifiers held.
type chord struct {
	mods ui.Modifiers
	key  ui.Key
}

// command is something the keyboard does: its keys are the window's
// shortcuts, the menu bar's, and what the page of shortcuts lists.
type command struct {
	id    string
	group string
	label string
	keys  []chord // the first is the one shown
	run   func(a *App)
	// menu puts the command in the menu bar under that title.
	menu string
}

const move = ui.Alt | ui.Shift // the modifiers of the keys that go to a page

func page(path string) func(a *App) { return func(a *App) { a.goTo(path) } }

// commands are the app's, in the order the page of shortcuts lists them.
// They are set as the program starts, not where they are declared: some
// of what they do leads back to them.
var commands []command

func init() {
	commands = []command{
		{id: "toggle", group: "Playback", label: "Play or pause", keys: []chord{{0, ui.KeySpace}}, menu: "Playback",
			run: func(a *App) { a.player.toggle() }},
		{id: "next", group: "Playback", label: "Next song", keys: []chord{{primary | ui.Shift, ui.KeyRight}}, menu: "Playback",
			run: func(a *App) { a.player.skip() }},
		{id: "previous", group: "Playback", label: "Previous song", keys: []chord{{primary | ui.Shift, ui.KeyLeft}}, menu: "Playback",
			run: func(a *App) { a.player.previous() }},
		{id: "forward", group: "Playback", label: "Forward 5 seconds", keys: []chord{{ui.Shift, ui.KeyRight}},
			run: func(a *App) { a.seekBy(5 * time.Second) }},
		{id: "rewind", group: "Playback", label: "Back 5 seconds", keys: []chord{{ui.Shift, ui.KeyLeft}},
			run: func(a *App) { a.seekBy(-5 * time.Second) }},
		{id: "louder", group: "Playback", label: "Volume up", keys: []chord{{primary, ui.KeyUp}}, menu: "Playback",
			run: func(a *App) { a.setVolume(a.settings.Volume + 0.05) }},
		{id: "quieter", group: "Playback", label: "Volume down", keys: []chord{{primary, ui.KeyDown}}, menu: "Playback",
			run: func(a *App) { a.setVolume(a.settings.Volume - 0.05) }},
		{id: "mute", group: "Playback", label: "Mute", keys: []chord{{primary | ui.Shift, ui.KeyDown}, {0, ui.KeyM}}, menu: "Playback",
			run: func(a *App) { a.toggleMute() }},
		{id: "loudest", group: "Playback", label: "Full volume", keys: []chord{{primary | ui.Shift, ui.KeyUp}},
			run: func(a *App) { a.setVolume(1) }},
		{id: "shuffle", group: "Playback", label: "Shuffle", keys: []chord{{primary, ui.KeyS}}, menu: "Playback",
			run: func(a *App) { a.player.setShuffle(!a.settings.Shuffle) }},
		{id: "repeat", group: "Playback", label: "Repeat", keys: []chord{{primary, ui.KeyR}}, menu: "Playback",
			run: func(a *App) { a.player.cycleRepeat() }},
		{id: "like", group: "Playback", label: "Add the song playing to Favorites, or remove it", keys: []chord{{move, ui.KeyB}}, menu: "Playback",
			run: func(a *App) {
				if s := a.player.current(); s != nil {
					a.setFavorite(s.ID, &s.Favorite, !s.Favorite)
				}
			}},

		{id: "back", group: "Getting around", label: "Back", keys: []chord{{primary, ui.KeyLeft}, {primary, ui.KeyBracketLeft}, {ui.Alt, ui.KeyLeft}}, menu: "View",
			run: func(a *App) { a.router.Back() }},
		{id: "onward", group: "Getting around", label: "Forward", keys: []chord{{primary, ui.KeyRight}, {primary, ui.KeyBracketRight}, {ui.Alt, ui.KeyRight}}, menu: "View",
			run: func(a *App) { a.router.Forward() }},
		{id: "search", group: "Getting around", label: "Search", keys: []chord{{primary, ui.KeyK}, {primary, ui.KeyL}, {primary, ui.KeyF}, {0, ui.KeySlash}}, menu: "View",
			run: func(a *App) { a.search.focus = true }},
		{id: "home", group: "Getting around", label: "Home", keys: []chord{{move, ui.KeyH}, {primary, ui.Key1}}, menu: "View", run: page("/home")},
		{id: "albums", group: "Getting around", label: "Albums", keys: []chord{{move, ui.Key4}, {primary, ui.Key2}}, menu: "View", run: page("/albums")},
		{id: "artists", group: "Getting around", label: "Artists", keys: []chord{{move, ui.Key3}, {primary, ui.Key3}}, menu: "View", run: page("/artists")},
		{id: "songs", group: "Getting around", label: "Songs", keys: []chord{{move, ui.Key2}, {primary, ui.Key4}}, menu: "View", run: page("/songs")},
		{id: "favorites", group: "Getting around", label: "Favorites", keys: []chord{{move, ui.KeyS}, {primary, ui.Key5}}, menu: "View", run: page("/favorites")},
		{id: "playlists", group: "Getting around", label: "Playlists", keys: []chord{{move, ui.Key1}, {primary, ui.Key6}}, menu: "View", run: page("/playlists")},
		{id: "downloads", group: "Getting around", label: "Downloads", keys: []chord{{move, ui.KeyD}}, menu: "View", run: page("/downloads")},
		{id: "playing", group: "Getting around", label: "The album of the song playing", keys: []chord{{move, ui.KeyJ}}, menu: "View",
			run: func(a *App) {
				if s := a.player.current(); s != nil && s.AlbumID != "" {
					a.goTo("/album/" + s.AlbumID)
				}
			}},
		{id: "queue", group: "Getting around", label: "Show or hide the queue", keys: []chord{{move, ui.KeyQ}}, menu: "View",
			run: func(a *App) {
				a.settings.QueueOpen = !a.settings.QueueOpen
				a.saveSettings()
			}},
		{id: "lyrics", group: "Getting around", label: "Lyrics", keys: []chord{{move, ui.KeyL}}, menu: "View",
			run: func(a *App) {
				if a.router.Path() == "/lyrics" {
					a.router.Back()
				} else {
					a.goTo("/lyrics")
				}
			}},
		{id: "record", group: "Getting around", label: "The record and the lyrics, in place of the app", keys: []chord{{0, ui.KeyV}}, menu: "View",
			run: func(a *App) {
				if a.stage.on && !a.stage.mini {
					a.closeStage()
				} else {
					a.openStage()
				}
			}},
		{id: "stage", group: "Getting around", label: "The record in full screen", keys: []chord{{primary | ui.Shift, ui.KeyF}, {0, ui.KeyF}}, menu: "View",
			run: func(a *App) { a.stageFull(!a.inFull()) }},
		{id: "mini", group: "Getting around", label: "The record in a small window of its own", keys: []chord{{primary | ui.Shift, ui.KeyM}}, menu: "View",
			run: func(a *App) {
				if a.mini != nil {
					a.closeMini()
				} else {
					a.popOut()
				}
			}},
		{id: "sidebar", group: "Getting around", label: "Show or hide the sidebar", keys: []chord{{primary, ui.KeyB}}, menu: "View",
			run: func(a *App) { a.toggleSidebar() }},
		{id: "settings", group: "Getting around", label: "Settings", keys: []chord{{primary, ui.KeyComma}}, menu: "View", run: page("/settings")},
		{id: "shortcuts", group: "Getting around", label: "Keyboard shortcuts", keys: []chord{{primary, ui.KeySlash}, {ui.Shift, ui.KeySlash}}, menu: "View", run: page("/shortcuts")},
		{id: "sync", group: "Getting around", label: "Update the library", keys: []chord{{primary | ui.Shift, ui.KeyR}}, menu: "Library",
			run: func(a *App) { a.sync() }},

		{id: "down", group: "On a page", label: "The next item, or the one below", keys: []chord{{0, ui.KeyJ}}, run: func(a *App) { a.cursor.step(0, 1) }},
		{id: "up", group: "On a page", label: "The item before, or the one above", keys: []chord{{0, ui.KeyK}}, run: func(a *App) { a.cursor.step(0, -1) }},
		{id: "left", group: "On a page", label: "The item to the left", keys: []chord{{0, ui.KeyH}}, run: func(a *App) { a.cursor.step(-1, 0) }},
		{id: "right", group: "On a page", label: "The item to the right", keys: []chord{{0, ui.KeyL}}, run: func(a *App) { a.cursor.step(1, 0) }},
		{id: "open", group: "On a page", label: "Open the item, or play the song", keys: []chord{{0, ui.KeyEnter}}, run: func(a *App) { a.cursor.open() }},
		{id: "leave", group: "On a page", label: "Put the marker away", keys: []chord{{0, ui.KeyEscape}}, run: func(a *App) { a.cursor.hide() }},
	}
}

func (a *App) seekBy(d time.Duration) {
	st := a.player.state()
	if a.player.current() == nil || st.Duration <= 0 {
		return
	}
	a.player.seek(max(0, min(st.Duration-time.Second, st.Position+d)))
}

// shortcuts runs the commands whose keys were pressed. Space and Enter
// are asked of root, the element everything is built in: asked of the
// window, the button clicked last would take them first.
func (a *App) shortcuts(c *ui.Context, root ui.Element) {
	for i := range commands {
		cmd := &commands[i]
		for _, k := range cmd.keys {
			pressed := c.Shortcut(k.mods, k.key)
			switch {
			case k == chord{0, ui.KeySpace}:
				pressed = root.Shortcut(k.mods, k.key) || pressed
			case k == chord{0, ui.KeyEnter}, k == chord{0, ui.KeyEscape}:
				// Only while the marker shows: they are a dialog's and a
				// field's otherwise.
				pressed = a.cursor.shown && (root.Shortcut(k.mods, k.key) || pressed)
			}
			if pressed {
				a.do(cmd)
				return
			}
		}
	}
}

// do runs a command, of a key or of the menu bar.
func (a *App) do(cmd *command) {
	if !a.signedIn() {
		return
	}
	cmd.run(a)
}

// keyNames are the keys as the menu bar's accelerators name them, and
// as the page of shortcuts shows them.
var keyNames = map[ui.Key][2]string{
	ui.KeySpace: {"Space", "Space"}, ui.KeyEnter: {"Enter", "Enter"}, ui.KeyEscape: {"Escape", "Esc"},
	ui.KeyLeft: {"Left", "←"}, ui.KeyRight: {"Right", "→"}, ui.KeyUp: {"Up", "↑"}, ui.KeyDown: {"Down", "↓"},
	ui.KeyComma: {",", ","}, ui.KeySlash: {"/", "/"}, ui.KeyBracketLeft: {"[", "["}, ui.KeyBracketRight: {"]", "]"},
}

func keyName(k ui.Key, shown bool) string {
	i := 0
	if shown {
		i = 1
	}
	switch {
	case k >= ui.KeyA && k <= ui.KeyZ:
		return string(rune('A' + int(k-ui.KeyA)))
	case k >= ui.Key0 && k <= ui.Key9:
		return string(rune('0' + int(k-ui.Key0)))
	}
	return keyNames[k][i]
}

// accelerator is a chord as the menu bar takes it: "CmdOrCtrl+Shift+Right".
func (k chord) accelerator() string {
	var parts []string
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{{primary, "CmdOrCtrl"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}} {
		if k.mods&m.mod != 0 {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, keyName(k.key, false)), "+")
}

// caps are the keys of a chord as the page of shortcuts shows them, one
// key cap each: ⌘ ⇧ → on macOS, Ctrl Shift → elsewhere.
func (k chord) caps() []string {
	mac := runtime.GOOS == "darwin"
	var parts []string
	for _, m := range []struct {
		mod       ui.Modifiers
		mac, name string
	}{{ui.Ctrl, "⌃", "Ctrl"}, {ui.Alt, "⌥", "Alt"}, {ui.Shift, "⇧", "Shift"}, {ui.Super, "⌘", "Win"}} {
		if k.mods&m.mod != 0 {
			if mac {
				parts = append(parts, m.mac)
			} else {
				parts = append(parts, m.name)
			}
		}
	}
	if k == (chord{ui.Shift, ui.KeySlash}) {
		return []string{"?"}
	}
	return append(parts, keyName(k.key, true))
}

// cursor is the marker the keys move over the items of a page: the
// albums of a grid, the songs of a list. It is at a row of the page's
// list and a place in the row.
type cursor struct {
	shown    bool
	row, col int
	page     string // the page it is on
	reveal   bool   // the item is to be scrolled to
	// What the frame being built, and the one before it, showed.
	items, last []cursorItem
	// Where the item being built is.
	list  *ui.ListState
	at    int
	place int
	// A step waiting for rows that were not built yet.
	pending [2]int
	tries   int
	wake    bool
}

type cursorItem struct {
	row, col int
	list     *ui.ListState
	open     func()
}

// begin starts a frame at a page.
func (cu *cursor) begin(page string) {
	if page != cu.page {
		*cu = cursor{page: page, items: cu.items[:0], last: cu.last[:0]}
	}
	cu.last, cu.items = cu.items, cu.last[:0]
	cu.list = nil
	if cu.pending != [2]int{} {
		dx, dy := cu.pending[0], cu.pending[1]
		cu.pending = [2]int{}
		cu.move(dx, dy)
	}
}

// in tells that what is built next is row i of a page's list.
func (cu *cursor) in(list *ui.ListState, i int) { cu.list, cu.at, cu.place = list, i, 0 }

// item adds an item of the page, and reports whether the marker is on
// it.
func (a *App) cursorItem(el ui.Element, open func()) bool {
	cu := &a.cursor
	if cu.list == nil {
		return false // not of the page: the queue's, say
	}
	it := cursorItem{row: cu.at, col: cu.place, list: cu.list, open: open}
	cu.place++
	cu.items = append(cu.items, it)
	on := cu.shown && it.row == cu.row && it.col == cu.col
	if on {
		el.Shadow(0, 0, 0, 2, a.pal.accent)
		if cu.reveal {
			el.ScrollIntoView()
			cu.reveal = false
		}
	}
	return on
}

// step moves the marker by a place or by a row, showing it first where
// the page is.
func (cu *cursor) step(dx, dy int) {
	cu.tries = 0
	cu.move(dx, dy)
}

func (cu *cursor) move(dx, dy int) {
	items := cu.last
	if len(items) == 0 {
		return
	}
	cur := -1
	for i, it := range items {
		if it.row == cu.row && it.col == cu.col {
			cur = i
			break
		}
	}
	if !cu.shown || cur < 0 {
		// At the first item in view.
		first := 0
		if lo, hi := items[0].list.Visible(); hi >= lo {
			for i, it := range items {
				if it.row >= lo {
					first = i
					break
				}
			}
		}
		cu.set(items[first])
		return
	}
	if dx != 0 {
		if next := cur + dx; next >= 0 && next < len(items) {
			cu.set(items[next])
		}
		return
	}
	// The row above or below that has items, at the same place or its
	// last.
	target := -1
	if dy > 0 {
		for i := cur + 1; i < len(items); i++ {
			if items[i].row == cu.row {
				continue
			}
			if target >= 0 && items[i].row != items[target].row {
				break
			}
			target = i
			if items[i].col == cu.col {
				break
			}
		}
	} else {
		for i := cur - 1; i >= 0; i-- {
			if items[i].row == cu.row {
				continue
			}
			if target >= 0 && items[i].row != items[target].row {
				break
			}
			if target < 0 || items[i].col >= cu.col {
				target = i
			}
			if items[i].col <= cu.col {
				break
			}
		}
	}
	if target >= 0 {
		cu.set(items[target])
		return
	}
	// None built: the list shows only the rows in view. It is scrolled a
	// row on, and the step taken once that row is there.
	if cu.tries < 8 {
		edge := items[len(items)-1].row + 1
		if dy < 0 {
			edge = items[0].row - 1
		}
		if edge >= 0 {
			items[cur].list.ScrollIntoView(edge)
			cu.pending, cu.wake = [2]int{dx, dy}, true
			cu.tries++
		}
	}
}

func (cu *cursor) set(it cursorItem) {
	cu.shown, cu.row, cu.col, cu.reveal, cu.wake = true, it.row, it.col, true, true
}

func (cu *cursor) open() {
	for _, it := range cu.last {
		if cu.shown && it.row == cu.row && it.col == cu.col && it.open != nil {
			it.open()
			return
		}
	}
}

func (cu *cursor) hide() { cu.shown = false }

// shortcutsPage lists the keys.
func (a *App) shortcutsPage(c *ui.Context) {
	p := a.pal
	ui.Scroll(c.Key("shortcuts")).Grow(1).MinHeight(0).Padding(0, 0, 40).Children(func() {
		a.pageTitle(c, "Keyboard shortcuts", "", nil)
		group := ""
		var rows []*command
		flush := func() {
			if len(rows) == 0 {
				return
			}
			title, list := group, rows
			ui.Column(c).Padding(10, pagePad, 6).Gap(8).MaxWidth(760).Children(func() {
				a.sectionTitle(c, title)
				a.card(c, func() {
					for i, cmd := range list {
						ui.Row(c.Key(i)).Padding(9, 16).Gap(12).AlignItems(ui.Center).Children(func() {
							ui.Text(c, cmd.label).Grow(1).MinWidth(0)
							for j, k := range cmd.keys {
								if j > 0 {
									ui.Text(c, "or").FontSize(12).TextColor(p.faint)
								}
								ui.Row(c).Gap(4).Shrink(0).Children(func() {
									for _, part := range k.caps() {
										ui.Box(c).Height(24).MinWidth(24).Padding(0, 7).Center().Radius(6).Background(p.hover).Border(1, p.border).Children(func() {
											ui.Text(c, part).FontSize(12).FontWeight(600).TextColor(p.muted).SingleLine()
										})
									}
								})
							}
						})
					}
				})
			})
			rows = nil
		}
		for i := range commands {
			if commands[i].group != group {
				flush()
				group = commands[i].group
			}
			rows = append(rows, &commands[i])
		}
		flush()
	})
}

// toggleMute mutes what plays, here or on the device that plays, or
// lets it sound again.
func (a *App) toggleMute() {
	if f := a.player.far; f != nil {
		f.muted = !f.muted
		a.send("mute", func(ctx context.Context, c *jellyfin.Client) error {
			return c.Command(ctx, f.dev.ID, "ToggleMute", nil)
		})
		return
	}
	a.settings.Muted = !a.settings.Muted
	a.player.applyVolume()
	a.saveSettings()
}

package main

import (
	"bytes"
	"image"
	"math"
	"os"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
)

// stage is the record: the cover of the song as a record that turns
// while it plays, over its own colors, with the lyrics under it, the
// line being sung coming up as it is reached. It shows in the app's
// window in place of the app, over the whole screen, or in a small
// window of its own that can stay above the others.
//
// It moves a lot and must cost little, so all of it that moves is one
// drawing (stageDraw), which paints again by itself without the view
// being built: the record a little further round, the lines a little
// higher. The view is built only when something happens.
type stage struct {
	on bool
	// full is set when the stage put the window in full screen, and so
	// takes it out again; mini, when it shows in a small window of its
	// own.
	full bool
	mini bool

	// The picture of the song showing, turned, and its backdrop.
	cover    string // the picture loaded, or being loaded
	turner   *turner
	backdrop *ui.Bitmap
	angle    float64   // of the record, in radians
	speed    float64   // in radians a second
	painted  time.Time // when the stage was last painted
	turned   time.Time // when the record was last turned

	// The lyrics of the song showing, and the line the drawing is at:
	// place moves to line, from where it was when the line changed.
	song         string
	lines        []jellyfin.LyricLine
	line         int
	from, place  float64
	lineAt       time.Time
	lineStarted  bool
	pointerAt    time.Time // when the pointer last moved: the buttons show for a while after
	buttonsShown bool
}

const (
	stageTurn = 2 * math.Pi / 20 // the record's speed: a turn in 20 seconds
	// stageFrame is the time between two paintings of the stage while
	// it moves: under the 16.7 ms of a frame at 60 a second, so that the
	// next painting makes the next frame of such a display, and every
	// other frame of one of 120, which is as smooth to the eye at this
	// speed and half the work.
	stageFrame  = 12 * time.Millisecond
	stageSlide  = 420 * time.Millisecond // a line of lyrics coming up
	stageIdle   = 2500 * time.Millisecond
	stageCoverP = 600 // the most pixels across the record is turned at
)

// openStage shows the record and the lyrics in place of the app, in its
// window.
func (a *App) openStage() {
	if a.player.current() == nil {
		a.toast("Play a song to see its record")
		return
	}
	if a.stage.on && !a.stage.mini {
		return
	}
	a.closeMini()
	a.stage = stage{on: true, pointerAt: time.Now(), line: -1}
}

// closeStage comes back to the app, out of full screen if the stage
// went into it.
func (a *App) closeStage() {
	st := &a.stage
	if !st.on {
		return
	}
	if st.mini {
		a.closeMini()
		return
	}
	if st.full && a.win != nil && a.win.IsFullScreen() {
		a.win.SetFullScreen(false)
	}
	*st = stage{}
}

// stageFull puts the stage in full screen, opening it if need be, or
// takes it out.
func (a *App) stageFull(on bool) {
	st := &a.stage
	if on {
		a.openStage()
	}
	if !st.on || st.mini || a.win == nil {
		return
	}
	switch {
	case on && !a.win.IsFullScreen():
		st.full = true
		a.win.SetFullScreen(true)
	case !on && st.full:
		st.full = false
		if a.win.IsFullScreen() {
			a.win.SetFullScreen(false)
		}
	}
}

// inFull reports whether the stage fills the screen.
func (a *App) inFull() bool {
	return a.stage.on && !a.stage.mini && a.win != nil && a.win.IsFullScreen()
}

// popOut moves the stage to a small window of its own, which can stay
// above the others; the app has its window back.
func (a *App) popOut() {
	if a.player.current() == nil {
		a.toast("Play a song to see its record")
		return
	}
	if a.mini != nil {
		a.mini.Show()
		a.mini.Focus()
		return
	}
	a.closeStage()
	a.stage = stage{on: true, mini: true, pointerAt: time.Now(), line: -1}
	opts := mygo.WindowOptions{
		Title:           "Aurelia",
		Width:           320,
		Height:          400,
		MinWidth:        miniMinW,
		MinHeight:       miniMinH,
		StateKey:        "mini",
		AlwaysOnTop:     a.settings.MiniPinned,
		BackgroundColor: "#08080a",
		Content:         ui.View(a.miniView),
	}
	switch {
	case runtime.GOOS == "darwin":
		opts.TitleBarStyle = mygo.TitleBarHidden
	case ownControls:
		opts.Frameless = true
	}
	win := mygo.NewWindow(opts)
	a.mini = win
	win.OnClosed(func() {
		a.mini = nil
		a.update(func() {
			if a.stage.mini {
				a.stage = stage{}
			}
		})
	})
}

// The least size of the small window: the record, the song, a line.
const (
	miniMinW = 220
	miniMinH = 260
)

// closeMini closes the small window.
func (a *App) closeMini() {
	if a.stage.mini {
		a.stage = stage{}
	}
	if w := a.mini; w != nil {
		a.mini = nil
		w.Close()
	}
}

// pinMini keeps the small window above the others, or lets it go under.
func (a *App) pinMini(on bool) {
	a.settings.MiniPinned = on
	a.saveSettings()
	if a.mini != nil {
		a.mini.SetAlwaysOnTop(on)
	}
}

// miniView is the view of the small window.
func (a *App) miniView(c *ui.Context) {
	if a.pal != nil {
		c.SetTheme(a.pal.widgets)
	}
	if !a.stage.on || !a.stage.mini {
		return
	}
	a.stageView(c)
}

// stagePage builds the stage in the app's window.
func (a *App) stagePage(c *ui.Context) { a.stageView(c) }

// stageView builds the stage: a drawing, and the buttons over it while
// the pointer moves.
func (a *App) stageView(c *ui.Context) {
	st := &a.stage
	song := a.player.current()
	root := ui.Box(c).Fill()
	if st.mini {
		root.DragWindow() // it has no title bar: all of it moves it
	}
	escape := c.Shortcut(0, ui.KeyEscape) || root.Shortcut(0, ui.KeyEscape)
	a.shortcuts(c, root)
	switch {
	case song == nil:
		a.closeStage()
		c.After(time.Millisecond)
		return
	case escape:
		// Out of full screen first, then out of the stage.
		if a.inFull() && st.full {
			a.stageFull(false)
		} else {
			a.closeStage()
		}
		c.After(time.Millisecond)
		return
	}
	if !st.mini {
		a.tellSystem()
	}
	if st.song != song.ID {
		st.song, st.lines, st.line, st.lineStarted = song.ID, nil, -1, false
	}
	if lines, known := a.lyricLines(song); known {
		st.lines = lines
	}
	a.stageCover(c, song)

	// The buttons show while the pointer moves, and while nothing plays.
	shown := time.Since(st.pointerAt) < stageIdle || !a.player.playing()
	st.buttonsShown = shown
	if shown && a.player.playing() {
		c.After(stageIdle - time.Since(st.pointerAt) + 20*time.Millisecond)
	}
	win := a.win
	if st.mini {
		win = a.mini
	}
	root.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputPointerMove {
			st.pointerAt = time.Now()
			if !st.buttonsShown && win != nil {
				win.Invalidate()
			}
		}
		return false
	})
	root.Draw(func(g *ui.Painter, r ui.Rect) { a.stageDraw(g, r) })
	root.Children(func() {
		if shown {
			a.stageButtons(c, song)
		}
	})
}

// stageButtons are the few buttons of the stage, light on its dark
// whatever the theme: where it shows at the top, the player's at the
// bottom.
func (a *App) stageButtons(c *ui.Context, song *library.Song) {
	st := &a.stage
	white := ui.RGB(255, 255, 255)
	w, _ := c.Size()
	small := w < 420
	button := func(name, label string, size, glyph float32, filled, lit bool) ui.Element {
		b := ui.ButtonBase(c.Key(label)).Size(size, size).Radius(size / 2).Shrink(0).Label(label).Tooltip(label).Cursor(ui.CursorPointer)
		fg := white.Alpha(0.86)
		switch {
		case filled:
			b.Background(white)
			fg = ui.RGB(12, 12, 14)
			if b.Hovered() {
				b.Background(white.Alpha(0.88))
			}
		case b.Hovered():
			b.Background(white.Alpha(0.16))
			fg = white
		case lit:
			b.Background(white.Alpha(0.12))
			fg = white
		}
		if b.Hovered() {
			st.pointerAt = time.Now() // not to vanish under the pointer
		}
		b.Transition(fade)
		b.Children(func() { ui.Icon(c, icon(name)).Size(glyph, glyph).TextColor(fg) })
		return b
	}
	top := float32(14)
	if bar := c.TitleBar(); bar.Height > 0 && !st.mini {
		top = max(top, (bar.Height-34)/2)
	}
	ui.Row(c).Attach(ui.AnchorTopRight, ui.AnchorTopRight).Top(top).Right(14).Gap(4).Padding(3).Radius(20).
		Background(ui.RGBA(20, 20, 24, 0.6)).Children(func() {
		switch {
		case st.mini:
			pinned := a.mini != nil && a.mini.IsAlwaysOnTop()
			name, label := "pin", "Keep above other windows"
			if pinned {
				name, label = "pin-off", "Let other windows cover it"
			}
			if button(name, label, 30, 15, false, pinned).Clicked() {
				a.pinMini(!pinned)
			}
			if button("maximize", "Back into Aurelia's window", 30, 15, false, false).Clicked() {
				a.closeMini()
				a.openStage()
				a.show()
			}
			if button("x", "Close", 30, 15, false, false).Clicked() {
				a.closeMini()
			}
		default:
			if button("picture-in-picture-2", "In a small window of its own", 34, 16, false, false).Clicked() {
				a.popOut()
			}
			if a.inFull() {
				if button("minimize", "Leave full screen", 34, 16, false, false).Clicked() {
					a.stageFull(false)
				}
			} else if button("maximize", "Full screen", 34, 16, false, false).Clicked() {
				a.stageFull(true)
			}
			if button("x", "Back to Aurelia", 34, 16, false, false).Clicked() {
				a.closeStage()
			}
		}
	})
	side, mid, gap := float32(38), float32(48), float32(14)
	if small {
		side, mid, gap = 32, 40, 6
	}
	ui.Row(c).Attach(ui.AnchorBottom, ui.AnchorBottom).Bottom(max(14, min(30, w*0.05))).Gap(gap).Padding(6, 14).Radius(34).
		Background(ui.RGBA(20, 20, 24, 0.72)).Border(1, white.Alpha(0.08)).Children(func() {
		pl := a.player
		if button("skip-back-fill", "Previous", side, side*0.46, false, false).Clicked() {
			pl.previous()
		}
		glyph, label := "play-fill", "Play"
		if pl.playing() {
			glyph, label = "pause-fill", "Pause"
		}
		if button(glyph, label, mid, mid*0.42, true, false).Clicked() {
			pl.toggle()
		}
		if button("skip-forward-fill", "Next", side, side*0.46, false, false).Clicked() {
			pl.skip()
		}
		heart, name := "heart", "Add to Favorites"
		if song.Favorite {
			heart, name = "heart-fill", "Remove from Favorites"
		}
		if button(heart, name, side, side*0.45, false, false).Clicked() {
			a.setFavorite(song.ID, &song.Favorite, !song.Favorite)
		}
	})
}

// stageCover has the picture of the song loaded, to turn: large, from
// the pictures kept on disk, which fetch it when it is not there.
func (a *App) stageCover(c *ui.Context, song *library.Song) {
	st := &a.stage
	if song.ImageTag == "" || song.ImageItem == "" {
		st.cover, st.turner, st.backdrop = "", nil, nil
		return
	}
	const px = 768
	key := imageKey(song.ImageItem, song.ImageTag, px)
	if st.cover == key {
		return
	}
	cl := a.clientNow()
	if cl == nil {
		return
	}
	path := a.images.file(key, cl.ImageURL(song.ImageItem, "Primary", song.ImageTag, px))
	if path == "" {
		c.After(250 * time.Millisecond) // it is being fetched
		return
	}
	st.cover = key
	// As large as the record shows, and no larger than turns cheaply.
	// A small window may grow: not so small that it shows then.
	w, h := c.Size()
	size := int(min(max(float64(stageDisc(w, h, true)), 300)*a.scale, stageCoverP))
	size = max(size, 240) &^ 1
	go func() {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		pic, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		t, bg := newTurner(pic, size), backdrop(pic)
		a.update(func() {
			if st.on && st.cover == key {
				st.turner, st.backdrop = t, bg
			}
		})
	}()
}

// stageDisc is how wide the record is in a window, in points: larger
// when there are no lyrics to leave room for, and as wide as a narrow
// window lets it be.
func stageDisc(w, h float32, lyrics bool) float32 {
	across, down := float32(0.42), float32(0.50)
	if !lyrics {
		across, down = 0.5, 0.62
	}
	if w < 1.25*h {
		across = 0.74 // a window taller than wide: the record fills its width
	}
	return max(96, min(w*across, h*down))
}

// stageDraw paints the full screen, and asks to paint it again while it
// moves.
func (a *App) stageDraw(g *ui.Painter, r ui.Rect) {
	st := &a.stage
	song := a.player.current()
	if song == nil {
		return
	}
	now := g.Now()
	white := ui.RGB(255, 255, 255)
	moving := false

	// The picture's colors behind everything, dimmed: one drawing over
	// the whole window, which is most of what a frame costs.
	if st.backdrop != nil {
		grow := max(r.W, r.H) * 0.08
		g.Image(st.backdrop, ui.Rect{X: r.X - grow, Y: r.Y - grow, W: r.W + 2*grow, H: r.H + 2*grow}, ui.Cover)
	} else {
		g.Fill(r, ui.RGB(8, 8, 10), 0)
	}

	lyrics := len(st.lines) > 0
	d := stageDisc(r.W, r.H, lyrics)
	cx := r.X + r.W/2
	top := r.Y + max(r.H*0.075, 30)
	if !lyrics {
		top = r.Y + max((r.H-d-96)/2, 30)
	}
	disc := ui.Rect{X: cx - d/2, Y: top, W: d, H: d}

	// The record turns while the song plays, and takes a moment to come
	// up to speed and to stop, as one does.
	playing := a.player.playing()
	dt := now.Sub(st.painted).Seconds()
	if st.painted.IsZero() || dt > 0.25 {
		dt = 0
	}
	target := 0.0
	if playing {
		target = stageTurn
	}
	if dt > 0 {
		st.speed += (target - st.speed) * (1 - math.Exp(-dt/0.6))
		st.angle = math.Mod(st.angle+st.speed*dt, 2*math.Pi)
	}
	spinning := playing || st.speed > 0.004
	if !spinning {
		st.speed = 0
	}
	if t := st.turner; t != nil && t.ok && (spinning || st.turned.IsZero()) && now.Sub(st.turned) >= stageFrame-2*time.Millisecond {
		t.turn(st.angle)
		st.turned = now
	}
	st.painted = now
	turning := spinning && st.turner != nil && st.turner.ok
	a.drawRecord(g, disc)

	// The song, under the record.
	y := top + d + r.H*0.035
	title := ui.Span{Text: song.Name, Size: max(15, min(30, r.H*0.034)), Weight: 700, Color: white}
	margin := max(16, min(40, r.W*0.06))
	if w, h := g.MeasureText(0, title); w <= r.W-2*margin {
		g.RichText(cx-w/2, y, 0, title)
		y += h + 4
	} else {
		_, h := g.RichText(r.X+margin, y, r.W-2*margin, title)
		y += h + 4
	}
	artist := ui.Span{Text: song.Artist, Size: max(12, min(18, r.H*0.022)), Weight: 500, Color: white.Alpha(0.62)}
	if w, h := g.MeasureText(0, artist); song.Artist != "" {
		g.RichText(cx-min(w, r.W-2*margin)/2, y, 0, artist)
		y += h
	}

	// The lyrics have what room is left, clear of the buttons.
	below := max(20, min(96, r.H*0.11))
	if lyrics && a.drawLyrics(g, ui.Rect{X: r.X, Y: y + r.H*0.03, W: r.W, H: r.Y + r.H - below - (y + r.H*0.03)}, now) {
		moving = true
	}

	// How far the song is, along the bottom.
	state := a.player.state()
	if state.Duration > 0 {
		frac := float32(max(0, min(1, float64(state.Position)/float64(state.Duration))))
		bar := ui.Rect{X: r.X, Y: r.Y + r.H - 3, W: r.W, H: 3}
		g.Fill(bar, white.Alpha(0.10), 0)
		g.Fill(ui.Rect{X: bar.X, Y: bar.Y, W: bar.W * frac, H: bar.H}, white.Alpha(0.75), 0)
	}
	switch {
	case moving || turning:
		g.After(stageFrame)
	case playing:
		g.After(500 * time.Millisecond) // the bar of the song, and the next line
	}
}

// drawRecord paints the record: its shadow, the picture, which has the
// grooves and the label of one pressed into it (press), and the light
// on it, which stays where it is while the picture turns.
func (a *App) drawRecord(g *ui.Painter, disc ui.Rect) {
	st := &a.stage
	d := disc.W
	white := ui.RGB(255, 255, 255)
	g.Shadow(disc, d/2, 0, d*0.03, d*0.05, 0, ui.RGBA(0, 0, 0, 0.55))
	if t := st.turner; t != nil {
		g.Image(t.bmp, disc, ui.FillBox)
	} else {
		// No picture: a record all the same.
		g.Fill(disc, ui.RGB(18, 18, 21), d/2)
		hub := ui.Rect{X: disc.X + d*0.4, Y: disc.Y + d*0.4, W: d * 0.2, H: d * 0.2}
		g.Fill(hub, ui.RGB(34, 34, 38), hub.W/2)
		in := hub.W * 0.24
		g.Icon(icon("music"), ui.Rect{X: hub.X + in, Y: hub.Y + in, W: hub.W - 2*in, H: hub.H - 2*in}, white.Alpha(0.55))
	}
	g.FillGradient(disc, ui.LinearGradient{From: white.Alpha(0.15), To: white.Alpha(0), Angle: 135, Start: 0.05, End: 0.55}, d/2)
	g.FillGradient(disc, ui.LinearGradient{From: ui.RGBA(0, 0, 0, 0), To: ui.RGBA(0, 0, 0, 0.2), Angle: 135, Start: 0.45, End: 1}, d/2)
	g.Stroke(disc, ui.RGBA(0, 0, 0, 0.5), d/2, 1.5) // a smooth edge to the picture's
}

// drawLyrics paints the lyrics in box: the line being sung in the
// middle, bright, the lines around it dimmer the further they are, all
// of them sliding up as the song goes from one line to the next. It
// reports whether they are on their way.
func (a *App) drawLyrics(g *ui.Painter, box ui.Rect, now time.Time) (moving bool) {
	st := &a.stage
	lines := st.lines
	if box.H < 28 || len(lines) == 0 {
		return false
	}
	state := a.player.state()
	white := ui.RGB(255, 255, 255)
	size := max(15, min(38, box.H*0.16, box.W*0.05))
	width := min(box.W-2*max(16, min(48, box.W*0.06)), 1100)

	// Where the song is among the lines: the last that began, when they
	// are timed; else as far through them as the song is through itself.
	if timed := lines[0].Start >= 0; timed {
		line := -1
		for i, l := range lines {
			if l.Start <= state.Position+180*time.Millisecond {
				line = i
			}
		}
		if line != st.line || !st.lineStarted {
			if !st.lineStarted || math.Abs(float64(line)-st.place) > 4 {
				// The first line, or a jump in the song: from just beside.
				st.place = float64(line) - math.Copysign(1.2, float64(line)-st.place)
			}
			st.line, st.from, st.lineAt, st.lineStarted = line, st.place, now, true
		}
		t := float64(now.Sub(st.lineAt)) / float64(stageSlide)
		if t >= 1 {
			st.place = float64(st.line)
		} else {
			st.place = st.from + (float64(st.line)-st.from)*float64(ui.EaseOut(float32(max(t, 0))))
			moving = true
		}
	} else {
		st.line = -1
		if state.Duration > 0 {
			st.place = float64(len(lines)-1) * float64(state.Position) / float64(state.Duration)
		}
		moving = !state.Paused
	}

	span := func(i int, alpha float32) ui.Span {
		text := lines[i].Text
		if text == "" {
			text = "♪"
		}
		return ui.Span{Text: text, Size: size, Weight: 700, Color: white.Alpha(alpha)}
	}
	// The lines near the place, each as high as its text wrapped.
	base := int(math.Floor(st.place))
	const around = 4
	lo, hi := max(0, base-around), min(len(lines)-1, base+around+1)
	if lo > hi {
		return moving
	}
	gap := size * 0.55
	heights := make([]float32, hi-lo+1)
	widths := make([]float32, hi-lo+1)
	for i := lo; i <= hi; i++ {
		widths[i-lo], heights[i-lo] = g.MeasureText(width, span(i, 1))
	}
	// centers[i] is the middle of line lo+i, measured from that of lo.
	centers := make([]float32, hi-lo+1)
	for i := 1; i < len(centers); i++ {
		centers[i] = centers[i-1] + heights[i-1]/2 + gap + heights[i]/2
	}
	at := func(place float64) float32 {
		p := max(float64(lo), min(float64(hi), place)) - float64(lo)
		i := min(int(p), len(centers)-1)
		if i+1 >= len(centers) {
			return centers[i]
		}
		return centers[i] + (centers[i+1]-centers[i])*float32(p-float64(i))
	}
	middle := box.Y + box.H*0.42
	origin := middle - at(st.place)
	g.Clip(box, 0, func() {
		for i := lo; i <= hi; i++ {
			far := float32(math.Abs(float64(i) - st.place))
			alpha := float32(0)
			switch {
			case st.line < 0:
				alpha = max(0, 0.9-0.28*far) // not timed: all much alike
			case far < 1:
				alpha = 1 - 0.66*far
			default:
				alpha = max(0, 0.34-0.12*(far-1))
			}
			y := origin + centers[i-lo] - heights[i-lo]/2
			// A line at the edge of the room fades out before it is cut.
			edge := min(y-box.Y, box.Y+box.H-(y+heights[i-lo]))
			alpha *= max(0, min(1, edge/(size*0.9)))
			if alpha <= 0.02 {
				continue
			}
			g.RichText(box.X+(box.W-widths[i-lo])/2, y, width, span(i, alpha))
		}
	})
	return moving
}

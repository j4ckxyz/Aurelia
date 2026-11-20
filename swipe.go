package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// Two fingers swiped across a trackpad go back and forward, as in the
// system's browsers: to the right goes back, to the left forward. MyGo
// hands the app a touchpad's swipe as the horizontal scroll it is, on
// macOS, Windows (precision touchpads) and Linux (libinput) alike; a
// swipe is told from a scroll by going sideways from its start, and an
// arrow at the side of the page follows the fingers until they have gone
// far enough. A mouse's wheel never navigates; its side buttons do, as
// MyGo's router has them.

const (
	// swipeQuiet is the pause that ends a gesture: the next scroll starts
	// another.
	swipeQuiet = 220 * time.Millisecond
	// swipeDecide is how far the fingers go before the gesture is taken
	// to be a swipe, or a scroll.
	swipeDecide = 12
	// swipeFar is how far they go to navigate, in points.
	swipeFar = 150
)

// swipe follows a gesture of two fingers on a trackpad.
type swipe struct {
	at     time.Time // the last scroll of the gesture
	sx, sy float32   // where it has gone while undecided
	axis   int8      // 0 undecided, 1 sideways, -1 up or down
	travel float32   // how far sideways, once a swipe: below 0 back
	done   bool      // it navigated; the rest of it, and its momentum, does nothing
	can    func(dir int) bool
}

// scroll follows a scroll of dx, dy points, and returns -1 to go back, 1 to
// go forward, or 0; taken reports that the scroll was the swipe's, which
// scrolls nothing else.
func (s *swipe) scroll(now time.Time, dx, dy float32, precise bool) (dir int, taken bool) {
	if dx == 0 && dy == 0 {
		// GTK's end of a scroll: the fingers left the touchpad.
		s.reset()
		return 0, false
	}
	if !precise {
		return 0, false // a mouse's wheel scrolls
	}
	if now.Sub(s.at) > swipeQuiet {
		s.reset()
	}
	s.at = now
	if s.done {
		// The momentum after the swipe, or the fingers going on.
		return 0, s.axis > 0
	}
	switch s.axis {
	case 0:
		s.sx += dx
		s.sy += dy
		if abs32(s.sx)+abs32(s.sy) < swipeDecide {
			return 0, false
		}
		// Sideways, clearly: a scroll down a page wanders a little.
		if abs32(s.sx) > 2*abs32(s.sy) {
			s.axis, s.travel = 1, s.sx
		} else {
			s.axis = -1
			return 0, false
		}
	case -1:
		return 0, false
	default:
		s.travel += dx
	}
	d := s.direction()
	if d == 0 {
		return 0, true
	}
	if s.can != nil && !s.can(d) {
		s.travel = 0 // nothing that way: the arrow does not come
		return 0, true
	}
	if abs32(s.travel) >= swipeFar {
		s.done = true
		return d, true
	}
	return 0, true
}

// direction is where the swipe leads: -1 back, 1 forward.
func (s *swipe) direction() int {
	switch {
	case s.travel < 0:
		return -1
	case s.travel > 0:
		return 1
	}
	return 0
}

// progress is how far the swipe has gone to navigating, from -1 (back) to
// 1 (forward), while one goes on.
func (s *swipe) progress(now time.Time) float32 {
	if s.axis <= 0 || s.done || now.Sub(s.at) > swipeQuiet {
		return 0
	}
	if s.can != nil && !s.can(s.direction()) {
		return 0
	}
	return max(-1, min(1, s.travel/swipeFar))
}

func (s *swipe) reset() {
	can := s.can
	*s = swipe{can: can}
}

// swipeInput is the scroll of a page, which a swipe takes to go back or
// forward.
func (a *App) swipeInput(to func(dir int)) func(ev ui.InputEvent) bool {
	return func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll || ev.Mods != 0 {
			return false
		}
		if !swipeNavigates() {
			return false
		}
		dir, taken := a.swiping.scroll(time.Now(), ev.DX, ev.DY, ev.Precise)
		if dir != 0 {
			to(dir)
		}
		return taken
	}
}

// navigate goes back or forward in the pages.
func (a *App) navigate(dir int) {
	if dir < 0 {
		a.router.Back()
	} else {
		a.router.Forward()
	}
}

// canNavigate reports whether there is a page that way.
func (a *App) canNavigate(dir int) bool {
	if dir < 0 {
		return a.router.CanGoBack()
	}
	return a.router.CanGoForward()
}

// swipeArrow shows how far a swipe has gone, at the side of the page it
// comes from: an arrow that comes in with the fingers, and is filled when
// letting them go on would navigate.
func (a *App) swipeArrow(c *ui.Context) {
	now := time.Now()
	pr := a.swiping.progress(now)
	if pr == 0 {
		return
	}
	c.After(swipeQuiet + 10*time.Millisecond) // to go once the fingers stop
	p := a.pal
	const size = 44
	k := float32(math.Abs(float64(pr)))
	ease := 1 - (1-k)*(1-k)
	glyph, anchor := "chevron-left", ui.AnchorLeft
	if pr > 0 {
		glyph, anchor = "chevron-right", ui.AnchorRight
	}
	off := -size + ease*(size+18)
	b := ui.Box(c.Key("swipe-arrow")).Attach(anchor, anchor).Size(size, size).Radius(size/2).Center().
		Background(p.surface).Border(1, p.border).Shadow(0, 6, 20, 0, p.shadow).Opacity(0.4 + 0.6*k)
	if anchor == ui.AnchorLeft {
		b.Left(off)
	} else {
		b.Right(off)
	}
	fg := p.muted
	if k > 0.85 {
		b.Background(p.accent).BorderColor(p.accent)
		fg = p.onAcc
	}
	b.Children(func() { ui.Icon(c, icon(glyph)).Size(20, 20).TextColor(fg) })
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

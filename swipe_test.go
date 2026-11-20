package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// gesture sends a gesture's scrolls, 8 ms apart, and returns where it
// led: -1 back, 1 forward, 0 nowhere.
func gesture(s *swipe, start time.Time, steps [][2]float32, precise bool) (int, time.Time) {
	at := start
	led := 0
	for _, st := range steps {
		if d, _ := s.scroll(at, st[0], st[1], precise); d != 0 {
			if led != 0 {
				panic("a gesture navigated twice")
			}
			led = d
		}
		at = at.Add(8 * time.Millisecond)
	}
	return led, at
}

func repeat(n int, dx, dy float32) [][2]float32 {
	out := make([][2]float32, n)
	for i := range out {
		out[i] = [2]float32{dx, dy}
	}
	return out
}

func TestSwipe(t *testing.T) {
	t0 := time.Unix(1000, 0)
	cases := []struct {
		name    string
		steps   [][2]float32
		precise bool
		want    int
	}{
		{"two fingers to the right go back", repeat(30, -8, 0.5), true, -1},
		{"to the left go forward", repeat(30, 8, -0.5), true, 1},
		{"a short swipe goes nowhere", repeat(10, -8, 0), true, 0},
		{"a scroll down the page that wanders goes nowhere", append(repeat(5, -1, 12), repeat(40, -9, 2)...), true, 0},
		{"a mouse's wheel tilted goes nowhere", repeat(30, -33, 0), false, 0},
		{"diagonal goes nowhere", repeat(30, -8, -6), true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var s swipe
			if got, _ := gesture(&s, t0, c.steps, c.precise); got != c.want {
				t.Errorf("led %d, want %d", got, c.want)
			}
		})
	}
}

// The momentum after a swipe, and a swipe going on, navigate once; a
// pause starts another gesture.
func TestSwipeOnce(t *testing.T) {
	t0 := time.Unix(1000, 0)
	var s swipe
	got, at := gesture(&s, t0, repeat(60, -8, 0), true) // panics on twice
	if got != -1 {
		t.Fatalf("led %d", got)
	}
	// The momentum, slowing.
	for v := float32(-8); v < 0; v += 0.25 {
		if d, taken := s.scroll(at, v, 0, true); d != 0 || !taken {
			t.Fatalf("the momentum led %d, taken %v", d, taken)
		}
		at = at.Add(16 * time.Millisecond)
	}
	if got, _ := gesture(&s, at.Add(time.Second), repeat(30, -8, 0), true); got != -1 {
		t.Errorf("a second swipe led %d", got)
	}
	// GTK says when the fingers leave: the next swipe goes at once.
	var g swipe
	gesture(&g, t0, repeat(30, -8, 0), true)
	g.scroll(t0.Add(250*time.Millisecond), 0, 0, false)
	if got, _ := gesture(&g, t0.Add(251*time.Millisecond), repeat(30, 8, 0), true); got != 1 {
		t.Errorf("after the fingers left, a swipe led %d", got)
	}
}

// Where there is no page to go to, a swipe shows nothing and goes
// nowhere; a scroll down a page is left to the page.
func TestSwipeCannot(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := swipe{can: func(dir int) bool { return dir > 0 }}
	if got, _ := gesture(&s, t0, repeat(30, -8, 0), true); got != 0 {
		t.Errorf("with nothing behind, led %d", got)
	}
	if p := s.progress(t0.Add(30 * 8 * time.Millisecond)); p != 0 {
		t.Errorf("with nothing behind, the arrow is at %v", p)
	}
	var v swipe
	for i := range 20 {
		if _, taken := v.scroll(t0.Add(time.Duration(i)*8*time.Millisecond), 0.5, 10, true); taken {
			t.Fatal("a scroll down the page was taken from it")
		}
	}
}

func TestSwipeProgress(t *testing.T) {
	t0 := time.Unix(1000, 0)
	var s swipe
	_, at := gesture(&s, t0, repeat(8, -8, 0), true)
	p := s.progress(at)
	if p >= 0 || p <= -1 {
		t.Errorf("half way back, the arrow is at %v", p)
	}
	if p := s.progress(at.Add(time.Second)); p != 0 {
		t.Errorf("after the fingers stopped, the arrow is at %v", p)
	}
}

// A swipe on a page goes back to the page before, and forward again; on
// the record it gives the app back.
func TestSwipeNavigates(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 780)
	tt.Frame()
	a.goTo("/albums")
	tt.Frame()
	a.goTo("/album/al1")
	tt.Frame()
	a.swiping.can = a.canNavigate
	in := a.swipeInput(a.navigate)
	send := func(dx float32) {
		for range 40 {
			in(ui.InputEvent{Kind: ui.InputScroll, DX: dx, Precise: true})
		}
		a.swiping.reset()
	}
	send(-8)
	tt.Frame()
	if a.router.Path() != "/albums" {
		t.Fatalf("a swipe back went to %q", a.router.Path())
	}
	send(8)
	tt.Frame()
	if a.router.Path() != "/album/al1" {
		t.Fatalf("a swipe forward went to %q", a.router.Path())
	}
	// Mouse wheels and modifier keys: no.
	in(ui.InputEvent{Kind: ui.InputScroll, DX: -400})
	in(ui.InputEvent{Kind: ui.InputScroll, DX: -400, Precise: true, Mods: ui.Shift})
	tt.Frame()
	if a.router.Path() != "/album/al1" {
		t.Errorf("a wheel went to %q", a.router.Path())
	}
}

// The arrow shows as the fingers go, at the side they come from.
func TestSwipeArrowShows(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1240, 780)
	tt.Frame()
	a.goTo("/albums")
	tt.Frame()
	a.swiping.can = a.canNavigate
	in := a.swipeInput(a.navigate)
	for range 10 {
		in(ui.InputEvent{Kind: ui.InputScroll, DX: -8, Precise: true})
	}
	tt.Frame()
	if p := a.swiping.progress(time.Now()); p >= 0 {
		t.Fatalf("the swipe is at %v", p)
	}
	if a.router.Path() != "/albums" {
		t.Errorf("a short swipe went to %q", a.router.Path())
	}
}

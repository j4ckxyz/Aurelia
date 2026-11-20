package main

import (
	"log"
	"sync"

	"github.com/ebitengine/purego/objc"
)

// On macOS the system's setting decides how a swipe goes back and forward
// (System Settings ▸ Trackpad ▸ More Gestures ▸ Swipe between pages):
// with two fingers, which come as scrolls, or with three, which come as
// a swipe of their own. Aurelia does as the setting says, as Safari does.

// swipeNavigates reports whether two fingers swiped across the trackpad go
// back and forward, as the system's setting says.
func swipeNavigates() bool {
	defer func() {
		if err := recover(); err != nil {
			log.Print("the trackpad's setting: ", err)
		}
	}()
	ns := objc.ID(objc.GetClass("NSEvent"))
	if ns == 0 {
		return true
	}
	return objc.Send[bool](ns, sel("isSwipeTrackingFromScrollEventsEnabled"))
}

var swipeOnce sync.Once

// listenForSwipes has the view of MyGo's windows answer the swipes of
// three fingers, which AppKit sends when the setting asks for them: to the
// right goes back, to the left forward.
func (a *App) listenForSwipes() {
	swipeOnce.Do(func() {
		defer func() {
			if err := recover(); err != nil {
				log.Print("the trackpad's swipes: ", err)
			}
		}()
		view := objc.GetClass("MyGoSurfaceView")
		if view == 0 {
			log.Print("the trackpad's swipes: no view to take them")
			return
		}
		imp := objc.NewIMP(func(self objc.ID, _ objc.SEL, ev objc.ID) {
			dx := objc.Send[float64](ev, sel("deltaX"))
			dir := 0
			switch {
			case dx > 0:
				dir = -1
			case dx < 0:
				dir = 1
			}
			if dir == 0 {
				return
			}
			a.update(func() {
				if a.stage.on && !a.stage.mini {
					if dir < 0 {
						a.closeStage()
					}
					return
				}
				if a.canNavigate(dir) {
					a.navigate(dir)
				}
			})
		})
		if !view.AddMethod(sel("swipeWithEvent:"), imp, "v@:@") {
			log.Print("the trackpad's swipes: the view has its own")
		}
	})
}

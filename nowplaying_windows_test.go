package main

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"aurelia/internal/library"
)

// The media controls of a real window: what Windows is told is what it
// tells back, and its buttons reach the player. On a Windows without the
// controls, as some servers, the test is skipped, unless
// AURELIA_REQUIRE_SMTC says they must be there.
func TestSystemMediaControls(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	user32 := windows.NewLazySystemDLL("user32.dll")
	class, _ := windows.UTF16PtrFromString("STATIC")
	title, _ := windows.UTF16PtrFromString("Aurelia test")
	const overlappedWindow = 0x00CF0000
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), overlappedWindow, 0, 0, 200, 100, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatalf("no window: %v", err)
	}
	defer user32.NewProc("DestroyWindow").Call(hwnd)

	a := testApp(t)
	var n nowPlaying
	if err := n.attach(a, hwnd); err != nil {
		if os.Getenv("AURELIA_REQUIRE_SMTC") != "" {
			t.Fatal(err)
		}
		t.Skipf("this Windows gives no media controls: %v", err)
	}
	if !n.handlesKeys() {
		t.Error("the media keys are not taken through the controls")
	}
	// What was set reads back, each from its own place in the tables.
	var enabled, next uint8
	if err := vcall(n.controls, controlsGetEnabled, uintptr(unsafe.Pointer(&enabled))); err != nil || enabled != 1 {
		t.Errorf("the controls are not enabled: %d, %v", enabled, err)
	}
	if err := vcall(n.controls, controlsGetNext, uintptr(unsafe.Pointer(&next))); err != nil || next != 1 {
		t.Errorf("the Next button is not enabled: %d, %v", next, err)
	}
	var kind int32
	if err := vcall(n.updater, updaterGetType, uintptr(unsafe.Pointer(&kind))); err != nil || kind != typeMusic {
		t.Errorf("the display's type is %d, %v", kind, err)
	}
	n.set(playing{id: "s1", title: "Shape of You", artist: "Ed Sheeran", album: "Divide", artURL: "https://music.example/cover.jpg"})
	got := n.describe()
	for _, want := range []string{`title="Shape of You"`, `artist="Ed Sheeran"`, "status=3", `art="https://music.example/cover.jpg"`} {
		if !strings.Contains(got, want) {
			t.Errorf("Windows holds %s; want %s", got, want)
		}
	}
	if n.music2 != nil && !strings.Contains(got, `album="Divide"`) {
		t.Errorf("Windows holds %s; want the album", got)
	}
	n.set(playing{id: "s1", title: "Shape of You", artist: "Ed Sheeran", paused: true})
	if got := n.describe(); !strings.Contains(got, "status=4") || !strings.Contains(got, `art=""`) {
		t.Errorf("paused and without a picture, Windows holds %s", got)
	}
	n.set(playing{})
	if got := n.describe(); !strings.Contains(got, "status=2") || !strings.Contains(got, `title=""`) {
		t.Errorf("with nothing playing, Windows holds %s", got)
	}

	// The handler Windows calls is the interface it asks for, and no
	// other.
	h := buttonsHandler(a)
	var out unsafe.Pointer
	if err := vcall(h, 0, uintptr(unsafe.Pointer(&iidButtonsEvent)), uintptr(unsafe.Pointer(&out))); err != nil || out != h {
		t.Errorf("the handler is not the buttons' handler: %v", err)
	}
	if err := vcall(h, 0, uintptr(unsafe.Pointer(&iidInterop)), uintptr(unsafe.Pointer(&out))); err == nil {
		t.Error("the handler claims an interface it is not")
	}
	// The buttons, and so the keyboard's media keys.
	p := a.player
	p.play([]*library.Song{a.lib.Song("s1"), a.lib.Song("s2")}, 0)
	for _, step := range []struct {
		button int32
		check  func() bool
		what   string
	}{
		{buttonPause, func() bool { return p.paused }, "Pause pauses"},
		{buttonPlay, func() bool { return !p.paused }, "Play plays on"},
		{buttonNext, func() bool { return p.current().ID == "s2" }, "Next goes on"},
		{buttonPrevious, func() bool { return p.current().ID == "s1" }, "Previous goes back"},
	} {
		pressed(a, step.button)
		a.drain()
		if !step.check() {
			t.Errorf("%s: it does not", step.what)
		}
	}
}

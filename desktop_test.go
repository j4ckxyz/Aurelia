package main

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The tray's icon is a picture with a see-through background and something
// drawn in it.
func TestTrayIcon(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 800, 600)
	tt.Frame()
	data := a.trayIcon()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the icon is not a picture: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 44 || b.Dy() != 44 {
		t.Errorf("the icon is %v, want 44 × 44 (22 points at 2×)", b.Size())
	}
	if _, _, _, alpha := img.At(0, 0).RGBA(); alpha != 0 {
		t.Errorf("the corner is not see-through: alpha %d", alpha)
	}
	solid := 0
	for y := 0; y < 44; y++ {
		for x := 0; x < 44; x++ {
			if _, _, _, alpha := img.At(x, y).RGBA(); alpha > 0x8000 {
				solid++
			}
		}
	}
	if solid < 100 {
		t.Errorf("only %d pixels of the icon are drawn", solid)
	}
}

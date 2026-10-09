// Mkicon draws Aurelia's icon, resources/icon.png: a moon jellyfish, the
// animal the app is named after, whose tentacles are the bars of a sound
// level, in the squircle of macOS icons.
//
//	go run ./tools/mkicon
package main

import (
	"image/png"
	"log"
	"os"

	"github.com/egoist/mygo/ui"
)

const size = 1024

func view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	// The squircle of macOS's icon grid: 824 wide, inset by 100.
	ui.Box(c).Absolute().Left(100).Top(100).Size(824, 824).Radius(186).
		LinearGradient(ui.LinearGradient{From: ui.Hex("#b9a2ff"), To: ui.Hex("#5a3fd6"), Angle: 160, Oklab: true}).
		Shadow(0, 14, 36, 0, ui.RGBA(40, 20, 90, 0.35)).
		Draw(func(p *ui.Painter, r ui.Rect) {
			white := ui.Hex("#ffffff")
			cx := r.X + r.W/2
			// The bell: a dome, flat below.
			top, base, half := r.Y+178, r.Y+430, float32(238)
			var bell ui.Path
			bell.MoveTo(cx-half, base)
			bell.CubeTo(cx-half, top+40, cx-half*0.55, top, cx, top)
			bell.CubeTo(cx+half*0.55, top, cx+half, top+40, cx+half, base)
			bell.CubeTo(cx+half, base+34, cx-half, base+34, cx-half, base)
			bell.Close()
			p.FillPath(&bell, white)
			// A glint inside the bell.
			p.Fill(ui.Rect{X: cx - 130, Y: top + 92, W: 150, H: 34}, ui.RGBA(132, 104, 240, 0.34), 17)
			// The tentacles: the bars of a sound's level.
			const bar, gap = 58, 40
			heights := []float32{150, 250, 190, 290, 130}
			x := cx - (5*bar+4*gap)/2
			for i, h := range heights {
				alpha := float32(1)
				if i%2 == 0 {
					alpha = 0.82
				}
				p.Fill(ui.Rect{X: x, Y: base + 62, W: bar, H: h}, white.Alpha(alpha), bar/2)
				x += bar + gap
			}
		})
}

func main() {
	img := ui.Render(view, size, size, 1)
	f, err := os.Create("resources/icon.png")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

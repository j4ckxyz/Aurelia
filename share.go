package main

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"

	"aurelia/internal/library"
)

// A song or an album can be made a picture to post: the cover, the name
// and the artist, on white, on black, on a color, or on the cover's own
// colors, in the shapes that networks show.

// cardFormat is a shape of the picture, in pixels.
type cardFormat struct {
	id, name string
	w, h     int
}

var cardFormats = []cardFormat{
	{"square", "Square", 1080, 1080},
	{"story", "Story", 1080, 1920},
	{"wide", "Wide", 1200, 630},
}

// The backgrounds of the picture.
const (
	bgWhite = "white"
	bgBlack = "black"
	bgColor = "color"
	bgBlend = "blend"
)

// shareState is the picture being made.
type shareState struct {
	open bool
	// What it is of.
	title, artist, sub string
	imageID, imageTag  string
	// How it looks.
	format int
	bg     string
	color  ui.Color
	hex    string // the color as it is typed
	mark   bool   // "Aurelia" at the foot of it

	// The cover, once it is loaded: large, and its two colors.
	cover         image.Image
	bmp           *ui.Bitmap
	vibrant, base ui.Color
	loading       bool
}

// cardSpec is all a card is drawn from.
type cardSpec struct {
	w, h float32 // the picture, in points
	k    float32 // points per pixel of the picture: 1 for the file, less for a preview
	f    cardFormat
	sh   *shareState
}

// shareSong opens the dialog for a song.
func (a *App) shareSong(s *library.Song) {
	a.openShare(s.Name, s.Artist, s.Album, s.ImageItem, s.ImageTag)
}

// shareAlbum opens the dialog for an album.
func (a *App) shareAlbum(al *library.Album) {
	a.openShare(al.Name, al.Artist, "", al.ID, al.ImageTag)
}

func (a *App) openShare(title, artist, sub, imageID, imageTag string) {
	sh := &a.share
	keep := *sh
	*sh = shareState{
		open: true, title: title, artist: artist, sub: sub, imageID: imageID, imageTag: imageTag,
		format: keep.format, bg: keep.bg, color: keep.color, hex: keep.hex, mark: keep.mark,
	}
	if sh.bg == "" {
		sh.bg = bgBlend
	}
	if sh.color == (ui.Color{}) {
		sh.color = ui.RGB(0x7c, 0x5c, 0xff)
		sh.hex = "#7C5CFF"
	}
}

// closeShare lets go of the large cover.
func (a *App) closeShare() {
	sh := &a.share
	sh.open, sh.cover, sh.bmp, sh.loading = false, nil, nil, false
}

// loadShareCover brings the cover in as large as the picture needs: from
// the pictures kept on disk, which fetch it when it is not there.
func (a *App) loadShareCover(c *ui.Context) {
	sh := &a.share
	if sh.cover != nil || sh.loading || sh.imageID == "" {
		return
	}
	if a.covers != nil {
		a.setShareCover(a.covers(sh.imageID))
		return
	}
	cl := a.clientNow()
	if cl == nil || sh.imageTag == "" {
		return
	}
	const px = 1200
	id := sh.imageID
	path := a.images.file(imageKey(id, sh.imageTag, px), cl.ImageURL(id, "Primary", sh.imageTag, px))
	if path == "" {
		c.After(200 * time.Millisecond) // it is being fetched
		return
	}
	sh.loading = true
	go func() {
		data, err := os.ReadFile(path)
		var pic image.Image
		if err == nil {
			pic, _, err = image.Decode(bytes.NewReader(data))
		}
		a.update(func() {
			sh.loading = false
			if err == nil && sh.open && sh.imageID == id {
				a.setShareCover(pic)
			}
		})
	}()
}

func (a *App) setShareCover(pic image.Image) {
	sh := &a.share
	sh.cover, sh.bmp = pic, ui.NewBitmap(pic)
	sh.vibrant, sh.base = cardColors(pic)
}

// cardColors finds the two colors a cover is mixed from: the most vivid
// of its hues, and the average of it all.
func cardColors(pic image.Image) (vibrant, base ui.Color) {
	const n = 32
	small := image.NewRGBA(image.Rect(0, 0, n, n))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), pic, pic.Bounds(), draw.Src, nil)
	type bucket struct{ w, r, g, b float64 }
	var buckets [12]bucket
	var sr, sg, sb float64
	for i := 0; i < len(small.Pix); i += 4 {
		r, g, b := float64(small.Pix[i]), float64(small.Pix[i+1]), float64(small.Pix[i+2])
		sr, sg, sb = sr+r, sg+g, sb+b
		h, s, v := rgbToHSV(r/255, g/255, b/255)
		// Vivid and bright pixels count for more than the grey around them.
		w := s * s * v
		bk := &buckets[int(h/30)%12]
		bk.w, bk.r, bk.g, bk.b = bk.w+w, bk.r+r*w, bk.g+g*w, bk.b+b*w
	}
	px := float64(n * n)
	base = ui.RGB(uint8(sr/px), uint8(sg/px), uint8(sb/px))
	best := 0
	for i := range buckets {
		if buckets[i].w > buckets[best].w {
			best = i
		}
	}
	bk := buckets[best]
	if bk.w < 0.5 {
		return base, base // a cover of greys: no hue to bring out
	}
	return ui.RGB(uint8(bk.r/bk.w), uint8(bk.g/bk.w), uint8(bk.b/bk.w)), base
}

func rgbToHSV(r, g, b float64) (h, s, v float64) {
	hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	v = hi
	d := hi - lo
	if hi > 0 {
		s = d / hi
	}
	switch {
	case d == 0:
		return 0, s, v
	case hi == r:
		h = math.Mod((g-b)/d, 6)
	case hi == g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, v
}

// luminance is how light a color is, from 0 to 1.
func luminance(c ui.Color) float64 {
	lin := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// cardLook is the colors of a background: the fill, or the two ends of the
// gradient, and the text on it.
type cardLook struct {
	from, to      ui.Color
	gradient      bool
	text, subtext ui.Color
	shadow        float32
}

// look works out the colors of the background chosen.
func (sh *shareState) look() cardLook {
	dark := func(l *cardLook) {
		l.text, l.subtext = ui.RGB(255, 255, 255), ui.RGBA(255, 255, 255, 0.68)
		l.shadow = 0.5
	}
	light := func(l *cardLook) {
		l.text, l.subtext = ui.RGB(0x14, 0x14, 0x17), ui.RGBA(0x14, 0x14, 0x17, 0.6)
		l.shadow = 0.22
	}
	var l cardLook
	black := ui.RGB(0, 0, 0)
	switch sh.bg {
	case bgWhite:
		l.from, l.to = ui.RGB(255, 255, 255), ui.RGB(255, 255, 255)
		light(&l)
	case bgBlack:
		l.from, l.to = ui.RGB(0x0b, 0x0b, 0x0d), ui.RGB(0x0b, 0x0b, 0x0d)
		dark(&l)
	case bgColor:
		l.from, l.to = sh.color, sh.color
		if luminance(sh.color) > 0.42 {
			light(&l)
		} else {
			dark(&l)
		}
	default:
		// The cover's own colors: its most vivid hue at the top, darkened so
		// that white reads on it, easing to its average at the foot.
		v, b := sh.vibrant, sh.base
		if sh.cover == nil {
			v, b = ui.RGB(0x5b, 0x4b, 0xc8), ui.RGB(0x1d, 0x1a, 0x3a)
		}
		l.from, l.to, l.gradient = v.Mix(black, 0.32), b.Mix(black, 0.66), true
		dark(&l)
		// A light cover still leaves the top light: more black until white
		// text reads.
		for i := 0; i < 4 && luminance(l.from) > 0.30; i++ {
			l.from = l.from.Mix(black, 0.25)
		}
	}
	return l
}

// cardView draws the picture: the cover, the name and the artist, on the
// background chosen. Sizes are in pixels of the file, times spec.k, so
// that a preview is the same picture smaller.
func cardView(c *ui.Context, sp *cardSpec) {
	sh := sp.sh
	l := sh.look()
	k := sp.k
	px := func(v float32) float32 { return v * k }
	root := ui.Box(c).Fill()
	if l.gradient {
		root.LinearGradient(ui.LinearGradient{From: l.from, To: l.to, Angle: 165, Oklab: true})
	} else {
		root.Background(l.from)
	}
	wide := sp.f.id == "wide"
	var cover float32
	var title, artist, sub float32
	switch sp.f.id {
	case "story":
		cover, title, artist, sub = 800, 76, 48, 36
	case "wide":
		cover, title, artist, sub = 470, 58, 38, 30
	default:
		cover, title, artist, sub = 620, 56, 38, 30
	}
	root.Children(func() {
		layout := ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center)
		gap := float32(60)
		if wide {
			layout = ui.Row(c).Fill().Justify(ui.Center).AlignItems(ui.Center)
			gap = 64
		}
		layout.Gap(px(gap)).Padding(px(70))
		layout.Children(func() {
			// The cover, with a soft shadow.
			pic := ui.Box(c).Size(px(cover), px(cover)).Radius(px(cover*0.045)).Clip().Shrink(0).
				Background(l.text.Alpha(0.08)).Shadow(0, px(28), px(70), 0, ui.RGBA(0, 0, 0, l.shadow))
			pic.Children(func() {
				if sh.bmp != nil {
					ui.Image(c, sh.bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover)
				} else {
					ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Children(func() {
						ui.Icon(c, icon("disc-3")).FontSize(px(cover * 0.3)).TextColor(l.subtext)
					})
				}
			})
			// Its name, and the artist's.
			align := ui.Center
			if wide {
				align = ui.Start
			}
			textW := px(float32(sp.f.w) - 140)
			if wide {
				textW = px(float32(sp.f.w) - 140 - cover - gap)
			}
			col := ui.Column(c).Gap(px(14)).Shrink(1).MinWidth(0)
			if wide {
				col.AlignItems(ui.Start)
			} else {
				col.AlignItems(ui.Center)
			}
			col.Children(func() {
				ui.Text(c, sh.title).FontSize(px(title)).FontWeight(700).LineHeight(1.12).TextColor(l.text).
					TextAlign(align).MaxLines(2).MaxWidth(textW).LetterSpacing(-px(title) * 0.01)
				if sh.artist != "" {
					ui.Text(c, sh.artist).FontSize(px(artist)).FontWeight(500).TextColor(l.subtext).
						TextAlign(align).MaxLines(1).MaxWidth(textW)
				}
				if sh.sub != "" && sp.f.id != "wide" && sh.sub != sh.title {
					ui.Text(c, sh.sub).FontSize(px(sub)).TextColor(l.subtext.Alpha(0.75)).
						TextAlign(align).MaxLines(1).MaxWidth(textW)
				}
			})
		})
		if sh.mark {
			markBox := ui.Box(c).Absolute().Left(0).Right(0).Bottom(px(34)).Center()
			if wide {
				markBox = ui.Box(c).Absolute().Right(px(50)).Bottom(px(34))
			}
			markBox.Children(func() {
				ui.Row(c).Gap(px(10)).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icon("logo")).Size(px(30), px(30)).TextColor(l.subtext)
					ui.Text(c, "Aurelia").FontSize(px(26)).FontWeight(600).TextColor(l.subtext)
				})
			})
		}
	})
}

// renderCard draws the picture at the size of the file.
func (a *App) renderCard() image.Image {
	sh := &a.share
	f := cardFormats[sh.format]
	sp := &cardSpec{w: float32(f.w), h: float32(f.h), k: 1, f: f, sh: sh}
	tt := ui.NewTester(func(c *ui.Context) { cardView(c, sp) }, f.w, f.h)
	tt.SetScale(1)
	tt.Frame()
	return tt.Image()
}

// cardPNG is the picture, encoded.
func (a *App) cardPNG() ([]byte, error) {
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, a.renderCard()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// copyCard puts the picture on the clipboard.
func (a *App) copyCard() {
	data, err := a.cardPNG()
	if err != nil {
		a.toastError("Could not make the picture", err)
		return
	}
	if err := mygo.Clipboard.Write(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, data)))); err != nil {
		a.toastError("Could not copy the picture", err)
		return
	}
	a.toast("Copied the picture: paste it where you post")
}

// saveCard asks where to save the picture, and writes it there.
func (a *App) saveCard() {
	data, err := a.cardPNG()
	if err != nil {
		a.toastError("Could not make the picture", err)
		return
	}
	name := cardFileName(a.share.artist, a.share.title)
	win := a.win
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      win,
			Title:       "Save the picture",
			DefaultPath: name,
			Filters:     []mygo.FileFilter{{Name: "PNG pictures", Extensions: []string{"png"}}},
		})
		if err != nil || path == "" {
			return
		}
		if filepath.Ext(path) == "" {
			path += ".png"
		}
		err = os.WriteFile(path, data, 0o644)
		a.update(func() {
			if err != nil {
				a.toastError("Could not save the picture", err)
			} else {
				a.toast("Saved " + filepath.Base(path))
			}
		})
	}()
}

// cardFileName makes a name for the file of the picture of "artist - title".
func cardFileName(artist, title string) string {
	name := strings.TrimSpace(artist + " - " + title)
	if artist == "" {
		name = title
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, name)
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	if strings.TrimSpace(name) == "" {
		name = "Aurelia"
	}
	return name + ".png"
}

// parseHexColor reads "#rgb" or "#rrggbb", with or without the hash.
func parseHexColor(s string) (ui.Color, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return ui.Color{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return ui.Color{}, false
	}
	return ui.RGB(uint8(v>>16), uint8(v>>8), uint8(v)), true
}

// shareSwatches are the colors offered to start from.
var shareSwatches = []string{"#7C5CFF", "#FF5C8A", "#FF8A3D", "#F2C94C", "#3DDC97", "#38B6FF", "#E8E1D6", "#1F2A44"}

// chip is a small button that shows whether it is chosen.
func (a *App) chip(c *ui.Context, label string, on bool) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key("chip:"+label)).Height(30).Padding(0, 14).Radius(15).Shrink(0).Cursor(ui.CursorPointer).Label(label).Checked(on)
	fg := p.muted
	switch {
	case on:
		b.Background(p.accent)
		fg = p.onAcc
	case b.Hovered():
		b.Background(p.hover)
		fg = p.text
	default:
		b.Background(p.surface)
	}
	b.Transition(fade)
	b.Children(func() { ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(fg).SingleLine() })
	return b
}

// swatch is a round button of a background.
func (a *App) swatch(c *ui.Context, key, label string, on bool, fill func(el ui.Element)) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key("sw:"+key)).Size(34, 34).Radius(17).Shrink(0).Cursor(ui.CursorPointer).Label(label).Tooltip(label).Checked(on)
	fill(b)
	ring := p.border
	if on {
		ring = p.accent
	}
	b.Border(2, ring)
	b.Transition(fade)
	return b
}

// shareDialog is the dialog that makes the picture.
func (a *App) shareDialog(c *ui.Context) {
	sh := &a.share
	if !sh.open {
		return
	}
	a.loadShareCover(c)
	open := sh.open
	p := a.pal
	ui.Modal(c, &open, func() {
		f := cardFormats[sh.format]
		// The preview, as large as fits.
		const maxW, maxH = 330, 400
		k := float32(math.Min(maxW/float64(f.w), maxH/float64(f.h)))
		sp := &cardSpec{w: float32(f.w) * k, h: float32(f.h) * k, k: k, f: f, sh: sh}
		ui.Column(c).Gap(14).Padding(20).Children(func() {
			ui.Text(c, "Share as a picture").FontSize(18).FontWeight(700)
			ui.Row(c).Gap(24).AlignItems(ui.Start).Children(func() {
				ui.Box(c).Size(maxW, maxH).Shrink(0).Center().Children(func() {
					ui.Box(c.Key("preview")).Size(sp.w, sp.h).Radius(8).Clip().Shrink(0).Shadow(0, 6, 22, 0, p.shadow).Children(func() {
						cardView(c, sp)
					})
				})
				ui.Column(c).Gap(14).Width(300).Shrink(0).Children(func() {
					label := func(s string) {
						ui.Text(c, s).FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(p.faint)
					}
					label("SHAPE")
					ui.Row(c).Gap(6).Wrap().Children(func() {
						for i, cf := range cardFormats {
							if a.chip(c, cf.name, sh.format == i).Clicked() {
								sh.format = i
							}
						}
					})
					ui.Text(c, strconv.Itoa(f.w)+" × "+strconv.Itoa(f.h)+" pixels").FontSize(12).TextColor(p.muted)
					label("BACKGROUND")
					ui.Row(c).Gap(10).Children(func() {
						if a.swatch(c, "white", "White", sh.bg == bgWhite, func(el ui.Element) { el.Background(ui.RGB(255, 255, 255)) }).Clicked() {
							sh.bg = bgWhite
						}
						if a.swatch(c, "black", "Black", sh.bg == bgBlack, func(el ui.Element) { el.Background(ui.RGB(0x0b, 0x0b, 0x0d)) }).Clicked() {
							sh.bg = bgBlack
						}
						if a.swatch(c, "color", "Your color", sh.bg == bgColor, func(el ui.Element) { el.Background(sh.color) }).Clicked() {
							sh.bg = bgColor
						}
						if a.swatch(c, "blend", "The cover's colors", sh.bg == bgBlend, func(el ui.Element) {
							l := (&shareState{bg: bgBlend, vibrant: sh.vibrant, base: sh.base, cover: sh.cover}).look()
							el.LinearGradient(ui.LinearGradient{From: l.from, To: l.to, Angle: 150})
						}).Clicked() {
							sh.bg = bgBlend
						}
					})
					if sh.bg == bgColor {
						ui.Row(c).Gap(6).Wrap().Children(func() {
							for _, hex := range shareSwatches {
								col, _ := parseHexColor(hex)
								b := ui.ButtonBase(c.Key("c:"+hex)).Size(24, 24).Radius(12).Shrink(0).Background(col).Cursor(ui.CursorPointer).Label(hex).Border(2, p.border)
								if sh.color == col {
									b.Border(2, p.accent)
								}
								if b.Clicked() {
									sh.color, sh.hex = col, hex
								}
							}
						})
						in := ui.TextInput(c.Key("hex"), &sh.hex).Width(120).Font("monospace").Placeholder("#RRGGBB").Label("Color in hex")
						if in.Changed() {
							if col, ok := parseHexColor(sh.hex); ok {
								sh.color = col
							}
						}
					}
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Switch(c.Key("mark"), &sh.mark).Label("Aurelia mark")
						ui.Text(c, "Put “Aurelia” at the foot").TextColor(p.muted)
					})
					ui.Spacer(c)
					ui.Row(c).Gap(8).Children(func() {
						if a.pillButton(c, "copy", "Copy", true).Clicked() {
							a.copyCard()
						}
						if a.pillButton(c, "download", "Save…", false).Clicked() {
							a.saveCard()
						}
					})
				})
			})
		})
	})
	if !open {
		a.closeShare()
	}
}

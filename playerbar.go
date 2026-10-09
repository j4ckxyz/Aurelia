package main

import (
	"context"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/audio"
	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
)

// playerBar is the bar along the bottom of the window: what plays, the
// buttons that play, and the volume.
func (a *App) playerBar(c *ui.Context) {
	p := a.pal
	pl := a.player
	song := pl.current()
	st := pl.state()
	if song != nil && !st.Paused {
		// The times change every second; the bar itself moves by its
		// own drawing.
		c.After(time.Second - st.Position%time.Second + 10*time.Millisecond)
		pl.reportProgress(false)
	}
	ui.Row(c).Height(playerH).Shrink(0).Padding(0, 16).Gap(16).Background(p.bar).BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
		// What plays.
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).Gap(12).Children(func() {
			if song == nil {
				return
			}
			pic := a.art(c, song.ImageItem, song.ImageTag, thumbArt, "music", nil).Size(50, 50).Radius(max(p.radius-2, 3)).Cursor(ui.CursorPointer)
			if pic.Clicked() && song.AlbumID != "" {
				a.goTo("/album/" + song.AlbumID)
			}
			ui.Column(c).MinWidth(0).Shrink(1).Gap(3).Children(func() {
				title := ui.Text(c, song.Name).FontWeight(600).SingleLine().Cursor(ui.CursorPointer)
				if title.Hovered() {
					title.Underline()
				}
				if title.Clicked() && song.AlbumID != "" {
					a.goTo("/album/" + song.AlbumID)
				}
				artist := ui.Text(c, song.Artist).FontSize(12).TextColor(p.muted).SingleLine()
				if id := a.artistOf(song.ArtistIDs, song.AlbumArtistIDs); id != "" {
					artist.Cursor(ui.CursorPointer)
					if artist.Hovered() {
						artist.Underline().TextColor(p.text)
					}
					if artist.Clicked() {
						a.goTo("/artist/" + id)
					}
				}
			})
			glyph, label := "heart", "Add to Favorites"
			if song.Favorite {
				glyph, label = "heart-fill", "Remove from Favorites"
			}
			if a.toggleIcon(c, glyph, label, song.Favorite, 30, 16).Clicked() {
				a.setFavorite(song.ID, &song.Favorite, !song.Favorite)
			}
		})

		// The buttons, over the place in the song.
		ui.Column(c).Grow(1.4).Basis(0).MinWidth(260).MaxWidth(680).Gap(2).AlignItems(ui.Stretch).Children(func() {
			ui.Row(c).Justify(ui.Center).Gap(8).Children(func() {
				if a.toggleIcon(c, "shuffle", "Shuffle", a.settings.Shuffle, 32, 16).Clicked() {
					pl.setShuffle(!a.settings.Shuffle)
				}
				if a.iconButton(c, "skip-back-fill", "Previous", 32, 17).Disabled(song == nil).Clicked() {
					pl.previous()
				}
				a.playButton(c, song, st)
				if a.iconButton(c, "skip-forward-fill", "Next", 32, 17).Disabled(song == nil).Clicked() {
					pl.skip()
				}
				glyph, label := "repeat", "Repeat"
				switch a.settings.Repeat {
				case repeatAll:
					label = "Repeat all"
				case repeatOne:
					glyph, label = "repeat-1", "Repeat one"
				}
				if a.toggleIcon(c, glyph, label, a.settings.Repeat != repeatOff, 32, 16).Clicked() {
					pl.cycleRepeat()
				}
			})
			a.seekBar(c, song, st)
		})

		// The queue, the lyrics and the volume.
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).Justify(ui.End).Gap(4).Children(func() {
			lyrics := a.router.Path() == "/lyrics"
			if a.toggleIcon(c, "mic-vocal", "Lyrics", lyrics, 32, 16).Clicked() {
				if lyrics {
					a.router.Back()
				} else {
					a.goTo("/lyrics")
				}
			}
			if a.toggleIcon(c, "list-end", "Queue", a.settings.QueueOpen, 32, 16).Clicked() {
				a.settings.QueueOpen = !a.settings.QueueOpen
				a.saveSettings()
			}
			a.volume(c)
		})
	})
}

// playButton plays and pauses; it turns while a song waits for its
// download.
func (a *App) playButton(c *ui.Context, song *library.Song, st audio.State) {
	p := a.pal
	glyph, label := "play-fill", "Play"
	if song != nil && !st.Paused {
		glyph, label = "pause-fill", "Pause"
	}
	b := ui.ButtonBase(c.Key("play")).Size(36, 36).Radius(18).Shrink(0).Label(label).Tooltip(label).Disabled(song == nil).Cursor(ui.CursorPointer)
	fg := p.bg
	switch {
	case song == nil:
		b.Background(p.surface)
		fg = p.faint
	case b.Hovered():
		b.Background(p.text)
	default:
		b.Background(p.text.Alpha(0.92))
	}
	b.Children(func() {
		if song != nil && st.Buffering {
			spin := ui.Icon(c, icon("loader-circle")).Size(17, 17).TextColor(fg)
			spin.Rotate(spin.Loop("spin", 900*time.Millisecond, ui.Linear) * 360)
			return
		}
		ic := ui.Icon(c, icon(glyph)).Size(15, 15).TextColor(fg)
		if glyph == "play-fill" {
			ic.Margin(0, 0, 0, 2)
		}
	})
	if b.Clicked() {
		a.player.toggle()
	}
}

// seekBar shows the place in the song between its times, and moves there
// when dragged.
func (a *App) seekBar(c *ui.Context, song *library.Song, st audio.State) {
	p := a.pal
	dur := st.Duration
	pos := st.Position
	if a.scrubbing {
		pos = time.Duration(a.scrub * float64(dur))
	}
	ui.Row(c).Gap(10).Children(func() {
		times := func(s string, align ui.Align) {
			ui.Text(c, s).FontSize(11).TextColor(p.muted).FontFeatures("tnum").Width(40).TextAlign(align).Shrink(0)
		}
		if song == nil {
			times("", ui.End)
		} else {
			times(clock(pos), ui.End)
		}
		frac := 0.0
		if dur > 0 {
			frac = float64(pos) / float64(dur)
		}
		v := frac
		bar := ui.SliderBase(c.Key("seek"), &v, 0, 1).Grow(1).Height(16).Label("Position").Disabled(song == nil || dur == 0)
		if bar.Changed() {
			a.scrubbing, a.scrub = true, v
		}
		if a.scrubbing && !bar.Pressed() {
			// Let go: the song moves there.
			a.player.seek(time.Duration(a.scrub * float64(dur)))
			a.scrubbing = false
		}
		active := bar.Hovered() || bar.Pressed()
		scrubbing, scrub := a.scrubbing, a.scrub
		bar.Draw(func(g *ui.Painter, r ui.Rect) {
			// Painted again as the song advances, without building the
			// window anew.
			f := scrub
			loaded := 0.0
			if !scrubbing {
				f = 0
				if now := a.player.state(); now.Duration > 0 && a.player.current() != nil {
					f = float64(now.Position) / float64(now.Duration)
					loaded = now.Loaded
					if !now.Paused {
						// As often as the bar moves a pixel.
						step := time.Duration(float64(now.Duration) / float64(max(r.W*g.Scale(), 1)))
						g.After(max(16*time.Millisecond, min(step, 500*time.Millisecond)))
					}
				}
			}
			f = math.Max(0, math.Min(1, f))
			h := float32(4)
			track := ui.Rect{X: r.X, Y: r.Y + (r.H-h)/2, W: r.W, H: h}
			g.Fill(track, p.text.Alpha(0.14), h/2)
			if loaded > 0 && loaded < 1 {
				g.Fill(ui.Rect{X: track.X, Y: track.Y, W: track.W * float32(loaded), H: h}, p.text.Alpha(0.16), h/2)
			}
			fill := p.text
			if active {
				fill = p.accent
			}
			if w := track.W * float32(f); w > 0 {
				g.Fill(ui.Rect{X: track.X, Y: track.Y, W: max(w, h), H: h}, fill, h/2)
			}
			if active {
				const knob = 12
				x := track.X + track.W*float32(f)
				g.Fill(ui.Rect{X: min(max(x-knob/2, r.X), r.X+r.W-knob), Y: r.Y + (r.H-knob)/2, W: knob, H: knob}, p.text, knob/2)
			}
		})
		if song == nil {
			times("", ui.Start)
		} else {
			times(clock(dur), ui.Start)
		}
	})
}

// volume is the volume's button, which mutes, and its slider.
func (a *App) volume(c *ui.Context) {
	p := a.pal
	v := a.settings.Volume
	if a.settings.Muted {
		v = 0
	}
	glyph := "volume-2"
	switch {
	case v == 0:
		glyph = "volume-x"
	case v < 0.5:
		glyph = "volume-1"
	}
	label := "Mute"
	if a.settings.Muted {
		label = "Unmute"
	}
	if a.iconButton(c, glyph, label, 32, 16).Clicked() {
		a.settings.Muted = !a.settings.Muted
		a.player.applyVolume()
		a.saveSettings()
	}
	bar := ui.SliderBase(c.Key("volume"), &v, 0, 1).Width(96).Height(16).Label("Volume").Shrink(0)
	if bar.Changed() {
		a.settings.Volume, a.settings.Muted = v, false
		a.player.applyVolume()
		a.volumeDirty = true
	}
	if a.volumeDirty && !bar.Pressed() {
		// Saved when let go, not for every pixel on the way.
		a.volumeDirty = false
		a.saveSettings()
	}
	active := bar.Hovered() || bar.Pressed()
	bar.Draw(func(g *ui.Painter, r ui.Rect) {
		h := float32(4)
		track := ui.Rect{X: r.X, Y: r.Y + (r.H-h)/2, W: r.W, H: h}
		g.Fill(track, p.text.Alpha(0.14), h/2)
		fill := p.muted
		if active {
			fill = p.accent
		}
		if w := track.W * float32(v); w > 0 {
			g.Fill(ui.Rect{X: track.X, Y: track.Y, W: max(w, h), H: h}, fill, h/2)
		}
		if active {
			const knob = 12
			x := track.X + track.W*float32(v)
			g.Fill(ui.Rect{X: min(max(x-knob/2, r.X), r.X+r.W-knob), Y: r.Y + (r.H-knob)/2, W: knob, H: knob}, p.text, knob/2)
		}
	})
}

// queuePanel lists what plays and what follows, beside the pages.
func (a *App) queuePanel(c *ui.Context) {
	p := a.pal
	pl := a.player
	ui.Column(c).Width(queueW).Shrink(0).Background(p.sidebar).BorderWidth(0, 0, 0, 1).BorderColor(p.border).Children(func() {
		ui.Row(c).Height(topBarH).Shrink(0).Padding(0, 10, 0, 18).DragWindow().Children(func() {
			ui.Text(c, "Queue").FontSize(15).FontWeight(700).Grow(1)
			if len(pl.queue) > pl.index+1 && pl.index >= 0 {
				if a.textButton(c, "Clear").Clicked() {
					pl.clearUpcoming()
				}
			}
			if a.iconButton(c, "x", "Close the queue", 30, 16).Clicked() {
				a.settings.QueueOpen = false
				a.saveSettings()
			}
		})
		if pl.current() == nil {
			a.emptyState(c, "list-end", "Nothing is playing", "Songs you play line up here.")
			return
		}
		// The song playing, then the songs after it, which drag to
		// another place.
		label := func(text string, top float32) {
			ui.Text(c, text).FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(p.faint).Padding(top, 18, 6).Shrink(0)
		}
		label("NOW PLAYING", 6)
		ui.Box(c).Shrink(0).Children(func() { a.queueRow(c.Key("current"), pl.current(), pl.index) })
		start := pl.index + 1
		n := len(pl.queue) - start
		if n <= 0 {
			return
		}
		label("NEXT UP", 16)
		remove := -1
		state := &a.pages.queueList
		state.Reorder = func(rows []int, to int) {
			from := make([]int, len(rows))
			for i, r := range rows {
				from[i] = start + r
			}
			pl.move(from, start+to)
		}
		// A click chooses a song and a double click plays it: the list
		// tells, so that its rows stay free to be dragged.
		chosen := &a.pages.queueChosen
		if *chosen >= n {
			*chosen = -1
		}
		state.Selected = chosen
		list := ui.List(c.Key("queue"), state, n, func(i int) {
			if at := start + i; at < len(pl.queue) && a.queueRow(c.Key(at), pl.queue[at], at) {
				remove = at
			}
		}).Grow(1).MinHeight(0).Padding(0, 0, 12)
		switch {
		case remove >= 0:
			pl.remove(remove)
			*chosen = -1
		case list.Submitted() && *chosen >= 0:
			pl.failures = 0
			pl.playIndex(start + *chosen)
			*chosen = -1
		}
	})
}

// queueRow is a song of the queue; it reports whether its button took it
// out.
func (a *App) queueRow(c *ui.Context, s *library.Song, at int) (removed bool) {
	p := a.pal
	pl := a.player
	current := at == pl.index
	row := ui.Row(c).Height(48).Padding(0, 8, 0, 10).Gap(10).Radius(p.radius).Margin(0, 8)
	hovered := row.Hovered()
	if hovered {
		row.Background(p.surface)
	}
	row.Children(func() {
		a.art(c, s.ImageItem, s.ImageTag, thumbArt, "music", nil).Size(36, 36).Radius(max(p.radius-4, 3))
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			title := ui.Text(c, s.Name).FontWeight(500).SingleLine()
			if current {
				title.TextColor(p.accent)
			}
			ui.Text(c, s.Artist).FontSize(12).TextColor(p.muted).SingleLine()
		})
		if hovered && !current {
			if a.iconButton(c, "x", "Remove from the queue", 26, 14).Clicked() {
				removed = true
			}
		} else {
			ui.Text(c, clock(s.Duration())).FontSize(12).TextColor(p.muted).FontFeatures("tnum").Shrink(0)
		}
	})
	row.ContextMenu(func(m *ui.Menu) {
		if !current && m.Item("Play").Chosen() {
			pl.playIndex(at)
		}
		if !current && m.Item("Remove from Queue").Chosen() {
			removed = true
		}
		a.songMenu(m, s)
	})
	return removed
}

// lyricsState is the lyrics of songs, as the server gave them.
type lyricsState struct {
	bySong  map[string][]jellyfin.LyricLine
	asked   map[string]bool
	current int // the line being sung, to scroll to it once
	forSong string
}

// lines returns the lyrics of a song, nil while they come or when it has
// none, and whether the server answered.
func (a *App) lyricLines(s *library.Song) (lines []jellyfin.LyricLine, known bool) {
	ly := &a.lyrics
	if ly.bySong == nil {
		ly.bySong, ly.asked = map[string][]jellyfin.LyricLine{}, map[string]bool{}
	}
	if lines, ok := ly.bySong[s.ID]; ok {
		return lines, true
	}
	if !s.HasLyrics {
		return nil, true
	}
	if cl := a.clientNow(); cl != nil && !ly.asked[s.ID] {
		ly.asked[s.ID] = true
		id := s.ID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			lines, err := cl.Lyrics(ctx, id)
			a.update(func() {
				if err != nil {
					delete(ly.asked, id) // asked again when the page shows again
					lines = nil
				}
				ly.bySong[id] = lines
			})
		}()
	}
	return nil, false
}

// lyricsPage shows the song playing large, with its lyrics, the line
// being sung lit.
func (a *App) lyricsPage(c *ui.Context) {
	p := a.pal
	song := a.player.current()
	if song == nil {
		a.emptyState(c, "mic-vocal", "Nothing is playing", "Play a song to see its lyrics.")
		return
	}
	lines, known := a.lyricLines(song)
	st := a.player.state()
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Padding(0, pagePad, 0).Gap(36).Children(func() {
		ui.Column(c).Width(bigArt).Shrink(0).Justify(ui.Center).Gap(6).Padding(0, 0, 40).Children(func() {
			a.art(c, song.ImageItem, song.ImageTag, bigArt, "music", nil).Size(bigArt, bigArt).Radius(p.radius+4).Shadow(0, 16, 40, 0, p.shadow)
			ui.Text(c, song.Name).FontSize(20).FontWeight(700).MaxLines(2).Margin(18, 0, 0)
			ui.Text(c, song.Artist).TextColor(p.muted).SingleLine()
			if song.Album != "" {
				ui.Text(c, song.Album).TextColor(p.faint).FontSize(12).SingleLine()
			}
		})
		switch {
		case !known:
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, "Fetching the lyrics…").TextColor(p.muted)
			})
		case len(lines) == 0:
			ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
				ui.Icon(c, icon("text-quote")).Size(36, 36).TextColor(p.faint)
				ui.Text(c, "No lyrics for this song").FontWeight(600).FontSize(16)
				ui.Text(c, "Jellyfin shows lyrics for songs with a .lrc file or embedded lyrics.").TextColor(p.muted).TextAlign(ui.Center).MaxWidth(360)
			})
		default:
			// The line being sung: the last that began.
			timed := lines[0].Start >= 0
			cur := -1
			if timed {
				for i, l := range lines {
					if l.Start <= st.Position+150*time.Millisecond {
						cur = i
					}
				}
				if !st.Paused {
					c.After(120 * time.Millisecond)
				}
			}
			ly := &a.lyrics
			moved := ly.forSong != song.ID || ly.current != cur
			ly.forSong, ly.current = song.ID, cur
			ui.Scroll(c.Key("lyrics-"+song.ID)).Grow(1).MinWidth(0).Padding(60, 12, 200, 0).Gap(14).Children(func() {
				for i, l := range lines {
					text := l.Text
					if text == "" {
						text = "♪"
					}
					line := ui.Text(c.Key(i), text).FontSize(24).FontWeight(700).LineHeight(1.3)
					switch {
					case !timed:
						line.TextColor(p.text).FontSize(18).FontWeight(500)
					case i == cur:
						line.TextColor(p.text)
						if moved {
							line.ScrollIntoView()
						}
					default:
						line.TextColor(p.text.Alpha(0.32))
					}
					if timed {
						line.Cursor(ui.CursorPointer)
						if line.Hovered() && i != cur {
							line.TextColor(p.text.Alpha(0.7))
						}
						if line.Clicked() {
							a.player.seek(l.Start)
						}
					}
				}
			})
		}
	})
}

package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// The sleep timer stops the music for someone falling asleep: after a time,
// with the sound fading out, or where the song or the album ends.

const (
	sleepTime  = "time"
	sleepSong  = "song"
	sleepAlbum = "album"
)

// sleepFade is how long the sound takes to go.
const sleepFade = 2500 * time.Millisecond

// sleepTimer is what is asked: none, a time, the end of the song or of the
// album.
type sleepTimer struct {
	mode  string
	until time.Time
}

// sleepChoices are the times offered.
var sleepChoices = []time.Duration{
	5 * time.Minute, 15 * time.Minute, 30 * time.Minute, 45 * time.Minute, time.Hour, 2 * time.Hour,
}

// sleepWords writes a time as "15 minutes" or "1 hour".
func sleepWords(d time.Duration) string {
	switch {
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	case d >= time.Hour && d%time.Hour == 0:
		return "1 hour"
	case d >= time.Minute:
		return fmt.Sprintf("%d minutes", int((d + 30*time.Second).Minutes()))
	}
	return "a moment"
}

// setSleep asks for the music to stop: after d for sleepTime, at the end of
// the song or the album for the others, never for "".
func (p *player) setSleep(mode string, d time.Duration) {
	p.sleepGen.Add(1) // a fade that is going stops
	p.sleep = sleepTimer{mode: mode}
	if mode == sleepTime {
		p.sleep.until = time.Now().Add(d)
	}
	p.applyVolume() // a fade the user cancelled gives the sound back
	p.armNext()
}

// sleepStops reports whether the song at next must not follow the one
// playing, for the sleep timer's sake.
func (p *player) sleepStops(next int) bool {
	switch p.sleep.mode {
	case sleepSong:
		return true
	case sleepAlbum:
		cur := p.current()
		return cur == nil || cur.AlbumID == "" || next < 0 || next >= len(p.queue) || p.queue[next].AlbumID != cur.AlbumID
	}
	return false
}

// sleepDue reports whether the time asked for has come.
func (p *player) sleepDue(now time.Time) bool {
	return p.sleep.mode == sleepTime && !now.Before(p.sleep.until)
}

// tickSleep is called every second, whether or not the window draws.
func (p *player) tickSleep(now time.Time) {
	if p.sleepDue(now) {
		p.sleepNow()
	}
}

// sleepNow brings the sound down and pauses it.
func (p *player) sleepNow() {
	p.sleep = sleepTimer{}
	gen := p.sleepGen.Add(1)
	if !p.playing() {
		return
	}
	e := p.engine
	if e == nil || p.far != nil {
		p.toggle()
		p.app.toast("Sleep timer: paused")
		return
	}
	vol := p.app.settings.Volume
	if p.app.settings.Muted {
		vol = 0
	}
	go func() {
		const steps = 25
		for i := 1; i <= steps; i++ {
			if p.sleepGen.Load() != gen {
				return // the user turned it off, or asked for another
			}
			e.SetVolume(vol * (1 - float64(i)/steps))
			time.Sleep(sleepFade / steps)
		}
		p.app.update(func() {
			if p.sleepGen.Load() != gen {
				return
			}
			if p.playing() {
				p.toggle()
			}
			p.applyVolume()
			p.app.toast("Sleep timer: paused")
		})
	}()
}

// sleepEnded is told that the song or the album the timer waited for has
// ended: the music is stopped, as it was asked.
func (p *player) sleepEnded() {
	if p.sleep.mode == sleepSong || p.sleep.mode == sleepAlbum {
		p.sleep = sleepTimer{}
		p.app.toast("Sleep timer: the music has stopped")
	}
}

// sleepText says what the timer is set to, "" for none.
func (p *player) sleepText(now time.Time) string {
	switch p.sleep.mode {
	case sleepTime:
		left := p.sleep.until.Sub(now)
		return "Stops in " + sleepWords(max(left, time.Minute/2))
	case sleepSong:
		return "Stops at the end of the song"
	case sleepAlbum:
		return "Stops at the end of the album"
	}
	return ""
}

// sleepButton is the moon of the player's bar, which asks for the music to
// stop.
func (a *App) sleepButton(c *ui.Context) {
	p := a.player
	text := p.sleepText(time.Now())
	label := "Sleep timer"
	if text != "" {
		label = text
		if p.sleep.mode == sleepTime {
			c.After(10 * time.Second) // the time left, kept near
		}
	}
	b := a.toggleIcon(c, "moon", label, p.sleep.mode != "", 32, 16)
	b.Menu(func(m *ui.Menu) {
		if text != "" {
			m.Item(text).Disabled(true)
			if m.Item("Turn off").Chosen() {
				p.setSleep("", 0)
				a.toast("Sleep timer off")
			}
			m.Separator()
		}
		for _, d := range sleepChoices {
			if m.Item("In " + sleepWords(d)).Chosen() {
				p.setSleep(sleepTime, d)
				a.toast("The music stops in " + sleepWords(d))
			}
		}
		m.Separator()
		if m.Item("At the end of the song").Chosen() {
			p.setSleep(sleepSong, 0)
			a.toast("The music stops at the end of the song")
		}
		if m.Item("At the end of the album").Chosen() {
			p.setSleep(sleepAlbum, 0)
			a.toast("The music stops at the end of the album")
		}
	})
}

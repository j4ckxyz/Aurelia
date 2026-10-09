package main

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/audio"
	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
)

// Aurelia and the other apps of Jellyfin control one another through the
// server. This file is both halves: the devices the user can play on in
// place of this computer, which then shows what they play and passes
// them what it is told; and the orders this computer takes from them.

// remote is what the app knows of the other devices.
type remote struct {
	devices   []jellyfin.Device
	fetching  bool
	fetchedAt time.Time
	// stop ends the listening for orders, of a session that is over.
	stop context.CancelFunc
}

// far is a device that plays in this computer's place. The player's
// queue then mirrors the device's own, once it plays.
type far struct {
	dev jellyfin.Device
	// Where its song was when the server last told, or the user last
	// moved it: it has played on since, unless paused.
	position time.Duration
	at       time.Time
	// told is the position the server last told, to know a new one by.
	told     time.Duration
	volume   int // from 0 to 100, -1 while the device has not told
	muted    bool
	queueKey string // the queue mirrored, to tell when the device's changes
	polling  bool
	polledAt time.Time
	missed   int // times in a row the server did not list it
	// quiet is until when what the server tells is not believed: an
	// order was just sent, and the server has yet to hear of its effect.
	quiet time.Time
	// The volume to send, when the slider rests.
	volumeTimer *time.Timer
}

// window is the songs of the queue a device is given, and told about:
// those around the one playing, and its place among them. A queue of a
// whole library is too long for an address.
func (p *player) window() (ids []string, index int) {
	if p.index < 0 {
		return nil, 0
	}
	from, to := max(0, p.index-20), min(len(p.queue), p.index+200)
	for _, s := range p.queue[from:to] {
		ids = append(ids, s.ID)
	}
	return ids, p.index - from
}

// refreshDevices asks the server which devices there are, unless it
// just did.
func (a *App) refreshDevices() {
	r := &a.remote
	c := a.clientNow()
	if c == nil || r.fetching || time.Since(r.fetchedAt) < 2*time.Second {
		return
	}
	r.fetching = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		devices, err := c.Devices(ctx)
		a.update(func() {
			r.fetching, r.fetchedAt = false, time.Now()
			if err == nil {
				r.devices = devices
			}
		})
	}()
}

// send passes a device an order, off the interface's goroutine, and
// asks the server what came of it.
func (a *App) send(what string, fn func(ctx context.Context, c *jellyfin.Client) error) {
	c := a.clientNow()
	f := a.player.far
	if c == nil || f == nil {
		return
	}
	f.quiet = time.Now().Add(1200 * time.Millisecond)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := fn(ctx, c)
		cancel()
		if err != nil {
			a.update(func() { a.toastError("Could not "+what+" on "+f.dev.Title(), err) })
			return
		}
		time.Sleep(700 * time.Millisecond)
		a.update(a.pollFar)
	}()
}

// playOn has a device play in this computer's place. What plays here
// goes on there, from where it is; a queue that waits here waits on,
// and goes there when it is played.
func (a *App) playOn(dev jellyfin.Device) {
	p := a.player
	if p.far != nil {
		if p.far.dev.ID == dev.ID {
			return
		}
		a.playHere(false)
	}
	st := p.state()
	playing := p.playing()
	p.reportStop()
	if p.engine != nil {
		p.engine.Stop()
	}
	p.track, p.next, p.nextIndex = nil, nil, -1
	p.far = &far{dev: dev, position: st.Position, at: time.Now(), volume: -1}
	p.cold = false
	switch {
	case playing:
		p.farPlay(st.Position)
	case p.current() != nil:
		p.paused = true
	}
	a.pollFar()
	a.toast("Playing on " + dev.Title())
}

// farPlay has the device play the queue from the song playing, at a
// place in it.
func (p *player) farPlay(at time.Duration) {
	f := p.far
	ids, start := p.window()
	if f == nil || len(ids) == 0 {
		return
	}
	p.paused, p.finished = false, false
	f.position, f.at = at, time.Now()
	f.queueKey = strings.Join(ids, ",")
	p.app.send("play", func(ctx context.Context, c *jellyfin.Client) error {
		return c.PlayOn(ctx, f.dev.ID, "PlayNow", ids, start, at)
	})
}

// playHere comes back to this computer: with the device's queue, at its
// place, which plays on here when resume asks and it was playing there.
func (a *App) playHere(resume bool) {
	p := a.player
	f := p.far
	if f == nil {
		return
	}
	st := p.state()
	playing := p.playing() && f.dev.Playing != nil
	if f.volumeTimer != nil {
		f.volumeTimer.Stop()
	}
	if c := a.clientNow(); c != nil && f.dev.Playing != nil {
		id := f.dev.ID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c.Playstate(ctx, id, "Stop", 0)
		}()
	}
	p.far = nil
	p.applyVolume()
	if p.current() == nil {
		return
	}
	p.unshuffled = append([]*library.Song(nil), p.queue...)
	p.cold, p.paused, p.finished, p.resumeAt = true, true, false, st.Position
	if playing && resume {
		p.toggle()
	}
	a.saveQueue()
}

// pollFar asks the server what the device plays, unless it is being
// asked.
func (a *App) pollFar() {
	p := a.player
	f := p.far
	c := a.clientNow()
	if f == nil || c == nil || f.polling {
		return
	}
	f.polling = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		devices, err := c.Devices(ctx)
		a.update(func() {
			f.polling, f.polledAt = false, time.Now()
			if p.far != f || err != nil {
				return // the server did not answer: the next time may
			}
			a.remote.devices, a.remote.fetchedAt = devices, time.Now()
			for i := range devices {
				// A device that was restarted has a new session.
				if devices[i].ID == f.dev.ID || devices[i].DeviceID == f.dev.DeviceID {
					f.missed = 0
					a.mirror(devices[i])
					return
				}
			}
			if f.missed++; f.missed >= 3 {
				name := f.dev.Title()
				a.playHere(false)
				a.toast(name + " is no longer there. Playing on this computer.")
			}
		})
	}()
}

// mirror makes the player show what the device plays.
func (a *App) mirror(dev jellyfin.Device) {
	p := a.player
	f := p.far
	f.dev = dev
	pl := dev.Playing
	if time.Now().Before(f.quiet) {
		return // it has yet to hear of what it was just told
	}
	if pl == nil {
		if f.queueKey != "" {
			// It stopped what it was playing: the queue waits.
			f.queueKey = ""
			p.paused = true
			f.position, f.at = 0, time.Now()
		}
		return
	}
	if key := strings.Join(pl.Queue, ","); key != f.queueKey || len(p.queue) == 0 {
		f.queueKey = key
		queue := make([]*library.Song, 0, len(pl.Queue))
		for _, id := range pl.Queue {
			if s := a.lib.Song(id); s != nil {
				queue = append(queue, s)
			}
		}
		p.queue, p.unshuffled = queue, queue
	}
	// The song playing, by its place in the device's queue if the
	// library has all of it, else by its name.
	p.index = -1
	if pl.Index >= 0 && pl.Index < len(p.queue) && p.queue[pl.Index].ID == pl.Item.ID {
		p.index = pl.Index
	} else {
		for i, s := range p.queue {
			if s.ID == pl.Item.ID {
				p.index = i
				break
			}
		}
	}
	if p.index < 0 {
		// Not a song of the queue, or not one the library has yet.
		s := a.lib.Song(pl.Item.ID)
		if s == nil {
			sg := library.SongOf(&pl.Item)
			library.PrepareSong(&sg)
			s = &sg
		}
		p.queue, p.unshuffled, p.index = []*library.Song{s}, []*library.Song{s}, 0
		f.queueKey = ""
	}
	p.finished = false
	// The server hears of the place every few seconds: between two
	// tellings the song is where it was plus the time since.
	if pl.Position != f.told || pl.Paused != p.paused {
		f.told = pl.Position
		f.position, f.at = pl.Position, time.Now()
		if !pl.Paused {
			// It has played on since it told: for as long as the server
			// says, where its clock and this one agree; else for half
			// the time between two askings, which is the average.
			if age := time.Since(pl.ToldAt); !pl.ToldAt.IsZero() && age >= 0 && age < 20*time.Second {
				f.at = pl.ToldAt
			} else {
				f.at = f.at.Add(-farPoll / 2)
			}
		}
	}
	p.paused = pl.Paused
	if pl.Volume >= 0 && (f.volumeTimer == nil) {
		f.volume, f.muted = pl.Volume, pl.Muted
	}
}

// farPoll is how often the server is asked what the device plays.
const farPoll = 1500 * time.Millisecond

// farState is the state of the device's song, as far as is known.
func (p *player) farState(song *library.Song) audio.State {
	f := p.far
	st := audio.State{TrackID: song.ID, Paused: p.paused, Duration: song.Duration(), Position: f.position, Loaded: 1}
	if !p.paused {
		st.Position += time.Since(f.at)
	}
	if st.Duration > 0 && st.Position > st.Duration {
		st.Position = st.Duration
	}
	return st
}

// volumeNow is the volume the slider shows: the device's while one
// plays, else this computer's.
func (a *App) volumeNow() (v float64, muted bool) {
	if f := a.player.far; f != nil && f.volume >= 0 {
		return float64(f.volume) / 100, f.muted
	}
	return a.settings.Volume, a.settings.Muted
}

// farVolume sets the device's volume, once the slider rests: it moves
// faster than orders travel.
func (a *App) farVolume(v float64) {
	f := a.player.far
	f.volume, f.muted = int(math.Round(v*100)), false
	if f.volumeTimer != nil {
		f.volumeTimer.Stop()
	}
	f.volumeTimer = time.AfterFunc(180*time.Millisecond, func() {
		a.update(func() {
			if a.player.far != f {
				return
			}
			f.volumeTimer = nil
			level := strconv.Itoa(f.volume)
			a.send("set the volume", func(ctx context.Context, c *jellyfin.Client) error {
				return c.Command(ctx, f.dev.ID, "SetVolume", map[string]string{"Volume": level})
			})
		})
	})
}

// playstate sends the device a command of playing.
func (a *App) playstate(command string, at time.Duration) {
	f := a.player.far
	what := map[string]string{"Pause": "pause", "Unpause": "play", "NextTrack": "skip", "PreviousTrack": "go back", "Seek": "seek"}[command]
	a.send(what, func(ctx context.Context, c *jellyfin.Client) error {
		return c.Playstate(ctx, f.dev.ID, command, at)
	})
}

var repeatModes = []string{repeatOff: "RepeatNone", repeatAll: "RepeatAll", repeatOne: "RepeatOne"}

// deviceButton is the button of the player's bar that chooses where the
// music plays.
func (a *App) deviceButton(c *ui.Context) {
	f := a.player.far
	b := a.toggleIcon(c, "monitor-speaker", "Play on another device", f != nil, 32, 16)
	if b.Hovered() {
		a.refreshDevices() // ready by the click
	}
	b.Menu(func(m *ui.Menu) {
		a.refreshDevices()
		if m.Item("This computer").Checked(f == nil).Chosen() && f != nil {
			a.playHere(true)
		}
		m.Separator()
		shown := 0
		for _, dev := range a.remote.devices {
			shown++
			label := dev.Title()
			if dev.Playing != nil && (f == nil || f.dev.ID != dev.ID) {
				label += " — " + dev.Playing.Item.Name
			}
			if m.Item(label).Checked(f != nil && f.dev.ID == dev.ID).Chosen() {
				a.playOn(dev)
			}
		}
		if shown == 0 {
			m.Item("No other devices").Disabled(true)
			m.Item("Open Aurelia or a Jellyfin app elsewhere, signed in as you").Disabled(true)
		}
	})
}

// farStrip tells, above the player's bar, that another device plays.
func (a *App) farStrip(c *ui.Context) {
	f := a.player.far
	if f == nil {
		return
	}
	p := a.pal
	ui.Row(c).Height(farStripH).Shrink(0).Padding(0, 16).Gap(8).Justify(ui.Center).Background(p.accent).Children(func() {
		ui.Icon(c, icon("monitor-speaker")).Size(14, 14).TextColor(p.onAcc)
		ui.Text(c, "Playing on "+f.dev.Title()).FontSize(12).FontWeight(600).TextColor(p.onAcc).SingleLine()
		b := ui.ButtonBase(c).Height(20).Padding(0, 9).Radius(10).Margin(0, 0, 0, 6).Background(p.onAcc.Alpha(0.16)).Cursor(ui.CursorPointer).Label("Play here")
		if b.Hovered() {
			b.Background(p.onAcc.Alpha(0.28))
		}
		b.Children(func() { ui.Text(c, "Play here").FontSize(11).FontWeight(600).TextColor(p.onAcc).SingleLine() })
		if b.Clicked() {
			a.playHere(true)
		}
	})
}

const farStripH = 28

// listen takes the orders of other devices for as long as the session
// lasts, connecting again when the connection is lost.
func (a *App) listen() {
	r := &a.remote
	if r.stop != nil {
		r.stop()
		r.stop = nil
	}
	c := a.clientNow()
	if c == nil || a.silent {
		return
	}
	ctx, stop := context.WithCancel(context.Background())
	r.stop = stop
	go func() {
		wait := 2 * time.Second
		for ctx.Err() == nil {
			began := time.Now()
			c.Listen(ctx, func(o jellyfin.Order) { a.update(func() { a.obey(o) }) })
			if time.Since(began) > time.Minute {
				wait = 2 * time.Second
			}
			select {
			case <-ctx.Done():
			case <-time.After(wait):
			}
			wait = min(2*wait, time.Minute)
		}
	}()
}

// obey does what another device asks of this one.
func (a *App) obey(o jellyfin.Order) {
	p := a.player
	if p.far != nil {
		// It plays elsewhere itself: an order to play here brings it back.
		if o.Kind != "Play" {
			return
		}
		a.playHere(false)
	}
	switch o.Kind {
	case "Play":
		a.songsOf(o.ItemIDs, func(songs []*library.Song) {
			if len(songs) == 0 {
				return
			}
			switch o.PlayCommand {
			case "PlayNext":
				p.playNext(songs)
			case "PlayLast":
				p.enqueue(songs)
			case "PlayShuffle":
				p.playShuffled(songs)
			default:
				// In the order given, whatever this computer's setting.
				shuffled := a.settings.Shuffle
				a.settings.Shuffle = false
				p.play(songs, o.StartIndex)
				a.settings.Shuffle = shuffled
				if o.Position > 0 {
					p.seek(o.Position)
				}
			}
		})
	case "Playstate":
		switch o.Command {
		case "Stop":
			p.reportStop()
			if p.engine != nil {
				p.engine.Stop()
			}
			p.finished = true
		case "Pause":
			if p.playing() {
				p.toggle()
			}
		case "Unpause":
			if !p.playing() {
				p.toggle()
			}
		case "PlayPause":
			p.toggle()
		case "NextTrack":
			p.skip()
		case "PreviousTrack":
			p.previous()
		case "Seek":
			p.seek(o.Position)
		case "Rewind":
			a.seekBy(-10 * time.Second)
		case "FastForward":
			a.seekBy(10 * time.Second)
		}
	case "GeneralCommand":
		switch o.Command {
		case "SetVolume":
			if v, err := strconv.ParseFloat(o.Arguments["Volume"], 64); err == nil {
				a.setVolume(v / 100)
			}
		case "VolumeUp":
			a.setVolume(a.settings.Volume + 0.05)
		case "VolumeDown":
			a.setVolume(a.settings.Volume - 0.05)
		case "Mute", "Unmute", "ToggleMute":
			a.settings.Muted = o.Command == "Mute" || o.Command == "ToggleMute" && !a.settings.Muted
			p.applyVolume()
			a.saveSettings()
		case "SetRepeatMode":
			for mode, name := range repeatModes {
				if name == o.Arguments["RepeatMode"] {
					a.settings.Repeat = mode
					a.saveSettings()
					p.next, p.nextIndex = nil, -1
					p.armNext()
				}
			}
		case "SetShuffleQueue":
			p.setShuffle(o.Arguments["ShuffleMode"] == "Shuffle")
		case "DisplayMessage":
			if text := strings.TrimSpace(o.Arguments["Header"] + " " + o.Arguments["Text"]); text != "" {
				a.toast(text)
			}
		}
	}
	// The device that asked shows what came of it.
	p.reportProgress(true)
}

// songsOf finds the songs of what another device names: songs, or
// albums, artists and playlists, which stand for theirs.
func (a *App) songsOf(ids []string, then func([]*library.Song)) {
	var songs []*library.Song
	for _, id := range ids {
		switch {
		case a.lib.Song(id) != nil:
			songs = append(songs, a.lib.Song(id))
		case a.lib.Album(id) != nil:
			songs = append(songs, a.lib.AlbumSongs(id)...)
		case a.lib.Artist(id) != nil:
			songs = append(songs, a.artistSongs(id)...)
		case a.lib.Playlist(id) != nil && len(ids) == 1:
			// Its songs are the server's to tell.
			a.fetchPlaylist(id, then)
			return
		}
	}
	then(songs)
}

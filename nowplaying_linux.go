package main

import (
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// nowPlaying tells the desktop what plays, as MPRIS has players do: a
// service on the session's D-Bus that GNOME, KDE and the others read to
// show the song, its picture and its buttons, and to which they send the
// keyboard's media keys.
type nowPlaying struct {
	app   *App
	conn  *dbus.Conn
	props *prop.Properties
	ok    bool
	last  playing
}

const (
	mprisName   = "org.mpris.MediaPlayer2.aurelia"
	mprisPath   = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisRoot   = "org.mpris.MediaPlayer2"
	mprisPlayer = "org.mpris.MediaPlayer2.Player"
)

// mprisApp is org.mpris.MediaPlayer2: the app itself.
type mprisApp struct{ a *App }

func (m mprisApp) Raise() *dbus.Error {
	m.a.update(func() { m.a.show() })
	return nil
}

func (m mprisApp) Quit() *dbus.Error {
	go mygo.App.Quit()
	return nil
}

// mprisControls is org.mpris.MediaPlayer2.Player: what plays, and its
// buttons. D-Bus calls it on its own goroutines.
type mprisControls struct{ a *App }

func (m mprisControls) do(fn func(p *player)) *dbus.Error {
	m.a.update(func() { fn(m.a.player) })
	return nil
}

func (m mprisControls) Next() *dbus.Error     { return m.do(func(p *player) { p.skip() }) }
func (m mprisControls) Previous() *dbus.Error { return m.do(func(p *player) { p.previous() }) }
func (m mprisControls) PlayPause() *dbus.Error {
	return m.do(func(p *player) { p.toggle() })
}

func (m mprisControls) Play() *dbus.Error {
	return m.do(func(p *player) {
		if !p.playing() {
			p.toggle()
		}
	})
}

func (m mprisControls) Pause() *dbus.Error {
	return m.do(func(p *player) {
		if p.playing() {
			p.toggle()
		}
	})
}

func (m mprisControls) Stop() *dbus.Error { return m.Pause() }

// SeekBy is MPRIS's Seek, under a name that Go's tools do not take for
// io.Seeker's: it moves by offset microseconds.
func (m mprisControls) SeekBy(offset int64) *dbus.Error {
	return m.do(func(p *player) {
		p.seek(p.state().Position + time.Duration(offset)*time.Microsecond)
	})
}

// SetPosition moves to a place in the track named, in microseconds.
func (m mprisControls) SetPosition(track dbus.ObjectPath, position int64) *dbus.Error {
	return m.do(func(p *player) {
		if s := p.current(); s != nil && trackPath(s.ID) == track {
			p.seek(time.Duration(position) * time.Microsecond)
		}
	})
}

func (m mprisControls) OpenUri(string) *dbus.Error { return nil }

// trackPath names a song as MPRIS names tracks.
func trackPath(id string) dbus.ObjectPath {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, id)
	if clean == "" {
		return "/org/mpris/MediaPlayer2/TrackList/NoTrack"
	}
	return dbus.ObjectPath("/dev/aurelia/track/" + clean)
}

// init takes the player's name on the session bus. Without a bus, as on
// a system without a desktop, there is nobody to tell.
func (n *nowPlaying) init(a *App) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	n.app, n.conn = a, conn
	reply, err := conn.RequestName(mprisName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return
	}
	conn.Export(mprisApp{a}, mprisPath, mprisRoot)
	conn.ExportWithMap(mprisControls{a}, map[string]string{"SeekBy": "Seek"}, mprisPath, mprisPlayer)
	set := func(apply func(v any)) func(*prop.Change) *dbus.Error {
		return func(c *prop.Change) *dbus.Error {
			v := c.Value
			a.update(func() { apply(v) })
			return nil
		}
	}
	constant := func(v any) *prop.Prop { return &prop.Prop{Value: v, Emit: prop.EmitTrue} }
	n.props, err = prop.Export(conn, mprisPath, prop.Map{
		mprisRoot: {
			"CanQuit":             constant(true),
			"CanRaise":            constant(true),
			"HasTrackList":        constant(false),
			"Identity":            constant("Aurelia"),
			"DesktopEntry":        constant("aurelia"),
			"SupportedUriSchemes": constant([]string{}),
			"SupportedMimeTypes":  constant([]string{}),
		},
		mprisPlayer: {
			"PlaybackStatus": constant("Stopped"),
			"Metadata":       constant(map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(trackPath(""))}),
			"Rate":           constant(1.0),
			"MinimumRate":    constant(1.0),
			"MaximumRate":    constant(1.0),
			// The position is asked for, not announced: it changes all
			// the time. A seek is announced, with Seeked.
			"Position":      {Value: int64(0), Emit: prop.EmitFalse},
			"CanGoNext":     constant(true),
			"CanGoPrevious": constant(true),
			"CanPlay":       constant(true),
			"CanPause":      constant(true),
			"CanSeek":       constant(true),
			"CanControl":    {Value: true, Emit: prop.EmitFalse},
			"Volume": {Value: a.settings.Volume, Writable: true, Emit: prop.EmitTrue, Callback: set(func(v any) {
				if f, ok := v.(float64); ok {
					a.setVolume(f)
				}
			})},
			"Shuffle": {Value: a.settings.Shuffle, Writable: true, Emit: prop.EmitTrue, Callback: set(func(v any) {
				if on, ok := v.(bool); ok && on != a.settings.Shuffle {
					a.player.setShuffle(on)
				}
			})},
			"LoopStatus": {Value: loopStatus(a.settings.Repeat), Writable: true, Emit: prop.EmitTrue, Callback: set(func(v any) {
				if s, ok := v.(string); ok {
					for a.settings.Repeat != loopOf(s) {
						a.player.cycleRepeat()
					}
				}
			})},
		},
	})
	if err != nil {
		conn.Close()
		return
	}
	// What tools that list a service's methods read.
	controls := introspect.Methods(mprisControls{a})
	for i := range controls {
		if controls[i].Name == "SeekBy" {
			controls[i].Name = "Seek"
		}
	}
	node := &introspect.Node{
		Name: string(mprisPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{Name: mprisRoot, Methods: introspect.Methods(mprisApp{a}), Properties: n.props.Introspection(mprisRoot)},
			{Name: mprisPlayer, Methods: controls, Properties: n.props.Introspection(mprisPlayer),
				Signals: []introspect.Signal{{Name: "Seeked", Args: []introspect.Arg{{Name: "Position", Type: "x", Direction: "out"}}}}},
		},
	}
	conn.Export(introspect.NewIntrospectable(node), mprisPath, "org.freedesktop.DBus.Introspectable")
	n.ok = true
}

func loopStatus(repeat int) string {
	switch repeat {
	case repeatAll:
		return "Playlist"
	case repeatOne:
		return "Track"
	}
	return "None"
}

func loopOf(status string) int {
	switch status {
	case "Playlist":
		return repeatAll
	case "Track":
		return repeatOne
	}
	return repeatOff
}

// set tells the desktop what plays; an empty title is nothing playing.
func (n *nowPlaying) set(p playing) {
	if !n.ok {
		return
	}
	status := "Playing"
	switch {
	case p.title == "":
		status = "Stopped"
	case p.paused:
		status = "Paused"
	}
	meta := map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(trackPath(p.id))}
	if p.title != "" {
		meta["xesam:title"] = dbus.MakeVariant(p.title)
		meta["xesam:artist"] = dbus.MakeVariant([]string{p.artist})
		meta["xesam:album"] = dbus.MakeVariant(p.album)
		meta["mpris:length"] = dbus.MakeVariant(p.duration.Microseconds())
		// The picture: its file, which shows without the network, else
		// where the server has it.
		switch {
		case p.art != "":
			meta["mpris:artUrl"] = dbus.MakeVariant((&url.URL{Scheme: "file", Path: p.art}).String())
		case p.artURL != "":
			meta["mpris:artUrl"] = dbus.MakeVariant(p.artURL)
		}
	}
	// A place other than a clock would have it at is a seek.
	sought := n.last.id == p.id && p.id != ""
	n.last = p
	n.props.SetMust(mprisPlayer, "Position", p.position.Microseconds())
	n.props.SetMust(mprisPlayer, "Metadata", meta)
	n.props.SetMust(mprisPlayer, "PlaybackStatus", status)
	a := n.app
	n.props.SetMust(mprisPlayer, "Volume", a.settings.Volume)
	n.props.SetMust(mprisPlayer, "Shuffle", a.settings.Shuffle)
	n.props.SetMust(mprisPlayer, "LoopStatus", loopStatus(a.settings.Repeat))
	if sought {
		n.conn.Emit(mprisPath, mprisPlayer+".Seeked", p.position.Microseconds())
	}
}

// progress keeps the place in the song for whoever asks.
func (n *nowPlaying) progress(position time.Duration) {
	if n.ok {
		n.props.SetMust(mprisPlayer, "Position", position.Microseconds())
	}
}

func (n *nowPlaying) describe() string {
	if !n.ok {
		return "not on the session bus"
	}
	return "MPRIS as " + mprisName
}

// handlesKeys reports whether the desktop sends the media keys through
// this: it does, to the players on the bus.
func (n *nowPlaying) handlesKeys() bool { return n.ok }

package main

import (
	"log"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// nowPlaying tells macOS what plays: the system then shows it in Control
// Center and on the lock screen, and sends the app the keyboard's play,
// next and previous keys, and those of headphones. It talks to the
// MediaPlayer framework through the Objective-C runtime, without cgo.
type nowPlaying struct {
	center                                         objc.ID
	title, artist, album, duration, elapsed, speed objc.ID
	ok                                             bool
}

func sel(name string) objc.SEL { return objc.RegisterName(name) }

// init registers the app for the system's commands. It runs on the main
// thread, as everything of nowPlaying does.
func (n *nowPlaying) init(a *App) {
	lib, err := purego.Dlopen("/System/Library/Frameworks/MediaPlayer.framework/MediaPlayer", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		log.Print("now playing: ", err)
		return
	}
	// The keys of the dictionary are strings the framework exports.
	constant := func(name string) objc.ID {
		p, err := purego.Dlsym(lib, name)
		if err != nil || p == 0 {
			return 0
		}
		return **(**objc.ID)(unsafe.Pointer(&p))
	}
	n.title, n.artist, n.album = constant("MPMediaItemPropertyTitle"), constant("MPMediaItemPropertyArtist"), constant("MPMediaItemPropertyAlbumTitle")
	n.duration, n.elapsed, n.speed = constant("MPMediaItemPropertyPlaybackDuration"), constant("MPNowPlayingInfoPropertyElapsedPlaybackTime"), constant("MPNowPlayingInfoPropertyPlaybackRate")
	infoClass, commandClass := objc.GetClass("MPNowPlayingInfoCenter"), objc.GetClass("MPRemoteCommandCenter")
	if infoClass == 0 || commandClass == 0 || n.title == 0 || n.elapsed == 0 {
		return
	}
	n.center = objc.ID(infoClass).Send(sel("defaultCenter"))
	commands := objc.ID(commandClass).Send(sel("sharedCommandCenter"))
	const success = 0 // MPRemoteCommandHandlerStatusSuccess
	on := func(command string, fn func(event objc.ID)) {
		cmd := commands.Send(sel(command))
		if cmd == 0 {
			return
		}
		cmd.Send(sel("setEnabled:"), true)
		// The block lives as long as the app: the command keeps it.
		cmd.Send(sel("addTargetWithHandler:"), objc.NewBlock(func(_ objc.Block, event objc.ID) int {
			fn(event)
			return success
		}))
	}
	do := func(fn func()) func(objc.ID) {
		return func(objc.ID) { a.update(fn) }
	}
	on("togglePlayPauseCommand", do(func() { a.player.toggle() }))
	on("playCommand", do(func() {
		if !a.player.playing() {
			a.player.toggle()
		}
	}))
	on("pauseCommand", do(func() {
		if a.player.playing() {
			a.player.toggle()
		}
	}))
	on("nextTrackCommand", do(func() { a.player.skip() }))
	on("previousTrackCommand", do(func() { a.player.previous() }))
	on("changePlaybackPositionCommand", func(event objc.ID) {
		at := objc.Send[float64](event, sel("positionTime"))
		a.update(func() { a.player.seek(seconds(at)) })
	})
	n.ok = true
}

// set tells the system what plays, from where, and whether it is paused;
// an empty title is nothing playing.
func (n *nowPlaying) set(title, artist, album string, duration, position float64, paused bool) {
	if !n.ok {
		return
	}
	const playing, pausedState, stopped = 1, 2, 3 // MPNowPlayingPlaybackState
	if title == "" {
		n.center.Send(sel("setNowPlayingInfo:"), objc.ID(0))
		n.center.Send(sel("setPlaybackState:"), stopped)
		return
	}
	str := func(s string) objc.ID {
		return objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), s)
	}
	num := func(v float64) objc.ID {
		return objc.Send[objc.ID](objc.ID(objc.GetClass("NSNumber")), sel("numberWithDouble:"), v)
	}
	info := objc.ID(objc.GetClass("NSMutableDictionary")).Send(sel("dictionary"))
	put := func(key, value objc.ID) {
		if key != 0 && value != 0 {
			info.Send(sel("setObject:forKey:"), value, key)
		}
	}
	put(n.title, str(title))
	put(n.artist, str(artist))
	put(n.album, str(album))
	put(n.duration, num(duration))
	put(n.elapsed, num(position))
	rate, state := 1.0, playing
	if paused {
		rate, state = 0, pausedState
	}
	put(n.speed, num(rate))
	n.center.Send(sel("setNowPlayingInfo:"), info)
	n.center.Send(sel("setPlaybackState:"), state)
}

// describe returns what the system holds, for tests of the bridge.
func (n *nowPlaying) describe() string {
	if !n.ok {
		return ""
	}
	info := n.center.Send(sel("nowPlayingInfo"))
	if info == 0 {
		return "nothing"
	}
	desc := info.Send(sel("description"))
	p := objc.Send[*byte](desc, sel("UTF8String"))
	if p == nil {
		return ""
	}
	var b []byte
	for ; *p != 0; p = (*byte)(unsafe.Add(unsafe.Pointer(p), 1)) {
		b = append(b, *p)
	}
	return string(b)
}

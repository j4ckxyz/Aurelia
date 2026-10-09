package main

import (
	"fmt"
	"log"
	"path/filepath"
	"sync/atomic"
	"time"
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
	artworkKey                                     objc.ID
	ok                                             bool

	// The picture of what plays: the file it was made of, the artwork
	// the system is given, and the image the artwork's block returns,
	// which the system asks for on a thread of its own.
	artFile string
	artwork objc.ID
	image   atomic.Uintptr
	block   objc.Block
}

// cgSize is CGSize.
type cgSize struct{ W, H float64 }

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
	n.artworkKey = constant("MPMediaItemPropertyArtwork")
	// One block for every artwork: it returns the picture of what plays
	// now, at whatever size it is asked for. Blocks are never freed.
	n.block = objc.NewBlock(func(_ objc.Block, width, height float64) objc.ID {
		return objc.ID(n.image.Load())
	})
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
func (n *nowPlaying) set(p playing) {
	if !n.ok {
		return
	}
	const playingState, pausedState, stopped = 1, 2, 3 // MPNowPlayingPlaybackState
	if p.title == "" {
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
	put(n.title, str(p.title))
	put(n.artist, str(p.artist))
	put(n.album, str(p.album))
	put(n.duration, num(p.duration.Seconds()))
	put(n.elapsed, num(p.position.Seconds()))
	rate, state := 1.0, playingState
	if p.paused {
		rate, state = 0, pausedState
	}
	put(n.speed, num(rate))
	put(n.artworkKey, n.cover(p.art, str))
	n.center.Send(sel("setNowPlayingInfo:"), info)
	n.center.Send(sel("setPlaybackState:"), state)
}

// cover returns the artwork of the picture in file, 0 for none: made
// once for a file, and kept until another takes its place.
func (n *nowPlaying) cover(file string, str func(string) objc.ID) objc.ID {
	if file == n.artFile {
		return n.artwork
	}
	old, oldImage := n.artwork, objc.ID(n.image.Load())
	n.artFile, n.artwork = file, 0
	if file != "" && n.artworkKey != 0 {
		image := objc.ID(objc.GetClass("NSImage")).Send(sel("alloc")).Send(sel("initWithContentsOfFile:"), str(file))
		if image != 0 {
			n.image.Store(uintptr(image))
			art := objc.ID(objc.GetClass("MPMediaItemArtwork")).Send(sel("alloc"))
			n.artwork = objc.Send[objc.ID](art, sel("initWithBoundsSize:requestHandler:"), cgSize{coverPixels, coverPixels}, n.block)
		}
	}
	if n.artwork == 0 {
		n.image.Store(0)
	}
	// What the system still holds of the picture before, it holds on
	// its own account.
	if old != 0 {
		old.Send(sel("release"))
	}
	if oldImage != 0 && uintptr(oldImage) != n.image.Load() {
		oldImage.Send(sel("release"))
	}
	return n.artwork
}

// describe returns what the system holds, for tests of the bridge.
func (n *nowPlaying) describe() string {
	if !n.ok {
		return "not registered with the system"
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
	out := string(b)
	// The picture, as the system gets it when it asks for one of a size.
	if art := info.Send(sel("objectForKey:"), n.artworkKey); art != 0 {
		image := objc.Send[objc.ID](art, sel("imageWithSize:"), cgSize{200, 200})
		if image != 0 {
			size := objc.Send[cgSize](image, sel("size"))
			out += fmt.Sprintf(" artwork=%vx%v from %s", size.W, size.H, filepath.Base(n.artFile))
		} else {
			out += " artwork=none"
		}
	}
	return out
}

// progress has nothing to do: the system counts on from the place and the
// rate it was told.
func (n *nowPlaying) progress(position time.Duration) {}

// handlesKeys reports whether the system sends the app the media keys
// through this, so that the app need not take them otherwise.
func (n *nowPlaying) handlesKeys() bool { return n.ok }

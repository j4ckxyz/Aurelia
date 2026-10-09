package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// debugHook lets a script drive the app while AURELIA_DEBUG names a
// directory: each line of its file "do" is run, as "go /albums" or "shot
// albums.png", which captures the window there.
func (a *App) debugHook() {
	dir := os.Getenv("AURELIA_DEBUG")
	if dir == "" {
		return
	}
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	go func() {
		for range time.Tick(100 * time.Millisecond) {
			req := filepath.Join(dir, "do")
			b, err := os.ReadFile(req)
			if err != nil {
				continue
			}
			os.Remove(req)
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				a.debug(dir, strings.TrimSpace(line))
			}
			os.WriteFile(filepath.Join(dir, "done"), []byte(time.Now().String()), 0o644)
		}
	}()
}

func (a *App) debug(dir, line string) {
	verb, arg, _ := strings.Cut(line, " ")
	win := a.win
	if win == nil {
		return
	}
	// do runs fn on the interface's goroutine and waits for it.
	do := func(fn func()) {
		done := make(chan struct{})
		win.Update(func() { fn(); close(done) })
		<-done
	}
	switch verb {
	case "shot":
		time.Sleep(150 * time.Millisecond)
		// "shot mini.png" of a name beginning with mini is of the small
		// window of the record.
		from := win
		if strings.HasPrefix(arg, "mini") && a.mini != nil {
			from = a.mini
		}
		png, err := from.CapturePage()
		if err != nil {
			log.Print("shot: ", err)
			return
		}
		os.WriteFile(filepath.Join(dir, arg), png, 0o644)
	case "minisize":
		w, h, _ := strings.Cut(arg, " ")
		wi, _ := strconv.Atoi(w)
		hi, _ := strconv.Atoi(h)
		if a.mini != nil {
			a.mini.SetSize(wi, hi)
		}
	case "sleep":
		d, _ := time.ParseDuration(arg)
		time.Sleep(d)
	case "go":
		do(func() { a.goTo(arg) })
	case "back":
		do(func() { a.router.Back() })
	case "forward":
		do(func() { a.router.Forward() })
	case "size":
		w, h, _ := strings.Cut(arg, " ")
		wi, _ := strconv.Atoi(w)
		hi, _ := strconv.Atoi(h)
		win.SetSize(wi, hi)
	case "theme":
		do(func() { a.settings.Theme = arg })
	case "search":
		do(func() {
			a.search.query = arg
			a.router.Push("/search?q=" + strings.ReplaceAll(arg, " ", "+"))
		})
	case "queue":
		do(func() { a.settings.QueueOpen = arg == "on" })
	case "play":
		// "play album <n>" plays the nth album by name; "play" toggles.
		do(func() {
			if arg == "lyrics" {
				// The first song that has lyrics, with its album after it.
				for _, sg := range a.songsBy("Title") {
					if sg.HasLyrics {
						songs := a.lib.AlbumSongs(sg.AlbumID)
						for i, o := range songs {
							if o == sg {
								a.player.play(songs, i)
								return
							}
						}
						a.player.play([]*library.Song{sg}, 0)
						return
					}
				}
				return
			}
			if strings.HasPrefix(arg, "album") {
				n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(arg, "album")))
				if albums := a.albumsBy("Name"); n < len(albums) {
					a.playAlbum(albums[n], false)
				}
				return
			}
			a.player.toggle()
		})
	case "download":
		// "download album <n>" downloads the nth album by name.
		do(func() {
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(arg, "album")))
			if albums := a.albumsBy("Name"); n < len(albums) {
				a.downloads.addAlbum(albums[n], a.lib.AlbumSongs(albums[n].ID))
				a.goTo("/album/" + albums[n].ID)
			}
		})
	case "next":
		do(func() { a.player.skip() })
	case "seek":
		d, _ := time.ParseDuration(arg)
		do(func() { a.player.seek(d) })
	case "volume":
		v, _ := strconv.ParseFloat(arg, 64)
		do(func() { a.setVolume(v) })
	case "import":
		do(func() { a.importThemeFiles([]string{arg}) })
	case "state":
		// What the app holds, for a script to read.
		var out string
		do(func() {
			st := a.player.state()
			name := ""
			if s := a.player.current(); s != nil {
				name = s.Name
			}
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			out = fmt.Sprintf("route=%s\nalbums=%d artists=%d songs=%d playlists=%d syncing=%v syncErr=%q\nsong=%q position=%v duration=%v paused=%v buffering=%v loaded=%.2f queue=%d\nimages=%d imageBytes=%d heap=%d sys=%d\n",
				a.router.Location(), len(a.lib.Albums), len(a.lib.Artists), len(a.lib.Songs), len(a.lib.Playlists), a.syncing, a.syncErr,
				name, st.Position.Round(time.Millisecond), st.Duration.Round(time.Second), st.Paused, st.Buffering, st.Loaded, len(a.player.queue),
				len(a.images.mem), a.images.memBytes, m.HeapAlloc, m.Sys)
			out += fmt.Sprintf("downloads: %d songs asked, %d here, %d to go, %d failed, offline=%v\n", len(a.downloads.songs), len(a.downloads.done), len(a.downloads.order)+len(a.downloads.active), len(a.downloads.failed), a.offline)
			u := &a.updates
			offered := ""
			if u.offer && u.available != nil {
				offered = u.available.Version
			}
			out += fmt.Sprintf("version=%s update: checking=%v installing=%v ready=%q upToDate=%v err=%q offered=%q signedIn=%v\n", appVersion(), u.checking, u.installing, u.ready, u.upToDate, u.err, offered, a.signedIn())
			on := "this computer"
			if f := a.player.far; f != nil {
				on = fmt.Sprintf("%q volume=%d muted=%v", f.dev.Title(), f.volume, f.muted)
			}
			out += fmt.Sprintf("plays on: %s; volume here=%.2f muted=%v shuffle=%v repeat=%d; devices:", on, a.settings.Volume, a.settings.Muted, a.settings.Shuffle, a.settings.Repeat)
			for _, dev := range a.remote.devices {
				what := "nothing"
				if dev.Playing != nil {
					what = fmt.Sprintf("%q at %v paused=%v queue=%d", dev.Playing.Item.Name, dev.Playing.Position.Round(time.Second), dev.Playing.Paused, len(dev.Playing.Queue))
				}
				out += fmt.Sprintf(" [%s device=%.6s plays %s]", dev.Title(), dev.DeviceID, what)
			}
			out += "\n"
			shown := a.settings.Proxy
			if u, _, err := parseProxy(shown); err == nil && u != nil {
				shown = u.Redacted() // not its password, in a log
			}
			out += fmt.Sprintf("proxy=%q %s %s\n", shown, a.connection.status, a.connection.err)
			out += "system: " + strings.Join(strings.Fields(a.system.describe()), " ") + " (told " + a.told + ")\n"
		})
		os.WriteFile(filepath.Join(dir, "state"), []byte(out), 0o644)
	case "scroll":
		// "scroll albums 200" scrolls a long list row by row, a frame
		// each, for MYGO_FRAME_STATS to time real frames.
		name, count, _ := strings.Cut(arg, " ")
		n, _ := strconv.Atoi(count)
		state := map[string]*ui.ListState{"albums": &a.pages.albumList, "artists": &a.pages.artistsList, "songs": &a.pages.songList}[name]
		if state == nil {
			return
		}
		do(func() { a.goTo("/" + name) })
		time.Sleep(300 * time.Millisecond)
		log.Printf("scroll %s begins", name)
		for i := 0; i < n; i++ {
			do(func() { state.ScrollTo(i, ui.Start) })
			time.Sleep(9 * time.Millisecond)
		}
		log.Printf("scroll %s ends", name)
	case "devices":
		// "devices" asks the server for the devices to play on.
		do(func() { a.remote.fetchedAt = time.Time{}; a.refreshDevices() })
	case "playon":
		// "playon Aurelia" plays on the first device whose name has that
		// in it; "playon" comes back to this computer.
		do(func() {
			if arg == "" {
				a.playHere(true)
				return
			}
			for _, dev := range a.remote.devices {
				if strings.Contains(strings.ToLower(dev.Title()+" "+dev.DeviceID), strings.ToLower(arg)) {
					a.playOn(dev)
					return
				}
			}
		})
	case "do":
		// "do next" runs a command of the keyboard by its name.
		do(func() {
			for i := range commands {
				if commands[i].id == arg {
					a.do(&commands[i])
				}
			}
		})
	case "proxy":
		// "proxy socks5://host:1080" reaches the server through a proxy,
		// "proxy" through none.
		do(func() {
			a.connection.proxy = arg
			a.applyProxy()
		})
	case "update":
		// "update" checks as Settings does; "update accept" and "update
		// decline" answer the page that offers a version.
		do(func() {
			switch arg {
			case "accept":
				a.acceptUpdate()
			case "decline":
				a.declineUpdate()
			case "preview":
				// The page, with a version made up, to look at.
				a.updates.available = &mygo.Update{Version: "9.9.9", Notes: "## 9.9.9\n\n- Keyboard shortcuts for everything, with a page that lists them.\n- A full screen with the record turning and the lyrics\n  following the song.\n- Play on another device, and control it from here.\n"}
				a.updates.offer = true
			default:
				a.checkForUpdates(checkAsked)
			}
		})
	case "restart":
		do(func() { a.restart() })
	case "heap":
		// A profile of what the heap holds, for go tool pprof.
		runtime.GC()
		if f, err := os.Create(filepath.Join(dir, arg)); err == nil {
			pprof.WriteHeapProfile(f)
			f.Close()
		}
	case "quit":
		mygo.App.Quit()
	}
}

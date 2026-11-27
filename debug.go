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

	"aurelia/internal/jam"
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
	case "ask":
		// "ask Remove all downloads?": the question, as a thing that
		// cannot be undone asks it; the answer is logged.
		do(func() {
			a.ask(promptState{title: arg, text: "Asked by the debug hook.", confirm: "Remove All", danger: true,
				run: func(string) { log.Print("debug: the question was answered yes") }})
		})
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
			out += fmt.Sprintf("output: chosen=%q (%s) on=%q known=%d\n", a.settings.OutputDevice, a.settings.OutputName, a.output.on, len(a.output.devices))
			out += "system: " + strings.Join(strings.Fields(a.system.describe()), " ") + " (told " + a.told + ")\n"
		})
		os.WriteFile(filepath.Join(dir, "state"), []byte(out), 0o644)
	case "jam":
		// "jam start", "jam join <invite or code>", "jam ask <address>"
		// (joins without the code), "jam leave", "jam end",
		// "jam open", "jam nearby" (joins the first Jam found), "jam allow"
		// (lets in the first who asks), "jam add <song name>" (to the
		// queue, as a guest asks), and "jam state", written to the file
		// jam.
		verb, rest, _ := strings.Cut(arg, " ")
		var out string
		do(func() {
			switch verb {
			case "start":
				a.settings.Jam = true
				a.startJam()
			case "join":
				a.settings.Jam = true
				if t, ok := jam.ParseTarget(rest); ok {
					a.joinTarget(t)
				}
			case "relay":
				// "jam relay <url> [key]": the user's own relay, as Settings
				// sets it; "jam relay" alone, none: the public one, unless
				// "jam relay off".
				base, key, _ := strings.Cut(rest, " ")
				a.settings.JamPublicRelayOff = base == "off"
				if base == "off" {
					base = ""
				}
				a.settings.JamRelay, a.settings.JamRelayKey = base, key
				a.jamRelay()
			case "ask":
				// Joins the Jam at an address, without the code: its host
				// is asked, or over a tailnet, lets in a device of its own.
				a.settings.Jam = true
				a.joinJam(rest, "")
			case "nearby":
				// Joins the first Jam found nearby, asking its host.
				if len(a.jam.nearby) > 0 {
					a.joinJam(a.jam.nearby[0].Addr, "")
				}
			case "allow":
				if len(a.jam.requests) > 0 {
					a.answerJam(a.jam.requests[0].ID, true)
				}
			case "leave", "end":
				a.stopJam()
			case "open":
				a.openJam()
			case "add":
				for i := range a.lib.Songs {
					if s := &a.lib.Songs[i]; strings.EqualFold(s.Name, rest) {
						a.player.enqueue([]*library.Song{a.lib.Song(s.ID)})
					}
				}
			case "state":
				j := &a.jam
				role := "none"
				switch {
				case j.host != nil:
					role = "host code=" + j.host.Code() + " invite=" + a.jamInvite() + " relay=" + j.relayAddr + " relayErr=" + strconv.Quote(j.relayErr)
				case j.guest != nil:
					role = "guest of " + j.hostName + " at=" + j.guest.Addr()
				}
				st := a.player.state()
				out = fmt.Sprintf("role=%s away=%v joining=%v waiting=%v err=%q nearby=%d looking=%v members=%d requests=%d open=%v\n", role, j.away, j.joining, j.waiting, j.joinErr, len(j.nearby), j.looking != nil, len(j.members), len(j.requests), j.open)
				for _, m := range j.members {
					out += fmt.Sprintf("member %q away=%v host=%v peer=%+v\n", m.Name, m.Away, m.Host, m.Peer)
				}
				out += fmt.Sprintf("now=%d position=%dms paused=%v buffering=%v index=%d queue=%d\n", time.Now().UnixMilli(), st.Position.Milliseconds(), st.Paused, st.Buffering, a.player.index, len(a.player.queue))
				if j.guest != nil && j.state != nil {
					// Where the host is, by its clock: what the guest follows.
					hp := time.Duration(j.state.Position) * time.Millisecond
					if j.state.Playing {
						hp += j.guest.HostNow().Sub(time.UnixMilli(j.state.At))
					}
					out += fmt.Sprintf("host index=%d position=%dms playing=%v off=%dms lag=%dms clock=%dms catchUp=%dms seekCost=%dms\n", j.state.Index, hp.Milliseconds(), j.state.Playing, (st.Position - hp).Milliseconds(), j.stateLag.Milliseconds(), time.Until(j.guest.HostNow()).Milliseconds(), j.catchUp.Milliseconds(), j.seekCost.Milliseconds())
				}
				for i, s := range a.player.queue {
					mark := " "
					if i == a.player.index {
						mark = ">"
					}
					out += fmt.Sprintf("%s %s %q foreign=%v by=%q\n", mark, a.songKey(s), s.Name, a.foreign(s), j.by[s])
				}
			}
		})
		if verb == "state" {
			os.WriteFile(filepath.Join(dir, "jam"), []byte(out), 0o644)
		}
	case "tail":
		// "tail state", written to the file tail; "tail invite <device>",
		// "tail join <device>" (the Jam it holds), "tail accept" (the
		// first invite come), "tail copy" (the invite link, into the file
		// tail too).
		verb, rest, _ := strings.Cut(arg, " ")
		var out string
		do(func() {
			t := &a.tail
			pick := func() *tailDevice {
				for _, d := range a.tailDevices() {
					if strings.EqualFold(d.Name, rest) || d.ID == rest {
						return &d
					}
				}
				return nil
			}
			switch verb {
			case "invite":
				if d := pick(); d != nil {
					a.tailInvite(d.Device)
				}
			case "join":
				if d := pick(); d != nil {
					a.tailJoin(*d)
				}
			case "accept":
				if len(t.invites) > 0 {
					a.answerInvite(t.invites[0].id, true)
				}
			case "copy":
				out = a.tailInviteLink() + "\n"
			case "state":
				out = fmt.Sprintf("known=%v running=%v state=%s self=%q ips=%v door=%v doorErr=%q probing=%v probed=%v invites=%d\n",
					t.known, t.net.Running, t.net.State, t.net.Self.Name, t.net.Self.IPs, t.doorOn, t.doorErr, t.probing, !t.probedAt.IsZero(), len(t.invites))
				for _, i := range t.invites {
					out += fmt.Sprintf("invite from %q (%s) at %s code=%s\n", inviter(i.from), i.from.Who(), i.addr, i.code)
				}
				for _, d := range a.tailDevices() {
					about := "-"
					if d.about != nil {
						about = fmt.Sprintf("%+v", *d.about)
						if d.about.Live != nil {
							about += fmt.Sprintf(" live=%+v", *d.about.Live)
						}
					}
					out += fmt.Sprintf("device %q owner=%q sharee=%v online=%v seen=%v member=%v sent=%q about=%s\n", d.Name, d.Owner.Label(), d.Sharee, d.Online, d.seen, d.member, t.sent[d.ID], about)
				}
			}
		})
		if out != "" {
			os.WriteFile(filepath.Join(dir, "tail"), []byte(out), 0o644)
		}
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
	case "output":
		// "output list" writes the outputs to the file "outputs"; "output
		// BlackHole" plays on the first whose name has that in it; "output"
		// on the system's default.
		do(func() {
			if arg == "list" {
				a.output.readAt = time.Time{}
				a.readOutputs(a.settleOutput)
				return
			}
			if arg == "" {
				a.chooseOutput("", "")
				return
			}
			for _, d := range a.output.devices {
				if strings.Contains(strings.ToLower(d.Name), strings.ToLower(arg)) {
					a.chooseOutput(d.ID, d.Name)
					return
				}
			}
			a.chooseOutput("gone-"+arg, arg) // one that is not plugged in
		})
	case "motion":
		// "motion on", "motion reduced" or "motion" for the system's.
		do(func() {
			a.settings.Motion = arg
			a.applyMotion()
		})
	case "share":
		// "share album 3 blend square" opens the dialog on the fourth album
		// by name; "share song" on the song playing; "share png file.png"
		// writes the picture, as it would be saved, to the debug directory.
		do(func() {
			f := strings.Fields(arg)
			if len(f) == 0 {
				return
			}
			if f[0] == "save" {
				a.saveCard()
				return
			}
			if f[0] == "copy" {
				a.copyCard()
				return
			}
			if f[0] == "png" && len(f) == 2 {
				if data, err := a.cardPNG(); err == nil {
					os.WriteFile(filepath.Join(dir, f[1]), data, 0o644)
				} else {
					log.Print("share png: ", err)
				}
				return
			}
			switch f[0] {
			case "album":
				n := 0
				if len(f) > 1 {
					n, _ = strconv.Atoi(f[1])
				}
				if albums := a.albumsBy("Name"); n < len(albums) {
					a.shareAlbum(albums[n])
				}
			case "song":
				if s := a.player.current(); s != nil {
					a.shareSong(s)
				}
			}
			for _, w := range f[1:] {
				switch w {
				case bgWhite, bgBlack, bgColor, bgBlend:
					a.share.bg = w
				case "mark":
					a.share.mark = true
				default:
					for i, cf := range cardFormats {
						if cf.id == w {
							a.share.format = i
						}
					}
				}
			}
		})
	case "sleeptimer":
		// "sleeptimer time 6s", "sleeptimer song", "sleeptimer album" or
		// "sleeptimer off".
		do(func() {
			mode, d, _ := strings.Cut(arg, " ")
			dur, _ := time.ParseDuration(d)
			if mode == "off" {
				mode = ""
			}
			a.player.setSleep(mode, dur)
		})
	case "info":
		// "info" opens the details of the song playing, and writes what the
		// server told of its file to the file "info".
		do(func() {
			if s := a.player.current(); s != nil {
				a.openInfo(s)
			}
		})
		for i := 0; i < 100 && a.info.loading; i++ {
			time.Sleep(100 * time.Millisecond)
		}
		var out string
		do(func() {
			in := &a.info
			switch {
			case in.err != nil:
				out = "error: " + in.err.Error()
			case in.item != nil && len(in.item.MediaSources) > 0:
				src := &in.item.MediaSources[0]
				out = fmt.Sprintf("file: %s\nsize: %s\nkind: %s\nplaying as: %s\n", fileLine(src), bytesText(src.Size), src.Container,
					playingAs(src, a.settings.MaxBitrate, a.player.engineRate(), false))
			default:
				out = "nothing"
			}
		})
		os.WriteFile(filepath.Join(dir, "info"), []byte(out), 0o644)
	case "mix":
		// "mix Songs" asks the server for songs like the one playing, "mix
		// Albums" like its album; the first are written to the file "mix".
		kind := strings.TrimSpace(arg)
		var ids []string
		do(func() {
			if s := a.player.current(); s != nil {
				id := s.ID
				if kind == "Albums" {
					id = s.AlbumID
				}
				a.mixOf(kind, id, 12, func(songs []*library.Song, err error) {
					out := fmt.Sprintf("err=%v n=%d\n", err, len(songs))
					for _, sg := range songs {
						out += sg.Name + " — " + sg.Artist + "\n"
					}
					os.WriteFile(filepath.Join(dir, "mix"), []byte(out), 0o644)
				})
			}
		})
		_ = ids
		time.Sleep(3 * time.Second)
	case "desktop":
		// "desktop tray on|off", "desktop notify on|off"
		do(func() {
			what, on, _ := strings.Cut(arg, " ")
			switch what {
			case "tray":
				a.settings.Tray = on == "on"
			case "notify":
				a.settings.Notify = on == "on"
			}
			a.told = ""
		})
	case "similar":
		// "similar" writes the artists like the one of the song playing to
		// the file "similar".
		var id string
		do(func() {
			if s := a.player.current(); s != nil {
				id = a.artistOf(s.ArtistIDs, s.AlbumArtistIDs)
				a.similarArtists(id)
			}
		})
		time.Sleep(3 * time.Second)
		do(func() {
			out := ""
			for _, ar := range a.similarArtists(id) {
				out += ar.Name + "\n"
			}
			os.WriteFile(filepath.Join(dir, "similar"), []byte(out), 0o644)
		})
	case "xfade":
		// "xfade 4" crossfades by 4 seconds, "xfade 0" not.
		n, _ := strconv.Atoi(arg)
		do(func() {
			a.settings.CrossfadeSecs = n
			if e := a.player.engine; e != nil {
				e.SetCrossfade(time.Duration(n) * time.Second)
			}
		})
	case "radio":
		// "radio" plays songs like the one playing.
		do(func() {
			if s := a.player.current(); s != nil {
				a.startRadio("Songs", s.ID, s.Name, s)
			}
		})
	case "seekend":
		// "seekend 8s" goes to 8 seconds before the end of the song.
		d, _ := time.ParseDuration(arg)
		do(func() {
			if s := a.player.current(); s != nil {
				a.player.seek(s.Duration() - d)
			}
		})
	case "front":
		// A covered window paints no frames, so a shot would show an old
		// one: the window is brought forward.
		do(func() { a.show() })
		time.Sleep(400 * time.Millisecond)
	case "settingsy":
		// "settingsy 1200" scrolls the settings that far down.
		y, _ := strconv.ParseFloat(arg, 64)
		do(func() { a.settingsScroll.Y = float32(y) })
	case "eq":
		// "eq on", "eq off", "eq preset Rock" and "eq band 5 6" (the sixth
		// band, +6 dB) set the equalizer.
		do(func() {
			f := strings.Fields(arg)
			switch {
			case len(f) == 0:
			case f[0] == "on" || f[0] == "off":
				a.settings.EQ.On = f[0] == "on"
			case f[0] == "preset":
				if pr, ok := a.settings.presetNamed(strings.TrimSpace(strings.TrimPrefix(arg, "preset"))); ok {
					a.settings.EQ.Bands, a.settings.EQ.Preamp = pr.Bands, pr.Preamp
				}
			case f[0] == "band" && len(f) == 3:
				i, _ := strconv.Atoi(f[1])
				v, _ := strconv.ParseFloat(f[2], 64)
				a.settings.EQ.Bands[i%10] = v
			}
			a.eqChanged()
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

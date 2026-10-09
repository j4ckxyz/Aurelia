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
		png, err := win.CapturePage()
		if err != nil {
			log.Print("shot: ", err)
			return
		}
		os.WriteFile(filepath.Join(dir, arg), png, 0o644)
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
			if strings.HasPrefix(arg, "album") {
				n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(arg, "album")))
				if albums := a.albumsBy("Name"); n < len(albums) {
					a.playAlbum(albums[n], false)
				}
				return
			}
			a.player.toggle()
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
		})
		os.WriteFile(filepath.Join(dir, "state"), []byte(out), 0o644)
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

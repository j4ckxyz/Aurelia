package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Pictures are fetched once, kept on disk and in memory, asked for again
// only after a while when the server has none, and over a few
// connections that stay open.
func TestImageCache(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	jpeg.Encode(&buf, img, nil)
	_ = color.RGBA{}
	var requests, conns atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write(buf.Bytes())
	}))
	srv.Config.ConnState = func(c net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	dir := t.TempDir()
	// What the loaders hand back runs here, as on the interface's
	// goroutine.
	var mu sync.Mutex
	var posted []func()
	ic := newImageCache(filepath.Join(dir, "cache"), filepath.Join(dir, "pinned"), 1<<20, 1<<20, func(fn func()) {
		mu.Lock()
		posted = append(posted, fn)
		mu.Unlock()
	})
	frame := func() {
		mu.Lock()
		fns := posted
		posted = nil
		mu.Unlock()
		for _, fn := range fns {
			fn()
		}
	}
	// Sixty pictures, asked for in every frame until they are there.
	keys := make([]string, 60)
	for i := range keys {
		keys[i] = "k" + itoaTest(i)
	}
	deadline := time.Now().Add(10 * time.Second)
	for loaded := 0; loaded < len(keys); {
		loaded = 0
		for _, k := range keys {
			if ic.get(k, srv.URL+"/"+k) != nil {
				loaded++
			}
		}
		ic.get("missing", srv.URL+"/missing")
		ic.tick()
		frame()
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d pictures after 10 s", loaded, len(keys))
		}
		time.Sleep(time.Millisecond)
	}
	if n := requests.Load(); n != int64(len(keys))+1 {
		t.Errorf("%d requests for %d pictures and one the server has not", n, len(keys))
	}
	// A connection or two more than loaders may open, when a request
	// starts as another's connection comes free: not one for each picture.
	if n := conns.Load(); n > 2*imageLoaders {
		t.Errorf("%d connections for %d pictures and %d loaders", n, len(keys), imageLoaders)
	}
	if des, _ := os.ReadDir(ic.dir); len(des) != len(keys) {
		t.Errorf("%d files on disk", len(des))
	}
	// Shown no more, they leave memory down to what it may hold, but
	// never those the last frame showed.
	ic.maxBytes = 20 * 32 * 32 * 4
	for _, k := range keys[:30] {
		ic.get(k, "")
	}
	ic.shrink(0)
	if len(ic.mem) != 30 {
		t.Errorf("%d pictures in memory, with thirty showing and room for twenty", len(ic.mem))
	}
	ic.tick()
	ic.shrink(0)
	if len(ic.mem) != 20 {
		t.Errorf("%d pictures in memory a frame later, with room for twenty", len(ic.mem))
	}
	ic.maxBytes = 1 << 20
	// A new cache on the same directory needs no server.
	srv.Close()
	ic2 := newImageCache(ic.dir, ic.pinDir, 1<<20, 1<<20, func(fn func()) {
		mu.Lock()
		posted = append(posted, fn)
		mu.Unlock()
	})
	for deadline := time.Now().Add(5 * time.Second); ic2.get("k7", "http://127.0.0.1:1/none") == nil; time.Sleep(time.Millisecond) {
		frame()
		if time.Now().After(deadline) {
			t.Fatal("a picture on disk did not load without the server")
		}
	}
	// A picture asked for once, and not in the frames after, is not
	// worth loading: it scrolled away.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(30 * time.Millisecond)
		w.Write(buf.Bytes())
	}))
	defer srv2.Close()
	before := requests.Load()
	for i := 0; i < 200; i++ {
		ic.get("gone"+itoaTest(i), srv2.URL+"/gone")
		ic.tick()
	}
	time.Sleep(300 * time.Millisecond)
	frame()
	if n := requests.Load() - before; n > 2*imageLoaders+2 {
		t.Errorf("%d of 200 pictures that scrolled away were fetched", n)
	}
	// The disk is trimmed to its limit.
	ic.maxDisk.Store(10 * int64(buf.Len()))
	ic.trim()
	if des, _ := os.ReadDir(ic.dir); len(des) > 10 || len(des) < 5 {
		t.Errorf("%d files on disk after trimming to ten", len(des))
	}
	// A pinned picture stays when the cache is cleared.
	ic.pin("k7", "http://127.0.0.1:1/none") // on disk or not, k7 was fetched: pin copies it, or leaves it be
	ic.clear()
	if des, _ := os.ReadDir(ic.dir); len(des) != 0 {
		t.Errorf("%d files on disk after clearing", len(des))
	}
}

func itoaTest(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoaTest(n/10) + string(rune('0'+n%10))
}

package main

import (
	"bytes"
	"container/list"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// imageCache gives the view the pictures it shows, at once when it has
// them: decoded in memory for those shown lately, as files on disk for
// all it ever fetched, and from the server otherwise. The view asks in
// every frame for what it shows and draws a placeholder for what is not
// there yet; the cache loads on other goroutines and asks for a frame
// when a picture is in.
type imageCache struct {
	dir    string
	client *http.Client
	// post runs a function on the interface's goroutine and draws a
	// frame.
	post func(func())

	// On the interface's goroutine only.
	mem      map[string]*list.Element
	lru      *list.List // of *memImage, the last shown first
	memBytes int64
	maxBytes int64
	failed   map[string]time.Time

	// Shared with the loaders.
	mu      sync.Mutex
	cond    *sync.Cond
	pending map[string]*imageReq
	stack   []*imageReq // the last asked is loaded first
	low     []*imageReq // what warms the disk, when nothing else waits
	started bool
}

type memImage struct {
	key   string
	bmp   *ui.Bitmap
	bytes int64
}

type imageReq struct {
	key, url string
	wanted   time.Time
	diskOnly bool // fetch to disk, decode nothing
}

func newImageCache(dir string, maxBytes int64, post func(func())) *imageCache {
	os.MkdirAll(dir, 0o755)
	ic := &imageCache{
		dir: dir, client: &http.Client{Timeout: 30 * time.Second}, post: post,
		mem: map[string]*list.Element{}, lru: list.New(), maxBytes: maxBytes,
		failed: map[string]time.Time{}, pending: map[string]*imageReq{},
	}
	ic.cond = sync.NewCond(&ic.mu)
	return ic
}

// get returns the picture of key, or nil while it loads from url. Call it
// in every frame that shows the picture: one no longer asked for is not
// loaded.
func (ic *imageCache) get(key, url string) *ui.Bitmap {
	if el, ok := ic.mem[key]; ok {
		ic.lru.MoveToFront(el)
		return el.Value.(*memImage).bmp
	}
	if at, bad := ic.failed[key]; bad {
		if time.Since(at) < 30*time.Second {
			return nil
		}
		delete(ic.failed, key)
	}
	now := time.Now()
	ic.mu.Lock()
	if req := ic.pending[key]; req != nil {
		req.wanted = now
		if req.diskOnly {
			// It was only warming the disk: now it is to show.
			req.diskOnly = false
			ic.stack = append(ic.stack, req)
			ic.cond.Signal()
		}
	} else {
		req = &imageReq{key: key, url: url, wanted: now}
		ic.pending[key] = req
		ic.stack = append(ic.stack, req)
		ic.start()
		ic.cond.Signal()
	}
	ic.mu.Unlock()
	return nil
}

// warm fetches pictures to disk, behind everything the view asks for, so
// that they show at once later.
func (ic *imageCache) warm(key, url string) {
	if _, err := os.Stat(ic.path(key)); err == nil {
		return
	}
	ic.mu.Lock()
	if ic.pending[key] == nil {
		req := &imageReq{key: key, url: url, diskOnly: true}
		ic.pending[key] = req
		ic.low = append(ic.low, req)
		ic.start()
		ic.cond.Signal()
	}
	ic.mu.Unlock()
}

// start runs the loaders. The caller holds mu.
func (ic *imageCache) start() {
	if ic.started {
		return
	}
	ic.started = true
	for range 4 {
		go ic.loader()
	}
}

func (ic *imageCache) path(key string) string { return filepath.Join(ic.dir, key+".jpg") }

func (ic *imageCache) loader() {
	for {
		ic.mu.Lock()
		var req *imageReq
		for req == nil {
			switch {
			case len(ic.stack) > 0:
				req = ic.stack[len(ic.stack)-1]
				ic.stack = ic.stack[:len(ic.stack)-1]
				if req.diskOnly {
					req = nil // a stale entry of one that warm made
				} else if time.Since(req.wanted) > 2*time.Second {
					// Scrolled away before its turn came.
					delete(ic.pending, req.key)
					req = nil
				}
			case len(ic.low) > 0:
				req = ic.low[0]
				ic.low = ic.low[1:]
				if !req.diskOnly {
					req = nil // the view asked meanwhile: it is on the stack
				}
			default:
				ic.cond.Wait()
			}
		}
		diskOnly := req.diskOnly
		ic.mu.Unlock()

		bmp, size, err := ic.load(req, diskOnly)
		ic.mu.Lock()
		// The view may have asked for it while it was fetched for the
		// disk alone: it is still on the stack, and loads from the disk.
		upgraded := diskOnly && !req.diskOnly
		if !upgraded {
			delete(ic.pending, req.key)
		}
		ic.mu.Unlock()
		if diskOnly {
			continue
		}
		ic.post(func() {
			if err != nil || bmp == nil {
				ic.failed[req.key] = time.Now()
				return
			}
			ic.insert(req.key, bmp, size)
		})
	}
}

// load reads a picture from the disk, or from the server into the disk.
func (ic *imageCache) load(req *imageReq, diskOnly bool) (*ui.Bitmap, int64, error) {
	path := ic.path(req.key)
	data, err := os.ReadFile(path)
	if err != nil {
		if diskOnly {
			// Warming is a courtesy: slowly, so that the server and the
			// pictures the view waits for are not held up.
			time.Sleep(60 * time.Millisecond)
		}
		resp, err := ic.client.Get(req.url)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, 0, fmt.Errorf("image: the server answered %s", resp.Status)
		}
		if data, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20)); err != nil {
			return nil, 0, err
		}
		tmp := path + ".tmp"
		if os.WriteFile(tmp, data, 0o644) == nil {
			os.Rename(tmp, path)
		}
	}
	if diskOnly {
		return nil, 0, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		os.Remove(path) // not a picture: fetched again later
		return nil, 0, err
	}
	b := img.Bounds()
	return ui.NewBitmap(img), int64(b.Dx()) * int64(b.Dy()) * 4, nil
}

// insert keeps a picture in memory, and drops those shown longest ago
// beyond the cache's size.
func (ic *imageCache) insert(key string, bmp *ui.Bitmap, size int64) {
	if _, ok := ic.mem[key]; ok {
		return
	}
	ic.mem[key] = ic.lru.PushFront(&memImage{key: key, bmp: bmp, bytes: size})
	ic.memBytes += size
	for ic.memBytes > ic.maxBytes && ic.lru.Len() > 1 {
		el := ic.lru.Back()
		m := ic.lru.Remove(el).(*memImage)
		delete(ic.mem, m.key)
		ic.memBytes -= m.bytes
	}
}

// diskSize returns the bytes of the pictures on disk.
func (ic *imageCache) diskSize() int64 {
	des, _ := os.ReadDir(ic.dir)
	var total int64
	for _, de := range des {
		if info, err := de.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

// clear forgets every picture, in memory and on disk.
func (ic *imageCache) clear() {
	ic.mem, ic.memBytes = map[string]*list.Element{}, 0
	ic.lru.Init()
	des, _ := os.ReadDir(ic.dir)
	for _, de := range des {
		os.Remove(filepath.Join(ic.dir, de.Name()))
	}
}

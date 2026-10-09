// Package audio plays tracks: it downloads them into a cache on disk as
// they play, decodes FLAC and MP3 in Go, resamples to the device's rate and
// feeds the sound card, one track after the other without a gap.
package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrAborted is what a File blocked on data not downloaded yet returns
// once it is aborted.
var ErrAborted = errors.New("audio: aborted")

// Cache keeps the files of tracks on disk, downloading each once. A file
// can be read while it downloads: reads wait for the bytes they need.
type Cache struct {
	dir    string
	client *http.Client

	mu       sync.Mutex
	entries  map[string]*entry
	maxBytes int64
	seq      int
}

// NewCache returns a cache of at most maxBytes in dir.
func NewCache(dir string, maxBytes int64, client *http.Client) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	// Downloads that a crash or a quit cut short.
	if parts, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(parts) > 0 {
		for _, p := range parts {
			os.Remove(p)
		}
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &Cache{dir: dir, client: client, entries: map[string]*entry{}, maxBytes: maxBytes}, nil
}

// SetMaxBytes changes the size the cache is kept under.
func (c *Cache) SetMaxBytes(n int64) {
	c.mu.Lock()
	c.maxBytes = n
	c.mu.Unlock()
	go c.evict()
}

// entry is a file of the cache that is open, downloading or not.
type entry struct {
	c    *Cache
	key  string
	path string
	part string // where it downloads, a name of its own

	mu   sync.Mutex
	cond *sync.Cond
	f    *os.File // read with ReadAt, under mu
	n    int64    // bytes on disk
	size int64    // of the whole file, -1 until known
	done bool
	err  error

	refs   int // under c.mu
	cancel context.CancelFunc
}

// Open returns the file of key, starting its download with the request
// that newRequest makes unless the cache has it. It never blocks on the
// network: reads do.
func (c *Cache) Open(key string, newRequest func(ctx context.Context) (*http.Request, error)) (*File, error) {
	key = safeKey(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[key]; e != nil {
		e.refs++
		return &File{e: e}, nil
	}
	e := &entry{c: c, key: key, path: filepath.Join(c.dir, key+".audio"), size: -1, refs: 1}
	e.cond = sync.NewCond(&e.mu)
	if f, err := os.Open(e.path); err == nil {
		if st, err := f.Stat(); err == nil && st.Size() > 0 {
			e.f, e.n, e.size, e.done = f, st.Size(), st.Size(), true
			now := time.Now()
			os.Chtimes(e.path, now, now) // the cache drops the files opened longest ago
			c.entries[key] = e
			return &File{e: e}, nil
		}
		f.Close()
	}
	c.seq++
	e.part = fmt.Sprintf("%s.%d.part", e.path, c.seq)
	w, err := os.Create(e.part)
	if err != nil {
		return nil, err
	}
	r, err := os.Open(e.part)
	if err != nil {
		w.Close()
		return nil, err
	}
	e.f = r
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	c.entries[key] = e
	go e.download(ctx, w, newRequest)
	return &File{e: e}, nil
}

// Has reports whether the cache holds the whole file of key.
func (c *Cache) Has(key string) bool {
	key = safeKey(key)
	c.mu.Lock()
	e := c.entries[key]
	c.mu.Unlock()
	if e != nil {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.done && e.err == nil
	}
	st, err := os.Stat(filepath.Join(c.dir, key+".audio"))
	return err == nil && st.Size() > 0
}

func (e *entry) download(ctx context.Context, w *os.File, newRequest func(context.Context) (*http.Request, error)) {
	err := e.fetch(ctx, w, newRequest)
	w.Close()
	e.mu.Lock()
	if err == nil {
		// The file takes its name once whole. Windows renames no open
		// file: close it, and open it again under its name.
		e.f.Close()
		if err = os.Rename(e.part, e.path); err == nil {
			e.f, err = os.Open(e.path)
		}
		if err != nil {
			e.f = nil
		}
	}
	if err != nil {
		e.err = err
		os.Remove(e.part)
	} else {
		e.size = e.n
	}
	e.done = true
	e.cond.Broadcast()
	e.mu.Unlock()
	if err == nil {
		e.c.evict()
	}
}

func (e *entry) fetch(ctx context.Context, w *os.File, newRequest func(context.Context) (*http.Request, error)) error {
	req, err := newRequest(ctx)
	if err != nil {
		return err
	}
	resp, err := e.c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("audio: the server answered %s", resp.Status)
	}
	if resp.ContentLength > 0 {
		e.mu.Lock()
		e.size = resp.ContentLength
		e.cond.Broadcast()
		e.mu.Unlock()
	}
	buf := make([]byte, 64<<10)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			e.mu.Lock()
			e.n += int64(n)
			e.cond.Broadcast()
			e.mu.Unlock()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	e.mu.Lock()
	n := e.n
	e.mu.Unlock()
	if n == 0 {
		return errors.New("audio: the server sent an empty file")
	}
	return nil
}

// release drops a reference; a download nobody reads any more stops.
func (e *entry) release() {
	c := e.c
	c.mu.Lock()
	e.refs--
	if e.refs > 0 {
		c.mu.Unlock()
		return
	}
	delete(c.entries, e.key)
	c.mu.Unlock()
	if e.cancel != nil {
		e.cancel() // download removes the part file if it was not whole
	}
	e.mu.Lock()
	if e.done && e.f != nil {
		e.f.Close()
		e.f = nil
	} else if !e.done {
		// The download goroutine closes nothing of ours: wait for it to
		// end, then close the reader.
		go func() {
			e.mu.Lock()
			for !e.done {
				e.cond.Wait()
			}
			if e.f != nil {
				e.f.Close()
				e.f = nil
			}
			e.mu.Unlock()
		}()
	}
	e.mu.Unlock()
}

// evict removes the files opened longest ago until the cache fits.
func (c *Cache) evict() {
	c.mu.Lock()
	limit := c.maxBytes
	c.mu.Unlock()
	if limit <= 0 {
		return
	}
	des, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type file struct {
		name string
		size int64
		at   time.Time
	}
	var files []file
	var total int64
	for _, de := range des {
		if !strings.HasSuffix(de.Name(), ".audio") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		files = append(files, file{de.Name(), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total <= limit {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	for _, f := range files {
		if total <= limit {
			break
		}
		key := strings.TrimSuffix(f.name, ".audio")
		c.mu.Lock()
		_, open := c.entries[key]
		c.mu.Unlock()
		if open {
			continue
		}
		if os.Remove(filepath.Join(c.dir, f.name)) == nil {
			total -= f.size
		}
	}
}

// Size returns the bytes the cache holds on disk.
func (c *Cache) Size() int64 {
	des, _ := os.ReadDir(c.dir)
	var total int64
	for _, de := range des {
		if info, err := de.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

// Clear removes every file that is not open.
func (c *Cache) Clear() {
	des, _ := os.ReadDir(c.dir)
	for _, de := range des {
		if !strings.HasSuffix(de.Name(), ".audio") {
			continue
		}
		key := strings.TrimSuffix(de.Name(), ".audio")
		c.mu.Lock()
		_, open := c.entries[key]
		c.mu.Unlock()
		if !open {
			os.Remove(filepath.Join(c.dir, de.Name()))
		}
	}
}

func safeKey(key string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, key)
}

// File reads a file of the cache, waiting for what is not downloaded yet.
// It is an io.ReadSeeker for one goroutine; Abort and Progress are safe
// from others.
type File struct {
	e   *entry
	off int64

	// under e.mu
	aborted bool
	closed  bool
}

func (f *File) Read(p []byte) (int, error) {
	e := f.e
	e.mu.Lock()
	defer e.mu.Unlock()
	for f.off >= e.n && !e.done && !f.aborted {
		e.cond.Wait()
	}
	if f.aborted {
		return 0, ErrAborted
	}
	if f.off >= e.n {
		if e.err != nil {
			return 0, e.err
		}
		return 0, io.EOF
	}
	if e.f == nil {
		return 0, os.ErrClosed
	}
	if max := e.n - f.off; int64(len(p)) > max {
		p = p[:max]
	}
	n, err := e.f.ReadAt(p, f.off)
	f.off += int64(n)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return n, err
}

// Seek moves the place of the next Read. Seeking from the end waits for
// the size of the file, which a transcoded one has only once whole.
func (f *File) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += f.off
	case io.SeekEnd:
		size, err := f.Size()
		if err != nil {
			return 0, err
		}
		offset += size
	default:
		return 0, errors.New("audio: invalid whence")
	}
	if offset < 0 {
		return 0, errors.New("audio: negative position")
	}
	f.off = offset
	return offset, nil
}

// Size returns the size of the whole file, waiting until it is known.
func (f *File) Size() (int64, error) {
	e := f.e
	e.mu.Lock()
	defer e.mu.Unlock()
	for e.size < 0 && !e.done && !f.aborted {
		e.cond.Wait()
	}
	if f.aborted {
		return 0, ErrAborted
	}
	if e.size < 0 {
		if e.err != nil {
			return 0, e.err
		}
		return e.n, nil
	}
	return e.size, nil
}

// Progress returns the bytes downloaded and the size of the file, -1
// while unknown, without waiting.
func (f *File) Progress() (have, size int64, done bool) {
	e := f.e
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.n, e.size, e.done
}

// Abort makes the reads waiting for data, and those to come, fail with
// ErrAborted.
func (f *File) Abort() {
	e := f.e
	e.mu.Lock()
	f.aborted = true
	e.cond.Broadcast()
	e.mu.Unlock()
}

// Resume undoes Abort.
func (f *File) Resume() {
	e := f.e
	e.mu.Lock()
	if !f.closed {
		f.aborted = false
	}
	e.mu.Unlock()
}

// Close releases the file. A download no file reads any more stops.
func (f *File) Close() error {
	e := f.e
	e.mu.Lock()
	if f.closed {
		e.mu.Unlock()
		return nil
	}
	f.closed, f.aborted = true, true
	e.cond.Broadcast()
	e.mu.Unlock()
	e.release()
	return nil
}

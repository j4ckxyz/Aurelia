package audio

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A paced download keeps only so far ahead of what is read: it goes on
// as the reads do, and at once to a place read far ahead.
func TestPacedDownload(t *testing.T) {
	const size = 8 << 20 // a song of 80 s: the pace of 10 s is 1 MB
	body := bytes.Repeat([]byte{7}, size)
	var sent atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "8388608")
		for off := 0; off < size; off += 32 << 10 {
			n, err := w.Write(body[off : off+32<<10])
			sent.Add(int64(n))
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close) // after the cache lets go of its downloads
	c, err := NewCache(t.TempDir(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing of the cache is open as its directory goes, as Windows
	// removes no open file.
	t.Cleanup(c.dropIdle)
	open := func(key string, ahead time.Duration) *File {
		f, err := c.OpenPaced(key, func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
		}, ahead, 80*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	have := func(f *File) int64 { n, _, _ := f.Progress(); return n }
	settle := func(f *File) int64 {
		last := int64(-1)
		for range 50 {
			time.Sleep(20 * time.Millisecond)
			if n := have(f); n == last {
				return n
			} else {
				last = n
			}
		}
		return last
	}
	f := open("paced", 10*time.Second)
	defer f.Close()
	// Read nothing: it stops about a pace ahead.
	if n := settle(f); n < 1<<20 || n > 2<<20 {
		t.Fatalf("unread, it has %d bytes", n)
	}
	// Read 2 MB: it goes on, as far again ahead.
	if _, err := io.ReadFull(f, make([]byte, 2<<20)); err != nil {
		t.Fatal(err)
	}
	if n := settle(f); n < 3<<20 || n > 4<<20 {
		t.Errorf("read to 2 MB, it has %d bytes", n)
	}
	// Read far ahead: it is had, without waiting for the pace.
	if _, err := f.Seek(6<<20, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := io.ReadFull(f, make([]byte, 64<<10)); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a read far ahead waits")
	}
	// Closed, the download it held up ends.
	f.Close()
	// Not paced: all of it, read or not.
	g := open("whole", 0)
	defer g.Close()
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
	if n := have(g); n != size {
		t.Errorf("unpaced, it has %d bytes", n)
	}
}

// A song closed while it downloads, and opened again at once, as to play
// it from another place, goes on downloading: it is not asked for anew.
func TestReopenedDownloadGoesOn(t *testing.T) {
	var asked atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.Header().Set("Content-Length", "131072")
		w.Write(make([]byte, 65536))
		w.(http.Flusher).Flush()
		<-release
		w.Write(make([]byte, 65536))
	}))
	t.Cleanup(srv.Close) // after the cache lets go of its downloads
	defer close(release)
	c, err := NewCache(t.TempDir(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.dropIdle)
	open := func() *File {
		f, err := c.Open("song", func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
		})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	f := open()
	if _, err := io.ReadFull(f, make([]byte, 1000)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	g := open()
	defer g.Close()
	// The first download's bytes come to it.
	deadline := time.Now().Add(3 * time.Second)
	for n, _, _ := g.Progress(); n < 65536; n, _, _ = g.Progress() {
		if time.Now().After(deadline) {
			t.Fatalf("opened again, it has %d bytes", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := asked.Load(); n != 1 {
		t.Errorf("asked for %d times", n)
	}
}

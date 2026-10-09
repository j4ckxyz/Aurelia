package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/audio"
	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
)

// downloads are the songs kept on this computer to play without the
// server: those the user asked for, one by one or as albums and
// playlists. Unlike the cache of songs played lately, nothing here is
// removed but by the user. Its methods run on the interface's goroutine.
type downloads struct {
	app   *App
	store *audio.Cache
	file  string

	songs     map[string]*library.Song // asked for, by ID
	albums    map[string]bool
	playlists map[string]*downloadedPlaylist

	done    map[string]bool        // whole on disk
	queued  map[string]bool        // waiting for their turn
	order   []string               // the order they wait in
	active  map[string]*audio.File // downloading now
	failed  map[string]string
	bytes   int64 // what they take on disk, as last counted
	counted bool
}

// downloadedPlaylist is a playlist kept for offline, as it was when it
// was downloaded.
type downloadedPlaylist struct {
	Name     string   `json:"name"`
	ImageTag string   `json:"image,omitempty"`
	Songs    []string `json:"songs"`
}

// savedDownloads is downloads.json.
type savedDownloads struct {
	Songs     []*library.Song                `json:"songs"`
	Albums    []string                       `json:"albums,omitempty"`
	Playlists map[string]*downloadedPlaylist `json:"playlists,omitempty"`
}

// Download states.
const (
	dlNone = iota
	dlQueued
	dlActive
	dlDone
)

func newDownloads(app *App) *downloads {
	d := &downloads{
		app: app, file: filepath.Join(app.dirs.downloads(), "downloads.json"),
		songs: map[string]*library.Song{}, albums: map[string]bool{}, playlists: map[string]*downloadedPlaylist{},
		done: map[string]bool{}, queued: map[string]bool{}, active: map[string]*audio.File{}, failed: map[string]string{},
	}
	var err error
	if d.store, err = audio.NewCache(app.dirs.downloads(), 0, &http.Client{}); err != nil {
		log.Print("downloads: ", err)
		return d
	}
	var saved savedDownloads
	if b, err := os.ReadFile(d.file); err == nil {
		json.Unmarshal(b, &saved)
	}
	for _, s := range saved.Songs {
		library.PrepareSong(s)
		d.songs[s.ID] = s
	}
	for _, id := range saved.Albums {
		d.albums[id] = true
	}
	if saved.Playlists != nil {
		d.playlists = saved.Playlists
	}
	for _, key := range d.store.Keys() {
		if d.songs[key] != nil {
			d.done[key] = true
		} else {
			d.store.Remove(key) // left by a download removed while it played
		}
	}
	// What was not finished when the app last quit goes on.
	for id := range d.songs {
		if !d.done[id] {
			d.queued[id] = true
			d.order = append(d.order, id)
		}
	}
	slices.Sort(d.order)
	return d
}

func (d *downloads) save() {
	saved := savedDownloads{Playlists: d.playlists}
	for _, s := range d.songs {
		saved.Songs = append(saved.Songs, s)
	}
	slices.SortFunc(saved.Songs, func(a, b *library.Song) int {
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	for id := range d.albums {
		saved.Albums = append(saved.Albums, id)
	}
	slices.Sort(saved.Albums)
	b, err := json.Marshal(saved)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(d.file), 0o755)
	if os.WriteFile(d.file+".tmp", b, 0o644) == nil {
		os.Rename(d.file+".tmp", d.file)
	}
}

// state tells how far the download of a song is, and for one downloading
// now, the part of it that is here.
func (d *downloads) state(id string) (state int, part float64) {
	switch {
	case d.done[id]:
		return dlDone, 1
	case d.active[id] != nil:
		if have, size, _ := d.active[id].Progress(); size > 0 {
			part = float64(have) / float64(size)
		}
		return dlActive, part
	case d.queued[id]:
		return dlQueued, 0
	}
	return dlNone, 0
}

// group tells how far the downloads of several songs are, as one: done
// when all are, downloading while any is asked for and not here yet.
func (d *downloads) group(songs []*library.Song) (state int, part float64) {
	if len(songs) == 0 {
		return dlNone, 0
	}
	n, wanted := 0.0, 0
	for _, s := range songs {
		st, p := d.state(s.ID)
		if st != dlNone {
			wanted++
		}
		if st == dlDone {
			n++
		} else {
			n += p
		}
	}
	switch {
	case wanted == 0:
		return dlNone, 0
	case n >= float64(len(songs)):
		return dlDone, 1
	case wanted < len(songs) && len(d.active) == 0 && n == float64(wanted):
		return dlNone, 0 // some songs of it, downloaded on their own
	}
	return dlActive, n / float64(len(songs))
}

// busy reports whether anything downloads or waits to.
func (d *downloads) busy() bool { return len(d.active) > 0 || len(d.order) > 0 }

// add asks for songs.
func (d *downloads) add(songs ...*library.Song) {
	if d.store == nil {
		return
	}
	for _, s := range songs {
		if d.songs[s.ID] != nil && (d.done[s.ID] || d.queued[s.ID] || d.active[s.ID] != nil) {
			continue
		}
		d.songs[s.ID] = s
		delete(d.failed, s.ID)
		d.queued[s.ID] = true
		d.order = append(d.order, s.ID)
		d.app.pinArt(s.ImageItem, s.ImageTag)
	}
	d.save()
	d.pump()
	d.app.favGen++
}

// addAlbum asks for the songs of an album, and remembers the album.
func (d *downloads) addAlbum(al *library.Album, songs []*library.Song) {
	d.albums[al.ID] = true
	d.app.pinArt(al.ID, al.ImageTag)
	d.add(songs...)
}

// addPlaylist asks for the songs of a playlist, and remembers it as it
// is now.
func (d *downloads) addPlaylist(pl *library.Playlist, songs []*library.Song) {
	saved := &downloadedPlaylist{Name: pl.Name, ImageTag: pl.ImageTag}
	for _, s := range songs {
		saved.Songs = append(saved.Songs, s.ID)
	}
	d.playlists[pl.ID] = saved
	d.app.pinArt(pl.ID, pl.ImageTag)
	d.add(songs...)
}

// remove forgets songs and deletes their files, unless an album or a
// playlist downloaded still holds them.
func (d *downloads) remove(songs ...*library.Song) {
	for _, s := range songs {
		id := s.ID
		if d.songs[id] == nil || d.held(id) {
			continue
		}
		delete(d.songs, id)
		delete(d.queued, id)
		delete(d.failed, id)
		if i := slices.Index(d.order, id); i >= 0 {
			d.order = slices.Delete(d.order, i, i+1)
		}
		if f := d.active[id]; f != nil {
			f.Abort() // its goroutine ends, and removes what came
		}
		if d.done[id] {
			delete(d.done, id)
			d.store.Remove(id)
		}
	}
	d.counted = false
	d.save()
	d.app.favGen++
}

// held reports whether a downloaded album or playlist holds a song.
func (d *downloads) held(id string) bool {
	if s := d.songs[id]; s != nil && d.albums[s.AlbumID] {
		return true
	}
	for _, pl := range d.playlists {
		if slices.Contains(pl.Songs, id) {
			return true
		}
	}
	return false
}

func (d *downloads) removeAlbum(al *library.Album) {
	delete(d.albums, al.ID)
	var songs []*library.Song
	for _, s := range d.songs {
		if s.AlbumID == al.ID {
			songs = append(songs, s)
		}
	}
	d.remove(songs...)
}

func (d *downloads) removePlaylist(id string) {
	pl := d.playlists[id]
	if pl == nil {
		return
	}
	delete(d.playlists, id)
	var songs []*library.Song
	for _, sid := range pl.Songs {
		if s := d.songs[sid]; s != nil {
			songs = append(songs, s)
		}
	}
	d.remove(songs...)
}

// removeAll forgets everything downloaded.
func (d *downloads) removeAll() {
	clear(d.albums)
	clear(d.playlists)
	var songs []*library.Song
	for _, s := range d.songs {
		songs = append(songs, s)
	}
	d.remove(songs...)
	d.app.images.unpinAll()
}

// pump starts the downloads whose turn it is: two at a time.
func (d *downloads) pump() {
	for len(d.active) < 2 && len(d.order) > 0 {
		id := d.order[0]
		d.order = d.order[1:]
		delete(d.queued, id)
		if d.songs[id] == nil || d.done[id] {
			continue
		}
		d.start(id)
	}
}

func (d *downloads) start(id string) {
	app := d.app
	quality := jellyfin.Quality{MaxBitrate: app.settings.MaxBitrate * 1000}
	// A song the cache of songs played lately holds whole is copied
	// from it, without the network.
	cached := ""
	if app.player.cache != nil {
		cached = app.player.cache.Path(id + "-" + quality.Key())
	}
	f, err := d.store.Open(id, func(ctx context.Context) (*http.Request, error) {
		c := app.clientNow()
		if c == nil {
			return nil, jellyfin.ErrUnauthorized
		}
		return c.StreamRequest(ctx, id, quality)
	})
	if err != nil {
		d.failed[id] = friendly(err)
		return
	}
	d.active[id] = f
	go func() {
		var err error
		if cached != "" {
			// The download just opened is dropped for the copy.
			f.Close()
			err = d.store.Import(id, cached)
		} else {
			err = f.Wait()
			f.Close()
		}
		app.update(func() {
			delete(d.active, id)
			switch {
			case d.songs[id] == nil:
				d.store.Remove(id) // removed while it downloaded
			case err != nil:
				if !errors.Is(err, audio.ErrAborted) {
					d.failed[id] = friendly(err)
					if s := d.songs[id]; s != nil && len(d.failed) == 1 {
						app.toastError("Could not download "+s.Name, err)
					}
				}
			default:
				d.done[id] = true
				d.counted = false
			}
			app.favGen++
			d.pump()
		})
	}()
}

// retry asks again for the downloads that failed.
func (d *downloads) retry() {
	for id := range d.failed {
		if d.songs[id] != nil && !d.done[id] && !d.queued[id] {
			d.queued[id] = true
			d.order = append(d.order, id)
		}
	}
	clear(d.failed)
	d.pump()
}

// size returns what the downloads take on disk, counted on another
// goroutine and told a frame later.
func (d *downloads) size() int64 {
	if !d.counted && d.store != nil {
		d.counted = true
		go func() {
			n := d.store.Size()
			d.app.update(func() { d.bytes = n })
		}()
	}
	return d.bytes
}

// list returns the songs downloaded, by title.
func (d *downloads) list() []*library.Song {
	out := make([]*library.Song, 0, len(d.songs))
	for id, s := range d.songs {
		// The library's own song, where it has it: a favorite set on it
		// shows here.
		if own := d.app.lib.Song(id); own != nil {
			s = own
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b *library.Song) int {
		switch {
		case a.SortKey < b.SortKey:
			return -1
		case a.SortKey > b.SortKey:
			return 1
		case a.ID < b.ID:
			return -1
		}
		return 1
	})
	return out
}

// downloadMark shows how far a download is, in a box of size points: a
// ring filling while it downloads, a mark once it is here.
func (a *App) downloadMark(c *ui.Context, state int, part float64, size float32) {
	p := a.pal
	switch state {
	case dlDone:
		ui.Icon(c, icon("circle-arrow-down")).Size(size, size).TextColor(p.accent).Label("Downloaded")
	case dlQueued, dlActive:
		ui.Box(c).Size(size, size).Label("Downloading").Draw(func(g *ui.Painter, r ui.Rect) {
			ring(g, r, part, p.text.Alpha(0.2), p.accent)
		})
	}
}

// ring draws a circle of which part is filled, clockwise from its top.
func ring(g *ui.Painter, r ui.Rect, part float64, track, fill ui.Color) {
	const width = 2
	cx, cy, rad := r.X+r.W/2, r.Y+r.H/2, min(r.W, r.H)/2-width/2
	var circle ui.Path
	circle.Circle(cx, cy, rad)
	g.StrokePath(&circle, width, track)
	if part <= 0 {
		return
	}
	var arc ui.Path
	arcTo(&arc, cx, cy, rad, min(part, 1))
	g.StrokePath(&arc, width, fill)
}

// arcTo adds the arc of a circle from its top, clockwise, for part of a
// turn.
func arcTo(p *ui.Path, cx, cy, r float32, part float64) {
	const steps = 48
	n := max(2, int(steps*part))
	for i := 0; i <= n; i++ {
		a := -math.Pi/2 + 2*math.Pi*part*float64(i)/float64(n)
		x, y := cx+r*float32(math.Cos(a)), cy+r*float32(math.Sin(a))
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
}

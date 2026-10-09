package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
	"aurelia/internal/theme"
)

// App is the state of the app. The view reads and changes it on the
// interface's goroutine; other goroutines change it through update.
type App struct {
	win *mygo.Window
	// mini is the small window of the record, nil when it is not out.
	mini     *mygo.Window
	dirs     dirs
	settings Settings
	router   *ui.Router
	scale    float64 // device pixels per point, for the size of pictures
	// silent keeps the sound card closed, as tests of the view do.
	silent   bool
	started  bool
	quitting bool

	themes *theme.Store
	// systemDark is the system's appearance, which "Match system"
	// follows.
	systemDark bool
	pal        *palette
	palFor     string // the theme the palette is of, and its appearance
	// editing is the theme being edited, which shows as it changes.
	editing *themeEditor

	images *imageCache
	// pictures, when set, draws the pictures in place of the server's,
	// as the screenshots of the README do.
	pictures func(id string) *ui.Bitmap
	// covers, when set, gives the pictures the record is made of.
	covers    func(id string) image.Image
	player    *player
	downloads *downloads
	// offline is set while the server does not answer.
	offline bool
	// system is what the system was last told plays, and when.
	system   nowPlaying
	told     string
	toldAt   time.Time
	toldFrom time.Duration

	// client is read by other goroutines through clientNow.
	mu     sync.Mutex
	client *jellyfin.Client

	lib    *library.Library
	libGen int // counts the libraries shown, for what is derived of them
	// favGen counts the changes within a library: favorites, plays, and
	// songs fetched.
	favGen   int
	themeGen int // counts the readings of the themes
	syncing  bool
	progress library.Progress
	syncErr  string

	// The seek bar while it is dragged, and a volume not saved yet.
	scrubbing   bool
	scrub       float64
	volumeDirty bool

	// hiddenGen counts the albums taken out of "Jump back in".
	hiddenGen int
	// narrow is set while the window is too narrow to keep the sidebar,
	// and sidebarPeek shows it there all the same. The rest is of its
	// edge being dragged.
	narrow, sidebarPeek      bool
	sidebarFrom, sidebarDrag float32
	sidebarDirty             bool
	// stage is the full screen of the song playing.
	stage stage
	// remote is the other devices of the user, to play on.
	remote remote
	// cursor is the marker the keys move over a page's items.
	cursor cursor
	// lastIn is the page each section of the sidebar was left at.
	lastIn map[string]string

	sizes      cacheSizes
	updates    updates
	connection connection
	login      loginForm
	search     searchState
	pages      pageStates
	lyrics     lyricsState
	details    map[string]*detail // what the server tells of artists, by ID

	queueMu       sync.Mutex     // one writer of the queue's file at a time
	queueWrites   sync.WaitGroup // the writes not done yet
	queueRestored bool

	// pending are the functions of update waiting for a frame, where no
	// window runs them.
	pendingMu sync.Mutex
	pending   []func()
}

func newApp(d dirs, silent bool) *App {
	a := &App{dirs: d, settings: loadSettings(d), router: ui.NewRouter("/home"), scale: 2, details: map[string]*detail{}, silent: silent}
	a.router.Transition = ui.TransitionNone // pages show at once
	a.pages.queueChosen = -1
	if err := a.setProxy(a.settings.Proxy); err != nil {
		log.Print("the proxy of the settings: ", err)
	}
	a.login.proxy, a.login.proxyShown = a.settings.Proxy, a.settings.Proxy != ""
	a.connection.proxy = a.settings.Proxy
	a.themes = theme.NewStore(d.themes())
	a.lib = library.Empty()
	a.images = newImageCache(d.images(), filepath.Join(d.downloads(), "art"), 12<<20, int64(a.settings.PictureCacheMB)<<20, a.update)
	a.player = newPlayer(a)
	a.downloads = newDownloads(a)
	if s := a.settings.Session; s != nil {
		a.client = jellyfin.New(*s)
	}
	return a
}

// update runs fn on the interface's goroutine and draws a frame.
func (a *App) update(fn func()) {
	if a.win != nil {
		a.win.Update(func() {
			fn()
			if a.mini != nil {
				a.mini.Invalidate() // it shows the same state
			}
		})
		return
	}
	a.pendingMu.Lock()
	a.pending = append(a.pending, fn)
	a.pendingMu.Unlock()
}

// drain runs what update queued without a window, as tests do.
func (a *App) drain() {
	a.pendingMu.Lock()
	fns := a.pending
	a.pending = nil
	a.pendingMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// clientNow returns the server's client, nil when signed out, from any
// goroutine.
func (a *App) clientNow() *jellyfin.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client
}

func (a *App) setClient(c *jellyfin.Client) {
	a.mu.Lock()
	a.client = c
	a.mu.Unlock()
	a.listen() // for the orders of other devices, as this session
}

func (a *App) saveSettings() {
	if err := a.settings.save(a.dirs); err != nil {
		log.Print("saving the settings: ", err)
	}
}

// toasts are shown by the next frame, which has the context to show them.
var toasts []ui.Toast

func (a *App) toast(msg string) {
	toasts = append(toasts, ui.Toast{Title: msg})
	if a.win != nil {
		a.win.Invalidate()
	}
}

func (a *App) toastError(what string, err error) {
	t := ui.Toast{Title: what, Type: "error", Timeout: 6 * time.Second}
	if err != nil {
		t.Description = friendly(err)
	}
	toasts = append(toasts, t)
	if a.win != nil {
		a.win.Invalidate()
	}
}

// unreachable reports whether an error is of the network, not of the
// server: its name was not found, or nothing answered.
func unreachable(err error) bool {
	var dns *net.DNSError
	var op *net.OpError
	var timeout interface{ Timeout() bool }
	return errors.As(err, &dns) || errors.As(err, &op) || errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &timeout) && timeout.Timeout()
}

// start loads what the last run kept and signs in.
func (a *App) start() {
	switch {
	case a.settings.Session != nil:
		a.openLibrary()
		a.listen() // for the orders of other devices
		// And again now and then, for what is added while the app runs.
		go func() {
			for range time.Tick(30 * time.Minute) {
				a.update(a.sync)
			}
		}()
	case os.Getenv("JELLYFIN_URL") != "" && os.Getenv("JELLYFIN_USERNAME") != "":
		// Signing in from the environment, as development does.
		a.login.server, a.login.user, a.login.password = os.Getenv("JELLYFIN_URL"), os.Getenv("JELLYFIN_USERNAME"), os.Getenv("JELLYFIN_PASSWORD")
		a.signIn()
	}
}

// signedIn reports whether a session is open.
func (a *App) signedIn() bool { return a.settings.Session != nil }

func deviceName() string {
	host, _ := os.Hostname()
	if host == "" {
		host = runtime.GOOS
	}
	return host
}

// signIn signs in with what the login form holds.
func (a *App) signIn() {
	f := &a.login
	if f.busy {
		return
	}
	// The proxy of the form carries the sign-in itself.
	if err := a.setProxy(f.proxy); err != nil {
		f.err = "Proxy: " + err.Error()
		return
	}
	a.settings.Proxy = strings.TrimSpace(f.proxy)
	a.connection.proxy = a.settings.Proxy
	a.saveSettings()
	f.busy, f.err = true, ""
	server, user, password := f.server, f.user, f.password
	deviceID := a.settings.DeviceID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sess, err := jellyfin.Login(ctx, server, user, password, deviceID, deviceName())
		a.update(func() {
			f.busy = false
			if err != nil {
				f.err = friendly(err)
				// A server that cannot be reached at all may be one this
				// network blocks.
				if proxy.Load() == nil && unreachable(err) {
					f.err += " Where a network blocks the server, a proxy may reach it."
					f.proxyShown = true
				}
				return
			}
			f.password = ""
			a.settings.Session = sess
			a.saveSettings()
			a.setClient(jellyfin.New(*sess))
			a.router = ui.NewRouter("/home")
			a.router.Transition = ui.TransitionNone
			a.openLibrary()
		})
	}()
}

// signOut forgets the session, here and on the server.
func (a *App) signOut() {
	c := a.clientNow()
	a.player.stop()
	a.setClient(nil)
	a.settings.Session = nil
	a.saveSettings()
	a.setLibrary(library.Empty())
	a.syncErr = ""
	if c != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c.Logout(ctx)
		}()
	}
}

// openLibrary shows the library kept on disk, at once, and then reads the
// server's in the background.
func (a *App) openLibrary() {
	sess := a.settings.Session
	if sess == nil {
		return
	}
	path := a.dirs.libraryFile(sess)
	go func() {
		l, err := library.Load(path)
		debug.FreeOSMemory() // what reading the file took
		a.update(func() {
			if err == nil && a.settings.Session == sess {
				a.setLibrary(l)
			}
			a.sync()
		})
	}()
}

// sync reads the library from the server, unless it is being read.
func (a *App) sync() {
	c := a.clientNow()
	sess := a.settings.Session
	if a.syncing || c == nil || sess == nil {
		return
	}
	a.syncing, a.syncErr, a.progress = true, "", library.Progress{}
	path := a.dirs.libraryFile(sess)
	go func() {
		var last time.Time
		data, err := library.Sync(context.Background(), c, func(p library.Progress) {
			// The view need not hear of every page.
			if now := time.Now(); now.Sub(last) > 100*time.Millisecond || p.Done == p.Total {
				last = now
				a.update(func() { a.progress = p })
			}
		}, func(first *library.Data) {
			// A first run has nothing to show yet: the albums and the
			// artists, while the songs come.
			l := library.Build(*first)
			a.update(func() {
				if a.settings.Session == sess && a.lib.IsEmpty() {
					a.setLibrary(l)
				}
			})
		})
		var l *library.Library
		if err == nil {
			l = library.Build(*data)
			if err := library.Save(path, data); err != nil {
				log.Print("saving the library: ", err)
			}
		}
		data = nil
		a.update(func() {
			a.syncing = false
			// What reading the library took goes back to the system,
			// once the library before this one is let go.
			defer time.AfterFunc(2*time.Second, debug.FreeOSMemory)
			if a.settings.Session != sess {
				return // signed out meanwhile
			}
			switch {
			case errors.Is(err, jellyfin.ErrUnauthorized):
				a.signOut()
				a.login.err = "The server no longer accepts this sign-in. Sign in again."
			case err != nil:
				a.syncErr = friendly(err)
				a.offline = true
			default:
				a.offline = false
				a.setLibrary(l)
				a.warmImages()
				a.downloads.retry()
			}
		})
	}()
}

// setLibrary shows a library in place of the one shown.
func (a *App) setLibrary(l *library.Library) {
	a.lib = l
	a.libGen++
	// The orders the long pages show in are made now, once, so that the
	// pages open at once: sorting ten thousand songs takes ten
	// milliseconds, more than a frame has.
	if slices.Contains(songSorts, a.settings.SongSort) {
		a.songsBy(a.settings.SongSort)
	}
	a.songsBy(songSorts[0])
	if slices.Contains(albumSorts, a.settings.AlbumSort) {
		a.albumsBy(a.settings.AlbumSort)
	}
	a.restoreQueue()
	// The queue's songs are those of the library before: the same songs
	// of the new one take their place, so that a favorite set on one
	// shows on the other.
	p := a.player
	for _, list := range [][]*library.Song{p.queue, p.unshuffled} {
		for i, s := range list {
			if n := l.Song(s.ID); n != nil {
				list[i] = n
			}
		}
	}
}

// warmImages fetches the pictures of every album and artist to disk, a
// few at a time behind everything else, so that scrolling the library
// never waits for the network.
func (a *App) warmImages() {
	c := a.clientNow()
	if c == nil {
		return
	}
	px := a.pixels(tileArt)
	// As many as fit half the room pictures have, the albums added last
	// first: the rest are fetched as they show.
	budget := int(int64(a.settings.PictureCacheMB) << 20 / 2 / (40 << 10))
	albums := slices.Clone(a.albumsBy("Recently Added"))
	for _, al := range albums[:min(len(albums), budget)] {
		if al.ImageTag != "" {
			a.images.warm(imageKey(al.ID, al.ImageTag, px), c.ImageURL(al.ID, "Primary", al.ImageTag, px))
		}
	}
	budget -= len(albums)
	for i := range a.lib.Artists {
		if ar := &a.lib.Artists[i]; ar.ImageTag != "" && i < budget {
			a.images.warm(imageKey(ar.ID, ar.ImageTag, px), c.ImageURL(ar.ID, "Primary", ar.ImageTag, px))
		}
	}
}

// pinArt keeps the pictures of an item for good, at the sizes the app
// shows them, as those of what is downloaded.
func (a *App) pinArt(id, tag string) {
	c := a.clientNow()
	if c == nil || id == "" || tag == "" {
		return
	}
	for _, dip := range []float32{thumbArt, tileArt, heroArt} {
		px := a.pixels(dip)
		a.images.pin(imageKey(id, tag, px), c.ImageURL(id, "Primary", tag, px))
	}
}

// saveQueue keeps the queue for the next run.
func (a *App) saveQueue() {
	sess := a.settings.Session
	if sess == nil || a.player.far != nil {
		return // another device's queue is its own
	}
	p := a.player
	q := savedQueue{Server: sess.ServerID + "/" + sess.UserID, Index: p.index, Position: p.state().Position.Seconds()}
	// A queue of a whole library is kept from the song playing on.
	const limit = 2000
	start := 0
	if len(p.queue) > limit {
		start = max(0, min(p.index-10, len(p.queue)-limit))
	}
	for _, s := range p.queue[start:min(len(p.queue), start+limit)] {
		q.Songs = append(q.Songs, s.ID)
	}
	q.Index -= start
	if p.current() == nil {
		q = savedQueue{}
	}
	b, err := json.Marshal(q)
	if err != nil {
		return
	}
	file := a.dirs.queueFile()
	a.queueWrites.Add(1)
	go func() {
		defer a.queueWrites.Done()
		a.queueMu.Lock()
		defer a.queueMu.Unlock()
		os.MkdirAll(filepath.Dir(file), 0o755)
		if os.WriteFile(file+".tmp", b, 0o600) == nil {
			os.Rename(file+".tmp", file)
		}
	}()
}

// restoreQueue brings back the queue of the last run, once the library
// that holds its songs shows.
func (a *App) restoreQueue() {
	sess := a.settings.Session
	if a.queueRestored || sess == nil || len(a.lib.Songs) == 0 {
		return
	}
	a.queueRestored = true
	b, err := os.ReadFile(a.dirs.queueFile())
	if err != nil {
		return
	}
	var q savedQueue
	if json.Unmarshal(b, &q) != nil || q.Server != sess.ServerID+"/"+sess.UserID {
		return
	}
	var songs []*library.Song
	index := 0
	for i, id := range q.Songs {
		s := a.lib.Song(id)
		if s == nil {
			continue // gone from the library
		}
		if i == q.Index {
			index = len(songs)
		}
		songs = append(songs, s)
	}
	a.player.restore(songs, index, seconds(q.Position))
}

// setFavorite marks a song, an album or an artist, here at once and on
// the server.
func (a *App) setFavorite(id string, fav *bool, on bool) {
	c := a.clientNow()
	if c == nil {
		return
	}
	*fav = on
	a.favGen++
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.SetFavorite(ctx, id, on); err != nil {
			a.update(func() {
				*fav = !on
				a.favGen++
				a.toastError("Could not change the favorite", err)
			})
		}
	}()
}

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// playing is what the system is told plays.
type playing struct {
	title, artist, album string
	duration, position   time.Duration
	paused               bool
	// art is the file of the song's picture, "" while it is not on disk;
	// artURL is where the server has it.
	art, artURL string
	// id names the song, for systems that tell songs apart by it.
	id string
}

// coverPixels is the size of the picture the system is given: large
// enough for a lock screen.
const coverPixels = 512

// tellSystem keeps the system and the window's title telling what plays:
// when the song changes, pauses or plays on, when its picture arrives,
// and when it moves otherwise than a clock would move it, as a seek does.
func (a *App) tellSystem() {
	s := a.player.current()
	st := a.player.state()
	var now playing
	key := "nothing"
	if s != nil {
		now = playing{title: s.Name, artist: s.Artist, album: s.Album, duration: st.Duration, position: st.Position, paused: st.Paused, id: s.ID}
		if c := a.client; c != nil && s.ImageItem != "" && s.ImageTag != "" {
			now.artURL = c.ImageURL(s.ImageItem, "Primary", s.ImageTag, coverPixels)
			now.art = a.images.file(imageKey(s.ImageItem, s.ImageTag, coverPixels), now.artURL)
		}
		key = fmt.Sprint(s.ID, st.Paused, now.art != "", a.settings.Shuffle, a.settings.Repeat, int(a.settings.Volume*100))
		expected := a.toldFrom
		if !st.Paused {
			expected += time.Since(a.toldAt)
		}
		if d := st.Position - expected; a.told == key && d > -2*time.Second && d < 2*time.Second {
			a.system.progress(st.Position)
			return
		}
	} else if a.told == key {
		return
	}
	a.told, a.toldAt, a.toldFrom = key, time.Now(), st.Position
	func() {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("telling the system what plays: %v\n%s", err, debug.Stack())
			}
		}()
		a.system.set(now)
	}()
	title := "Aurelia"
	if s != nil {
		title = s.Name + " — " + s.Artist
	}
	if a.win != nil && a.win.Title() != title {
		a.win.SetTitle(title)
	}
}

// theme returns the theme to show: the one being edited, the one chosen,
// or Aurelia's own as the system's appearance is.
func (a *App) theme() *theme.Theme {
	if a.editing != nil {
		return a.editing.resolved()
	}
	if t := a.themes.Get(a.settings.Theme); t != nil {
		return t
	}
	if a.systemDark {
		return a.themes.Get(theme.DefaultDark)
	}
	return a.themes.Get(theme.DefaultLight)
}

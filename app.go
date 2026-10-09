package main

import (
	"context"
	"errors"
	"log"
	"os"
	"runtime"
	"runtime/debug"
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
	win      *mygo.Window
	dirs     dirs
	settings Settings
	router   *ui.Router
	scale    float64 // device pixels per point, for the size of pictures
	// silent keeps the sound card closed, as tests of the view do.
	silent  bool
	started bool

	themes *theme.Store
	// systemDark is the system's appearance, which "Match system"
	// follows.
	systemDark bool
	pal        *palette
	palFor     string // the theme the palette is of, and its appearance
	// editing is the theme being edited, which shows as it changes.
	editing *themeEditor

	images *imageCache
	player *player

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

	sizes   cacheSizes
	login   loginForm
	search  searchState
	pages   pageStates
	lyrics  lyricsState
	details map[string]*detail // what the server tells of artists, by ID

	// pending are the functions of update waiting for a frame, where no
	// window runs them.
	pendingMu sync.Mutex
	pending   []func()
}

func newApp(d dirs, silent bool) *App {
	a := &App{dirs: d, settings: loadSettings(d), router: ui.NewRouter("/home"), scale: 2, details: map[string]*detail{}, silent: silent}
	a.router.Transition = ui.TransitionNone // pages show at once
	a.themes = theme.NewStore(d.themes())
	a.lib = library.Empty()
	a.images = newImageCache(d.images(), 18<<20, a.update)
	a.player = newPlayer(a)
	if s := a.settings.Session; s != nil {
		a.client = jellyfin.New(*s)
	}
	return a
}

// update runs fn on the interface's goroutine and draws a frame.
func (a *App) update(fn func()) {
	if a.win != nil {
		a.win.Update(fn)
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

// friendly words an error for a person.
func friendly(err error) string {
	var netErr interface{ Timeout() bool }
	switch {
	case errors.Is(err, jellyfin.ErrUnauthorized):
		return "The server refused the sign-in."
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "The server took too long to answer."
	}
	return err.Error()
}

// start loads what the last run kept and signs in.
func (a *App) start() {
	switch {
	case a.settings.Session != nil:
		a.openLibrary()
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
			default:
				a.setLibrary(l)
				a.warmImages()
			}
		})
	}()
}

// setLibrary shows a library in place of the one shown.
func (a *App) setLibrary(l *library.Library) {
	a.lib = l
	a.libGen++
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
	for i := range a.lib.Albums {
		if al := &a.lib.Albums[i]; al.ImageTag != "" {
			a.images.warm(imageKey(al.ID, al.ImageTag, px), c.ImageURL(al.ID, "Primary", al.ImageTag, px))
		}
	}
	for i := range a.lib.Artists {
		if ar := &a.lib.Artists[i]; ar.ImageTag != "" {
			a.images.warm(imageKey(ar.ID, ar.ImageTag, px), c.ImageURL(ar.ID, "Primary", ar.ImageTag, px))
		}
	}
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

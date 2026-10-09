package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"aurelia/internal/jellyfin"
)

// Repeat modes.
const (
	repeatOff = iota
	repeatAll
	repeatOne
)

// Settings are what the app keeps between runs, in settings.json of its
// data directory.
type Settings struct {
	Session  *jellyfin.Session `json:"session,omitempty"`
	DeviceID string            `json:"deviceId"`
	// Theme is the ID of a theme, or "auto" for Aurelia's own, light or
	// dark as the system is.
	Theme     string  `json:"theme"`
	Volume    float64 `json:"volume"`
	Muted     bool    `json:"muted,omitempty"`
	Normalize bool    `json:"normalize"`
	// Level is how loud normalized songs play: "loud", "normal" or
	// "quiet".
	Level   string `json:"level,omitempty"`
	Shuffle bool   `json:"shuffle,omitempty"`
	Repeat  int    `json:"repeat,omitempty"`
	// MaxBitrate, in kbit/s, makes the server transcode above it; 0
	// plays files as they are.
	MaxBitrate int `json:"maxBitrate,omitempty"`
	// AudioCacheMB is the room the songs played lately may take on disk,
	// and PictureCacheMB the pictures.
	AudioCacheMB   int `json:"audioCacheMB"`
	PictureCacheMB int `json:"pictureCacheMB"`
	// Proxy is the address of a proxy to reach the network through, as
	// socks5://host:1080, for networks that block the server; "" is none.
	Proxy string `json:"proxy,omitempty"`
	// AutoUpdate installs new versions as they are released.
	AutoUpdate bool   `json:"autoUpdate"`
	AlbumSort  string `json:"albumSort,omitempty"`
	SongSort   string `json:"songSort,omitempty"`
	ArtistSort string `json:"artistSort,omitempty"`
	QueueOpen  bool   `json:"queueOpen,omitempty"`
	// HiddenRecent is the albums taken out of "Jump back in", with when:
	// one played since shows again.
	HiddenRecent map[string]int64 `json:"hiddenRecent,omitempty"`
	// SidebarHidden puts the sidebar away, and SidebarW is its width when
	// the user gave it one.
	SidebarHidden bool `json:"sidebarHidden,omitempty"`
	SidebarW      int  `json:"sidebarW,omitempty"`
	// MiniPinned keeps the small window of the record above the others.
	MiniPinned bool `json:"miniPinned,omitempty"`
	// Autoplay goes on with songs like the last when the queue ends.
	Autoplay bool `json:"autoplay,omitempty"`
	// Motion is "" to follow the system's Reduce motion, "on" or "reduced".
	Motion string `json:"motion,omitempty"`
	// OutputDevice is the ID of the output to play on, "" for the
	// system's default, and OutputName what it was called, for when it is
	// not plugged in.
	OutputDevice string `json:"outputDevice,omitempty"`
	OutputName   string `json:"outputName,omitempty"`
	// EQ is the equalizer, and EQPresets the presets the user saved.
	EQ        EQSettings `json:"eq"`
	EQPresets []EQPreset `json:"eqPresets,omitempty"`
	// Mono plays both channels as one; Balance goes from -1, the left
	// channel alone, to 1, the right.
	Mono    bool    `json:"mono,omitempty"`
	Balance float64 `json:"balance,omitempty"`
	// OthersPlaylists shows the playlists other people of the server
	// made public, among the user's own.
	OthersPlaylists bool `json:"othersPlaylists,omitempty"`
}

// The caches are small by default: the pictures of a library, and the
// songs of an evening. What is to stay is downloaded instead.
const (
	defaultAudioCacheMB   = 300
	defaultPictureCacheMB = 150
)

func defaultSettings() Settings {
	return Settings{Theme: "auto", Volume: 0.8, AudioCacheMB: defaultAudioCacheMB, PictureCacheMB: defaultPictureCacheMB, AutoUpdate: true}
}

// dirs are where the app keeps things.
type dirs struct {
	data  string // settings, themes, the library
	cache string // pictures and songs, which come again
}

// appDirs returns the app's directories; AURELIA_DIR names another place
// for both, as tests and development do.
func appDirs() dirs {
	if d := os.Getenv("AURELIA_DIR"); d != "" {
		return dirs{data: d, cache: filepath.Join(d, "cache")}
	}
	var d dirs
	var err error
	if d.data, err = mygo.App.Path(mygo.PathUserData); err != nil {
		home, _ := os.UserHomeDir()
		d.data = filepath.Join(home, ".aurelia")
	}
	if d.cache, err = mygo.App.Path(mygo.PathCache); err != nil {
		d.cache = filepath.Join(d.data, "cache")
	}
	return d
}

func (d dirs) settingsFile() string { return filepath.Join(d.data, "settings.json") }
func (d dirs) queueFile() string    { return filepath.Join(d.data, "queue.json") }
func (d dirs) themes() string       { return filepath.Join(d.data, "themes") }
func (d dirs) images() string       { return filepath.Join(d.cache, "images") }
func (d dirs) audio() string        { return filepath.Join(d.cache, "audio") }

// downloads is where the songs downloaded for offline are, with their
// pictures: with the app's data, which the system does not clear as it
// may a cache.
func (d dirs) downloads() string { return filepath.Join(d.data, "downloads") }

// libraryFile is where the library of a user of a server is kept.
func (d dirs) libraryFile(s *jellyfin.Session) string {
	return filepath.Join(d.data, "library-"+s.ServerID+"-"+s.UserID+".json.gz")
}

func loadSettings(d dirs) Settings {
	s := defaultSettings()
	if b, err := os.ReadFile(d.settingsFile()); err == nil {
		json.Unmarshal(b, &s)
	}
	if s.DeviceID == "" {
		var b [12]byte
		rand.Read(b[:])
		s.DeviceID = "aurelia-" + hex.EncodeToString(b[:])
	}
	if s.Theme == "" {
		s.Theme = "auto"
	}
	if s.AudioCacheMB <= 0 {
		s.AudioCacheMB = defaultAudioCacheMB
	}
	if s.PictureCacheMB <= 0 {
		s.PictureCacheMB = defaultPictureCacheMB
	}
	s.Volume = max(0, min(1, s.Volume))
	return s
}

// save writes the settings, which hold the session's token: for the user
// alone to read.
func (s *Settings) save(d dirs) error {
	if err := os.MkdirAll(d.data, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := d.settingsFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, d.settingsFile())
}

// savedQueue is the queue as it is kept between runs: the songs by their
// IDs, which one played, and where it was.
type savedQueue struct {
	Server   string   `json:"server"`
	Songs    []string `json:"songs"`
	Index    int      `json:"index"`
	Position float64  `json:"position"` // seconds
}

// levels are how loud normalized songs play, as Spotify has them: the
// loudness each aims at, and what that adds to the gain the server
// measured, which aims at -18 LUFS.
var levels = []struct {
	id, name string
	db       float64
}{
	{"loud", "Louder", 7},    // -11 LUFS
	{"normal", "Normal", 4},  // -14 LUFS
	{"quiet", "Quieter", -1}, // -19 LUFS
}

// levelDB is what the level of the settings adds to a song's gain.
func (s *Settings) levelDB() float64 {
	for _, l := range levels {
		if l.id == s.Level {
			return l.db
		}
	}
	return 4
}

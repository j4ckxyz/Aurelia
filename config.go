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
	Shuffle   bool    `json:"shuffle,omitempty"`
	Repeat    int     `json:"repeat,omitempty"`
	// MaxBitrate, in kbit/s, makes the server transcode above it; 0
	// plays files as they are.
	MaxBitrate int `json:"maxBitrate,omitempty"`
	// AudioCacheMB is the room songs may take on disk.
	AudioCacheMB int    `json:"audioCacheMB"`
	AlbumSort    string `json:"albumSort,omitempty"`
	SongSort     string `json:"songSort,omitempty"`
	ArtistSort   string `json:"artistSort,omitempty"`
	QueueOpen    bool   `json:"queueOpen,omitempty"`
}

func defaultSettings() Settings {
	return Settings{Theme: "auto", Volume: 0.8, AudioCacheMB: 2048, AlbumSort: "name", SongSort: "title"}
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
func (d dirs) themes() string       { return filepath.Join(d.data, "themes") }
func (d dirs) images() string       { return filepath.Join(d.cache, "images") }
func (d dirs) audio() string        { return filepath.Join(d.cache, "audio") }

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
		s.AudioCacheMB = 2048
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

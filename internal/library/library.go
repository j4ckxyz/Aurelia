// Package library keeps the whole music library of a server in memory
// and on disk, so that every page and every search is answered at once,
// without the network.
package library

import (
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"aurelia/internal/jellyfin"
)

// Album is an album of the library.
type Album struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Artist    string   `json:"artist,omitempty"` // as shown: the album's artists, joined
	ArtistIDs []string `json:"artistIds,omitempty"`
	Year      int      `json:"year,omitempty"`
	ImageTag  string   `json:"image,omitempty"`
	Added     int64    `json:"added,omitempty"` // seconds since 1970
	Favorite  bool     `json:"favorite,omitempty"`
	Songs     int      `json:"songs,omitempty"`
	Seconds   float64  `json:"seconds,omitempty"`
	Genres    []string `json:"genres,omitempty"`

	nameKey, extraKey string
	// SortKey and ArtistKey order albums by name and by artist.
	SortKey, ArtistKey string
}

// Artist is an artist that albums are by.
type Artist struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ImageTag    string `json:"image,omitempty"`
	BackdropTag string `json:"backdrop,omitempty"`
	Favorite    bool   `json:"favorite,omitempty"`

	nameKey, SortKey string
}

// Song is a song of the library.
type Song struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Album          string   `json:"album,omitempty"`
	AlbumID        string   `json:"albumId,omitempty"`
	Artist         string   `json:"artist,omitempty"` // as shown: the song's artists, joined
	ArtistIDs      []string `json:"artistIds,omitempty"`
	AlbumArtistIDs []string `json:"albumArtistIds,omitempty"`
	Track          int      `json:"track,omitempty"`
	Disc           int      `json:"disc,omitempty"`
	Year           int      `json:"year,omitempty"`
	Seconds        float64  `json:"seconds,omitempty"`
	// ImageItem and ImageTag name the song's picture: its album's, so
	// that the songs of an album share one.
	ImageItem  string  `json:"imageItem,omitempty"`
	ImageTag   string  `json:"image,omitempty"`
	Favorite   bool    `json:"favorite,omitempty"`
	Plays      int     `json:"plays,omitempty"`
	LastPlayed int64   `json:"lastPlayed,omitempty"`
	Added      int64   `json:"added,omitempty"`
	GainDB     float64 `json:"gain,omitempty"`
	HasLyrics  bool    `json:"lyrics,omitempty"`

	nameKey, extraKey string
	// SortKey, ArtistKey and AlbumKey order songs by title, artist and
	// album.
	SortKey, ArtistKey, AlbumKey string
}

// Duration is the song's length.
func (s *Song) Duration() time.Duration {
	return time.Duration(s.Seconds * float64(time.Second))
}

// Playlist is a playlist of the user's.
type Playlist struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageTag string `json:"image,omitempty"`
	Songs    int    `json:"songs,omitempty"`

	nameKey string
}

// Data is the library as it is kept on disk.
type Data struct {
	Version   int        `json:"version"`
	SyncedAt  time.Time  `json:"syncedAt"`
	Albums    []Album    `json:"albums"`
	Artists   []Artist   `json:"artists"`
	Songs     []Song     `json:"songs"`
	Playlists []Playlist `json:"playlists"`
}

const dataVersion = 1

// Library is the library with its indexes. It is read and changed on one
// goroutine, the interface's; Sync builds a new one elsewhere.
type Library struct {
	Data

	albums    map[string]*Album
	artists   map[string]*Artist
	songs     map[string]*Song
	playlists map[string]*Playlist

	albumSongs   map[string][]*Song  // in the album's order
	artistAlbums map[string][]*Album // newest first
	artistSongs  map[string][]*Song  // the songs an artist is on
	appearsOn    map[string][]*Album // albums of others with songs of the artist
}

// Empty returns a library with nothing in it.
func Empty() *Library { return Build(Data{Version: dataVersion}) }

// Build indexes data.
func Build(d Data) *Library {
	l := &Library{
		Data:         d,
		albums:       make(map[string]*Album, len(d.Albums)),
		artists:      make(map[string]*Artist, len(d.Artists)),
		songs:        make(map[string]*Song, len(d.Songs)),
		playlists:    make(map[string]*Playlist, len(d.Playlists)),
		albumSongs:   make(map[string][]*Song, len(d.Albums)),
		artistAlbums: make(map[string][]*Album, len(d.Artists)),
		artistSongs:  make(map[string][]*Song, len(d.Artists)),
		appearsOn:    map[string][]*Album{},
	}
	// The same names come again and again: one string for each.
	names := map[string]string{}
	intern := func(s string) string {
		if v, ok := names[s]; ok {
			return v
		}
		names[s] = s
		return s
	}
	for i := range l.Artists {
		a := &l.Artists[i]
		a.nameKey = Fold(a.Name)
		a.SortKey = sortKey(a.Name)
		l.artists[a.ID] = a
	}
	for i := range l.Albums {
		a := &l.Albums[i]
		a.Artist = intern(a.Artist)
		a.nameKey, a.extraKey = Fold(a.Name), intern(Fold(a.Artist))
		a.SortKey, a.ArtistKey = sortKey(a.Name), intern(sortKey(a.Artist))
		l.albums[a.ID] = a
		for _, id := range a.ArtistIDs {
			l.artistAlbums[id] = append(l.artistAlbums[id], a)
		}
	}
	for i := range l.Songs {
		s := &l.Songs[i]
		s.Album, s.Artist = intern(s.Album), intern(s.Artist)
		s.nameKey, s.extraKey = Fold(s.Name), intern(Fold(s.Artist+" "+s.Album))
		s.SortKey, s.ArtistKey, s.AlbumKey = s.nameKey, intern(sortKey(s.Artist)), intern(sortKey(s.Album))
		l.songs[s.ID] = s
		if s.AlbumID != "" {
			l.albumSongs[s.AlbumID] = append(l.albumSongs[s.AlbumID], s)
		}
		for _, id := range s.ArtistIDs {
			l.artistSongs[id] = append(l.artistSongs[id], s)
		}
		for _, id := range s.AlbumArtistIDs {
			if !slices.Contains(s.ArtistIDs, id) {
				l.artistSongs[id] = append(l.artistSongs[id], s)
			}
		}
	}
	for i := range l.Playlists {
		p := &l.Playlists[i]
		p.nameKey = Fold(p.Name)
		l.playlists[p.ID] = p
	}
	for id, songs := range l.albumSongs {
		slices.SortStableFunc(songs, func(a, b *Song) int {
			return cmp.Or(cmp.Compare(a.Disc, b.Disc), cmp.Compare(a.Track, b.Track), cmp.Compare(a.nameKey, b.nameKey))
		})
		if a := l.albums[id]; a != nil {
			a.Songs, a.Seconds = len(songs), 0
			for _, s := range songs {
				a.Seconds += s.Seconds
			}
		}
	}
	for _, albums := range l.artistAlbums {
		slices.SortStableFunc(albums, func(a, b *Album) int {
			return cmp.Or(cmp.Compare(b.Year, a.Year), cmp.Compare(a.SortKey, b.SortKey))
		})
	}
	// Albums an artist sings on without being theirs.
	for id, songs := range l.artistSongs {
		seen := map[string]bool{}
		for _, s := range songs {
			a := l.albums[s.AlbumID]
			if a == nil || seen[a.ID] || slices.Contains(a.ArtistIDs, id) {
				continue
			}
			seen[a.ID] = true
			l.appearsOn[id] = append(l.appearsOn[id], a)
		}
	}
	return l
}

// Album returns the album of an ID, or nil.
func (l *Library) Album(id string) *Album { return l.albums[id] }

// Artist returns the artist of an ID, or nil.
func (l *Library) Artist(id string) *Artist { return l.artists[id] }

// Song returns the song of an ID, or nil.
func (l *Library) Song(id string) *Song { return l.songs[id] }

// Playlist returns the playlist of an ID, or nil.
func (l *Library) Playlist(id string) *Playlist { return l.playlists[id] }

// AlbumSongs returns an album's songs, in its order.
func (l *Library) AlbumSongs(id string) []*Song { return l.albumSongs[id] }

// ArtistAlbums returns an artist's albums, the newest first.
func (l *Library) ArtistAlbums(id string) []*Album { return l.artistAlbums[id] }

// ArtistSongs returns the songs an artist is on.
func (l *Library) ArtistSongs(id string) []*Song { return l.artistSongs[id] }

// AppearsOn returns the albums of others that an artist has songs on.
func (l *Library) AppearsOn(id string) []*Album { return l.appearsOn[id] }

// IsEmpty reports whether the library holds nothing yet.
func (l *Library) IsEmpty() bool {
	return len(l.Albums) == 0 && len(l.Songs) == 0 && len(l.Artists) == 0
}

// Load reads the library kept at path.
func Load(path string) (*Library, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var d Data
	if err := json.NewDecoder(zr).Decode(&d); err != nil {
		return nil, err
	}
	if d.Version != dataVersion {
		return nil, os.ErrNotExist // of another version: synced anew
	}
	return Build(d), nil
}

// Save writes data to path, in place of what is there, whole or not at
// all.
func Save(path string, d *Data) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".library-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	zw, _ := gzip.NewWriterLevel(tmp, gzip.BestSpeed)
	if err := json.NewEncoder(zw).Encode(d); err != nil {
		tmp.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Progress tells how far a sync is.
type Progress struct {
	Done, Total int // items
}

// Sync reads the whole library from the server: the albums, artists and
// playlists first, which partial is called with as soon as they are in,
// so that a first run shows them while the songs, many more, still come.
// progress is called from several goroutines, never at once.
func Sync(ctx context.Context, c *jellyfin.Client, progress func(Progress), partial func(*Data)) (*Data, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	nAlbums, err := c.Count(ctx, "MusicAlbum")
	if err != nil {
		return nil, err
	}
	nSongs, err := c.Count(ctx, "Audio")
	if err != nil {
		return nil, err
	}
	const albumPage, songPage = 500, 1000
	albumPages := make([][]jellyfin.Item, (nAlbums+albumPage-1)/albumPage)
	songPages := make([][]jellyfin.Item, (nSongs+songPage-1)/songPage)
	var artists, playlists []jellyfin.Item

	var (
		mu       sync.Mutex
		firstErr error
		done     int
	)
	total := nAlbums + nSongs
	report := func(n int) {
		mu.Lock()
		done += n
		if progress != nil {
			progress(Progress{Done: min(done, total), Total: total})
		}
		mu.Unlock()
	}
	// A few requests at a time: a server lists a thousand songs in
	// seconds, and should not be asked for all of them at once.
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			if err := fn(); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
			}
		}()
	}
	run(func() (err error) { artists, err = c.AlbumArtists(ctx); return })
	run(func() (err error) { playlists, err = c.Playlists(ctx); return })
	for i := range albumPages {
		run(func() (err error) {
			albumPages[i], err = c.Albums(ctx, i*albumPage, albumPage)
			report(len(albumPages[i]))
			return
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	d := &Data{Version: dataVersion}
	for _, it := range artists {
		a := Artist{ID: it.ID, Name: it.Name, ImageTag: it.ImageTags["Primary"], Favorite: it.UserData.IsFavorite}
		if len(it.BackdropImageTags) > 0 {
			a.BackdropTag = it.BackdropImageTags[0]
		}
		d.Artists = append(d.Artists, a)
	}
	for _, it := range playlists {
		d.Playlists = append(d.Playlists, Playlist{ID: it.ID, Name: it.Name, ImageTag: it.ImageTags["Primary"], Songs: it.ChildCount})
	}
	seen := map[string]bool{}
	for _, p := range albumPages {
		for i := range p {
			if it := &p[i]; !seen[it.ID] {
				seen[it.ID] = true
				d.Albums = append(d.Albums, AlbumOf(it))
			}
		}
	}
	if partial != nil {
		first := *d
		first.Albums, first.Artists, first.Playlists = slices.Clone(d.Albums), slices.Clone(d.Artists), slices.Clone(d.Playlists)
		partial(&first)
	}
	for i := range songPages {
		run(func() (err error) {
			songPages[i], err = c.Songs(ctx, i*songPage, songPage)
			report(len(songPages[i]))
			return
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	for _, p := range songPages {
		for i := range p {
			if it := &p[i]; !seen[it.ID] {
				seen[it.ID] = true
				d.Songs = append(d.Songs, SongOf(it))
			}
		}
	}
	d.SyncedAt = time.Now()
	return d, nil
}

func ids(refs []jellyfin.NameID) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.ID
	}
	return out
}

func unix(t *time.Time) int64 {
	if t == nil || t.IsZero() {
		return 0
	}
	return t.Unix()
}

// AlbumOf converts an album of the server's.
func AlbumOf(it *jellyfin.Item) Album {
	a := Album{
		ID: it.ID, Name: it.Name, ArtistIDs: ids(it.AlbumArtists), Year: it.ProductionYear,
		ImageTag: it.ImageTags["Primary"], Added: unix(it.DateCreated), Favorite: it.UserData.IsFavorite,
		Songs: it.ChildCount, Seconds: it.Duration().Seconds(), Genres: it.Genres,
	}
	a.ArtistIDs = slices.Compact(a.ArtistIDs)
	names := make([]string, 0, len(it.AlbumArtists))
	for _, r := range it.AlbumArtists {
		if !slices.Contains(names, r.Name) { // servers list an artist twice at times
			names = append(names, r.Name)
		}
	}
	a.Artist = strings.Join(names, ", ")
	if a.Artist == "" {
		a.Artist = it.AlbumArtist
	}
	return a
}

// SongOf converts a song of the server's.
func SongOf(it *jellyfin.Item) Song {
	s := Song{
		ID: it.ID, Name: it.Name, Album: it.Album, AlbumID: it.AlbumID,
		ArtistIDs: ids(it.ArtistItems), AlbumArtistIDs: ids(it.AlbumArtists),
		Track: it.IndexNumber, Disc: it.ParentIndexNumber, Year: it.ProductionYear,
		Seconds: it.Duration().Seconds(), Favorite: it.UserData.IsFavorite, Plays: it.UserData.PlayCount,
		LastPlayed: unix(it.UserData.LastPlayedDate), Added: unix(it.DateCreated), HasLyrics: it.HasLyrics,
	}
	s.Artist = strings.Join(it.Artists, ", ")
	if s.Artist == "" {
		s.Artist = it.AlbumArtist
	}
	if it.NormalizationGain != nil {
		s.GainDB = *it.NormalizationGain
	}
	switch {
	case it.AlbumID != "" && it.AlbumPrimaryImageTag != "":
		s.ImageItem, s.ImageTag = it.AlbumID, it.AlbumPrimaryImageTag
	case it.ImageTags["Primary"] != "":
		s.ImageItem, s.ImageTag = it.ID, it.ImageTags["Primary"]
	}
	return s
}

// PrepareSong fills what Build would for a song that is not of the
// library, as one a playlist or the server's search returns.
func PrepareSong(s *Song) {
	s.nameKey, s.extraKey = Fold(s.Name), Fold(s.Artist+" "+s.Album)
	s.SortKey, s.ArtistKey, s.AlbumKey = s.nameKey, sortKey(s.Artist), sortKey(s.Album)
}

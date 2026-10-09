package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aurelia/internal/jellyfin"
)

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"Beyoncé":                 "beyonce",
		"  The  Dark Side…of the": "the dark side of the",
		"Don’t Stop Me Now":       "dont stop me now",
		"AC/DC":                   "ac dc",
		"Sigur Rós":               "sigur ros",
		"÷ (Deluxe)":              "deluxe",
	} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
	if got := sortKey("The Beatles"); got != "beatles" {
		t.Errorf("sortKey = %q", got)
	}
}

func sample() *Library {
	return Build(Data{
		Version: dataVersion,
		Artists: []Artist{{ID: "ar1", Name: "Ed Sheeran"}, {ID: "ar2", Name: "Beyoncé"}},
		Albums: []Album{
			{ID: "al1", Name: "Divide", Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}, Year: 2017},
			{ID: "al2", Name: "Lemonade", Artist: "Beyoncé", ArtistIDs: []string{"ar2"}, Year: 2016},
			{ID: "al3", Name: "Plus", Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}, Year: 2011},
		},
		Songs: []Song{
			{ID: "s2", Name: "Perfect", Album: "Divide", AlbumID: "al1", Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}, AlbumArtistIDs: []string{"ar1"}, Track: 5, Disc: 1, Seconds: 263},
			{ID: "s1", Name: "Shape of You", Album: "Divide", AlbumID: "al1", Artist: "Ed Sheeran", ArtistIDs: []string{"ar1"}, AlbumArtistIDs: []string{"ar1"}, Track: 4, Disc: 1, Seconds: 233, Plays: 9},
			{ID: "s3", Name: "Perfect Duet", Album: "Lemonade", AlbumID: "al2", Artist: "Ed Sheeran, Beyoncé", ArtistIDs: []string{"ar1", "ar2"}, AlbumArtistIDs: []string{"ar2"}, Track: 1, Disc: 1, Seconds: 259},
		},
		Playlists: []Playlist{{ID: "p1", Name: "Perfect mornings"}},
	})
}

func TestIndexes(t *testing.T) {
	l := sample()
	songs := l.AlbumSongs("al1")
	if len(songs) != 2 || songs[0].ID != "s1" || songs[1].ID != "s2" {
		t.Errorf("album songs out of order: %v", songs)
	}
	if a := l.Album("al1"); a.Songs != 2 || a.Seconds != 496 {
		t.Errorf("album totals: %d songs, %v s", a.Songs, a.Seconds)
	}
	if albums := l.ArtistAlbums("ar1"); len(albums) != 2 || albums[0].ID != "al1" {
		t.Errorf("artist albums: newest first wanted, got %v", albums)
	}
	if on := l.AppearsOn("ar1"); len(on) != 1 || on[0].ID != "al2" {
		t.Errorf("appears on: %v", on)
	}
	if len(l.ArtistSongs("ar2")) != 1 || len(l.ArtistSongs("ar1")) != 3 {
		t.Errorf("artist songs: %d, %d", len(l.ArtistSongs("ar1")), len(l.ArtistSongs("ar2")))
	}
}

func TestSearch(t *testing.T) {
	l := sample()
	r := l.Search("perf", Limits{})
	if len(r.Songs) != 2 || r.Songs[0].ID != "s2" || len(r.Playlists) != 1 {
		t.Errorf("perf: songs %v, playlists %v", r.Songs, r.Playlists)
	}
	if r := l.Search("beyonce", Limits{}); len(r.Artists) != 1 || len(r.Albums) != 1 || len(r.Songs) != 1 {
		t.Errorf("beyonce: %d artists, %d albums, %d songs", len(r.Artists), len(r.Albums), len(r.Songs))
	}
	// Words of the name and of the artist together.
	if r := l.Search("sheeran shape", Limits{}); len(r.Songs) != 1 || r.Songs[0].ID != "s1" {
		t.Errorf("sheeran shape: %v", r.Songs)
	}
	if r := l.Search("sheeran", Limits{Songs: 1}); len(r.Songs) != 1 || r.Songs[0].ID != "s1" {
		t.Errorf("limit and plays: %v", r.Songs)
	}
	if r := l.Search("  ", Limits{}); !r.Empty() {
		t.Error("an empty query found something")
	}
	if r := l.Search("zzz", Limits{}); !r.Empty() {
		t.Error("zzz found something")
	}
}

func TestSaveAndLoad(t *testing.T) {
	l := sample()
	path := filepath.Join(t.TempDir(), "lib.json.gz")
	if err := Save(path, &l.Data); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Songs) != 3 || got.Song("s1").Name != "Shape of You" || len(got.AlbumSongs("al1")) != 2 {
		t.Errorf("loaded %d songs", len(got.Songs))
	}
}

// The whole library of a real server, when JELLYFIN_URL and the rest name
// one.
func TestSyncWithAServer(t *testing.T) {
	url, user, pw := os.Getenv("JELLYFIN_URL"), os.Getenv("JELLYFIN_USERNAME"), os.Getenv("JELLYFIN_PASSWORD")
	if url == "" || user == "" {
		t.Skip("JELLYFIN_URL names no server")
	}
	ctx := context.Background()
	sess, err := jellyfin.Login(ctx, url, user, pw, "aurelia-test", "Aurelia tests")
	if err != nil {
		t.Fatal(err)
	}
	c := jellyfin.New(*sess)
	defer c.Logout(ctx)
	start := time.Now()
	var last Progress
	var firstAfter time.Duration
	d, err := Sync(ctx, c, func(p Progress) { last = p }, func(first *Data) {
		firstAfter = time.Since(start)
		if len(first.Albums) == 0 || len(first.Songs) != 0 {
			t.Errorf("the first part has %d albums and %d songs", len(first.Albums), len(first.Songs))
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	start = time.Now()
	l := Build(*d)
	built := time.Since(start)
	t.Logf("albums and artists after %v", firstAfter)
	t.Logf("synced %d albums, %d artists, %d songs, %d playlists in %v (progress %d/%d); indexed in %v",
		len(l.Albums), len(l.Artists), len(l.Songs), len(l.Playlists), took, last.Done, last.Total, built)
	if len(l.Albums) == 0 || len(l.Songs) == 0 || len(l.Artists) == 0 {
		t.Fatal("an empty library")
	}
	withSongs, withImage := 0, 0
	for i := range l.Albums {
		if len(l.AlbumSongs(l.Albums[i].ID)) > 0 {
			withSongs++
		}
		if l.Albums[i].ImageTag != "" {
			withImage++
		}
	}
	t.Logf("%d of %d albums have songs, %d a picture", withSongs, len(l.Albums), withImage)
	if withSongs < len(l.Albums)*8/10 {
		t.Errorf("only %d of %d albums have songs", withSongs, len(l.Albums))
	}
	path := filepath.Join(t.TempDir(), "lib.json.gz")
	start = time.Now()
	if err := Save(path, d); err != nil {
		t.Fatal(err)
	}
	saved := time.Since(start)
	st, _ := os.Stat(path)
	start = time.Now()
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	t.Logf("saved %d KB in %v, loaded and indexed in %v", st.Size()>>10, saved, time.Since(start))
	start = time.Now()
	const n = 200
	for i := 0; i < n; i++ {
		l.Search("love you", Limits{Artists: 8, Albums: 12, Songs: 50})
	}
	r := l.Search("love you", Limits{Artists: 8, Albums: 12, Songs: 50})
	t.Logf("a search of the library takes %v; \"love you\" finds %d songs, the first %q", time.Since(start)/n, len(r.Songs), r.Songs[0].Name)

	// What else the client calls, on a real album and song.
	al := &l.Albums[len(l.Albums)/2]
	songs, err := c.AlbumSongs(ctx, al.ID)
	if err != nil || len(songs) == 0 {
		t.Errorf("AlbumSongs(%q): %d songs, %v", al.Name, len(songs), err)
	}
	if len(l.Playlists) > 0 {
		if _, err := c.PlaylistItems(ctx, l.Playlists[0].ID); err != nil {
			t.Errorf("PlaylistItems: %v", err)
		}
	}
	for i := range l.Songs {
		if l.Songs[i].HasLyrics {
			lines, err := c.Lyrics(ctx, l.Songs[i].ID)
			if err != nil || len(lines) == 0 {
				t.Errorf("Lyrics(%q): %d lines, %v", l.Songs[i].Name, len(lines), err)
			}
			break
		}
	}
	if found, err := c.SearchSongs(ctx, "love", 5); err != nil || len(found) == 0 {
		t.Errorf("SearchSongs: %d, %v", len(found), err)
	}
	if _, err := c.Item(ctx, l.Artists[0].ID); err != nil {
		t.Errorf("Item: %v", err)
	}
}

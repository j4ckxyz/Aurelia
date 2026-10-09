package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
)

func flacSource() *jellyfin.MediaSource {
	return &jellyfin.MediaSource{
		Container: "flac", Size: 31 << 20, Bitrate: 912000,
		MediaStreams: []jellyfin.MediaStream{
			{Type: "Audio", Codec: "flac", SampleRate: 96000, BitDepth: 24, BitRate: 900000, Channels: 2},
		},
	}
}

func TestFileLine(t *testing.T) {
	if got, want := fileLine(flacSource()), "FLAC, 96 kHz, 24-bit, stereo, 900 kbps"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	mp3 := &jellyfin.MediaSource{Container: "mp3", Bitrate: 320000, MediaStreams: []jellyfin.MediaStream{
		{Type: "Video", Codec: "mjpeg"}, {Type: "Audio", Codec: "mp3", SampleRate: 44100, Channels: 2},
	}}
	if got, want := fileLine(mp3), "MP3, 44.1 kHz, stereo, 320 kbps"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := fileLine(&jellyfin.MediaSource{Container: "ogg", Bitrate: 160000}); got != "OGG, 160 kbps" {
		t.Errorf("a file without streams: %q", got)
	}
}

func TestPlayingAs(t *testing.T) {
	src := flacSource()
	for _, tc := range []struct {
		name       string
		src        *jellyfin.MediaSource
		max, out   int
		downloaded bool
		want       string
	}{
		{"as the file, resampled", src, 0, 44100, false, "FLAC, as the file is; resampled from 96 kHz to 44.1 kHz for the output"},
		{"as the file, same rate", &jellyfin.MediaSource{MediaStreams: []jellyfin.MediaStream{{Type: "Audio", Codec: "flac", SampleRate: 44100}}}, 0, 44100, false, "FLAC, as the file is"},
		{"converted for a low limit", src, 128, 44100, false, "MP3 at up to 128 kbps, converted by the server from FLAC"},
		{"under the limit", src, 2000, 44100, false, "FLAC, as the file is; resampled from 96 kHz to 44.1 kHz for the output"},
		{"downloaded", src, 128, 44100, true, "FLAC, the file kept on this computer; resampled from 96 kHz to 44.1 kHz for the output"},
		{"an ogg file", &jellyfin.MediaSource{MediaStreams: []jellyfin.MediaStream{{Type: "Audio", Codec: "vorbis", SampleRate: 44100}}}, 0, 44100, false, "FLAC, converted by the server from Ogg Vorbis"},
		{"no sound card", src, 0, 0, false, "FLAC, as the file is"},
	} {
		if got := playingAs(tc.src, tc.max, tc.out, tc.downloaded); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
	if playingAs(&jellyfin.MediaSource{}, 0, 44100, false) != "" {
		t.Error("a file without audio is said to play")
	}
}

func TestInfoDialog(t *testing.T) {
	a := testAppWith(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/Items/s1") || !strings.Contains(r.URL.RawQuery, "MediaSources") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"Id":"s1","Path":"/music/Beyonce/Formation.flac","MediaSources":[{"Container":"flac","Size":32505856,"Bitrate":912000,
			"MediaStreams":[{"Type":"Audio","Codec":"flac","SampleRate":44100,"BitDepth":16,"BitRate":912000,"Channels":2}]}]}`))
	}))
	tt := ui.NewTester(a.view, 1240, 800)
	tt.Frame()
	s := a.lib.Song("s1")
	a.openInfo(s)
	tt.Frame()
	wantTexts(t, tt, s.Name, "Artist", "Album", "Length")
	wait(t, a, "the file's details", func() bool { return a.info.item != nil || a.info.err != nil })
	tt.Frame()
	if a.info.err != nil {
		t.Fatal(a.info.err)
	}
	wantTexts(t, tt, "File", "FLAC, 44.1 kHz, 16-bit, stereo, 912 kbps", "Size", "31 MB", "/music/Beyonce/Formation.flac")
	// Through the song's own menu.
	a.info = infoState{}
	a.router.Push("/album/al1")
	tt.Frame()
	if err := tt.RightClick(s.Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Song Info…"); err != nil {
		t.Fatalf("%v; the menu: %q", err, tt.Menu())
	}
	if !a.info.open || a.info.song != s {
		t.Errorf("the menu did not open the details: %+v", a.info)
	}
}

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
)

// The details of a song: what the library says of it, and what the server
// says of its file, with how it is played.

// infoState is the dialog of a song's details.
type infoState struct {
	open    bool
	song    *library.Song
	item    *jellyfin.Item
	err     error
	loading bool
}

// openInfo shows the details of a song, and asks the server for those of
// its file.
func (a *App) openInfo(s *library.Song) {
	in := &a.info
	*in = infoState{open: true, song: s}
	cl := a.clientNow()
	if cl == nil {
		return
	}
	in.loading = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		it, err := cl.MediaInfo(ctx, s.ID)
		a.update(func() {
			if in.song != s {
				return // another song's details are open
			}
			in.loading, in.item, in.err = false, it, err
		})
	}()
}

// khz writes a sample rate: 44.1 kHz.
func khz(rate int) string {
	if rate <= 0 {
		return ""
	}
	if rate%1000 == 0 {
		return fmt.Sprintf("%d kHz", rate/1000)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(rate)/1000), ".0") + " kHz"
}

// kbps writes a bit rate: 320 kbps.
func kbps(bits int) string {
	if bits <= 0 {
		return ""
	}
	return fmt.Sprintf("%d kbps", (bits+500)/1000)
}

// codecName writes the name of a codec as people know it.
func codecName(codec string) string {
	switch c := strings.ToLower(codec); c {
	case "flac", "mp3", "aac", "opus", "alac", "wav":
		return strings.ToUpper(c)
	case "vorbis":
		return "Ogg Vorbis"
	case "pcm_s16le", "pcm_s24le", "pcm_s32le":
		return "WAV"
	case "":
		return ""
	default:
		return strings.ToUpper(codec)
	}
}

// fileLine describes a file: "FLAC, 44.1 kHz, 16-bit, stereo, 912 kbps".
func fileLine(src *jellyfin.MediaSource) string {
	var parts []string
	if au := src.Audio(); au != nil {
		parts = append(parts, codecName(au.Codec), khz(au.SampleRate))
		if au.BitDepth > 0 {
			parts = append(parts, fmt.Sprintf("%d-bit", au.BitDepth))
		}
		switch au.Channels {
		case 1:
			parts = append(parts, "mono")
		case 2:
			parts = append(parts, "stereo")
		default:
			if au.Channels > 2 {
				parts = append(parts, fmt.Sprintf("%d channels", au.Channels))
			}
		}
		rate := au.BitRate
		if rate == 0 {
			rate = src.Bitrate
		}
		parts = append(parts, kbps(rate))
	} else {
		parts = append(parts, codecName(src.Container), kbps(src.Bitrate))
	}
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

// playingAs says how the song is heard: as the file is, or converted by
// the server, and resampled for the output.
func playingAs(src *jellyfin.MediaSource, maxBitrate, outRate int, downloaded bool) string {
	au := src.Audio()
	if au == nil {
		return ""
	}
	codec := strings.ToLower(au.Codec)
	rate := au.SampleRate
	var what string
	switch {
	case downloaded:
		what = codecName(codec) + ", the file kept on this computer"
	case maxBitrate > 0 && src.Bitrate > maxBitrate*1000:
		what = fmt.Sprintf("MP3 at up to %d kbps, converted by the server from %s", maxBitrate, codecName(codec))
		rate = 0 // the server's, not the file's
	case codec != "flac" && codec != "mp3":
		what = "FLAC, converted by the server from " + codecName(codec)
	default:
		what = codecName(codec) + ", as the file is"
	}
	if rate > 0 && outRate > 0 && rate != outRate {
		what += fmt.Sprintf("; resampled from %s to %s for the output", khz(rate), khz(outRate))
	}
	return what
}

// infoDialog shows the details of a song.
func (a *App) infoDialog(c *ui.Context) {
	in := &a.info
	if !in.open {
		return
	}
	p := a.pal
	open := in.open
	s := in.song
	ui.Modal(c, &open, func() {
		row := func(label, value string) {
			if value == "" {
				return
			}
			ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
				ui.Text(c, label).TextColor(p.muted).Width(120).Shrink(0)
				ui.Text(c, value).Selectable().Grow(1).MinWidth(0)
			})
		}
		ui.Column(c).Gap(10).Padding(22).Width(560).Children(func() {
			ui.Text(c, s.Name).FontSize(18).FontWeight(700).MaxLines(2)
			ui.Column(c).Gap(8).Children(func() {
				row("Artist", s.Artist)
				row("Album", s.Album)
				if s.Track > 0 {
					track := fmt.Sprint(s.Track)
					if s.Disc > 1 {
						track = fmt.Sprintf("%d, disc %d", s.Track, s.Disc)
					}
					row("Track", track)
				}
				if s.Year > 0 {
					row("Year", fmt.Sprint(s.Year))
				}
				if al := a.lib.Album(s.AlbumID); al != nil && len(al.Genres) > 0 {
					row("Genres", strings.Join(al.Genres, ", "))
				}
				row("Length", clock(s.Duration()))
				if s.Plays > 0 {
					row("Played", count(s.Plays, "time", "times"))
				}
				if s.LastPlayed > 0 {
					row("Last played", time.Unix(s.LastPlayed, 0).Format("2 January 2006"))
				}
				if s.Added > 0 {
					row("Added", time.Unix(s.Added, 0).Format("2 January 2006"))
				}
				if s.GainDB != 0 {
					row("Loudness gain", fmt.Sprintf("%+.1f dB, from the server's measure", s.GainDB))
				}
			})
			switch {
			case in.loading:
				ui.Text(c, "Reading the file…").TextColor(p.muted)
			case in.err != nil:
				ui.Text(c, "The server did not tell of the file: "+in.err.Error()).TextColor(p.muted).MaxLines(3)
			case in.item != nil && len(in.item.MediaSources) > 0:
				src := &in.item.MediaSources[0]
				ui.Box(c).Height(1).Background(p.border).Margin(4, 0)
				ui.Column(c).Gap(8).Children(func() {
					row("File", fileLine(src))
					if src.Size > 0 {
						row("Size", bytesText(src.Size))
					}
					row("Kind", strings.ToUpper(src.Container))
					if a.player.current() == s {
						held := a.downloads != nil && a.downloads.held(s.ID)
						row("Playing as", playingAs(src, a.settings.MaxBitrate, a.player.engineRate(), held))
					}
					row("On the server", in.item.Path)
				})
			}
		})
	})
	if !open {
		*in = infoState{}
	}
}

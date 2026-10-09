package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"aurelia/internal/jellyfin"
	"aurelia/internal/library"
)

// Lyrics the server has none of can be looked up on LRCLIB, a free
// database of them, when the settings ask: it is told the artist, the
// title, the album and the length of the song, and nothing else.

const lrclibURL = "https://lrclib.net"

// parseLRC reads lyrics in the LRC format: lines with "[01:23.45]" before
// them, which may carry several times. Tags such as "[ar:Someone]" are
// dropped. Lyrics with no times at all come back untimed.
func parseLRC(text string) []jellyfin.LyricLine {
	var lines []jellyfin.LyricLine
	timed := false
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		raw = strings.TrimSpace(raw)
		var starts []time.Duration
		for strings.HasPrefix(raw, "[") {
			end := strings.IndexByte(raw, ']')
			if end < 0 {
				break
			}
			if d, ok := lrcTime(raw[1:end]); ok {
				starts = append(starts, d)
			} else if len(starts) == 0 {
				raw = "" // a tag: [ar:Artist], [length:3:07]
				break
			}
			raw = strings.TrimSpace(raw[end+1:])
		}
		if len(starts) == 0 {
			if raw != "" {
				lines = append(lines, jellyfin.LyricLine{Text: raw, Start: -1})
			}
			continue
		}
		timed = true
		for _, d := range starts {
			lines = append(lines, jellyfin.LyricLine{Text: raw, Start: d})
		}
	}
	if timed {
		lines = slices.DeleteFunc(lines, func(l jellyfin.LyricLine) bool { return l.Start < 0 })
		slices.SortStableFunc(lines, func(a, b jellyfin.LyricLine) int { return int(a.Start - b.Start) })
	}
	return lines
}

// lrcTime reads "01:23.45" or "1:23".
func lrcTime(s string) (time.Duration, bool) {
	m, rest, ok := strings.Cut(s, ":")
	if !ok {
		return 0, false
	}
	min, err := strconv.Atoi(m)
	if err != nil || min < 0 {
		return 0, false
	}
	sec, err := strconv.ParseFloat(rest, 64)
	if err != nil || sec < 0 || sec >= 60 {
		return 0, false
	}
	return time.Duration(min)*time.Minute + time.Duration(sec*float64(time.Second)), true
}

// lrclibLyrics asks LRCLIB for the lyrics of a song: timed where it has
// them, else plain; nil when it has none.
func lrclibLyrics(ctx context.Context, base string, s *library.Song) ([]jellyfin.LyricLine, error) {
	if base == "" {
		base = lrclibURL
	}
	q := url.Values{"track_name": {s.Name}, "artist_name": {s.Artist}}
	if s.Album != "" {
		q.Set("album_name", s.Album)
	}
	if s.Seconds > 0 {
		q.Set("duration", strconv.Itoa(int(s.Seconds+0.5)))
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(base, "/")+"/api/get?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, nil
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, fmt.Errorf("LRCLIB: %s", resp.Status)
	}
	var res struct {
		Instrumental bool
		PlainLyrics  string
		SyncedLyrics string
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	switch {
	case res.Instrumental:
		return nil, nil
	case strings.TrimSpace(res.SyncedLyrics) != "":
		return parseLRC(res.SyncedLyrics), nil
	case strings.TrimSpace(res.PlainLyrics) != "":
		return parseLRC(res.PlainLyrics), nil
	}
	return nil, nil
}

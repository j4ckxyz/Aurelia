package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseLRC(t *testing.T) {
	lines := parseLRC("[ar:Someone]\n[ti:Song]\n[length: 3:07]\n[00:12.50] First line\r\n[00:05]Before it\n[01:02.3][00:30.00] Chorus\n[00:40.00]\n")
	want := []struct {
		at   time.Duration
		text string
	}{
		{5 * time.Second, "Before it"}, {12500 * time.Millisecond, "First line"}, {30 * time.Second, "Chorus"},
		{40 * time.Second, ""}, {62300 * time.Millisecond, "Chorus"},
	}
	if len(lines) != len(want) {
		t.Fatalf("%d lines: %+v", len(lines), lines)
	}
	for i, w := range want {
		if lines[i].Start != w.at || lines[i].Text != w.text {
			t.Errorf("line %d is %v %q, want %v %q", i, lines[i].Start, lines[i].Text, w.at, w.text)
		}
	}
	plain := parseLRC("Just words\n\nSecond verse\n")
	if len(plain) != 2 || plain[0].Start >= 0 || plain[1].Text != "Second verse" {
		t.Errorf("untimed lyrics: %+v", plain)
	}
	if got := parseLRC(""); len(got) != 0 {
		t.Errorf("nothing: %+v", got)
	}
}

type lrclibServer struct {
	mu    sync.Mutex
	asked []string
	reply func(title string) (int, string)
}

func (l *lrclibServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	l.asked = append(l.asked, r.URL.RawQuery)
	l.mu.Unlock()
	if r.URL.Path != "/api/get" {
		http.NotFound(w, r)
		return
	}
	code, body := l.reply(r.URL.Query().Get("track_name"))
	w.WriteHeader(code)
	w.Write([]byte(body))
}

func TestLRCLIBAsksForTheSong(t *testing.T) {
	srv := &lrclibServer{reply: func(string) (int, string) {
		return 200, `{"plainLyrics":"words","syncedLyrics":"[00:01.00] one\n[00:02.00] two"}`
	}}
	ts := newServer(t, srv)
	a := testApp(t)
	s := a.lib.Song("s1")
	lines, err := lrclibLyrics(context.Background(), ts.URL, s)
	if err != nil || len(lines) != 2 || lines[0].Start != time.Second || lines[1].Text != "two" {
		t.Fatalf("got %+v, %v", lines, err)
	}
	q := srv.asked[0]
	for _, want := range []string{"track_name=", "artist_name=", "album_name=", "duration="} {
		if !strings.Contains(q, want) {
			t.Errorf("the query %q lacks %s", q, want)
		}
	}
	// Plain lyrics when there are no timed ones; none for an instrumental or a 404.
	for body, want := range map[string]int{
		`{"plainLyrics":"a\nb","syncedLyrics":""}`: 2,
		`{"instrumental":true,"plainLyrics":"x"}`:  0,
		`{}`: 0,
	} {
		srv.reply = func(string) (int, string) { return 200, body }
		if lines, _ := lrclibLyrics(context.Background(), ts.URL, s); len(lines) != want {
			t.Errorf("%s gave %d lines, want %d", body, len(lines), want)
		}
	}
	srv.reply = func(string) (int, string) { return 404, `{"statusCode":404}` }
	if lines, err := lrclibLyrics(context.Background(), ts.URL, s); lines != nil || err != nil {
		t.Errorf("a 404 gave %v, %v", lines, err)
	}
	srv.reply = func(string) (int, string) { return 500, `` }
	if _, err := lrclibLyrics(context.Background(), ts.URL, s); err == nil {
		t.Error("a 500 was not an error")
	}
}

func TestLyricsFallBackToLRCLIB(t *testing.T) {
	srv := &lrclibServer{reply: func(string) (int, string) {
		return 200, `{"syncedLyrics":"[00:01.00] found it"}`
	}}
	ts := newServer(t, srv)
	a := testApp(t)
	s := a.lib.Song("s1")
	s.HasLyrics = false
	// Off, the server's word is final, and LRCLIB is not asked.
	if lines, known := a.lyricLines(s); lines != nil || !known {
		t.Fatalf("with the setting off: %v, %v", lines, known)
	}
	srv.mu.Lock()
	if len(srv.asked) != 0 {
		t.Errorf("LRCLIB was asked with the setting off")
	}
	srv.mu.Unlock()
	a.settings.Lrclib, a.settings.LrclibURL = true, ts.URL
	if _, known := a.lyricLines(s); known {
		t.Fatal("the answer was known before LRCLIB spoke")
	}
	wait(t, a, "LRCLIB's lyrics", func() bool { _, ok := a.lyrics.bySong[s.ID]; return ok })
	lines, known := a.lyricLines(s)
	if !known || len(lines) != 1 || lines[0].Text != "found it" {
		t.Errorf("the lyrics are %+v, %v", lines, known)
	}
	if n := len(srv.asked); n != 1 {
		t.Errorf("LRCLIB was asked %d times", n)
	}
}

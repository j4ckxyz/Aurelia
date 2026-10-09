package main

import (
	"strings"
	"testing"
	"time"

	"aurelia/internal/library"
)

func songsOf(a *App, ids ...string) []*library.Song {
	var out []*library.Song
	for _, id := range ids {
		out = append(out, a.lib.Song(id))
	}
	return out
}

func TestSleepAtTheEndOfTheSong(t *testing.T) {
	a := testApp(t)
	p := a.player
	p.queue = songsOf(a, "s1", "s2", "s3")
	p.index = 0
	if p.following() != 1 {
		t.Fatalf("without a timer the next is %d", p.following())
	}
	p.setSleep(sleepSong, 0)
	if p.following() != -1 {
		t.Errorf("a timer for the end of the song lets %d follow", p.following())
	}
	// Repeating the song is no way round it.
	a.settings.Repeat = repeatOne
	if p.following() != -1 {
		t.Errorf("repeat one: %d follows", p.following())
	}
	a.settings.Repeat = repeatOff
	// Once it has stopped the music, the timer is gone and the queue plays.
	p.sleepEnded()
	if p.sleep.mode != "" || p.following() != 1 {
		t.Errorf("after it ended: %+v, next %d", p.sleep, p.following())
	}
}

func TestSleepAtTheEndOfTheAlbum(t *testing.T) {
	a := testApp(t)
	p := a.player
	// s1 and s2 are of one album; s3 is of another.
	all := songsOf(a, "s1", "s2", "s3")
	var same, other []*library.Song
	for _, s := range all[1:] {
		if s.AlbumID == all[0].AlbumID {
			same = append(same, s)
		} else {
			other = append(other, s)
		}
	}
	if len(same) == 0 || len(other) == 0 {
		t.Skip("the test library has no two albums here")
	}
	p.queue = append([]*library.Song{all[0]}, append(same, other...)...)
	p.index = 0
	p.setSleep(sleepAlbum, 0)
	if p.following() != 1 {
		t.Errorf("inside the album, %d follows", p.following())
	}
	p.index = len(same) // the last song of the album
	if p.following() != -1 {
		t.Errorf("at the end of the album, %d follows", p.following())
	}
	p.setSleep("", 0)
	if p.following() != len(same)+1 {
		t.Errorf("with the timer off, %d follows", p.following())
	}
}

func TestSleepAfterATime(t *testing.T) {
	a := testApp(t)
	p := a.player
	p.setSleep(sleepTime, 15*time.Minute)
	now := time.Now()
	if p.sleepDue(now.Add(14*time.Minute + 50*time.Second)) {
		t.Error("due too early")
	}
	if !p.sleepDue(now.Add(15*time.Minute + time.Second)) {
		t.Error("not due when the time has come")
	}
	if got := p.sleepText(now); !strings.HasPrefix(got, "Stops in 15 minutes") {
		t.Errorf("the text is %q", got)
	}
	if got := p.sleepText(now.Add(12 * time.Minute)); got != "Stops in 3 minutes" {
		t.Errorf("after 12 minutes the text is %q", got)
	}
	// Turned off, it is never due.
	p.setSleep("", 0)
	if p.sleepDue(now.Add(time.Hour)) || p.sleepText(now) != "" {
		t.Error("a timer that is off")
	}
	// It does not stop what is not playing, and clears itself.
	p.setSleep(sleepTime, time.Minute)
	p.tickSleep(now.Add(2 * time.Minute))
	if p.sleep.mode != "" {
		t.Errorf("still set after it came: %+v", p.sleep)
	}
}

func TestSleepWords(t *testing.T) {
	for d, want := range map[time.Duration]string{
		5 * time.Minute: "5 minutes", 45 * time.Minute: "45 minutes", time.Hour: "1 hour",
		2 * time.Hour: "2 hours", 20 * time.Second: "a moment",
	} {
		if got := sleepWords(d); got != want {
			t.Errorf("%v is %q, want %q", d, got, want)
		}
	}
}

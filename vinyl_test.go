package main

import (
	"slices"
	"testing"
	"time"

	"aurelia/internal/audio"
	"aurelia/internal/library"
)

// An album is laid out on a record as a pressing plant would: the fourteen
// songs of 200 seconds on two sides of seven, a song's place on its side
// where the songs before it end, the side cut for its loudest song.
func TestGrooveOfAnAlbum(t *testing.T) {
	a := albumApp(t)
	data := a.lib.Data
	data.Songs = slices.Clone(data.Songs)
	for i := range data.Songs {
		switch data.Songs[i].ID {
		case "a3":
			data.Songs[i].GainDB = -9
		case "a4":
			data.Songs[i].GainDB = -5
		}
	}
	data.Songs = append(data.Songs, library.Song{ID: "x1", Name: "Alone", Seconds: 180, GainDB: -3})
	a.setLibrary(library.Build(data))
	g := func(id string) *audio.Groove { return a.grooveOf(a.lib.Song(id)) }
	first, second, last, other := g("a1"), g("a2"), g("a7"), g("a8")
	if first.Speed != audio.LP || !first.First || first.Face != 0 || first.At != 0 {
		t.Errorf("the first song: %+v", first)
	}
	if second.At != 200*time.Second || second.Record != first.Record || second.Side != 0 {
		t.Errorf("the second song: %+v", second)
	}
	if !last.Last || last.Side != 0 || other.Side != 1 || other.Face != 1 || !other.First || other.Record != first.Record {
		t.Errorf("the sides: %+v and %+v", last, other)
	}
	if first.Gain != -9 || other.Gain != 0 {
		t.Errorf("side A is cut for a gain of %.0f dB and side B %.0f, want -9 and none", first.Gain, other.Gain)
	}
	if got := a.vinylNow(a.lib.Song("a8")); got != "side B of an LP, Very Good Plus" {
		t.Errorf("a8 plays from %q", got)
	}
	// A song of no album is a single of its own.
	if s := g("x1"); s.Speed != audio.Single || !s.First || !s.Last || s.Gain != -3 || s.SideLen != 3*time.Minute {
		t.Errorf("a song of no album: %+v", s)
	}
}

func TestVinylSettings(t *testing.T) {
	s := defaultSettings()
	if fx := s.effects(); fx.Vinyl {
		t.Fatal("vinyl on by default")
	}
	s.Vinyl, s.VinylGrade, s.VinylMore = true, "vg", 0.5
	fx := s.effects()
	if !fx.Vinyl || fx.VinylGrade != audio.VeryGood || fx.VinylMore != 0.5 {
		t.Errorf("the effects: %+v", fx)
	}
	s.VinylGrade = ""
	if fx := s.effects(); fx.VinylGrade >= 0 {
		t.Errorf("by the album's year is grade %v", fx.VinylGrade)
	}
}

package main

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/audio"
	"aurelia/internal/library"
)

// Vinyl: the music as from a record of its album's, on a turntable
// (internal/audio/vinyl.go). Each album is laid out on records as a
// pressing plant would lay it out, and each song plays from its place on
// its side: the needle drops into the lead-in as a side begins, the songs
// of a side follow in its groove, and the run-out plays as it ends. The
// record's condition is what its years have made it, or a grade chosen,
// and can be moved either way.

// vinylChoices are the conditions a record can be played in: what the
// album's year makes it, or a grade of Goldmine's, which collectors grade
// by.
var vinylChoices = []struct {
	id, name string
	grade    float64
}{
	{"", "By the album's year", -1},
	{"m", "Mint", audio.Mint},
	{"nm", "Near Mint", audio.NearMint},
	{"vg+", "Very Good Plus", audio.VeryGoodPlus},
	{"vg", "Very Good", audio.VeryGood},
	{"g+", "Good Plus", audio.GoodPlus},
	{"g", "Good", audio.Good},
	{"f", "Fair", audio.Fair},
}

// vinylGrade is the grade of a choice, below 0 for the album's year's.
func vinylGrade(id string) float64 {
	for _, ch := range vinylChoices {
		if ch.id == id {
			return ch.grade
		}
	}
	return -1
}

// vinylName is a choice as it is shown.
func vinylName(id string) string {
	for _, ch := range vinylChoices {
		if ch.id == id {
			return ch.name
		}
	}
	return vinylChoices[0].name
}

// grooveOf is where a song is cut on a record of its album's: its album's
// songs that the library has, by disc, laid out on records. A song of no
// album is a single of its own.
func (a *App) grooveOf(s *library.Song) *audio.Groove {
	year := s.Year
	if al := a.lib.Album(s.AlbumID); al != nil && al.Year > 0 {
		year = al.Year
	}
	songs := a.lib.AlbumSongs(s.AlbumID)
	at := -1
	for i, o := range songs {
		if o.ID == s.ID {
			at = i
		}
	}
	if s.AlbumID == "" || at < 0 {
		g := audio.CutRecord(audio.HashString(s.ID), year, [][]time.Duration{{s.Duration()}})[0][0]
		g.Gain = s.GainDB
		return &g
	}
	// The discs, in order, and where the song is among them.
	var discs [][]time.Duration
	var of [][]*library.Song
	disc, index := -1, 0
	for i, o := range songs {
		if i == 0 || o.Disc != songs[i-1].Disc {
			discs = append(discs, nil)
			of = append(of, nil)
		}
		d := len(discs) - 1
		if i == at {
			disc, index = d, len(discs[d])
		}
		discs[d] = append(discs[d], o.Duration())
		of[d] = append(of[d], o)
	}
	cut := audio.CutRecord(audio.HashString(s.AlbumID), year, discs)
	g := cut[disc][index]
	// The side is cut for its loudest song, as the server measured it.
	for i, h := range cut[disc] {
		if h.Record == g.Record && h.Side == g.Side && of[disc][i].GainDB != 0 {
			if g.Gain == 0 || of[disc][i].GainDB < g.Gain {
				g.Gain = of[disc][i].GainDB
			}
		}
	}
	return &g
}

// vinylHelp says what vinyl playback does, and, while it plays, what the
// song plays from.
func (a *App) vinylHelp() string {
	help := "Play music as if from its album's record: the hiss and crackle of the surface, heard in quiet passages and between songs; ticks and pops of dust and scratches; the needle dropping as a side begins, and the run-out as it ends."
	if !a.settings.Vinyl {
		return help
	}
	if s := a.player.current(); s != nil && a.player.far == nil {
		return help + " Now: " + a.vinylNow(s) + "."
	}
	return help
}

// vinylNow says what a song plays from: the side, the record and its
// condition. It is worked out once for each song and setting.
func (a *App) vinylNow(s *library.Song) string {
	key := fmt.Sprint(s.ID, a.libGen, a.settings.VinylGrade, a.settings.VinylMore)
	if key != a.vinylKey {
		a.vinylKey, a.vinylText = key, a.vinylOf(s)
	}
	return a.vinylText
}

func (a *App) vinylOf(s *library.Song) string {
	g := a.grooveOf(s)
	kind := map[audio.Speed]string{audio.LP: "LP", audio.Single: "45", audio.Shellac: "78"}[g.Speed]
	record := "a " + kind
	if g.Speed == audio.LP {
		record = "an LP"
	}
	if g.Year > 0 {
		record = fmt.Sprintf("a %d %s", g.Year, kind)
	}
	grade := audio.VinylGrade(g.Year, vinylGrade(a.settings.VinylGrade), a.settings.VinylMore)
	return fmt.Sprintf("side %c of %s, %s", 'A'+rune(g.Face), record, audio.GradeName(grade))
}

// conditionHelp says what the condition chosen means.
func (a *App) conditionHelp() string {
	if a.settings.VinylGrade == "" {
		return "As worn as a record of the album's year would be by now: played most when new, and handled ever since. A record of this year is near mint; one of twenty years ago, very good plus; one of the 1970s, nearer very good."
	}
	return "As collectors grade records. Very good has surface noise in quiet passages, intros and fades that does not overpower the music; good has a lot of it, with scratches and worn grooves."
}

// moreText says how far the crackle is moved from the condition.
func moreText(v float64) string {
	switch {
	case math.Abs(v) < 0.005:
		return "as the condition has it"
	case v < 0:
		return fmt.Sprintf("%.0f%% cleaner", -v*100)
	}
	return fmt.Sprintf("%.0f%% more worn", v*100)
}

// vinylSettings are the settings of vinyl playback, in the Playback card.
func (a *App) vinylSettings(c *ui.Context) {
	a.setting(c, "Vinyl", a.vinylHelp(), func() {
		if ui.Switch(c.Key("vinyl"), &a.settings.Vinyl).Label("Vinyl").Changed() {
			a.applyEffects()
			a.saveSettings()
		}
	})
	if !a.settings.Vinyl {
		return
	}
	a.setting(c, "Record condition", a.conditionHelp(), func() {
		names := make([]string, len(vinylChoices))
		for i, ch := range vinylChoices {
			names[i] = ch.name
		}
		at := vinylName(a.settings.VinylGrade)
		if ui.Select(c.Key("vinyl-grade"), &at, names).Width(190).Label("Record condition").Changed() {
			for _, ch := range vinylChoices {
				if ch.name == at {
					a.settings.VinylGrade = ch.id
				}
			}
			a.applyEffects()
			a.saveSettings()
		}
	})
	a.setting(c, "Wear", "Cleaner or more worn than the condition makes it: "+moreText(a.settings.VinylMore)+".", func() {
		s := ui.Slider(c.Key("vinyl-more"), &a.settings.VinylMore, -1, 1).Width(180).Step(0.05).Label("Wear")
		if s.Changed() {
			if math.Abs(a.settings.VinylMore) < 0.04 {
				a.settings.VinylMore = 0 // the condition itself holds
			}
			a.applyEffects()
			a.vinylDirty = true
		}
		if a.vinylDirty && !s.Pressed() {
			a.vinylDirty = false
			a.saveSettings()
		}
	})
}

// setVinyl turns vinyl playback on or off, from the player's "…".
func (a *App) setVinyl(on bool) {
	a.settings.Vinyl = on
	a.applyEffects()
	a.saveSettings()
}

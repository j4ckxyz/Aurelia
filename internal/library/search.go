package library

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

// folds maps accented letters to the letters people type for them.
var folds = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'ç': "c", 'ć': "c", 'č': "c", 'ĉ': "c", 'ċ': "c",
	'ď': "d", 'đ': "d", 'ð': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ğ': "g", 'ĝ': "g", 'ġ': "g", 'ģ': "g",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ī': "i", 'į': "i", 'ı': "i",
	'ł': "l", 'ľ': "l", 'ĺ': "l", 'ļ': "l",
	'ñ': "n", 'ń': "n", 'ň': "n", 'ņ': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o", 'ő': "o",
	'ř': "r", 'ŕ': "r",
	'ś': "s", 'š': "s", 'ş': "s", 'ș': "s", 'ŝ': "s",
	'ť': "t", 'ţ': "t", 'ț': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ū': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ý': "y", 'ÿ': "y",
	'ź': "z", 'ż': "z", 'ž': "z",
	'ß': "ss", 'æ': "ae", 'œ': "oe", 'þ': "th",
	'’': "'", '‘': "'", '“': `"`, '”': `"`, '‐': "-", '–': "-", '—': "-",
}

// Fold returns s as a search compares it: lower case, without accents,
// its punctuation gone and its spaces single.
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true // no leading space
	for _, r := range s {
		r = unicode.ToLower(r)
		if f, ok := folds[r]; ok {
			if f == "'" {
				continue // don't, dont
			}
			if f == "-" || f == `"` {
				r = ' '
			} else {
				b.WriteString(f)
				space = false
				continue
			}
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		case r == '\'':
		case !space:
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// sortKey is the name as lists order it: folded, without a leading
// article.
func sortKey(name string) string {
	k := Fold(name)
	for _, article := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(k, article) && len(k) > len(article) {
			return k[len(article):]
		}
	}
	return k
}

// Results are what a search found, the best first.
type Results struct {
	Artists   []*Artist
	Albums    []*Album
	Songs     []*Song
	Playlists []*Playlist
}

// Empty reports whether nothing was found.
func (r *Results) Empty() bool {
	return len(r.Artists)+len(r.Albums)+len(r.Songs)+len(r.Playlists) == 0
}

// match scores a name, and what else describes the item, against the
// terms of a query: -1 when a term is in neither, else lower is better.
func match(terms []string, whole, name, extra string) int {
	inName := 0
	for _, t := range terms {
		switch {
		case strings.Contains(name, t):
			inName++
		case extra != "" && strings.Contains(extra, t):
		default:
			return -1
		}
	}
	switch {
	case name == whole:
		return 0
	case strings.HasPrefix(name, whole):
		return 1
	case strings.Contains(name, " "+whole):
		return 2
	case inName == len(terms) && wordPrefix(name, terms[0]):
		return 3
	case inName == len(terms):
		return 4
	case inName > 0:
		return 5
	}
	return 6
}

// wordPrefix reports whether a word of s starts with t.
func wordPrefix(s, t string) bool {
	return strings.HasPrefix(s, t) || strings.Contains(s, " "+t)
}

type scored[T any] struct {
	item  T
	score int
	tie   int // more is better
}

func best[T any](found []scored[T], limit int) []T {
	slices.SortStableFunc(found, func(a, b scored[T]) int {
		return cmp.Or(cmp.Compare(a.score, b.score), cmp.Compare(b.tie, a.tie))
	})
	if limit > 0 && len(found) > limit {
		found = found[:limit]
	}
	out := make([]T, len(found))
	for i, f := range found {
		out[i] = f.item
	}
	return out
}

// Limits are how many of each kind a search returns; 0 is all.
type Limits struct{ Artists, Albums, Songs, Playlists int }

// Search finds what matches every word of query, in its name or, for
// albums and songs, their artist and album.
func (l *Library) Search(query string, lim Limits) Results {
	whole := Fold(query)
	terms := strings.Fields(whole)
	if len(terms) == 0 {
		return Results{}
	}
	var (
		artists   []scored[*Artist]
		albums    []scored[*Album]
		songs     []scored[*Song]
		playlists []scored[*Playlist]
	)
	for i := range l.Artists {
		a := &l.Artists[i]
		if s := match(terms, whole, a.nameKey, ""); s >= 0 {
			artists = append(artists, scored[*Artist]{a, s, len(l.artistAlbums[a.ID])})
		}
	}
	for i := range l.Albums {
		a := &l.Albums[i]
		if s := match(terms, whole, a.nameKey, a.extraKey); s >= 0 {
			albums = append(albums, scored[*Album]{a, s, a.Year})
		}
	}
	for i := range l.Songs {
		sg := &l.Songs[i]
		if s := match(terms, whole, sg.nameKey, sg.extraKey); s >= 0 {
			songs = append(songs, scored[*Song]{sg, s, sg.Plays})
		}
	}
	for i := range l.Playlists {
		p := &l.Playlists[i]
		if s := match(terms, whole, p.nameKey, ""); s >= 0 {
			playlists = append(playlists, scored[*Playlist]{p, s, p.Songs})
		}
	}
	return Results{
		Artists:   best(artists, lim.Artists),
		Albums:    best(albums, lim.Albums),
		Songs:     best(songs, lim.Songs),
		Playlists: best(playlists, lim.Playlists),
	}
}

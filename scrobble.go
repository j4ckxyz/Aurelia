package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"aurelia/internal/library"
)

// Scrobbling tells ListenBrainz (or a server that speaks as it does) what
// was listened to: the song as it begins, and once more when half of it,
// or four minutes, has been heard.

const listenBrainzURL = "https://api.listenbrainz.org"

// scrobbleAct is what the scrobbler asks to be done.
type scrobbleAct int

const (
	scrobbleNothing scrobbleAct = iota
	scrobbleStarted             // a song began: tell what plays now
	scrobbleHeard               // it was heard enough: submit it
)

// scrobbler counts how long a song is heard.
type scrobbler struct {
	songID  string
	played  time.Duration
	pos     time.Duration
	last    time.Time
	started bool
	sent    bool
}

// minHeard is how much of a song must be heard to count: half, or four
// minutes, and never under thirty seconds.
func minHeard(dur time.Duration) time.Duration {
	return min(dur/2, 4*time.Minute)
}

// step is told, every second, what plays; it says what to tell the server.
func (sc *scrobbler) step(now time.Time, songID string, dur, pos time.Duration, playing bool) scrobbleAct {
	if songID == "" {
		*sc = scrobbler{}
		return scrobbleNothing
	}
	// Another song, or the same one from its start again (repeat).
	if songID != sc.songID || pos+10*time.Second < sc.pos {
		*sc = scrobbler{songID: songID, last: now}
	}
	sc.pos = pos
	elapsed := now.Sub(sc.last)
	sc.last = now
	if !playing {
		return scrobbleNothing
	}
	sc.played += min(elapsed, 3*time.Second) // a stalled window does not count
	switch {
	case !sc.started:
		sc.started = true
		return scrobbleStarted
	case !sc.sent && dur >= 30*time.Second && sc.played >= minHeard(dur):
		sc.sent = true
		return scrobbleHeard
	}
	return scrobbleNothing
}

// listen is a song heard, as ListenBrainz takes it.
type listen struct {
	At                   int64 // seconds; 0 for the song playing now
	Artist, Title, Album string
	Duration             time.Duration
}

func (l listen) payload() map[string]any {
	info := map[string]any{
		"media_player": "Aurelia", "submission_client": "Aurelia", "submission_client_version": appVersion(),
	}
	if l.Duration > 0 {
		info["duration_ms"] = l.Duration.Milliseconds()
	}
	meta := map[string]any{"artist_name": l.Artist, "track_name": l.Title, "additional_info": info}
	if l.Album != "" {
		meta["release_name"] = l.Album
	}
	p := map[string]any{"track_metadata": meta}
	if l.At > 0 {
		p["listened_at"] = l.At
	}
	return p
}

// lbCall asks ListenBrainz something.
func lbCall(ctx context.Context, base, token, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	if base == "" {
		base = listenBrainzURL
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(base, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct{ Error string }
		json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("ListenBrainz: %s", e.Error)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// scrobbleTick is called every second, whether or not the window draws.
func (a *App) scrobbleTick(now time.Time) {
	sb := &a.scrobble
	if !a.settings.Scrobble || a.settings.ListenBrainzToken == "" {
		sb.sc = scrobbler{}
		return
	}
	s := a.player.current()
	st := a.player.state()
	id, dur := "", time.Duration(0)
	if s != nil && a.player.far == nil {
		id, dur = s.ID, s.Duration()
	}
	switch sb.sc.step(now, id, dur, st.Position, s != nil && !st.Paused && a.player.far == nil) {
	case scrobbleStarted:
		a.sendListen("playing_now", listenOf(s, 0))
	case scrobbleHeard:
		a.sendListen("single", listenOf(s, now.Add(-sb.sc.played).Unix()))
	}
	// What could not be sent is tried again every minute.
	if len(sb.pending) > 0 && !sb.sending && now.Sub(sb.tried) > time.Minute {
		sb.tried = now
		a.sendListen("", listen{})
	}
}

func listenOf(s *library.Song, at int64) listen {
	return listen{At: at, Artist: s.Artist, Title: s.Name, Album: s.Album, Duration: s.Duration()}
}

// sendListen tells ListenBrainz, in the background; listens it did not
// take are kept, up to a hundred, for later.
func (a *App) sendListen(kind string, l listen) {
	sb := &a.scrobble
	switch kind {
	case "single":
		sb.pending = append(sb.pending, l)
		if len(sb.pending) > 100 {
			sb.pending = sb.pending[len(sb.pending)-100:]
		}
	case "playing_now":
		base, token := a.settings.ListenBrainzURL, a.settings.ListenBrainzToken
		go func() { // it matters only now: not kept, not retried
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			lbCall(ctx, base, token, "POST", "/1/submit-listens", map[string]any{"listen_type": "playing_now", "payload": []any{l.payload()}}, nil)
		}()
		return
	}
	if sb.sending || len(sb.pending) == 0 {
		return
	}
	sb.sending = true
	batch := append([]listen(nil), sb.pending...)
	base, token := a.settings.ListenBrainzURL, a.settings.ListenBrainzToken
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var payload []any
		for _, b := range batch {
			payload = append(payload, b.payload())
		}
		kind := "single"
		if len(payload) > 1 {
			kind = "import"
		}
		err := lbCall(ctx, base, token, "POST", "/1/submit-listens", map[string]any{"listen_type": kind, "payload": payload}, nil)
		a.update(func() {
			sb.sending = false
			if err != nil {
				sb.err = err.Error()
				return
			}
			sb.err = ""
			sb.pending = sb.pending[min(len(batch), len(sb.pending)):]
		})
	}()
}

// scrobbleState is what the app holds of scrobbling.
type scrobbleState struct {
	sc       scrobbler
	pending  []listen
	sending  bool
	tried    time.Time
	err      string
	checking bool
	user     string // who the token belongs to, once checked
	checkErr string
}

// checkListenBrainz asks whom the token belongs to.
func (a *App) checkListenBrainz() {
	sb := &a.scrobble
	if sb.checking || a.settings.ListenBrainzToken == "" {
		return
	}
	sb.checking, sb.user, sb.checkErr = true, "", ""
	base, token := a.settings.ListenBrainzURL, strings.TrimSpace(a.settings.ListenBrainzToken)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var res struct {
			Valid    bool   `json:"valid"`
			UserName string `json:"user_name"`
			Message  string `json:"message"`
		}
		err := lbCall(ctx, base, token, "GET", "/1/validate-token", nil, &res)
		a.update(func() {
			sb.checking = false
			switch {
			case err != nil:
				sb.checkErr = err.Error()
			case !res.Valid:
				sb.checkErr = "The token is not one of ListenBrainz's."
			default:
				sb.user = res.UserName
			}
		})
	}()
}

// scrobbleSettings are the rows of the settings for scrobbling.
func (a *App) scrobbleSettings(c *ui.Context) {
	sb := &a.scrobble
	a.setting(c, "Send plays to ListenBrainz", "Songs you listen to are sent to ListenBrainz (or a server that speaks as it does) once half has played, and the one playing now as it begins.", func() {
		if ui.Switch(c.Key("scrobble"), &a.settings.Scrobble).Label("Send plays to ListenBrainz").Changed() {
			a.saveSettings()
			if a.settings.Scrobble && sb.user == "" {
				a.checkListenBrainz()
			}
		}
	})
	if !a.settings.Scrobble {
		return
	}
	help := "Your user token, from listenbrainz.org/settings."
	switch {
	case sb.checking:
		help = "Checking the token…"
	case sb.user != "":
		help = "Signed in as " + sb.user + "."
	case sb.checkErr != "":
		help = sb.checkErr
	}
	if sb.err != "" {
		help += " Last send failed: " + sb.err
	}
	a.setting(c, "ListenBrainz token", help, func() {
		in := ui.TextInput(c.Key("lbtoken"), &a.settings.ListenBrainzToken).Width(250).Password().Placeholder("User token").Label("ListenBrainz token")
		if in.Changed() {
			a.settings.ListenBrainzToken = strings.TrimSpace(a.settings.ListenBrainzToken)
			sb.user, sb.checkErr = "", ""
			a.lbDirty = true
		}
		if a.lbDirty && !in.Focused() {
			a.lbDirty = false
			a.saveSettings()
			a.checkListenBrainz()
		}
		if a.pillButton(c, "check", "Check", false).Clicked() {
			a.saveSettings()
			a.checkListenBrainz()
		}
	})
	a.setting(c, "ListenBrainz server", "Empty uses listenbrainz.org. Another address suits a server of your own.", func() {
		in := ui.TextInput(c.Key("lburl"), &a.settings.ListenBrainzURL).Width(250).Placeholder(listenBrainzURL).Label("ListenBrainz server")
		if in.Changed() {
			a.lbDirty = true
		}
		if a.lbDirty && !in.Focused() {
			a.lbDirty = false
			a.saveSettings()
		}
	})
}

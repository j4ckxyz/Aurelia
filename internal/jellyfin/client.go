// Package jellyfin talks to a Jellyfin server: signing in, the music
// library, images, streams and what plays.
package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Version is sent to the server as the client's; the app sets it to its
// own.
var Version = "0.1.2"

// Session is a signed-in user on a server, which the app keeps between
// runs. It holds a token, never the password.
type Session struct {
	Server     string `json:"server"` // the base URL, without a final slash
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	UserID     string `json:"userId"`
	UserName   string `json:"userName"`
	Token      string `json:"token"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
}

// Client calls the server of a session.
type Client struct {
	Session
	HTTP *http.Client
}

// ErrUnauthorized is returned when the server refuses the token or the
// password.
var ErrUnauthorized = errors.New("jellyfin: the server refused the sign-in")

// New returns the client of a session.
func New(s Session) *Client {
	return &Client{Session: s, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

func authorization(deviceName, deviceID, token string) string {
	esc := func(s string) string { return strings.NewReplacer(`"`, "", `\`, "").Replace(s) }
	h := fmt.Sprintf(`MediaBrowser Client="Aurelia", Device="%s", DeviceId="%s", Version="%s"`, esc(deviceName), esc(deviceID), Version)
	if token != "" {
		h += fmt.Sprintf(`, Token="%s"`, token)
	}
	return h
}

// NormalizeServer tidies what a user types as a server's address into
// the URLs to try, the likeliest first.
func NormalizeServer(s string) []string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" {
		return nil
	}
	if strings.Contains(s, "://") {
		return []string{s}
	}
	return []string{"https://" + s, "http://" + s}
}

// PublicInfo is what a server tells anyone.
type PublicInfo struct {
	ServerName string
	Version    string
	ID         string `json:"Id"`
}

// Probe asks the server at base who it is.
func Probe(ctx context.Context, base string) (*PublicInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/System/Info/Public", nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("jellyfin: %s answered %s", base, resp.Status)
	}
	var info PublicInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil || info.ID == "" {
		return nil, fmt.Errorf("jellyfin: %s is not a Jellyfin server", base)
	}
	return &info, nil
}

// Login signs in with a name and a password, and returns the session.
func Login(ctx context.Context, server, user, password, deviceID, deviceName string) (*Session, error) {
	var base string
	var info *PublicInfo
	var err error
	for _, u := range NormalizeServer(server) {
		if info, err = Probe(ctx, u); err == nil {
			base = u
			break
		}
	}
	if base == "" {
		if err == nil {
			err = errors.New("jellyfin: no server address")
		}
		return nil, err
	}
	body, _ := json.Marshal(map[string]string{"Username": user, "Pw": password})
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/Users/AuthenticateByName", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authorization(deviceName, deviceID, ""))
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("jellyfin: signing in: the server answered %s", resp.Status)
	}
	var out struct {
		User struct {
			Name string
			ID   string `json:"Id"`
		}
		AccessToken string
		ServerID    string `json:"ServerId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, ErrUnauthorized
	}
	return &Session{
		Server: base, ServerID: out.ServerID, ServerName: info.ServerName,
		UserID: out.User.ID, UserName: out.User.Name, Token: out.AccessToken,
		DeviceID: deviceID, DeviceName: deviceName,
	}, nil
}

// NewRequest makes a request to a path of the server, signed.
func (c *Client) NewRequest(ctx context.Context, method, path string, q url.Values, body io.Reader) (*http.Request, error) {
	u := c.Server + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authorization(c.DeviceName, c.DeviceID, c.Token))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := c.NewRequest(ctx, method, path, q, body)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)) // so that the connection serves again
		if resp.StatusCode == 401 {
			return ErrUnauthorized
		}
		return fmt.Errorf("jellyfin: %s %s: the server answered %s", method, path, resp.Status)
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// NameID is a reference to an artist or a genre.
type NameID struct {
	Name string
	ID   string `json:"Id"`
}

// UserData is what the user did with an item.
type UserData struct {
	IsFavorite     bool
	PlayCount      int
	LastPlayedDate *time.Time
}

// Item is an album, an artist, a song or a playlist, as the server
// describes it: the fields Aurelia uses.
type Item struct {
	ID                   string `json:"Id"`
	Name                 string
	Type                 string
	Album                string
	AlbumID              string `json:"AlbumId"`
	AlbumArtist          string
	AlbumArtists         []NameID
	ArtistItems          []NameID
	Artists              []string
	IndexNumber          int
	ParentIndexNumber    int
	ProductionYear       int
	RunTimeTicks         int64
	ImageTags            map[string]string
	BackdropImageTags    []string
	AlbumPrimaryImageTag string
	UserData             UserData
	DateCreated          *time.Time
	Genres               []string
	ChildCount           int
	Container            string
	NormalizationGain    *float64
	HasLyrics            bool
	Overview             string
	PlaylistItemID       string `json:"PlaylistItemId"`
}

// Duration is the item's length.
func (it *Item) Duration() time.Duration { return time.Duration(it.RunTimeTicks) * 100 }

type itemsResult struct {
	Items            []Item
	TotalRecordCount int
}

func (c *Client) userQuery() url.Values {
	return url.Values{"userId": {c.UserID}}
}

// page adds the part of a list to ask for.
func page(q url.Values, start, limit int) {
	q.Set("StartIndex", strconv.Itoa(start))
	if limit > 0 {
		q.Set("Limit", strconv.Itoa(limit))
	}
}

// Count returns the number of items of a type in the library.
func (c *Client) Count(ctx context.Context, itemType string) (int, error) {
	q := c.userQuery()
	q.Set("IncludeItemTypes", itemType)
	q.Set("Recursive", "true")
	q.Set("Limit", "0")
	var res itemsResult
	err := c.do(ctx, "GET", "/Items", q, nil, &res)
	return res.TotalRecordCount, err
}

// Albums returns a page of the library's albums.
func (c *Client) Albums(ctx context.Context, start, limit int) ([]Item, error) {
	q := c.userQuery()
	q.Set("IncludeItemTypes", "MusicAlbum")
	q.Set("Recursive", "true")
	q.Set("SortBy", "SortName")
	q.Set("EnableImageTypes", "Primary")
	q.Set("ImageTypeLimit", "1")
	q.Set("Fields", "DateCreated,ChildCount,Genres")
	q.Set("EnableTotalRecordCount", "false")
	page(q, start, limit)
	var res itemsResult
	err := c.do(ctx, "GET", "/Items", q, nil, &res)
	return res.Items, err
}

// Songs returns a page of the library's songs.
func (c *Client) Songs(ctx context.Context, start, limit int) ([]Item, error) {
	q := c.userQuery()
	q.Set("IncludeItemTypes", "Audio")
	q.Set("Recursive", "true")
	q.Set("SortBy", "SortName")
	q.Set("EnableImageTypes", "Primary")
	q.Set("ImageTypeLimit", "1")
	q.Set("Fields", "DateCreated")
	q.Set("EnableTotalRecordCount", "false")
	page(q, start, limit)
	var res itemsResult
	err := c.do(ctx, "GET", "/Items", q, nil, &res)
	return res.Items, err
}

// AlbumArtists returns the artists that albums are by.
func (c *Client) AlbumArtists(ctx context.Context) ([]Item, error) {
	q := c.userQuery()
	q.Set("SortBy", "SortName")
	q.Set("EnableImageTypes", "Primary,Backdrop")
	q.Set("ImageTypeLimit", "1")
	var res itemsResult
	err := c.do(ctx, "GET", "/Artists/AlbumArtists", q, nil, &res)
	return res.Items, err
}

// Playlists returns the user's playlists.
func (c *Client) Playlists(ctx context.Context) ([]Item, error) {
	q := c.userQuery()
	q.Set("IncludeItemTypes", "Playlist")
	q.Set("Recursive", "true")
	q.Set("SortBy", "SortName")
	q.Set("EnableImageTypes", "Primary")
	q.Set("ImageTypeLimit", "1")
	q.Set("Fields", "ChildCount,DateCreated")
	var res itemsResult
	err := c.do(ctx, "GET", "/Items", q, nil, &res)
	return res.Items, err
}

// PlaylistItems returns the songs of a playlist, in its order.
func (c *Client) PlaylistItems(ctx context.Context, id string) ([]Item, error) {
	q := c.userQuery()
	q.Set("EnableImageTypes", "Primary")
	q.Set("ImageTypeLimit", "1")
	q.Set("Fields", "DateCreated")
	var res itemsResult
	err := c.do(ctx, "GET", "/Playlists/"+id+"/Items", q, nil, &res)
	return res.Items, err
}

// AlbumSongs returns the songs of an album, in its order.
func (c *Client) AlbumSongs(ctx context.Context, albumID string) ([]Item, error) {
	q := c.userQuery()
	q.Set("ParentId", albumID)
	q.Set("IncludeItemTypes", "Audio")
	q.Set("SortBy", "ParentIndexNumber,IndexNumber,SortName")
	q.Set("EnableImageTypes", "Primary")
	q.Set("ImageTypeLimit", "1")
	q.Set("Fields", "DateCreated")
	var res itemsResult
	err := c.do(ctx, "GET", "/Items", q, nil, &res)
	return res.Items, err
}

// Item returns one item whole, with its overview.
func (c *Client) Item(ctx context.Context, id string) (*Item, error) {
	var it Item
	err := c.do(ctx, "GET", "/Items/"+id, c.userQuery(), nil, &it)
	return &it, err
}

// SearchSongs asks the server for the songs matching a term, for
// libraries whose songs Aurelia has not listed yet.
func (c *Client) SearchSongs(ctx context.Context, term string, limit int) ([]Item, error) {
	q := c.userQuery()
	q.Set("searchTerm", term)
	q.Set("IncludeItemTypes", "Audio")
	q.Set("Recursive", "true")
	q.Set("EnableImageTypes", "Primary")
	q.Set("ImageTypeLimit", "1")
	q.Set("EnableTotalRecordCount", "false")
	page(q, 0, limit)
	var res itemsResult
	err := c.do(ctx, "GET", "/Items", q, nil, &res)
	return res.Items, err
}

// LyricLine is a line of a song's lyrics; Start is negative for lyrics
// that are not timed.
type LyricLine struct {
	Text  string
	Start time.Duration
}

// Lyrics returns a song's lyrics, nil when it has none.
func (c *Client) Lyrics(ctx context.Context, id string) ([]LyricLine, error) {
	var res struct {
		Lyrics []struct {
			Text  string
			Start *int64
		}
	}
	if err := c.do(ctx, "GET", "/Audio/"+id+"/Lyrics", nil, nil, &res); err != nil {
		return nil, err
	}
	lines := make([]LyricLine, len(res.Lyrics))
	for i, l := range res.Lyrics {
		lines[i] = LyricLine{Text: l.Text, Start: -1}
		if l.Start != nil {
			lines[i].Start = time.Duration(*l.Start) * 100
		}
	}
	return lines, nil
}

// SetFavorite marks an item as a favorite, or not.
func (c *Client) SetFavorite(ctx context.Context, id string, favorite bool) error {
	method := "POST"
	if !favorite {
		method = "DELETE"
	}
	return c.do(ctx, method, "/UserFavoriteItems/"+id, c.userQuery(), nil, nil)
}

// ImageURL is the address of an item's image, scaled by the server to
// fill a square of px pixels. kind is "Primary" or "Backdrop".
func (c *Client) ImageURL(itemID, kind, tag string, px int) string {
	q := url.Values{}
	if kind == "Backdrop" {
		q.Set("maxWidth", strconv.Itoa(px))
	} else {
		q.Set("fillWidth", strconv.Itoa(px))
		q.Set("fillHeight", strconv.Itoa(px))
	}
	q.Set("quality", "90")
	q.Set("format", "Jpg")
	if tag != "" {
		q.Set("tag", tag)
	}
	path := "/Items/" + itemID + "/Images/" + kind
	if kind == "Backdrop" {
		path += "/0"
	}
	return c.Server + path + "?" + q.Encode()
}

// Quality is how a song is asked of the server.
type Quality struct {
	// MaxBitrate, in bits a second, makes the server transcode songs
	// above it to MP3; 0 asks for the files as they are.
	MaxBitrate int
}

// Key names the quality in the audio cache.
func (q Quality) Key() string {
	if q.MaxBitrate <= 0 {
		return "orig"
	}
	return strconv.Itoa(q.MaxBitrate / 1000)
}

// StreamRequest makes the request for a song's sound: its file when it is
// FLAC or MP3, which Aurelia decodes, else the server's transcoding of it
// to FLAC, or to MP3 under a bit rate.
func (c *Client) StreamRequest(ctx context.Context, songID string, quality Quality) (*http.Request, error) {
	q := url.Values{}
	q.Set("UserId", c.UserID)
	q.Set("DeviceId", c.DeviceID)
	q.Set("TranscodingProtocol", "http")
	if quality.MaxBitrate > 0 {
		q.Set("Container", "mp3")
		q.Set("TranscodingContainer", "mp3")
		q.Set("AudioCodec", "mp3")
		q.Set("MaxStreamingBitrate", strconv.Itoa(quality.MaxBitrate))
	} else {
		q.Set("Container", "flac,mp3")
		q.Set("TranscodingContainer", "flac")
		q.Set("AudioCodec", "flac")
		q.Set("MaxStreamingBitrate", "140000000")
	}
	return c.NewRequest(ctx, "GET", "/Audio/"+songID+"/universal", q, nil)
}

// Playback is what plays, told to the server so that it counts plays and
// shows the session.
type Playback struct {
	SongID   string
	Position time.Duration
	Paused   bool
	// SessionID names one play of a song, from its start to its stop.
	SessionID string
}

func (p Playback) body() map[string]any {
	return map[string]any{
		"ItemId":        p.SongID,
		"PositionTicks": int64(p.Position / 100),
		"IsPaused":      p.Paused,
		"PlaySessionId": p.SessionID,
		"PlayMethod":    "DirectPlay",
		"CanSeek":       true,
	}
}

// ReportStart tells the server a song began.
func (c *Client) ReportStart(ctx context.Context, p Playback) error {
	return c.do(ctx, "POST", "/Sessions/Playing", nil, p.body(), nil)
}

// ReportProgress tells the server where a song is.
func (c *Client) ReportProgress(ctx context.Context, p Playback) error {
	return c.do(ctx, "POST", "/Sessions/Playing/Progress", nil, p.body(), nil)
}

// ReportStop tells the server a song stopped; the server counts a play
// when it was heard to its end, or nearly.
func (c *Client) ReportStop(ctx context.Context, p Playback) error {
	return c.do(ctx, "POST", "/Sessions/Playing/Stopped", nil, p.body(), nil)
}

// Logout ends the session on the server, which forgets the token.
func (c *Client) Logout(ctx context.Context) error {
	return c.do(ctx, "POST", "/Sessions/Logout", nil, nil, nil)
}

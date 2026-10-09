package jellyfin

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

	"github.com/coder/websocket"
)

// Device is another place the user is signed in that plays music and
// takes commands: an Aurelia on another computer, Jellyfin's own apps.
type Device struct {
	ID       string // of its session, which commands are sent to
	Client   string // the app, as "Aurelia" or "Jellyfin Web"
	Name     string // the device, as its app names it
	DeviceID string
	// Playing is what it plays, nil when nothing.
	Playing *DevicePlaying
}

// Title names a device for a person: the device, and the app when that
// says more.
func (d *Device) Title() string {
	switch {
	case d.Name == "":
		return d.Client
	case d.Client == "" || strings.Contains(strings.ToLower(d.Name), strings.ToLower(d.Client)):
		return d.Name
	}
	return d.Name + " · " + d.Client
}

// DevicePlaying is what a device plays, as the server last heard.
type DevicePlaying struct {
	Item     Item
	Position time.Duration
	// ToldAt is when the device told the server that position, by the
	// server's clock; zero when the server does not say.
	ToldAt  time.Time
	Paused  bool
	Muted   bool
	Volume  int // from 0 to 100, -1 when the device does not tell
	Repeat  string
	Shuffle bool
	// Queue is the IDs of the songs it plays through, and Index the one
	// playing among them, -1 when it is not there.
	Queue []string
	Index int
}

type sessionInfo struct {
	ID                    string `json:"Id"`
	Client                string
	DeviceName            string
	DeviceID              string `json:"DeviceId"`
	SupportsRemoteControl bool
	SupportsMediaControl  bool
	PlayableMediaTypes    []string
	NowPlayingItem        *Item
	LastPlaybackCheckIn   time.Time
	PlaylistItemID        string `json:"PlaylistItemId"`
	PlayState             struct {
		PositionTicks int64
		IsPaused      bool
		IsMuted       bool
		VolumeLevel   *int
		RepeatMode    string
		PlaybackOrder string
	}
	NowPlayingQueue []struct {
		ID             string `json:"Id"`
		PlaylistItemID string `json:"PlaylistItemId"`
	}
}

// Devices returns the other devices of the user that can be told what
// to play.
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	q := url.Values{}
	q.Set("ControllableByUserId", c.UserID)
	var sessions []sessionInfo
	if err := c.do(ctx, "GET", "/Sessions", q, nil, &sessions); err != nil {
		return nil, err
	}
	var out []Device
	for i := range sessions {
		s := &sessions[i]
		if s.DeviceID == c.DeviceID || !s.SupportsRemoteControl || !s.SupportsMediaControl {
			continue
		}
		if len(s.PlayableMediaTypes) > 0 && !slices.Contains(s.PlayableMediaTypes, "Audio") {
			continue
		}
		d := Device{ID: s.ID, Client: s.Client, Name: s.DeviceName, DeviceID: s.DeviceID}
		if it := s.NowPlayingItem; it != nil {
			p := &DevicePlaying{
				Item: *it, Position: time.Duration(s.PlayState.PositionTicks) * 100, ToldAt: s.LastPlaybackCheckIn, Paused: s.PlayState.IsPaused,
				Muted: s.PlayState.IsMuted, Volume: -1, Repeat: s.PlayState.RepeatMode, Shuffle: s.PlayState.PlaybackOrder == "Shuffle", Index: -1,
			}
			if v := s.PlayState.VolumeLevel; v != nil {
				p.Volume = *v
			}
			for j, e := range s.NowPlayingQueue {
				p.Queue = append(p.Queue, e.ID)
				if p.Index < 0 && (e.PlaylistItemID != "" && e.PlaylistItemID == s.PlaylistItemID || s.PlaylistItemID == "" && e.ID == it.ID) {
					p.Index = j
				}
			}
			if p.Index < 0 {
				p.Index = slices.Index(p.Queue, it.ID)
			}
			d.Playing = p
		}
		out = append(out, d)
	}
	return out, nil
}

// PlayOn has a device play songs: command is PlayNow, which replaces
// what it plays and starts with the song at start, from at; PlayNext,
// which puts them after the song playing; or PlayLast, at the end.
func (c *Client) PlayOn(ctx context.Context, device, command string, ids []string, start int, at time.Duration) error {
	q := url.Values{}
	q.Set("playCommand", command)
	q.Set("itemIds", strings.Join(ids, ","))
	if command == "PlayNow" {
		q.Set("startIndex", strconv.Itoa(start))
		if at > 0 {
			q.Set("startPositionTicks", strconv.FormatInt(int64(at/100), 10))
		}
	}
	return c.do(ctx, "POST", "/Sessions/"+device+"/Playing", q, nil, nil)
}

// Playstate sends a device one of the commands of playing: Pause,
// Unpause, PlayPause, Stop, NextTrack, PreviousTrack, or Seek to at.
func (c *Client) Playstate(ctx context.Context, device, command string, at time.Duration) error {
	q := url.Values{}
	if command == "Seek" {
		q.Set("seekPositionTicks", strconv.FormatInt(int64(at/100), 10))
	}
	return c.do(ctx, "POST", "/Sessions/"+device+"/Playing/"+command, q, nil, nil)
}

// Command sends a device a general command, as SetVolume with its
// Volume, SetRepeatMode with its RepeatMode, or ToggleMute.
func (c *Client) Command(ctx context.Context, device, name string, args map[string]string) error {
	if args == nil {
		args = map[string]string{}
	}
	return c.do(ctx, "POST", "/Sessions/"+device+"/Command", nil, map[string]any{"Name": name, "Arguments": args}, nil)
}

// The commands Aurelia takes from other devices.
var remoteCommands = []string{"VolumeUp", "VolumeDown", "Mute", "Unmute", "ToggleMute", "SetVolume", "SetRepeatMode", "SetShuffleQueue", "DisplayMessage", "PlayState", "PlayNext", "PlayMediaSource"}

// Announce tells the server that this device plays music and takes
// commands, which is what lists it among the devices of the others.
func (c *Client) Announce(ctx context.Context) error {
	return c.do(ctx, "POST", "/Sessions/Capabilities/Full", nil, map[string]any{
		"PlayableMediaTypes":           []string{"Audio"},
		"SupportedCommands":            remoteCommands,
		"SupportsMediaControl":         true,
		"SupportsPersistentIdentifier": true,
	}, nil)
}

// Order is what another device asks of this one.
type Order struct {
	// Kind is Play, Playstate or GeneralCommand.
	Kind string
	// Of Play: the items, the one to start with, where in it, and
	// PlayNow, PlayNext, PlayLast or PlayShuffle.
	ItemIDs     []string
	StartIndex  int
	Position    time.Duration
	PlayCommand string
	// Of Playstate: Pause, Unpause, PlayPause, Stop, NextTrack,
	// PreviousTrack, Seek (to Position), Rewind or FastForward. Of
	// GeneralCommand: its name, and its arguments.
	Command   string
	Arguments map[string]string
}

// Listen takes the orders other devices send this one through the
// server, until the connection ends or ctx does: it returns why. The
// server lists a device among those that take commands only while it
// listens.
func (c *Client) Listen(ctx context.Context, order func(Order)) error {
	u, err := url.Parse(c.Server)
	if err != nil {
		return err
	}
	u.Scheme = map[string]string{"http": "ws", "https": "wss"}[u.Scheme]
	u.Path = strings.TrimRight(u.Path, "/") + "/socket"
	q := url.Values{}
	q.Set("api_key", c.Token)
	q.Set("deviceId", c.DeviceID)
	u.RawQuery = q.Encode()
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	// The app's own transport: the proxy of the settings carries this too.
	conn, _, err := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{HTTPClient: &http.Client{}, HTTPHeader: http.Header{"User-Agent": {"Aurelia/" + Version}}})
	cancel()
	if err != nil {
		return fmt.Errorf("jellyfin: listening for other devices: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)
	if err := c.Announce(ctx); err != nil {
		return err
	}
	// The server closes a connection that says nothing for the time it
	// names: half of it is the pace it asks for.
	alive := time.NewTicker(30 * time.Second)
	defer alive.Stop()
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-alive.C:
				wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Write(wctx, websocket.MessageText, []byte(`{"MessageType":"KeepAlive"}`))
				cancel()
				if err != nil {
					stop()
					return
				}
			}
		}
	}()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var msg struct {
			MessageType string
			Data        json.RawMessage
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.MessageType {
		case "ForceKeepAlive":
			var seconds int
			if json.Unmarshal(msg.Data, &seconds) == nil && seconds >= 2 {
				alive.Reset(time.Duration(seconds) * time.Second / 2)
			}
		case "Play":
			var d struct {
				ItemIds            []string
				StartIndex         *int
				StartPositionTicks *int64
				PlayCommand        string
			}
			if json.Unmarshal(msg.Data, &d) != nil {
				continue
			}
			o := Order{Kind: "Play", ItemIDs: d.ItemIds, PlayCommand: d.PlayCommand}
			if d.StartIndex != nil {
				o.StartIndex = *d.StartIndex
			}
			if d.StartPositionTicks != nil {
				o.Position = time.Duration(*d.StartPositionTicks) * 100
			}
			order(o)
		case "Playstate":
			var d struct {
				Command           string
				SeekPositionTicks *int64
			}
			if json.Unmarshal(msg.Data, &d) != nil {
				continue
			}
			o := Order{Kind: "Playstate", Command: d.Command}
			if d.SeekPositionTicks != nil {
				o.Position = time.Duration(*d.SeekPositionTicks) * 100
			}
			order(o)
		case "GeneralCommand":
			var d struct {
				Name      string
				Arguments map[string]any
			}
			if json.Unmarshal(msg.Data, &d) != nil {
				continue
			}
			o := Order{Kind: "GeneralCommand", Command: d.Name, Arguments: map[string]string{}}
			for k, v := range d.Arguments {
				o.Arguments[k] = fmt.Sprint(v)
			}
			order(o)
		}
	}
}

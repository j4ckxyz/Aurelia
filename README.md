# Aurelia

A fast, native music player for [Jellyfin](https://jellyfin.org), for macOS,
Windows and Linux.

![Aurelia's home page](docs/home.png)

Aurelia is written in Go with [MyGo](https://github.com/egoist/mygo)'s native
UI: it draws its own interface on the GPU, with no webview and no Electron.
One binary of about 13 MB, and a library kept on disk so that every page
and every search shows at once. With a library of ten thousand songs in a
1240 × 800 window on a Retina display, it takes about 85 MB of memory at
rest, 105 to 120 MB while browsing, and about 140 MB while it plays; a
third to a half of that is the window's own frames on the GPU, which grow
with the window.

It plays the music side of Jellyfin only: albums, artists, songs and
playlists. It does not show movies or shows.

| | |
|---|---|
| ![The record: the cover turning, the lyrics coming up](docs/record.png) | ![An album](docs/album.png) |
| ![Lyrics and the queue](docs/lyrics.png) | ![A window at its smallest](docs/small.png) |
| ![Albums, in the light theme](docs/albums-light.png) | ![An artist, in Tokyo Night](docs/artist-tokyo-night.png) |
| ![Search, in Catppuccin Mocha](docs/search-catppuccin.png) | ![The themes](docs/themes.png) |
| ![Browsing by genre and decade](docs/browse.png) | ![The equalizer](docs/equalizer.png) |

![Sharing an album as a picture](docs/share.png)

The screenshots show a library made up for them, with pictures drawn by
`screenshots_test.go`: `AURELIA_SCREENSHOTS=docs go test -run Screenshots .`
draws them again.

## Install

**macOS and Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/j4ckxyz/Aurelia/main/install.sh | sh
```

**Windows**, in PowerShell

```powershell
irm https://raw.githubusercontent.com/j4ckxyz/Aurelia/main/install.ps1 | iex
```

Either installs the latest release for you alone, without administrator
rights: in `/Applications` on macOS, `~/.local` on Linux, and
`%LOCALAPPDATA%\Programs` on Windows. Running it again updates; `sh
install.sh --uninstall` removes the app on macOS and Linux, and Windows
lists it in "Installed apps".

Installing this way also spares you the warnings that macOS and Windows show
for apps whose developer has not paid for a certificate: those are shown for
files a browser downloaded, which it marks, and the scripts mark nothing.
The builds are signed ad hoc, not by Apple or Microsoft.

The [releases](../../releases) hold the same builds to download by hand: a
disk image for macOS (both kinds of Mac), an installer and a portable
`.exe` for Windows, and a `.deb` and a `.tar.gz` for Linux. A disk image
from a browser needs a right-click and Open the first time, or
`xattr -dr com.apple.quarantine /Applications/Aurelia.app`; on Windows,
"More info ▸ Run anyway".

Linux needs GTK 3, which desktops have, and PulseAudio, PipeWire's
PulseAudio service, or ALSA for sound. The Linux installer may say that
WebKitGTK is missing: Aurelia shows no web page and does not need it.

### Updates

Aurelia updates itself from the releases here. When it opens and a newer
version is out, it says so before anything else, on a page with what is
new: **Update and reopen** downloads it, checks that it was signed with
Aurelia's key, puts it in place of the app and opens the app again by
itself, signed in as you were, with your queue; **Not now** leaves it for
another day. Versions released while Aurelia runs are downloaded quietly,
and a restart offered. Settings ▸ Updates turns all of that off, and has a
button to check now. A copy installed from the `.deb` is updated by
installing a newer `.deb` instead.

## What it does

- **Your whole library, instantly.** Aurelia keeps an index of the library
  on disk and refreshes it in the background. It starts with everything on
  screen, and searching thousands of songs takes under a millisecond.
- **Albums, artists, songs, playlists, favorites**, and a search of all of
  them as you type. Home opens with the song you were on, to go on with,
  among the albums you played last (a right click takes one out), then
  what is new, the artists you play most and your playlists. The sidebar
  comes back to where you left each part of the library, an artist's page
  say; a second click goes to its first page.
- **Your playlists, not everyone's.** A Jellyfin server lists the
  playlists others made public among your own. Aurelia shows yours, and
  theirs once Settings ▸ Library asks.
- **A window of any size**, down to 480 × 420: narrow, it puts the sidebar
  away behind a button and keeps the buttons that play. The sidebar hides
  with its button or ⌘B, and its edge drags to resize it.
- **Playback** of FLAC and MP3 decoded in Go, with the server transcoding
  everything else. Gapless from one song to the next, seeking that does not
  wait for the whole file, a queue you can add to, take from and drag into
  another order, shuffle, repeat, and a quality limit for slow
  connections. Volume normalization plays every song about as loud, at one
  of three levels as in Spotify: Louder, Normal or Quieter. Plays are
  reported to the server.
- **An equalizer** of ten bands and a preamp, with presets and your own,
  heard the instant a slider moves; with mono audio and a balance.
- **Spatial audio** for headphones: music as if from speakers around you in
  a room, rather than from inside your head, in the manner of the spatial
  audio of AirPods. Automatic plays it on AirPods and other headphones
  alone, and follows them as they are put on and taken off; Always On
  plays it on anything. Settings ▸ Playback, or the "…" of the player's
  bar. It is Aurelia's own rendering, the same on every system, and keeps
  a voice in the middle at its level and its tone; it does not follow your
  head as Apple's own does with Apple's apps.
- **Vinyl**: music as if played from its album's record on a turntable.
  Each album is laid out on records as a pressing plant would (LPs of
  sides of 24 minutes at most, a 45 for a few songs, 78s before 1950),
  and the sound is worked out from the groove itself: the music cut with
  the RIAA curve into the walls of a stereo groove, worn by the round
  stylus that played it, with dust and scratches in it, traced by a
  stylus of today. So the hiss and crackle are heard where a record's
  are, in quiet passages and between songs; scratches tick once a
  revolution; worn records lose treble, and grit on loud highs; the
  inner grooves at the end of a side distort a little more; the needle
  drops into the lead-in as a side begins, and the run-out and the
  locked groove play as it ends, before the other side. The record's
  condition, by collectors' grades, is what the album's year has made it,
  or one you choose, made cleaner or more worn with a slider. Each record
  is the same each time: its scratches where they were. Settings ▸
  Playback, or the "…" of the player's bar.
- **Your choice of output.** Settings ▸ Playback lists the computer's
  outputs, and the one you choose is kept: the sound moves to it while a
  song plays, and goes back to the system's default when it is not plugged
  in, and back again when it is. **Crossfade** (2 to 12 seconds, never
  between songs that follow each other on an album) is there too, and a
  **sleep timer** (after a time, with the sound fading out, or at the end
  of the song or the album) is in the "…" of the player's bar.
- **Share as a picture.** "Share as a Picture…" in the menu of a song or an
  album makes a clean picture of its cover with its name and the artist's,
  on white, on black, on a color of your own or on the cover's own colors,
  square, tall for stories or wide, to copy or save as a PNG.
- **Radio.** "Start Radio" on a song, an album or an artist plays songs the
  server finds like it; "Keep playing similar songs" (off unless you turn it
  on) goes on with them when the queue ends. Artists' pages say who else
  their fans like.
- **Playlists, made here.** Add a song or an album to a playlist, make a new
  one, rename it, put its songs in another order, take one out, delete it,
  or keep the queue as a playlist. They are the server's, so every device
  has them.
- **Browse by genre and by decade**, "Your listening" (what you play most),
  and the songs sorted by what was played lately, most, or never. Pin
  albums, artists and playlists to the sidebar.
- **Lyrics**, with the line being sung lit when the lyrics are timed. When
  the server has none, Aurelia can ask LRCLIB, a free database of them, if
  you let it (Settings ▸ Playback; it is told the song's artist, title,
  album and length).
- **On your desktop.** An icon in the menu bar or the tray with the song and
  its buttons (and closing the window leaves the music playing): Aurelia's
  icon in a color of your choice, or the cover of the song playing, with
  the song, or the song and the artist, beside it in macOS's menu bar and
  in the panels of GNOME (with its AppIndicator extension), Ubuntu, Xfce,
  Cinnamon, MATE and Budgie (Windows and KDE Plasma show it when you point
  at the icon); Settings ▸ Desktop, with a picture of how it looks. A
  notification as the song changes while Aurelia is behind, and
  **scrobbling to ListenBrainz** or a server of your own that speaks as it
  does: your token in Settings ▸ Scrobbling.
- **Short animations**, a hint of movement as pages, panels and pictures
  come in, that follow the system's Reduce motion (Settings ▸ Appearance ▸
  Animations).
- **The record.** Now Playing in the player's bar, or V, shows the song in
  place of the app: its cover as a record that turns while it plays, over
  the cover's own colors, and the lyrics under it, each line sliding up as
  it is sung. F puts it over the whole screen. It also goes to a small
  window of its own (⇧⌘M) that a pin keeps above your other windows.
- **Play on another device.** Play On, in the "…" of the player's bar, lists
  the other places you are signed in to Jellyfin that play music: an
  Aurelia on another computer, Jellyfin's web and mobile apps. Choose one
  and what plays here goes on there; Aurelia then shows what that device
  plays and controls it: play and pause, next and previous, the place in
  the song, the volume, shuffle, repeat, and songs added to its queue.
  Aurelia is such a device to the others, so one Aurelia controls another.
- **The keyboard does everything**: see [Shortcuts](#shortcuts).
- **Back and forward** through the pages you visited, at the top left, as
  in a browser: buttons, a right click for the history, ⌘[ and ⌘] (Alt+←
  and Alt+→ on Windows and Linux), a mouse's side buttons, and two fingers
  swiped across a trackpad, to the right to go back (on macOS, Windows'
  precision touchpads and Linux; on a Mac, as its trackpad setting says,
  with two fingers or three).
- **At home on each system.** The window's buttons are the system's own:
  the traffic lights on macOS, Windows 11's caption buttons with their snap
  layouts, and your desktop's title buttons on Linux, or its title bar
  where it draws one. They, the menus and the questions that ask before
  something is undone (the system's own dialogs) are light or dark as your
  theme is. On Windows and Linux the menu at Aurelia's name, at the top of
  the sidebar, and Alt or F10, open the menu bar's menus.
- **Downloads for offline.** Download a song, an album or a playlist from
  its page or its menu, and it plays without the server. The Downloads page
  lists what is kept and the room it takes; downloads stay until you remove
  them. When the server does not answer, Aurelia says so and plays what is
  downloaded.
- **Small caches.** What is kept without asking is light: the library's
  index (a megabyte or two), pictures within 150 MB, and the songs played
  lately within 300 MB, so that they start at once and replay without the
  network. Both limits are in Settings ▸ Storage, the oldest make room, and
  a picture not fetched yet shows as an icon until it is.
- **The queue comes back.** Aurelia opens with the song it had, paused
  where it was.
- **Jams (beta).** Listen together with people on the same network, each
  on their own computer and with their own sound, even friends on another
  server or none: see [Jams](#jams-beta).
- **The system knows what plays**, with the album's cover: Control Center
  and the lock screen on macOS, the media flyout and the lock screen on
  Windows, and the desktop's media controls on Linux (MPRIS, as GNOME and
  KDE read it). Their buttons, and the keyboard's and headphones' play,
  next and previous keys, control Aurelia, with its window hidden too.
- **Themes.** Fourteen built in, a theme editor that shows changes as you
  make them, and themes as JSON files you can write by hand. Themes made
  for Visual Studio Code, Firefox and Chrome import as they are.

## Jams (beta)

A Jam is listening together with people on the same network, as
Spotify's Jams are: one person hosts, the others join, and everyone hears
the same queue, each on their own computer. Turn it on in Settings ▸
Beta; the player's bar then has a Jam button.

- **Starting one.** Start a Jam shows a code, as K7Q X4R. Others join
  with it from their own Jam button, or find your Jam among those nearby
  and ask you to let them in. Copy invite gives the code with your
  computer's address, for networks that do not let computers find one
  another.
- **Everyone adds songs.** A song played or added from anyone's library
  joins the Jam's queue, marked with who added it. The host chooses
  whether guests also play, pause, skip, seek and change the order, or
  only add songs.
- **Who did what.** The Jam's dialog keeps the Jam's activity, the latest
  first: who paused or played on, skipped, moved in a song, added,
  removed or moved songs, and who joined, left or lost the connection.
  What someone else does to what plays shows as a notification too.
- **Your own pause.** A pause from outside Aurelia's window, as the
  keyboard's media keys, headphones taken off, the system's media
  controls or the computer going to sleep, pauses only you: the Jam goes
  on for everyone else, and **Catch up** (or play) takes you back to
  where the host is. A computer paused as it went to sleep catches up by
  itself as it wakes. On Linux, Aurelia asks the desktop not to suspend
  the computer while music plays, as macOS and Windows do of themselves;
  closing the lid still puts it to sleep.
- **In step, with your own sound.** Each computer plays the music itself,
  within a fraction of a second of the host: on its own speakers or
  headphones, at its own volume, through its own equalizer, spatial
  audio, crossfade and vinyl. A guest whose connection drops keeps
  playing and comes back in by itself.
- **One copy is enough.** A guest of the same Jellyfin server plays the
  songs from it, as ever. Songs a guest's server does not have play from
  the host, which gets songs of a guest's server from that guest: so a
  friend on another server, or with none at all ("Join a friend's Jam" on
  the sign-in page), hears all of it. Only what is in the Jam's queue can
  be fetched, and only by the people in it; nobody's server password or
  sign-in is ever shared.
- **Leaving.** Your own queue comes back, paused where it was. When the
  host ends the Jam, the songs that came from guests leave the host's
  queue.

Jams work between computers that can reach one another: the same Wi-Fi
or wired network, the same tailnet (below), or any two networks through
a relay of your own (below that). The host listens on TCP
port 47475 (or another, when that is taken) and answers on UDP port
47474. The first time, Windows asks whether to let Aurelia through its
firewall, and macOS whether Aurelia may find devices on the local
network: both need a yes. Networks that keep their devices apart, as in
hotels and some offices, stop Jams too.

### Jams through your relay

A small server program, [`relay/`](relay/), run on a server of yours (a
few megabytes of memory), lets a Jam be joined from any network. Set its
address and key in Settings ▸ Beta ▸ **Jam relay**: a Jam you host then
connects to it as well, says "Reachable through your relay", and its
invite names the relay. A guest still tries the host's address on its own
network first, and the relay only when the host is not there; a guest
with only the code looks for it on the network, then on the relay set in
its own settings. Guests need no key: the code, or the host's yes, lets
them in. [relay/README.md](relay/README.md) says how to run it.

### Jams over Tailscale

With [Tailscale](https://tailscale.com) running and signed in, a Jam
reaches every device of your tailnet, wherever it is, and the Jam's
dialog has a **Tailscale** section: the devices of the tailnet that have
Aurelia open, each with whose it is ("Yours", "Sam (sam@github)", or a
tagged device's tags).

- **Invite** sends a device an invite, even one whose Jams are off: it
  asks "Ada invited you to a Jam (beta)", and **Turn on and join** turns
  the beta on there and joins. A device of yours that had Aurelia open
  before, but has it closed now or does not answer, gets **Copy invite
  link** instead, as does anyone else: the link is at your Tailscale
  address. **Join** joins a Jam a device of the tailnet holds.
- **Who is who is Tailscale's word.** Every connection is checked with
  Tailscale (WhoIs): invites show who really sent them, whatever they
  claim, your own devices join your Jam without the code, and someone
  else's device asks, as "Sam's ThinkPad (Sam) wants to join". Joining
  on the local network with the code works as before.
- **People you shared this computer with** show as "Can join you": they
  can reach you, you cannot reach them, so they join with the tailnet
  invite.
- **What is open.** While Aurelia runs and Tailscale is connected, it
  listens on TCP port 47475 of this computer's Tailscale addresses only,
  never on the local network, the Jams' beta on or off. It answers
  whether Aurelia is here (its version, the device's name, whether Jams
  are on and a Jam is held) and takes invites, from devices of the
  tailnet alone; the rest of Jams stays off until the beta is turned on.
  Settings ▸ Beta ▸ **Let devices on my tailnet invite me to Jams** (on by
  default) closes it. Tailscale encrypts what goes between its devices, so
  a Jam goes over it as plain HTTP, and a proxy in the settings never
  carries it.
- **Finding Tailscale.** Aurelia asks Tailscale's own app, as its command
  line does, so the `tailscale` command need not be installed: on Linux
  at its socket (no root needed), on Windows at its named pipe, on macOS
  the App Store app, the standalone app and the open-source tailscaled
  alike. A Flatpak or Snap of Aurelia cannot reach Tailscale's socket, and
  a tailscaled in userspace-networking mode has no Tailscale address to
  listen on; there, the section does not show. With the standalone macOS
  app, only an administrator's account may read what Aurelia needs to ask
  it. Without Tailscale, nothing of it shows.

## Behind a network that blocks your server

A network may block your server's address: its name does not resolve, or
connections to it go nowhere. Aurelia can reach it through a proxy instead,
HTTP or SOCKS5: **Connect through a proxy** on the sign-in page, or Settings
▸ Connection, which also tests the way to the server and says how long it
takes.

```
socks5://host:1080
http://user:password@host:8080
host:8080
```

Everything then goes through the proxy: signing in, the library, pictures,
songs and updates. The proxy is asked for the server by its name, so the
name need not resolve where you are.

- **What the proxy sees.** With a server at `https://`, the proxy carries a
  connection it cannot read: it learns which server you use and how much
  you send and receive, nothing more. With a server at `http://`, it can
  read everything, your password included, and Aurelia says so when you
  set one.
- **Which proxy.** One you control is the one to use: `ssh -D 1080
  you@a-machine-of-yours` makes a SOCKS5 proxy at `socks5://127.0.0.1:1080`
  out of any computer you can sign in to. Free public proxies exist, in
  lists that change by the hour; they are slow for music, they come and go,
  and strangers run them. Aurelia does not look for one for you.
- **Where it does not help.** A network that also blocks the proxy, or
  lets through only the sites it knows, blocks this too.

Without the setting, Aurelia follows `HTTPS_PROXY` and `HTTP_PROXY` of the
environment, as most programs do.

### A proxy of your own

`tools/aurelia-proxy` is a small proxy made for this, for a Linux machine
of yours with a public address, a cheap VPS for one. It needs no root and
no domain name, and it is of no use to anyone but you:

- it asks for a password, which it makes itself, and turns away for ten
  minutes an address that guesses ten times;
- it reaches only the servers you name, on port 443, and never an address
  inside its own machine or network;
- it speaks TLS with a certificate it makes itself, so the password is not
  sent in the clear. Aurelia is given the certificate's fingerprint, and
  talks to nothing else at that address;
- on Linux it shuts itself in, with Landlock and seccomp: once started it
  can open no file but the resolver's, connect to no port but 443 and DNS,
  and listen nowhere new. Were it broken into, the rest of the machine is
  out of its reach.

```
git clone https://github.com/j4ckxyz/Aurelia && cd Aurelia
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o aurelia-proxy ./tools/aurelia-proxy

# on the machine, 203.0.113.7 being its public address:
./aurelia-proxy -sandbox -name 203.0.113.7 -listen :8443 -allow music.example.com,github.com,*.githubusercontent.com
./aurelia-proxy -name 203.0.113.7 url
```

`url` prints what to paste into Aurelia, the password and the fingerprint
in it:

```
https://aurelia:password@203.0.113.7:8443#pin-sha256=...
```

The two GitHub names let Aurelia fetch its updates through it; leave them
out and updates wait for a network that reaches GitHub. The port must be
open in the machine's firewall. `-help` lists the rest, and the proxy's
files are in `~/.config/aurelia-proxy`: delete `password` and start it
again for a new one.

A network that restricts what is reached often lets only ports 443 and 80
through, and Aurelia then says that the proxy did not answer. `-listen`
takes several addresses, the first being the one `url` gives: with
`-listen :443,:8443` the proxy answers at both. Linux keeps ports under
1024 to root; this lets any program of the machine listen from 443 up, the
proxy among them:

```
echo 'net.ipv4.ip_unprivileged_port_start=443' | sudo tee /etc/sysctl.d/50-aurelia-proxy.conf
sudo sysctl --system
```

### When it does not connect

Aurelia says which step of the way failed, and how: the name not found, a
port that nothing answers at, a certificate the network put in the way,
the proxy's password refused, a page shown in the server's place,
Cloudflare stopping the request. Settings ▸ Connection ▸ Test asks the
server again and tells the same.

## Themes

Settings ▸ Appearance lists the themes. **New theme** opens the editor on a
copy of the current one; **Import…** reads a theme made elsewhere; **Themes
folder** opens the folder Aurelia reads, which is

| | |
|---|---|
| macOS | `~/Library/Application Support/Aurelia/themes` |
| Windows | `%AppData%\Aurelia\themes` |
| Linux | `~/.config/Aurelia/themes` |

Any theme file dropped in that folder shows in the list, and a file you
edit shows again as soon as you save it. Aurelia reads:

- its own themes (below);
- **Visual Studio Code** color themes: a `*-color-theme.json` file, with
  comments or not, or a whole extension as a `.vsix`, all of whose themes
  are imported;
- **Firefox and Chrome** themes: the `manifest.json` of a theme, or the
  extension as an `.xpi`, `.crx` or `.zip`.

A theme of Aurelia's is a name and colors, all of them optional: those left
out are made from the ones given, and a theme is always kept readable.

```json
{
  "name": "Tokyo Night",
  "type": "dark",
  "radius": 8,
  "font": "",
  "colors": {
    "background": "#1a1b26",
    "sidebar": "#16161e",
    "bar": "#16161e",
    "surface": "#24283b",
    "surfaceHover": "#2f3549",
    "border": "#292e42",
    "text": "#c0caf5",
    "textMuted": "#7f89b5",
    "accent": "#7aa2f7",
    "accentText": "#16161e",
    "selection": "#283457",
    "danger": "#f7768e",
    "success": "#9ece6a",
    "warning": "#e0af68"
  }
}
```

Colors are `#rgb`, `#rrggbb`, `#rrggbbaa`, `rgb()`, `rgba()` or `hsl()`.
`type` is `dark` or `light`; left out, the background tells.

## Shortcuts

⌘/ (Ctrl+/) lists them all in the app. ⌘ is Ctrl on Windows and Linux, and
⌥ is Alt.

| | |
|---|---|
| Space | Play or pause, wherever the focus is |
| ⇧⌘→ / ⇧⌘← | Next / previous song |
| ⇧→ / ⇧← | Forward / back 5 seconds |
| ⌘↑ / ⌘↓ | Volume; with ⇧, full volume and mute. M mutes too |
| ⌘S / ⌘R | Shuffle / repeat |
| ⌥⇧B | Add the song playing to Favorites, or remove it |
| ⌘← / ⌘→ | Back / forward a page; also ⌘[ and ⌘], ⌥← and ⌥→ |
| J / K | The next item of the page, or the one below / the one before, or above |
| H / L | The item to the left / to the right |
| Enter | Open the item, or play the song; Esc puts the marker away |
| ⌘K, ⌘F, / | Search |
| ⌥⇧H, ⌥⇧4, ⌥⇧3, ⌥⇧2, ⌥⇧S, ⌥⇧1, ⌥⇧D | Home, Albums, Artists, Songs, Favorites, Playlists, Downloads; ⌘1 … ⌘6 too |
| ⌥⇧J | The album of the song playing |
| ⌥⇧Q / ⌥⇧L | The queue / the lyrics |
| ⌥⇧E | The equalizer |
| V / F / ⇧⌘M | The record in the window / in full screen / in a small window |
| ⌘B | Show or hide the sidebar |
| ⇧⌘R | Update the library |
| ⌘, | Settings |

A double click on a song plays it; a right click on a song, an album or a
theme opens its menu.

## Build it yourself

Aurelia needs [Go](https://go.dev/dl/) 1.27 or later, and nothing else: no
C compiler, no Node.

```sh
git clone https://github.com/j4ckxyz/Aurelia.git
cd Aurelia
go tool mygo build     # build/<os>-<arch>/: the app and its installer
go tool mygo dev       # or run it, rebuilt as the code changes
go run .               # or just run it
```

`go tool mygo build -platform darwin/arm64,windows/amd64,linux/amd64`
builds for other platforms from any of them.

To skip the sign-in page while developing, set `JELLYFIN_URL`,
`JELLYFIN_USERNAME` and `JELLYFIN_PASSWORD`; `AURELIA_DIR` names another
directory for the app's data, apart from the installed app's.

```sh
go test ./...                      # the packages, and the view without a window
go run ./tools/mkicon              # redraw resources/icon.png
tools/update-test.sh               # install an old version, and watch it update itself
```

### Releasing

A new `version` in `mygo.json`, with its changes under a heading of its
number in `CHANGELOG.md`, pushed to `main`, is released by
`.github/workflows/release.yml`: the apps of every platform, their
installers, and the updates that installed copies fetch, signed with the
key in the repository's `MYGO_UPDATER_PRIVATE_KEY` secret. Installed apps
take only updates signed with that key: keep a copy of it somewhere safe.
Three workflows check what a release is for on a real macOS, Windows and
Linux: the tests, the install commands above against the latest release,
and an installed copy replacing itself with a newer one.

Some tests need more than the code: `JELLYFIN_URL` and the rest make the
library's tests read a real server, `AURELIA_TEST_FLAC=song.flac` gives the
audio tests a file, and `AURELIA_TEST_DEVICE=1` plays it on the sound card.

## How it is made

| | |
|---|---|
| `main.go`, `app.go` | the app, its window and menus; signing in and reading the library |
| `view.go`, `pages.go`, `widgets.go`, `playerbar.go`, `search.go`, `settings.go`, `themeeditor.go`, `login.go` | the interface |
| `player.go` | the queue: order, shuffle, repeat, and what the server is told |
| `downloads.go` | what is kept for offline |
| `update.go` | the app's own updates |
| `proxy.go` | the proxy the app reaches the network through, and the one transport everything uses |
| `keys.go` | the keyboard: one table of commands for the window, the menu bar and the page of shortcuts |
| `remote.go` | playing on another device, and taking the orders of one |
| `stage.go`, `spin.go` | the record: the cover turned by the app, the lyrics sliding up |
| `tools/aurelia-proxy` | a proxy to run on a machine of your own, for this app only |
| `nowplaying_darwin.go`, `nowplaying_linux.go`, `nowplaying_windows.go` | what plays, told to the system: Now Playing, MPRIS, and the System Media Transport Controls |
| `images.go` | pictures: in memory, on disk, and from the server |
| `internal/jellyfin` | the server's API |
| `internal/library` | the library in memory and on disk, and its search |
| `internal/audio` | the download cache, the FLAC and MP3 decoders, the resampler and the engine that plays without gaps |
| `internal/theme` | the theme format, the built-in themes, and the importers |

Where things are kept: settings, themes and the library's index in the
app's data directory (`~/Library/Application Support/Aurelia`,
`%AppData%\Aurelia`, `~/.config/Aurelia`), with the downloads; the caches
of pictures and of songs played lately in its cache directory. The
settings hold the server's token, never the password.

## Credits

Built with [MyGo](https://github.com/egoist/mygo). Icons by
[Lucide](https://lucide.dev). Sound through
[Oto](https://github.com/ebitengine/oto), FLAC by
[mewkiz/flac](https://github.com/mewkiz/flac) and MP3 by
[go-mp3](https://github.com/hajimehoshi/go-mp3). Their licenses are in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

Aurelia is not affiliated with the Jellyfin project.

## License

[MIT](LICENSE).

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
| ![An album](docs/album.png) | ![Lyrics and the queue](docs/lyrics.png) |
| ![Albums, in the light theme](docs/albums-light.png) | ![An artist, in Tokyo Night](docs/artist-tokyo-night.png) |
| ![Search, in Catppuccin Mocha](docs/search-catppuccin.png) | ![The themes](docs/themes.png) |

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

Aurelia updates itself from the releases here. A little after it opens,
and a few times a day, it looks for a newer version, downloads it, checks
that it was signed with Aurelia's key, puts it in place of the app, and
offers to restart; the version running plays on until you do. Settings ▸
Updates turns that off, and has a button to check now. A copy installed
from the `.deb` is updated by installing a newer `.deb` instead.

## What it does

- **Your whole library, instantly.** Aurelia keeps an index of the library
  on disk and refreshes it in the background. It starts with everything on
  screen, and searching thousands of songs takes under a millisecond.
- **Albums, artists, songs, playlists, favorites**, a home page of what is
  new and what you played, and a search of all of them as you type.
- **Playback** of FLAC and MP3 decoded in Go, with the server transcoding
  everything else. Gapless from one song to the next, seeking that does not
  wait for the whole file, a queue you can add to, take from and drag into
  another order, shuffle, repeat, volume normalization and a quality limit
  for slow connections. Plays are reported to the server.
- **Lyrics**, with the line being sung lit when the lyrics are timed.
- **Back and forward** through the pages you visited, at the top left, as
  in a browser: buttons, a right click for the history, ⌘[ and ⌘] (Alt+←
  and Alt+→ on Windows and Linux) and a mouse's side buttons.
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
- **The system knows what plays**, with the album's cover: Control Center
  and the lock screen on macOS, the media flyout and the lock screen on
  Windows, and the desktop's media controls on Linux (MPRIS, as GNOME and
  KDE read it). Their buttons, and the keyboard's and headphones' play,
  next and previous keys, control Aurelia, with its window hidden too.
- **Themes.** Fourteen built in, a theme editor that shows changes as you
  make them, and themes as JSON files you can write by hand. Themes made
  for Visual Studio Code, Firefox and Chrome import as they are.

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

| macOS | Windows, Linux | |
|---|---|---|
| Space | Space | Play or pause |
| ⌘→ / ⌘← | Ctrl+→ / Ctrl+← | Next / previous song |
| ⌘↑ / ⌘↓ | Ctrl+↑ / Ctrl+↓ | Volume |
| ⌘F, ⌘K | Ctrl+F, Ctrl+K | Search |
| ⌘[ / ⌘] | Alt+← / Alt+→ | Back / forward |
| ⌘1 … ⌘6 | Ctrl+1 … Ctrl+6 | Home, Albums, Artists, Songs, Favorites, Playlists |
| ⇧⌘U | Ctrl+Shift+U | Show the queue |
| ⇧⌘L | Ctrl+Shift+L | Lyrics |
| ⌘R | Ctrl+R | Update the library |
| ⌘, | Ctrl+, | Settings |

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

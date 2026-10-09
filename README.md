# Aurelia

A fast, native music player for [Jellyfin](https://jellyfin.org), for macOS,
Windows and Linux.

Aurelia is written in Go with [MyGo](https://github.com/egoist/mygo)'s native
UI: it draws its own interface on the GPU, with no webview and no Electron.
One binary of about 13 MB, about 80 MB of memory at rest and a little over
100 MB while it plays, and a
library kept on disk so that every page and every search shows at once.

It plays the music side of Jellyfin only: albums, artists, songs and
playlists. It does not show movies or shows.

## What it does

- **Your whole library, instantly.** Aurelia keeps an index of the library
  on disk and refreshes it in the background. It starts with everything on
  screen, and searching thousands of songs takes under a millisecond.
- **Albums, artists, songs, playlists, favorites**, a home page of what is
  new and what you played, and a search of all of them as you type.
- **Playback** of FLAC and MP3 decoded in Go, with the server transcoding
  everything else. Gapless from one song to the next, seeking that does not
  wait for the whole file, a queue you can add to and take from,
  shuffle, repeat, volume normalization and a quality limit for slow
  connections. Plays are reported to the server.
- **Lyrics**, with the line being sung lit when the lyrics are timed.
- **Back and forward** through the pages you visited, at the top left, as
  in a browser: buttons, a right click for the history, ⌘[ and ⌘] (Alt+←
  and Alt+→ on Windows and Linux) and a mouse's side buttons.
- **Caches.** Pictures and songs are kept on disk; a picture not fetched
  yet shows as an icon until it is. The pictures of the whole library are
  fetched ahead, quietly, so that scrolling never waits for the network.
- **Themes.** Fourteen built in, a theme editor that shows changes as you
  make them, and themes as JSON files you can write by hand. Themes made
  for Visual Studio Code, Firefox and Chrome import as they are.

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

## Download

Builds for every platform are attached to the
[releases](../../releases): a disk image for macOS, an installer and a
portable `.exe` for Windows, and a `.deb`, a `.tar.gz` and an install
script for Linux.

The builds are not signed with a developer certificate yet. macOS asks you
to confirm the first time: right-click the app and choose Open, or run
`xattr -dr com.apple.quarantine /Applications/Aurelia.app`. Windows
SmartScreen shows "More info ▸ Run anyway".

Linux needs GTK 3, which desktops have, and PulseAudio, PipeWire's
PulseAudio service, or ALSA for sound.

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
```

Some tests need more than the code: `JELLYFIN_URL` and the rest make the
library's tests read a real server, `AURELIA_TEST_FLAC=song.flac` gives the
audio tests a file, and `AURELIA_TEST_DEVICE=1` plays it on the sound card.

## How it is made

| | |
|---|---|
| `main.go`, `app.go` | the app, its window and menus; signing in and reading the library |
| `view.go`, `pages.go`, `widgets.go`, `playerbar.go`, `search.go`, `settings.go`, `themeeditor.go`, `login.go` | the interface |
| `player.go` | the queue: order, shuffle, repeat, and what the server is told |
| `images.go` | pictures: in memory, on disk, and from the server |
| `internal/jellyfin` | the server's API |
| `internal/library` | the library in memory and on disk, and its search |
| `internal/audio` | the download cache, the FLAC and MP3 decoders, the resampler and the engine that plays without gaps |
| `internal/theme` | the theme format, the built-in themes, and the importers |

Where things are kept: settings, themes and the library's index in the
app's data directory (`~/Library/Application Support/Aurelia`,
`%AppData%\Aurelia`, `~/.config/Aurelia`), pictures and songs in its cache
directory. The settings hold the server's token, never the password.

## Credits

Built with [MyGo](https://github.com/egoist/mygo). Icons by
[Lucide](https://lucide.dev). Sound through
[Oto](https://github.com/ebitengine/oto), FLAC by
[mewkiz/flac](https://github.com/mewkiz/flac) and MP3 by
[go-mp3](https://github.com/hajimehoshi/go-mp3). Their licenses are in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

Aurelia is not affiliated with the Jellyfin project.

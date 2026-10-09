# Changes

## 0.3.0

- An equalizer: ten bands and a preamp, presets and your own, heard as a
  slider moves. Mono audio and a balance beside it.
- A choice of output device in Settings ▸ Playback, kept between runs. The
  sound moves while a song plays, falls back to the default when the device
  is gone, and returns to it when it is back. On macOS, Linux (PulseAudio and
  ALSA) and Windows.
- Crossfade between songs, 2 to 12 seconds, never between songs that follow
  each other on an album; and a sleep timer, after a time (the sound fades)
  or at the end of the song or the album.
- Share as a Picture: the cover, the name and the artist on white, black,
  a color of your own or the cover's colors, as a square, a story or a wide
  picture, copied or saved as a PNG.
- Start Radio on songs, albums and artists, and "Keep playing similar
  songs" for when the queue ends. "Fans also like" on artists' pages.
- Playlists made here: add to a playlist, new, rename, delete, move and
  remove songs, and keep the queue as a playlist. Pin albums, artists and
  playlists to the sidebar.
- Browse by genre and by decade, Your listening, the songs by Recently
  Played and Never Played, and Song Info with how a song is played.
- Short animations as pages, panels and pictures come in, with a setting
  that follows the system's Reduce motion; and bars that move beside the
  song playing.
- An icon in the menu bar or the tray, a notification as the song changes,
  scrobbling to ListenBrainz, and lyrics from LRCLIB when the server has
  none (off until you turn it on).

## 0.2.0

- The record: a button of the player's bar, or V, shows the song as its
  cover turning like a record, with the lyrics sliding up line by line. F
  puts it over the whole screen, and it goes to a small window of its own
  that can be pinned above the others.
- Play on another device: choose an Aurelia on another computer or one of
  Jellyfin's apps, and control it from here. Aurelia takes the same
  orders from them.
- Keyboard shortcuts for everything, listed in the app (⌘/). Space plays
  and pauses wherever the focus is, ⌘← and ⌘→ go back and forward, J and K
  move over the items of a page. The next and previous song are now ⇧⌘→
  and ⇧⌘←.
- A new version is offered as Aurelia opens, on a page of its own, and
  Aurelia reopens by itself once it is installed, signed in as before.
- Home opens with the song to go on with among the albums played last,
  which a right click takes out; then the artists you play most and your
  playlists.
- The sidebar comes back to the page each part of the library was left
  at. It hides with its button or ⌘B, and its edge drags to resize it.
- The window goes down to 480 by 420 and adapts: narrow, the sidebar is
  put away and the player's bar keeps the buttons that play.
- Playlists that other people of the server made public are hidden until
  Settings ▸ Library asks for them.
- Normalized volume has three levels: Louder, Normal and Quieter.

## 0.1.5

- Errors of the network say what failed and why, in place of "The server
  took too long to answer": the proxy or the server, its address and
  port, and whether its name was not found, nothing answered there, the
  connection was refused or cut, its certificate was not trusted, its
  password was refused, or a web page or Cloudflare's check answered in
  the server's place.
- A connection that is not made within 8 seconds is given up, so that the
  step that does not answer is known, and told sooner.
- An address typed without http:// or https:// tells the failure of the
  first way tried, not of the second.
- aurelia-proxy listens on several addresses, as `-listen :443,:8443`,
  for networks that let only port 443 through.

## 0.1.4

- A proxy of your own: `tools/aurelia-proxy` runs on a Linux machine with
  a public address, without root or a domain name. It asks for a password,
  reaches only the servers you name, and shuts itself in.
- A proxy at `https://` with a certificate of its own making, as that one
  has, is trusted by its fingerprint: `https://user:password@host:8443#pin-sha256=...`.
- Aurelia names itself in what it asks of the network. Servers behind
  Cloudflare turned it away when it came through a proxy at a hosting
  company, taking it for a script.

## 0.1.3

- A proxy, HTTP or SOCKS5, to reach a server that the network blocks: on
  the sign-in page and in Settings, with a test of the way to the server.
  Aurelia says what a proxy could read when the server is not at https.
- Aurelia is under the MIT license.

## 0.1.2

- The album's cover shows with the song in the system's display of what
  plays: Control Center and the lock screen on macOS, the media flyout
  on Windows, and the desktop's media controls on Linux.
- On Windows and Linux the system's media buttons, and the keyboard's
  media keys, control Aurelia through the system, as on macOS.
- The system and the server are told what plays while the window is
  hidden or the screen is locked.
- install.sh works in the sh of macOS.

## 0.1.1

- Less memory: pictures that scroll away are not loaded, and a fast scroll
  through the library no longer takes hundreds of megabytes for a moment.
- Pictures load over connections that stay open, which matters on servers
  without HTTP/2.
- Songs and albums are put in order when the library is read, so their
  pages open at once the first time too.
- Aurelia no longer stops when it cannot make its cache directory.

## 0.1.0

The first version of Aurelia.

- The library of a Jellyfin server, kept on disk so that every page and
  every search shows at once: albums, artists, songs, playlists and
  favorites.
- Playback of FLAC and MP3 without gaps, a queue, shuffle and repeat,
  lyrics, and what plays told to the server and to macOS.
- Songs, albums and playlists downloaded to play offline.
- Themes: fourteen built in, an editor, and themes of Visual Studio Code,
  Firefox and Chrome imported.
- Updates from the releases on GitHub.

# Changes

## 0.8.2

- Jams: pausing pauses guests at the very place the host did, and
  playing again starts them where the host is, not as late as the word
  of it took to reach them. Guests keep within a tenth of a second of
  the host, not a third, and the host tells of a change within a tenth
  of a second.
- Jams over slow links, as Tailscale's relays: a guest downloads the
  host's songs only half a minute ahead of where it plays, so the
  host's word of a pause or a skip no longer waits behind a whole song
  coming down the same link. Its clock is set from more of the host's
  answers, the quickest of a minute of them.
- A song that was downloading, played again from another place or
  skipped to, goes on downloading where it was instead of starting over.
- The Jam's dialog scrolls under its title in a window too low for all
  of it, as with many people or devices, instead of running out of the
  window.
- Jams: the Jam's dialog shows who did what, the latest first: who
  paused or played on, skipped, moved in a song, added, removed or moved
  songs, and who joined, left or lost the connection. What someone else
  does to what plays shows as a notification too.
- Jams: a guest's pause from outside Aurelia (the keyboard's media keys,
  headphones taken off, the system's media controls, the computer going
  to sleep) pauses only that guest, not the Jam; play or Catch up takes
  it back to where the host is, and a computer paused as it went to
  sleep catches up as it wakes. On Linux, Aurelia keeps the computer
  from suspending on its own while music plays.

## 0.8.1

- Jams: guests stay in step with the host over slow links, as over
  Tailscale's relays. They were heard up to seconds behind, as a song
  started late by the time it took to come from the host; now that time
  is learned and allowed for, and guests are moved back in step when
  more than 0.3 s off, not 2.5 s.
- Jams over Tailscale: an invite from someone your computer is shared
  with, which Tailscale lets reach your computer but not the other way,
  offers to host a Jam they can join, instead of failing to join theirs.
  Joining a Jam that can't be reached over Tailscale says why.
- Devices of people your computer is shared with no longer show as
  "device-of-shared-to-user".

## 0.8.0

- Jams, in beta (Settings ▸ Beta): listening together with people on
  the same network, as Spotify's Jams. One person hosts and the others
  join with a code, or find the Jam nearby and ask to come in; everyone
  hears the same queue on their own computer, in step with the host, and
  with their own volume, equalizer, spatial audio, crossfade and vinyl.
  Everyone can add songs; the host chooses whether guests also play,
  pause, skip and reorder. Songs a guest's server lacks play from the
  host, who gets those of a guest's server from that guest, so friends
  on another server, or with none ("Join a friend's Jam" on the sign-in
  page), hear everything. A dropped connection comes back by itself, and
  leaving brings your own queue back.
- Jams over Tailscale: with Tailscale running, the Jam's dialog lists the
  devices of your tailnet that have Aurelia open, with whose each is,
  to invite (even with their Jams off: "Turn on and join") or join. Who
  sent an invite or asks to join is what Tailscale says, not what they
  claim; your own devices join without the code. Aurelia listens for
  this on its Tailscale addresses only, which Settings ▸ Beta can turn
  off.
- A proxy in the settings no longer carries traffic to Tailscale
  addresses (100.64.0.0/10, fd7a:115c:a1e0::/48) or MagicDNS names.

## 0.7.0

- Vinyl: music as if played from its album's record, on a turntable
  (Settings ▸ Playback, or Vinyl in the "…" of the player's bar). Each
  album is laid out on records as a pressing plant would: LPs of sides of
  24 minutes at most, a 45 for a few songs, 78s for albums before 1950.
  The sound is worked out from the groove itself: the music cut into it,
  worn by the stylus that played it, dust and scratches in it, traced by a
  stylus of today. So the hiss and crackle are heard where a record's are,
  in quiet passages and between songs; scratches tick once a revolution;
  worn records lose treble and are gritty on loud highs, the more so
  towards the end of a side. The needle drops into the lead-in as a side
  begins, and the run-out and the locked groove play before the other
  side; going from one album to another, the needle is lifted and dropped
  again, unless songs crossfade.
- The record's condition is what the album's year has made it, or a
  collector's grade from Mint to Fair, made cleaner or more worn with a
  slider. A record is the same each time it plays, its scratches where
  they were.

## 0.6.0

- The icon in the menu bar or the tray, as you like it (Settings ▸
  Desktop, with a picture of how it looks): Aurelia's icon in the menu
  bar's own color, the theme's, Sea glass, Lavender, Gold, Rose, Sky,
  white or black; or the cover of the song playing. Beside it, the song,
  or the song and the artist: in macOS's menu bar, and in the panels of
  GNOME (with its AppIndicator extension), Ubuntu, Xfce, Cinnamon, MATE
  and Budgie. Windows and KDE Plasma show the song when you point at the
  icon. The theme's color follows the theme as it changes.
- The queue kept for the next run is always the latest: two saves close
  together could be written in the wrong order, and an older queue come
  back.

## 0.5.1

- V types a V again in every field, the sign-in's too: on Linux and
  Windows the menu bar took it, as the key of Now Playing, so a server,
  name or password with a v could not be typed. V still opens Now
  Playing when nothing is being typed in. A copy that cannot sign in
  offers this update as it opens, on the sign-in page.

## 0.5.0

- Spatial audio for headphones: music as if from speakers around you in a
  room, rather than from inside your head, in the manner of the spatial
  audio of AirPods. Automatic plays it on AirPods and other headphones
  alone, and follows them as they are put on and taken off; Always On
  plays it on anything. In Settings ▸ Playback and the player's "…".
- Two fingers swiped across a trackpad go back (to the right) and forward
  (to the left), with an arrow at the side of the page, on macOS,
  Windows' precision touchpads and Linux; on a Mac as its trackpad
  setting says, with two fingers or three. On the record, a swipe back
  gives the app back.
- The window's buttons are the system's own: Windows 11's caption
  buttons, with their snap layouts, in place of Aurelia's; on Linux the
  desktop's title buttons over the window, or its title bar where the
  compositor draws one. They, the menus and the dialogs are light or dark
  as the theme is.
- On Windows and Linux, Aurelia's name at the top of the sidebar opens the
  menu bar's menus, as Alt and F10 do; questions before something cannot
  be undone (deleting a playlist, removing all downloads) are the
  system's own dialogs.

## 0.4.1

- A new look: sea glass, the water of the moon jellyfish Aurelia is named
  after, in place of the lavender, in the app and its icon. The lavender
  stays, as Lavender and Lavender Light, and a theme you chose stays yours.
- "/" and Cmd (Ctrl)+K put the keyboard in the search field at once; Up
  from the top result goes back to it.
- Downloaded songs show one quiet check, in their rows and on the album;
  the color shows only while a download goes on, and one that failed says
  so and tries again when clicked.
- A calmer player bar: Now Playing (the record and the lyrics), the queue,
  a "…" with the sleep timer and the device to play on, and the volume.
  Lyrics have a quote of their own, and Browse a colored icon.
- In a small window an album's buttons stay on one line, the song's name
  shows whole, and Shuffle and Repeat are in the player bar's "…".

## 0.4.0

- Play plays in order: an album, a playlist or a list plays as it is,
  and only Shuffle shuffles. A song chosen in search plays, then music
  like it, not the other results.
- A queue like Spotify's: songs added with Play Next and Add to Queue
  play right after the song playing, ahead of the rest of the album,
  under their own heading in the queue. Shuffling on and off keeps them,
  and keeps songs moved by hand; the order comes back after a restart,
  and Previous still goes back once shuffled. Clear can be undone.
- The music goes on past a song that will not load, rather than
  stopping; Play after the music stopped plays on from the song after.
  Next in a crossfade goes on with the song coming in, rather than
  starting it again. A seek back is no longer scrobbled twice.
- Choose several songs with a click, Shift and Cmd (Ctrl), and play,
  queue, like, download or add them to a playlist at once; drag them to
  a playlist in the sidebar or to the queue; drag songs of a playlist to
  another place in it.
- Search leads with its top result beside the best songs; Enter plays
  it, and Down takes the keyboard into the results.
- Playlists: others' mixes are no longer taken for yours; playlists of
  one name say how they differ; a menu on playlists in the sidebar and on
  tiles; Add to Playlist reaches every playlist, with a search, and asks
  before adding a song twice; removing a song can be undone.
- The playing song's row pauses under the pointer rather than starting
  again. Favorites has Play and a choice of order; the heart says what it
  did. Remove all downloads asks first. Home's Show all keeps your order
  of albums, and pages keep their place longer.

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

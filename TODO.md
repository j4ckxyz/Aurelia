# To do

What is wanted for Aurelia, and what of it is done.

## How this list is kept

Claude keeps this file up to date, every time, without being asked:

- An item moves to [Done](#done) only once it is built **and** verified
  working as it was asked for: run in the app, not only compiled or
  tested in parts. Until then it stays here.
- When it moves, it takes the date, the version or commit it went into,
  and a line on how it was verified.
- An item done in part stays here, with its finished parts ticked.
- Anything new that is asked for is added here first.
- The file is updated in the same change as the work, not later.

Every item holds to what Aurelia already promises: about 100 MB of
memory, 120 frames a second, no webview, pages that show at once, and
little kept on disk.

## To do

### Quick animations across the app

- [ ] Short, light animations so that the app does not feel bland, none
      of which makes anything wait: a page still shows at once.
  - [ ] Pressing and hovering: buttons, rows, cards and covers answer
        with a small movement, not only a change of color. *(the play
        button of a tile now rises in under the pointer; colors already
        fade; no press movement yet)*
  - [x] A page changing has a hint of movement: it comes in 8 points up
        over 150 ms, the page before goes at once. *(measured: no slow
        frame during navigation with it on that was not there with it off)*
  - [ ] The queue, the sidebar and pictures slide, collapse and fade in.
        *(written, and the tests and frame times are clean; not yet seen
        running, as the screen was locked)*
  - [x] The bars of the song playing in a list move with the music, rest
        when it is paused, and are painted twenty times a second only
        while it plays. *(seen in the app: 127 MB and about 5% CPU while
        playing with them on screen)*
  - [ ] The play button turning into pause, the heart when a song is
        liked.
  - [ ] Lists: a song added to, taken from or dragged in the queue moves
        to its place.
  - [x] A setting to turn animations down, following the system's
        "Reduce motion": Settings ▸ Appearance ▸ Animations. *(the desktop's
        wish is read by MyGo for macOS, Windows and GNOME; here it reads
        "off", and the setting's own On and Reduced were tried in the
        app)*
  - [ ] Measured: still 120 frames a second while they run, and no more
        memory at rest. *(no slow frames added; the frame rate itself not
        yet measured)*

### More audio settings

- [ ] Choose the output device, kept between runs as the default.
  - [x] A list of the computer's outputs in Settings ▸ Playback, with
        "System default" first. *(macOS verified)*
  - [x] The choice is kept; if that device is gone when Aurelia opens,
        it plays on the system's default and goes back when it returns.
        *(macOS verified; the return is checked every five seconds)*
  - [x] Changing the device while a song plays does not stop the song.
        *(macOS verified, to a virtual device and back)*
  - [ ] On macOS, Windows and Linux. macOS done; Linux (PulseAudio and
        ALSA) is tested in CI against two PulseAudio sinks; Windows
        (WASAPI) is written and its listing is run in CI, but no Windows
        machine with a sound card has played through it yet.
- [ ] *Suggested, to choose from:*
  - [ ] Crossfade between songs, with its length, and not within an
        album played in order.
  - [ ] Normalization by album as well as by song, so that an album
        keeps its own quiet and loud songs.
  - [ ] The output's sample rate following the song's, where the device
        allows, in place of resampling.

### Ideas from other players

Taken from Spotify and from other clients of Jellyfin and Navidrome
(Feishin, Finamp, Supersonic, Plexamp). These are Claude's suggestions,
not asked for one by one: strike out what is not wanted.

Playing


Library

- [ ] Select several songs (shift and ⌘ click) to queue, download, like
      or add to a playlist together.
- [ ] An artist's most played songs and similar artists on their page.
- [ ] Pin albums and playlists to the sidebar.

Desktop

- [ ] Scrobbling to Last.fm. *(ListenBrainz is done; Last.fm asks each
      user for an API key and secret of their own, which is more to set up
      than it is worth until asked for again)*
- [ ] Discord shows what plays.
- [ ] A visualizer on the record.
- [ ] A year in review: what you played most.

Formats

- [ ] Opus, AAC and Ogg Vorbis decoded here, in place of asking the
      server to convert them.

Other servers and services are under [Later](#later-one-player-for-everything).

## Later: one player for everything

Asked for on 2026-10-09 as where Aurelia is to go in time. It is not to
be started before the list above unless asked.

### What is wanted

- [ ] Aurelia plays from any kind of source, chosen when it is set up:
      Jellyfin, music files on the computer, Navidrome, Spotify (through
      its API, with Premium), or anything else.
- [ ] Each of them complete, not a lesser copy of the Jellyfin one:
      library, search, playlists, favorites, lyrics, downloads and the
      rest, as far as the service allows.
- [ ] Several services at once, in one of two ways, as the user
      chooses:
  - [ ] **Together:** all of them mixed into one library, one search and
        one queue.
  - [ ] **Apart:** each service or account in a profile of its own, with
        nothing crossing between them.

### How it might be built

A sketch, to be decided properly when the work starts.

**1. A source, in place of Jellyfin.** Today the pages, the player and
the downloads speak to `internal/jellyfin` directly, and
`internal/library` holds songs under Jellyfin's IDs. An interface goes
between them, which each service implements:

- sign in, and say who is signed in;
- the library: albums, artists, songs, playlists, and what changed
  since last time;
- a song's sound, as a stream the audio engine can read and seek;
- pictures and lyrics;
- favorites, plays reported, playlists changed;
- what it cannot do, so that the app hides what does not apply (no
  downloads, no lyrics, no other devices, sound not given to the app).

Jellyfin is moved behind it first, with nothing changing on screen: that
step is verified alone before any second service is written.

**2. IDs that say where a thing is from.** Every song, album, artist and
playlist carries its source with its ID, so that caches, downloads, the
saved queue and the albums hidden from Home never confuse two services.
What is on disk from before (settings, queue, downloads) is carried over
when the app updates.

**3. Accounts, not one session.** `Settings.Session` becomes a list of
accounts, each with its own index of the library on disk (the index is
already one file per session).

**4. The sources.**

- **Navidrome**, through the Subsonic API with the OpenSubsonic
  additions, which also gives Gonic, Airsonic, Ampache and the like. It
  is close to Jellyfin in shape, so it is the one to write second, to
  prove the interface.
- **Files on the computer.** Folders the user names are read for tags
  (ID3, Vorbis comments, MP4), covers inside the files or beside them,
  and `.lrc` lyrics; the folders are watched for changes. There is no
  server, so favorites, play counts and playlists are kept by Aurelia
  itself (playlists as `.m3u`, so that other players read them).
  Nothing converts for it either: it needs AAC, ALAC, Opus and Vorbis
  decoded here, the item under Formats above.
- **Spotify**, with its Web API and the user's own sign-in: library,
  playlists, search, and what plays. The API does not give the sound to
  another app. So the way within Spotify's terms is to drive Spotify
  Connect: Spotify's own app, or a speaker, makes the sound, and Aurelia
  shows and controls it, as "Play on another device" already does for
  Jellyfin. The equalizer, normalization, gapless playback, the choice
  of output and downloads would then not apply to Spotify's songs, and
  the app has to say so. Playing them inside Aurelia would take an
  unofficial library, against those terms: not planned. Spotify also
  limits apps it has not approved (few users, some of the API closed),
  so each user may need a key of their own: to be checked when the time
  comes, as it changes.
- **Anything else**, as candidates: Emby (nearly Jellyfin's API), Plex,
  Audiobookshelf for podcasts and audiobooks, internet radio, a folder
  on a network share.

**5. Together, or apart.** A profile is a set of sources with its own
library, queue, Home and history. Apart is one profile per service, and
a switch between them in the sidebar. Together is one profile with
several sources, which asks for more:

- The same album on two services shows once. Copies are matched by
  MusicBrainz IDs where the tags have them, and otherwise by artist,
  album, track number and length; every copy is kept, and one is played
  by preference: a file on the computer, then a download, then a
  lossless server, then the rest. A small mark says where a song is
  from.
- Search and the queue run over all of them.
- A playlist belongs to the service it came from. One that mixes
  services is Aurelia's own, kept on the computer.
- Favorites and plays are written back to the service the copy came
  from.
- What will not mix cleanly: a Spotify song between two others in the
  queue is a handover between Aurelia's sound and Spotify's, with a gap.

**6. Setting up.** The first page offers the services as cards. More
are added later in Settings, and each one asks whether it joins the
library or gets a profile of its own.

**7. In this order**, each step verified before the next: Jellyfin
behind the interface; Navidrome; files on the computer; profiles and
the mixed library; Spotify; the others.

**8. To watch.** Memory and the speed of search with several libraries
at once, measured against the promises at the top. The README's "the
music side of Jellyfin only" changes with it, and so do the screenshots.

## Done

Each entry is written as:

```
- [x] **What was asked** (2026-10-09, 0.3.0, `abc1234`): how it was
      verified.
```

- [x] **Sleep timer** (2026-10-09): the moon of the player's bar: stop in
      5, 15, 30, 45 minutes, 1 or 2 hours (the sound fades out over two
      and a half seconds, then pauses), or at the end of the song, or of
      the album. Verified in the app with the real sound engine: a timer
      of 5 s paused playback at 5 s; "end of the song" set 4 s before the
      end left the song stopped where it ended, while the same run without
      it went on to the next song; tests cover the song, the album (and
      repeat-one not getting round it), the time left, and cancelling.
- [x] **Song details, and how a song is played** (2026-10-09): "Song
      Info…" in the menus of songs: artist, album, track, year, genres,
      length, plays, dates, and the file as the server reads it (format,
      sample rate, bit depth, channels, bit rate, size, path); for the
      song playing, "Playing as": as the file is, or converted by the
      server (to MP3 under the streaming limit, to FLAC for formats it
      cannot decode here), kept as a download, and resampled for the
      output. Verified: tests of the wording for each case and of the
      dialog against a made-up server; read from the real server for two
      songs ("FLAC, 44.1 kHz, 16-bit, stereo, 866 kbps", 26 MB).
- [x] **Genres, and browsing by decade** (2026-10-09): "Browse" in the
      sidebar: every genre of the library (as one if written in two
      cases) and every decade as a tile, each opening its albums, with
      Shuffle. A genre like "R&B/Soul" is found by a slug of its name.
      Verified by tests (grouping, order, slugs, clicking through, a page
      that is not there) and a rendering of the page.
- [x] **What was played, in order, and what was never played** (2026-10-09):
      Songs ▸ sort by Recently Played, Most Played and Never Played (which
      lists only the songs not played). Verified by tests, including a
      song moving to the head as it is played.
- [x] **Song radio, and going on with similar songs** (2026-10-09): "Start
      Radio" in the menus of songs, albums and artists (and on an artist's
      page): fifty songs the server picks as being like it, the song first.
      Settings ▸ Playback ▸ "Keep playing similar songs" (off unless you
      turn it on): when the last song of the queue plays, songs like it are
      added after it, once, without those already in the queue, and not
      past a sleep timer. Verified: the real server's instant mixes of a
      song and an album were read and mapped into the library (12 songs
      each); tests with a made-up server check the request, the seed
      first, songs the library lacks being left out, asking only once, and
      the sleep timer still stopping the music.
- [x] **Playlists made and changed here** (2026-10-09): "Add to Playlist"
      (a submenu, with "New Playlist…") in the menus of songs and albums; a
      "New playlist" button on the Playlists page (also when there are
      none); on a playlist's page, Rename… and Delete Playlist… (asks
      first; the songs stay) in its menu, and Move Up, Move Down and
      Remove from This Playlist in each song's menu; "Save" in the queue
      keeps the queue as a playlist. Verified by tests that drive the real
      menus and dialogs against a server that records every request: the
      method, the path, the query and the body of each. **Not tried on the
      real server**, which is only read here: the Jellyfin calls are those
      of its API as documented (the rename call needs Jellyfin 10.9 or
      later).
- [x] **Lyrics from LRCLIB when the server has none** (2026-10-09):
      Settings ▸ Playback ▸ "Look up missing lyrics" (off unless you turn it
      on, as it tells LRCLIB the artist, title, album and length of the song):
      timed lyrics where it has them, else plain; a song with a .lrc on the
      server keeps the server's. Verified: the parser on LRC with tags,
      several times on one line and untimed text; the request and each kind
      of answer against a made-up server; the fallback flow; and one call to
      the real lrclib.net, whose answer for a well-known song has the shape
      the parser reads.
- [x] **An icon in the menu bar or the tray, and a notification as the song
      changes** (2026-10-09): Settings ▸ Desktop. The icon (the jellyfish,
      black for the menu bar to tint, in the accent elsewhere) opens a menu
      with the song, Play or Pause, Next, Previous, Show Aurelia and Quit; on
      Windows and Linux closing the window then leaves the music playing, as
      on macOS already. Verified on macOS in the running app: the status
      item exists, its menu names the song and changes with Next and Pause,
      it goes when turned off, and clicking Next in it moved playback to the
      next song. **Not verified:** the tray on Windows and Linux (it is
      MyGo's, built for both and vetted); the notification, which macOS only
      shows for a bundled app (the dev build says "not supported on this
      platform" and carries on).
- [x] **Scrobbling to ListenBrainz** (2026-10-09): Settings ▸ Scrobbling:
      your user token (checked, and shown as "Signed in as …"), and a server
      address for one of your own. The song playing is sent as it begins, and
      once more when half of it (or four minutes) has played, never for songs
      under thirty seconds or ones skipped early, and again for a repeat;
      what could not be sent is kept, up to a hundred, and tried each minute.
      Verified by tests of the counting (half, four minutes, skipping, short
      songs, pauses, stalls, repeats) and of the requests against a made-up
      ListenBrainz (the body, the token, a batch after a failure). **Not
      tried against listenbrainz.org**, for want of an account.
- [x] **An equalizer** (2026-10-09): ten bands from 31 Hz to 16 kHz and a
      preamp, 14 presets and presets of your own (saved, named, deleted),
      one switch, kept between runs, with a curve drawn over the sliders.
      It runs where the device takes the sound, so a slider is heard at
      once and not after the second of sound decoded ahead; the boosts
      turn the sound down by themselves, with a limiter behind, so that
      nothing clips. Verified: unit tests measure the gain of every band
      at its center (±0.15 dB), that other bands are left alone, that a
      flat or off equalizer leaves every sample as it was, that the full
      boost does not pass full scale, and that moving a band makes no
      click; in the app, a song played while the equalizer was turned on,
      changed and off, kept its place; settings survive a restart. Cost:
      about 22 µs per 1,024 frames, and 0.1% of the CPU while playing.
- [x] **Share a song or an album as a picture** (2026-10-09): "Share as
      a Picture…" in the right-click menu of a song, an album and the song
      in the player's bar. A dialog with a live preview; shapes Square
      (1080 × 1080), Story (1080 × 1920) and Wide (1200 × 630); white,
      black, a color of your own (swatches or hex) or a blend of the
      cover's own colors, always dark enough for white text; text that is
      dark on light backgrounds; an optional "Aurelia" mark. Copy puts the
      PNG on the clipboard, Save… writes it. Verified: the pictures were
      rendered for every shape and background and looked at; tests check
      the size, the background at the four corners, the cover's pixels,
      the text colors and a pale cover's blend; right-clicking an album
      and a song and choosing the item opens the dialog (test through the
      app's own menus); the clipboard was read back from macOS and holds a
      1080 × 1080 PNG of the right background. Not tried: the native Save
      panel (its file writing is the same PNG that was inspected).
- [x] **Mono, and balance between left and right** (2026-10-09), in
      Settings ▸ Playback. Verified by unit tests of the samples
      (mono of 1 and 0 is 0.5 in both; balance to the right silences the
      left; half to the left leaves the right at half) and by the rows
      showing and saving in the app.

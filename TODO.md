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

### Share a song or an album as a picture

- [ ] **Share** in the right-click menu of a song and of an album, making
      a picture to post on social networks.
  - [ ] The picture has the cover, the title and the artist's name, and
        looks clean: generous margins, good type, nothing else.
  - [ ] A choice of background: white, black, a color of your own, or a
        blend of the cover's own colors (as the record already draws).
  - [ ] A preview before it is made, with the background changing as it
        is chosen.
  - [ ] Copy the picture to the clipboard, and save it as a PNG.
  - [ ] *Suggested:* the shapes networks want: square (1080 × 1080),
        tall for stories (1080 × 1920) and wide for links (1200 × 630).
  - [ ] *Suggested:* the same from the song playing, in the player's bar
        and on the record.

### Quick animations across the app

- [ ] Short, light animations so that the app does not feel bland, none
      of which makes anything wait: a page still shows at once.
  - [ ] Pressing and hovering: buttons, rows, cards and covers answer
        with a small movement, not only a change of color.
  - [ ] Pages and panels: the queue, the lyrics, the sidebar and menus
        slide or fade in; a page changing has a hint of movement.
  - [ ] Playback: the play button turning into pause, the heart when a
        song is liked, the cover changing with the song, the bars of the
        song playing in a list.
  - [ ] Lists: a song added to, taken from or dragged in the queue moves
        to its place.
  - [ ] A setting to turn animations down, following the system's
        "Reduce motion".
  - [ ] Measured: still 120 frames a second while they run, and no more
        memory at rest.

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
  - [ ] What is playing, told plainly: format, sample rate, bit depth,
        and whether the server converted it.

### Ideas from other players

Taken from Spotify and from other clients of Jellyfin and Navidrome
(Feishin, Finamp, Supersonic, Plexamp). These are Claude's suggestions,
not asked for one by one: strike out what is not wanted.

Playing

- [ ] Sleep timer: stop after a time, or at the end of the song or album.
- [ ] Song radio: "Start Radio" on a song, album or artist, from the
      server's instant mix.
- [ ] Go on with similar songs when the queue ends.
- [ ] Save the queue as a playlist, and clear it.

Library

- [ ] Playlists made and changed here: new, rename, delete, "Add to
      Playlist" in the menus, songs dragged into another order.
- [ ] Select several songs (shift and ⌘ click) to queue, download, like
      or add to a playlist together.
- [ ] Genres, and browsing by year or decade.
- [ ] An artist's most played songs and similar artists on their page.
- [ ] A page of what was played, in order, and "most played" and "never
      played" lists.
- [ ] Pin albums and playlists to the sidebar.
- [ ] Song details: file format, bit rate, size, where it is on the
      server.
- [ ] Lyrics fetched from LRCLIB when the server has none.

Desktop

- [ ] An icon in the menu bar or tray with the song and its buttons, and
      the app kept running with its window closed.
- [ ] A notification when the song changes.
- [ ] Scrobbling to Last.fm and ListenBrainz.
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
- [x] **Mono, and balance between left and right** (2026-10-09), in
      Settings ▸ Playback. Verified by unit tests of the samples
      (mono of 1 and 0 is 0.5 in both; balance to the right silences the
      left; half to the left leaves the right at half) and by the rows
      showing and saving in the app.

# Changes

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

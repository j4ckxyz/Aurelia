//go:build !darwin

package main

// nowPlaying is macOS's: elsewhere the keyboard's media keys come as
// global shortcuts (mediaKeys).
type nowPlaying struct{}

func (n *nowPlaying) init(a *App) {}

func (n *nowPlaying) set(title, artist, album string, duration, position float64, paused bool) {}

func (n *nowPlaying) describe() string { return "" }

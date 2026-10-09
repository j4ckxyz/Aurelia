//go:build !darwin && !linux && !windows

package main

import "time"

// nowPlaying tells the system what plays, where Aurelia knows how: on
// macOS, Linux and Windows.
type nowPlaying struct{}

func (n *nowPlaying) init(a *App) {}

func (n *nowPlaying) set(p playing) {}

func (n *nowPlaying) progress(position time.Duration) {}

func (n *nowPlaying) describe() string { return "" }

func (n *nowPlaying) handlesKeys() bool { return false }

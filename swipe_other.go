//go:build !darwin

package main

// swipeNavigates reports whether a swipe of two fingers across a
// trackpad goes back and forward: on Windows and Linux it does, as in
// their browsers.
func swipeNavigates() bool { return true }

// listenForSwipes takes the system's own swipes of the window, where it
// has some: on Windows and Linux they come as scrolls.
func (a *App) listenForSwipes() {}

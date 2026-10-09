//go:build !darwin && !linux && !windows

package main

func systemReducesMotion() bool { return false }

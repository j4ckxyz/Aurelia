package main

import "os/exec"

// systemReducesMotion reads the desktop's setting for animations, which
// GNOME keeps and others that follow it.
func systemReducesMotion() bool {
	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "enable-animations").Output()
	return err == nil && quickText(out) == "false"
}

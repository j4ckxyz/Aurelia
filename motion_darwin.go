package main

import "os/exec"

// systemReducesMotion reads System Settings ▸ Accessibility ▸ Display ▸
// Reduce motion.
func systemReducesMotion() bool {
	out, err := exec.Command("defaults", "read", "com.apple.Accessibility", "ReduceMotionEnabled").Output()
	return err == nil && quickText(out) == "1"
}

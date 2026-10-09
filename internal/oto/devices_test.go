package oto

import "testing"

// Devices lists what the machine has; a build machine may have no sound
// card, which is not a failure.
func TestDevices(t *testing.T) {
	devs, err := Devices()
	if err != nil {
		t.Logf("no list of devices here: %v", err)
		return
	}
	defaults := 0
	for _, d := range devs {
		t.Logf("%q %q default=%v", d.ID, d.Name, d.Default)
		if d.ID == "" || d.Name == "" {
			t.Errorf("a device without an ID or a name: %+v", d)
		}
		if d.Default {
			defaults++
		}
	}
	if defaults > 1 {
		t.Errorf("%d devices are the default", defaults)
	}
}

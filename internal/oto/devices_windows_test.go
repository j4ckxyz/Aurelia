package oto

import "testing"

// A build machine has often no sound card: the list is then empty, which
// is not a failure. What is listed must have a name and an ID that
// WASAPI opens by.
func TestWindowsDevices(t *testing.T) {
	devs, err := Devices()
	if err != nil {
		t.Fatalf("listing the outputs: %v", err)
	}
	defaults := 0
	for _, d := range devs {
		t.Logf("%q %q default=%v", d.ID, d.Name, d.Default)
		if d.ID == "" || d.Name == "" {
			t.Errorf("an output without an ID or a name: %+v", d)
		}
		if d.Default {
			defaults++
		}
	}
	if defaults > 1 {
		t.Errorf("%d outputs are the default", defaults)
	}
	t.Logf("%d outputs", len(devs))
}

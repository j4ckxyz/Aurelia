package jellyfin

import "testing"

func TestAtLeast(t *testing.T) {
	for v, want := range map[string]bool{
		"10.9.0": true, "10.10.7": true, "10.11.0-rc2": true, "11.0.0": true,
		"10.8.13": false, "10.8": false, "9.9.9": false, "": false, "unstable": false,
	} {
		if got := AtLeast(v, 10, 9); got != want {
			t.Errorf("AtLeast(%q, 10, 9) = %v", v, got)
		}
	}
}

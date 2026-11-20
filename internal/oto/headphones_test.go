package oto

import "testing"

func TestNamedHeadphones(t *testing.T) {
	for name, want := range map[string]bool{
		"Robin's AirPods Pro":                      true,
		"Headphones (WH-1000XM5)":                  true,
		"Headset (Jabra Evolve2)":                  true,
		"Galaxy Buds2 Pro":                         true,
		"Beats Studio Pro":                         true,
		"MacBook Pro Speakers":                     false,
		"Speakers (Realtek(R) Audio)":              false,
		"LG HDR 4K (NVIDIA High Definition Audio)": false,
		"": false,
	} {
		if got := namedHeadphones(name); got != want {
			t.Errorf("%q: headphones %v, want %v", name, got, want)
		}
	}
}

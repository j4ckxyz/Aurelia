package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// The tests' Jams are reached on this computer, not through the
	// public relay.
	publicRelay = ""
	os.Exit(m.Run())
}

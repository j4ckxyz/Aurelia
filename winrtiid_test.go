package main

import "testing"

// The ID computed for the handler of the media buttons is the one
// Windows' headers give it, and that of another handler whose ID is
// well known is right too: so is the way they are computed.
func TestTypedEventHandlerIID(t *testing.T) {
	if got := typedEventHandlerIID(smtcClass, smtcIID, smtcArgsClass, smtcArgsIID); got != smtcHandlerIID {
		t.Errorf("the handler of the media buttons: %s, want %s", got, smtcHandlerIID)
	}
}

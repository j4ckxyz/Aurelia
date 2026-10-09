package main

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
)

// typedEventHandlerIID returns the interface ID of Windows'
// TypedEventHandler<sender, args>, as "{xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx}".
// Windows does not list the IDs of such interfaces: each is the SHA-1 of
// its description, as the Windows Runtime's type system defines, made
// into a UUID. sender and args are runtime classes, by their names and
// the IDs of their default interfaces.
//
// It is here, and not with the Windows code that uses it, so that every
// platform's tests check it.
func typedEventHandlerIID(senderClass, senderIID, argsClass, argsIID string) string {
	const typedEventHandler = "9de1c534-6ae1-11e0-84e1-18a905bcc53f"
	signature := fmt.Sprintf("pinterface({%s};rc(%s;{%s});rc(%s;{%s}))", typedEventHandler, senderClass, senderIID, argsClass, argsIID)
	// The namespace of the IDs of parameterized interfaces.
	namespace := []byte{0x11, 0xf4, 0x7a, 0xd5, 0x7b, 0x73, 0x42, 0xc0, 0xab, 0xae, 0x87, 0x8b, 0x1e, 0x16, 0xad, 0xee}
	h := sha1.Sum(append(namespace, signature...))
	h[6] = h[6]&0x0f | 0x50 // a UUID of version 5
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("{%08x-%04x-%04x-%04x-%012x}", binary.BigEndian.Uint32(h[0:4]), binary.BigEndian.Uint16(h[4:6]),
		binary.BigEndian.Uint16(h[6:8]), binary.BigEndian.Uint16(h[8:10]), h[10:16])
}

// The classes and interfaces of Windows' media controls that the handler
// of their buttons is made of.
const (
	smtcClass      = "Windows.Media.SystemMediaTransportControls"
	smtcIID        = "99fa3ff4-1742-42a6-902e-087d41f965ec"
	smtcArgsClass  = "Windows.Media.SystemMediaTransportControlsButtonPressedEventArgs"
	smtcArgsIID    = "b7f47116-a56f-4dc8-9e11-92031f4a87c2"
	smtcHandlerIID = "{0557e996-7b23-5bae-aa81-ea0d671143a4}" // as Windows' headers have it
)

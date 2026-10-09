package main

import (
	"syscall"
	"unsafe"
)

// systemReducesMotion reads Settings ▸ Accessibility ▸ Visual effects ▸
// Animation effects, which Windows keeps as the "client area animation".
func systemReducesMotion() bool {
	const spiGetClientAreaAnimation = 0x1042
	proc := syscall.NewLazyDLL("user32.dll").NewProc("SystemParametersInfoW")
	var on int32 = 1
	r, _, _ := proc.Call(spiGetClientAreaAnimation, 0, uintptr(unsafe.Pointer(&on)), 0)
	return r != 0 && on == 0
}

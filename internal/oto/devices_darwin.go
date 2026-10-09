//go:build darwin && !ios

package oto

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// What CoreAudio is asked, as four characters.
const (
	kAudioObjectSystemObject                  = 1
	kAudioHardwarePropertyDevices             = 0x64657623 // 'dev#'
	kAudioHardwarePropertyDefaultOutputDevice = 0x644f7574 // 'dOut'
	kAudioDevicePropertyDeviceUID             = 0x75696420 // 'uid '
	kAudioObjectPropertyName                  = 0x6c6e616d // 'lnam'
	kAudioDevicePropertyStreamConfiguration   = 0x736c6179 // 'slay'
	kAudioObjectPropertyScopeGlobal           = 0x676c6f62 // 'glob'
	kAudioObjectPropertyScopeOutput           = 0x6f757470 // 'outp'
	kAudioQueueProperty_CurrentDevice         = 0x61716364 // 'aqcd'
	kCFStringEncodingUTF8                     = 0x08000100
)

type audioObjectPropertyAddress struct {
	selector, scope, element uint32
}

var (
	coreAudioOnce sync.Once
	coreAudioErr  error

	_AudioObjectGetPropertyDataSize func(object uint32, address *audioObjectPropertyAddress, qualifierSize uint32, qualifier unsafe.Pointer, size *uint32) int32
	_AudioObjectGetPropertyData     func(object uint32, address *audioObjectPropertyAddress, qualifierSize uint32, qualifier unsafe.Pointer, size *uint32, data unsafe.Pointer) int32
	_AudioQueueSetProperty          func(queue _AudioQueueRef, property uint32, data unsafe.Pointer, size uint32) int32
	_CFStringCreateWithCString      func(allocator uintptr, s string, encoding uint32) uintptr
	_CFStringGetCString             func(s uintptr, buffer *byte, size int, encoding uint32) bool
	_CFRelease                      func(object uintptr)
)

// loadCoreAudio binds what listing and choosing devices need.
func loadCoreAudio() error {
	coreAudioOnce.Do(func() {
		hw, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			coreAudioErr = err
			return
		}
		tb, err := purego.Dlopen("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			coreAudioErr = err
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			coreAudioErr = err
			return
		}
		purego.RegisterLibFunc(&_AudioObjectGetPropertyDataSize, hw, "AudioObjectGetPropertyDataSize")
		purego.RegisterLibFunc(&_AudioObjectGetPropertyData, hw, "AudioObjectGetPropertyData")
		purego.RegisterLibFunc(&_AudioQueueSetProperty, tb, "AudioQueueSetProperty")
		purego.RegisterLibFunc(&_CFStringCreateWithCString, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&_CFStringGetCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&_CFRelease, cf, "CFRelease")
	})
	return coreAudioErr
}

// cfString reads a CFString into Go.
func cfString(s uintptr) string {
	if s == 0 {
		return ""
	}
	buf := make([]byte, 512)
	if !_CFStringGetCString(s, &buf[0], len(buf), kCFStringEncodingUTF8) {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// stringProperty reads a property of a device that is a CFString.
func stringProperty(device uint32, selector uint32) string {
	addr := audioObjectPropertyAddress{selector, kAudioObjectPropertyScopeGlobal, 0}
	var s uintptr
	size := uint32(unsafe.Sizeof(s))
	if _AudioObjectGetPropertyData(device, &addr, 0, nil, &size, unsafe.Pointer(&s)) != 0 || s == 0 {
		return ""
	}
	defer _CFRelease(s)
	return cfString(s)
}

// hasOutput reports whether the device has channels to play on.
func hasOutput(device uint32) bool {
	addr := audioObjectPropertyAddress{kAudioDevicePropertyStreamConfiguration, kAudioObjectPropertyScopeOutput, 0}
	var size uint32
	if _AudioObjectGetPropertyDataSize(device, &addr, 0, nil, &size) != 0 || size < 4 {
		return false
	}
	buf := make([]byte, size)
	if _AudioObjectGetPropertyData(device, &addr, 0, nil, &size, unsafe.Pointer(&buf[0])) != 0 {
		return false
	}
	// An AudioBufferList: a count, and for each buffer its channels, its
	// size and a pointer, aligned as the C struct is.
	n := *(*uint32)(unsafe.Pointer(&buf[0]))
	const stride = 16 // two uint32 and a pointer
	for i := uint32(0); i < n; i++ {
		off := 8 + int(i)*stride
		if off+4 > len(buf) {
			break
		}
		if *(*uint32)(unsafe.Pointer(&buf[off])) > 0 {
			return true
		}
	}
	return false
}

// Devices lists the outputs that sound can be played on.
func Devices() ([]Device, error) {
	if err := loadCoreAudio(); err != nil {
		return nil, err
	}
	addr := audioObjectPropertyAddress{kAudioHardwarePropertyDevices, kAudioObjectPropertyScopeGlobal, 0}
	var size uint32
	if st := _AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &addr, 0, nil, &size); st != 0 {
		return nil, fmt.Errorf("oto: listing the devices failed: %d", st)
	}
	ids := make([]uint32, size/4)
	if len(ids) == 0 {
		return nil, nil
	}
	if st := _AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, nil, &size, unsafe.Pointer(&ids[0])); st != 0 {
		return nil, fmt.Errorf("oto: listing the devices failed: %d", st)
	}
	var def uint32
	dAddr := audioObjectPropertyAddress{kAudioHardwarePropertyDefaultOutputDevice, kAudioObjectPropertyScopeGlobal, 0}
	dSize := uint32(4)
	_AudioObjectGetPropertyData(kAudioObjectSystemObject, &dAddr, 0, nil, &dSize, unsafe.Pointer(&def))

	var devices []Device
	for _, id := range ids {
		if !hasOutput(id) {
			continue
		}
		uid := stringProperty(id, kAudioDevicePropertyDeviceUID)
		if uid == "" {
			continue
		}
		name := stringProperty(id, kAudioObjectPropertyName)
		if name == "" {
			name = uid
		}
		devices = append(devices, Device{ID: uid, Name: name, Default: id == def})
	}
	return devices, nil
}

// useDevice sends the queue's sound to the device with that UID; "" leaves
// it to the system's default.
func useDevice(queue _AudioQueueRef, uid string) error {
	if uid == "" {
		return nil
	}
	if err := loadCoreAudio(); err != nil {
		return err
	}
	s := _CFStringCreateWithCString(0, uid, kCFStringEncodingUTF8)
	if s == 0 {
		return errors.New("oto: the device's name is not text")
	}
	defer _CFRelease(s)
	if st := _AudioQueueSetProperty(queue, kAudioQueueProperty_CurrentDevice, unsafe.Pointer(&s), uint32(unsafe.Sizeof(s))); st != 0 {
		return fmt.Errorf("oto: the device %q cannot be used: %d", uid, st)
	}
	return nil
}

package main

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// nowPlaying tells Windows what plays, through the System Media
// Transport Controls: the song, its artist and its picture show in the
// volume flyout, on the lock screen and in the taskbar's media controls,
// whose buttons, and the keyboard's media keys, come back to the app. It
// calls the Windows Runtime's COM interfaces by their tables of methods,
// without cgo.
type nowPlaying struct {
	app *App
	// The controls of the window, what they display, and the song's
	// properties in it.
	controls, updater, music, music2 unsafe.Pointer
	// The factories of the addresses and of the streams pictures come as.
	uris, streams unsafe.Pointer
	ok            bool
	art           string // the picture last given
}

var (
	combase                    = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize           = combase.NewProc("RoInitialize")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")
	procWindowsGetStringRawBuf = combase.NewProc("WindowsGetStringRawBuffer")
)

func mustGUID(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic(err)
	}
	return g
}

var (
	iidUnknown      = mustGUID("{00000000-0000-0000-C000-000000000046}")
	iidAgileObject  = mustGUID("{94EA2B94-E9CC-49E0-C0FF-EE64CA8F5B90}")
	iidInterop      = mustGUID("{DDB0472D-C911-4A1F-86D9-DC3D71A95F5A}") // ISystemMediaTransportControlsInterop
	iidControls     = mustGUID("{" + smtcIID + "}")
	iidMusic2       = mustGUID("{00368462-97D3-44B9-B00F-008AFCEFAF18}") // IMusicDisplayProperties2
	iidUriFactory   = mustGUID("{44A9796F-723E-4FDF-A218-033E75B0C084}") // IUriRuntimeClassFactory
	iidStreamRefs   = mustGUID("{857309DC-3FBF-4E7D-986F-EF3B1A07A964}") // IRandomAccessStreamReferenceStatics
	iidButtonsEvent = mustGUID(typedEventHandlerIID(smtcClass, smtcIID, smtcArgsClass, smtcArgsIID))
)

// The places of the methods in the tables of the interfaces, after the
// six every Windows Runtime interface begins with.
const (
	interopGetForWindow = 6

	controlsGetStatus      = 6
	controlsPutStatus      = 7
	controlsGetUpdater     = 8
	controlsGetEnabled     = 10
	controlsPutEnabled     = 11
	controlsPutPlay        = 13
	controlsPutPause       = 17
	controlsPutPrevious    = 25
	controlsGetNext        = 26
	controlsPutNext        = 27
	controlsAddButtonPress = 32

	updaterGetType      = 6
	updaterPutType      = 7
	updaterPutThumbnail = 11
	updaterGetMusic     = 12
	updaterClearAll     = 16
	updaterUpdate       = 17

	musicGetTitle  = 6
	musicPutTitle  = 7
	musicGetArtist = 10
	musicPutArtist = 11
	music2GetAlbum = 6
	music2PutAlbum = 7

	uriCreate          = 6
	streamsCreateByURI = 7
	argsGetButton      = 6
)

// What the controls show, and their buttons, as Windows numbers them.
const (
	typeMusic     = 1
	statusStopped = 2
	statusPlaying = 3
	statusPaused  = 4

	buttonPlay     = 0
	buttonPause    = 1
	buttonStop     = 2
	buttonNext     = 6
	buttonPrevious = 7
)

type hresult uint32

func (h hresult) Error() string { return fmt.Sprintf("Windows error 0x%08X", uint32(h)) }

// vcall calls the method at index of a COM object's table.
func vcall(obj unsafe.Pointer, index int, args ...uintptr) error {
	if obj == nil {
		return errors.New("no object")
	}
	table := *(*unsafe.Pointer)(obj)
	method := *(*uintptr)(unsafe.Add(table, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(obj))
	all = append(all, args...)
	hr, _, _ := syscall.SyscallN(method, all...)
	if int32(hr) < 0 {
		return hresult(hr)
	}
	return nil
}

func release(obj *unsafe.Pointer) {
	if *obj != nil {
		vcall(*obj, 2)
		*obj = nil
	}
}

// hstring makes a string of the Windows Runtime, to delete after use.
func hstring(s string) (uintptr, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h uintptr
	hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	if int32(hr) < 0 {
		return 0, hresult(hr)
	}
	return h, nil
}

func deleteHString(h uintptr) { procWindowsDeleteString.Call(h) }

// factory returns the activation factory of a class, as one of its
// interfaces.
func factory(class string, iid *windows.GUID) (unsafe.Pointer, error) {
	name, err := hstring(class)
	if err != nil {
		return nil, err
	}
	defer deleteHString(name)
	var out unsafe.Pointer
	hr, _, _ := procRoGetActivationFactory.Call(name, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if int32(hr) < 0 {
		return nil, fmt.Errorf("%s: %w", class, hresult(hr))
	}
	return out, nil
}

// putString sets a string property.
func putString(obj unsafe.Pointer, index int, s string) error {
	h, err := hstring(s)
	if err != nil {
		return err
	}
	defer deleteHString(h)
	return vcall(obj, index, h)
}

// getString reads a string property.
func getString(obj unsafe.Pointer, index int) (string, error) {
	var h uintptr
	if err := vcall(obj, index, uintptr(unsafe.Pointer(&h))); err != nil {
		return "", err
	}
	defer deleteHString(h)
	var n uint32
	p, _, _ := procWindowsGetStringRawBuf.Call(h, uintptr(unsafe.Pointer(&n)))
	if p == 0 || n == 0 {
		return "", nil
	}
	// The buffer is Windows', which gives its address as a number.
	buf := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	return windows.UTF16ToString(unsafe.Slice((*uint16)(buf), n)), nil
}

// buttons is the COM object Windows calls when a button of the controls,
// or a media key, is pressed: one for the process, never freed.
var buttons struct {
	once  sync.Once
	table [4]uintptr
	this  struct{ table *[4]uintptr }
	app   *App
}

func buttonsHandler(a *App) unsafe.Pointer {
	buttons.once.Do(func() {
		buttons.table = [4]uintptr{
			windows.NewCallback(func(this unsafe.Pointer, iid *windows.GUID, out *unsafe.Pointer) uintptr {
				if *iid == iidUnknown || *iid == iidAgileObject || *iid == iidButtonsEvent {
					*out = this
					return 0
				}
				*out = nil
				return 0x80004002 // E_NOINTERFACE
			}),
			windows.NewCallback(func(this unsafe.Pointer) uintptr { return 2 }), // AddRef
			windows.NewCallback(func(this unsafe.Pointer) uintptr { return 1 }), // Release
			windows.NewCallback(func(this, sender, args unsafe.Pointer) uintptr {
				var button int32
				if vcall(args, argsGetButton, uintptr(unsafe.Pointer(&button))) == nil {
					pressed(buttons.app, button)
				}
				return 0
			}),
		}
		buttons.this.table = &buttons.table
	})
	buttons.app = a
	return unsafe.Pointer(&buttons.this)
}

// pressed does what a button of the controls asks. Windows calls on a
// thread of its own.
func pressed(a *App, button int32) {
	if a == nil {
		return
	}
	a.update(func() {
		p := a.player
		switch button {
		case buttonPlay:
			if !p.playing() {
				p.toggle()
			}
		case buttonPause, buttonStop:
			if p.playing() {
				p.toggle()
			}
		case buttonNext:
			p.skip()
		case buttonPrevious:
			p.previous()
		}
	})
}

// init takes the media controls of the app's window.
func (n *nowPlaying) init(a *App) {
	if a.win == nil {
		return
	}
	if err := n.attach(a, a.win.NativeHandle()); err != nil {
		log.Print("the system's media controls: ", err)
	}
}

// attach takes the media controls of a window, on the thread that owns
// it.
func (n *nowPlaying) attach(a *App, hwnd uintptr) error {
	if procRoGetActivationFactory.Find() != nil || procWindowsCreateString.Find() != nil {
		return errors.New("this Windows has no Windows Runtime")
	}
	procRoInitialize.Call(0) // the thread's own, single-threaded: done already is as good
	interop, err := factory(smtcClass, &iidInterop)
	if err != nil {
		return err
	}
	defer release(&interop)
	if err := vcall(interop, interopGetForWindow, hwnd, uintptr(unsafe.Pointer(&iidControls)), uintptr(unsafe.Pointer(&n.controls))); err != nil {
		return fmt.Errorf("the window's controls: %w", err)
	}
	if err := vcall(n.controls, controlsGetUpdater, uintptr(unsafe.Pointer(&n.updater))); err != nil {
		return fmt.Errorf("their display: %w", err)
	}
	if err := vcall(n.updater, updaterPutType, typeMusic); err != nil {
		return fmt.Errorf("its type: %w", err)
	}
	if err := vcall(n.updater, updaterGetMusic, uintptr(unsafe.Pointer(&n.music))); err != nil {
		return fmt.Errorf("the song's properties: %w", err)
	}
	// The album's name is of a later version of the properties.
	vcall(n.music, 0, uintptr(unsafe.Pointer(&iidMusic2)), uintptr(unsafe.Pointer(&n.music2)))
	// Pictures come by their address; without these, songs show without.
	n.uris, _ = factory("Windows.Foundation.Uri", &iidUriFactory)
	n.streams, _ = factory("Windows.Storage.Streams.RandomAccessStreamReference", &iidStreamRefs)

	for _, put := range []int{controlsPutEnabled, controlsPutPlay, controlsPutPause, controlsPutPrevious, controlsPutNext} {
		if err := vcall(n.controls, put, 1); err != nil {
			return fmt.Errorf("enabling the controls: %w", err)
		}
	}
	var token int64
	if err := vcall(n.controls, controlsAddButtonPress, uintptr(buttonsHandler(a)), uintptr(unsafe.Pointer(&token))); err != nil {
		return fmt.Errorf("listening to the buttons: %w", err)
	}
	n.app, n.ok = a, true
	return nil
}

// set tells Windows what plays; an empty title is nothing playing.
func (n *nowPlaying) set(p playing) {
	if !n.ok {
		return
	}
	if p.title == "" {
		vcall(n.updater, updaterClearAll)
		vcall(n.updater, updaterPutType, typeMusic)
		vcall(n.updater, updaterUpdate)
		vcall(n.controls, controlsPutStatus, statusStopped)
		n.art = ""
		return
	}
	putString(n.music, musicPutTitle, p.title)
	putString(n.music, musicPutArtist, p.artist)
	if n.music2 != nil {
		putString(n.music2, music2PutAlbum, p.album)
	}
	// The picture, from the server's address, which Windows fetches
	// itself, else from its file.
	art := p.artURL
	if art == "" && p.art != "" {
		art = (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(p.art)}).String()
	}
	if art != n.art {
		n.art = art
		n.thumbnail(art)
	}
	vcall(n.updater, updaterUpdate)
	status := uintptr(statusPlaying)
	if p.paused {
		status = statusPaused
	}
	vcall(n.controls, controlsPutStatus, status)
}

// thumbnail gives the display the picture at an address, or none.
func (n *nowPlaying) thumbnail(address string) {
	if address == "" || n.uris == nil || n.streams == nil {
		vcall(n.updater, updaterPutThumbnail, 0)
		return
	}
	h, err := hstring(address)
	if err != nil {
		return
	}
	defer deleteHString(h)
	var uri, stream unsafe.Pointer
	if vcall(n.uris, uriCreate, h, uintptr(unsafe.Pointer(&uri))) != nil {
		return
	}
	defer release(&uri)
	if vcall(n.streams, streamsCreateByURI, uintptr(uri), uintptr(unsafe.Pointer(&stream))) != nil {
		return
	}
	defer release(&stream)
	vcall(n.updater, updaterPutThumbnail, uintptr(stream))
}

// progress has nothing to do: the controls show no place in the song.
func (n *nowPlaying) progress(position time.Duration) {}

// describe returns what Windows holds, for tests of the bridge.
func (n *nowPlaying) describe() string {
	if !n.ok {
		return "not registered with the system"
	}
	title, _ := getString(n.music, musicGetTitle)
	artist, _ := getString(n.music, musicGetArtist)
	album := ""
	if n.music2 != nil {
		album, _ = getString(n.music2, music2GetAlbum)
	}
	var status int32
	vcall(n.controls, controlsGetStatus, uintptr(unsafe.Pointer(&status)))
	return fmt.Sprintf("title=%q artist=%q album=%q status=%d art=%q", title, artist, album, status, n.art)
}

// handlesKeys reports whether Windows sends the media keys through the
// controls: it does, once they are the window's.
func (n *nowPlaying) handlesKeys() bool { return n.ok }

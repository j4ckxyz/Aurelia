package oto

import (
	"fmt"
)

// Devices lists the sound outputs that are plugged in and enabled, by the
// ID Windows gives each, which a WASAPI endpoint is opened by.
func Devices() ([]Device, error) {
	t, err := newCOMThread()
	if err != nil {
		return nil, err
	}
	defer t.Stop()
	var devices []Device
	var cerr error
	t.Run(func() { devices, cerr = listEndpoints() })
	return devices, cerr
}

func listEndpoints() ([]Device, error) {
	e, err := _CoCreateInstance(&uuidMMDeviceEnumerator, nil, uint32(_CLSCTX_ALL), &uuidIMMDeviceEnumerator)
	if err != nil {
		return nil, err
	}
	enum := (*_IMMDeviceEnumerator)(e)
	defer enum.Release()

	defID := ""
	if d, err := enum.GetDefaultAudioEndPoint(eRender, eConsole); err == nil {
		defID, _ = d.GetId()
		d.Release()
	}
	coll, err := enum.EnumAudioEndpoints(eRender, _DEVICE_STATE_ACTIVE)
	if err != nil {
		return nil, err
	}
	defer coll.Release()
	n, err := coll.GetCount()
	if err != nil {
		return nil, err
	}
	var devices []Device
	for i := uint32(0); i < n; i++ {
		d, err := coll.Item(i)
		if err != nil {
			continue
		}
		id, err := d.GetId()
		if err != nil {
			d.Release()
			continue
		}
		name := ""
		if ps, err := d.OpenPropertyStore(); err == nil {
			name, _ = ps.String(&pkeyDeviceFriendlyName)
			ps.Release()
		}
		d.Release()
		if name == "" {
			name = id
		}
		devices = append(devices, Device{ID: id, Name: name, Default: id == defID})
	}
	if len(devices) == 0 && n > 0 {
		return nil, fmt.Errorf("oto: no output could be read of %d", n)
	}
	return devices, nil
}

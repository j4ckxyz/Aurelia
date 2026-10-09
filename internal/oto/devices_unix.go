//go:build !android && !darwin && !js && !windows && !nintendosdk && !playstation5

package oto

// Devices lists the outputs that sound can be played on: those of the
// PulseAudio server, or where there is none the PCMs of ALSA.
func Devices() ([]Device, error) {
	devs, err := pulseDevices()
	if err == nil && len(devs) > 0 {
		return devs, nil
	}
	if newALSAContext == nil {
		return nil, err
	}
	return alsaDevices()
}

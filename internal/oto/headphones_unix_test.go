//go:build !android && !darwin && !js && !windows && !nintendosdk && !playstation5

package oto

import (
	"testing"

	"github.com/jfreymuth/pulse/proto"
)

// What PulseAudio and PipeWire say of a sink tells headphones.
func TestPulseHeadphones(t *testing.T) {
	sink := func(name, port string, props map[string]string) *proto.GetSinkInfoReply {
		s := &proto.GetSinkInfoReply{SinkName: name, Device: "Device", ActivePortName: port, Properties: proto.PropList{}}
		for k, v := range props {
			s.Properties[k] = proto.PropListString(v)
		}
		return s
	}
	for _, c := range []struct {
		name string
		sink *proto.GetSinkInfoReply
		want bool
	}{
		{"a Bluetooth sink", sink("bluez_output.AC_80_0A_00_00_00.1", "", map[string]string{"device.bus": "bluetooth"}), true},
		{"a headset's form factor", sink("alsa_output.usb-Jabra", "", map[string]string{"device.form_factor": "headset"}), true},
		{"the laptop's jack, in use", sink("alsa_output.pci-0000_00_1f.3.analog-stereo", "analog-output-headphones", nil), true},
		{"the laptop's speakers", sink("alsa_output.pci-0000_00_1f.3.analog-stereo", "analog-output-speaker", nil), false},
		{"speakers, said so", sink("bluez_output.speaker", "", map[string]string{"device.form_factor": "speaker", "device.bus": "bluetooth"}), false},
		{"HDMI", sink("alsa_output.pci-0000_01_00.1.hdmi-stereo", "hdmi-output-0", map[string]string{"device.bus": "pci"}), false},
	} {
		if got := pulseHeadphones(c.sink); got != c.want {
			t.Errorf("%s: headphones %v, want %v", c.name, got, c.want)
		}
	}
}

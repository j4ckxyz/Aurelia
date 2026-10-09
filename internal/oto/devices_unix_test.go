//go:build !android && !darwin && !js && !windows && !nintendosdk && !playstation5

package oto

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// sineReader is an endless tone, as the bytes of float32 samples.
type sineReader struct{ n int }

func (s *sineReader) Read(p []byte) (int, error) {
	const rate = 44100
	for i := 0; i+8 <= len(p); i += 8 {
		v := float32(0.2 * math.Sin(2*math.Pi*440*float64(s.n)/rate))
		s.n++
		binary.LittleEndian.PutUint32(p[i:], math.Float32bits(v))
		binary.LittleEndian.PutUint32(p[i+4:], math.Float32bits(v))
	}
	return len(p) / 8 * 8, nil
}

// pactl runs PulseAudio's command and returns its lines.
func pactl(t *testing.T, args ...string) []string {
	t.Helper()
	out, err := exec.Command("pactl", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("pactl %v: %v\n%s", args, err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// sinkOfStream names the sink that Aurelia's stream plays on, "" for none.
func sinkOfStream(t *testing.T) string {
	t.Helper()
	sinks := map[string]string{} // index to name
	for _, l := range pactl(t, "list", "sinks", "short") {
		if f := strings.Fields(l); len(f) >= 2 {
			sinks[f[0]] = f[1]
		}
	}
	for _, l := range pactl(t, "list", "sink-inputs", "short") {
		if f := strings.Fields(l); len(f) >= 2 {
			return sinks[f[1]]
		}
	}
	return ""
}

// With AURELIA_REQUIRE_PULSE set, a PulseAudio server with two sinks named
// aurelia_a and aurelia_b must be running: sound is played on one, moved
// to the other while it plays, and PulseAudio says where it went.
func TestPulseDevices(t *testing.T) {
	require := os.Getenv("AURELIA_REQUIRE_PULSE") != ""
	devs, err := pulseDevices()
	if err != nil {
		if require {
			t.Fatalf("no PulseAudio: %v", err)
		}
		t.Skipf("no PulseAudio here: %v", err)
	}
	have := map[string]bool{}
	for _, d := range devs {
		t.Logf("%q %q default=%v", d.ID, d.Name, d.Default)
		have[d.ID] = true
	}
	if !have[pulsePrefix+"aurelia_a"] || !have[pulsePrefix+"aurelia_b"] {
		if require {
			t.Fatalf("the sinks aurelia_a and aurelia_b are not listed: %v", devs)
		}
		t.Skip("no sinks to test with")
	}
	if all, err := Devices(); err != nil || len(all) != len(devs) {
		t.Errorf("Devices gave %d outputs and %v, pulseDevices %d", len(all), err, len(devs))
	}

	ctx, ready, err := NewContext(&NewContextOptions{
		SampleRate: 44100, ChannelCount: 2, Format: FormatFloat32LE,
		ApplicationName: "Aurelia test", DeviceID: pulsePrefix + "aurelia_a",
	})
	if err != nil {
		t.Fatal(err)
	}
	<-ready
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	player := ctx.NewPlayer(&sineReader{})
	player.Play()

	waitFor := func(want string) {
		t.Helper()
		var got string
		for i := 0; i < 50; i++ {
			if got = sinkOfStream(t); got == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("the sound plays on %q, want %q", got, want)
	}
	waitFor("aurelia_a")
	if err := ctx.SetDevice(pulsePrefix + "aurelia_b"); err != nil {
		t.Fatal(err)
	}
	waitFor("aurelia_b")
	if err := ctx.Err(); err != nil {
		t.Fatalf("the context failed after the move: %v", err)
	}
	// The sound is still being read, not stopped by the move.
	n := player.BufferedSize()
	time.Sleep(500 * time.Millisecond)
	if !player.IsPlaying() {
		t.Errorf("the player stopped after the move (buffered %d)", n)
	}
	// A device that does not exist: the default plays, and the error says so.
	if err := ctx.SetDevice(pulsePrefix + "no_such_sink"); err == nil {
		t.Error("a sink that is not there was accepted")
	}
	if sinkOfStream(t) == "" {
		t.Error("nothing plays after a failed move: the default should")
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("the context failed after the failed move: %v", err)
	}
}

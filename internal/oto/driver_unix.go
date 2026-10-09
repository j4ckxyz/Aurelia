// Copyright 2021 The Oto Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !android && !darwin && !js && !windows && !nintendosdk && !playstation5

package oto

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"aurelia/internal/oto/internal/mux"
)

// unixBackend is the part of a context that talks to the actual audio device.
type unixBackend interface {
	Suspend() error
	Resume() error
	Err() error
	// Close stops the backend and gives its device back. The mixer it read
	// from stays.
	Close() error
}

// newALSAContext creates the ALSA fallback backend. driver_alsa_unix.go's init sets it on the
// platforms where that file is built; it is nil elsewhere (e.g. OpenBSD), in which case only the
// PulseAudio backend is attempted.
var newALSAContext func(sampleRate, channelCount int, mux *mux.Mux, bufferSizeInBytes int, device string) (unixBackend, error)

type context struct {
	mux     *mux.Mux
	backend unixBackend

	sampleRate, channelCount, bufferSizeInBytes int
	applicationName                             string

	// switching serializes the changes of device; backendMu guards backend.
	switching sync.Mutex
	backendMu sync.Mutex

	ready chan struct{}
	err   atomicError
}

// The devices are named "pulse:" and the name of a sink of PulseAudio, or
// "alsa:" and the name of a PCM of ALSA; "" is the system's default.
const (
	pulsePrefix = "pulse:"
	alsaPrefix  = "alsa:"
)

func newContext(sampleRate int, channelCount int, format mux.Format, bufferSizeInBytes int, applicationName string, deviceID string) (*context, chan struct{}, error) {
	ctx := &context{
		mux:               mux.New(sampleRate, channelCount, format),
		ready:             make(chan struct{}),
		sampleRate:        sampleRate,
		channelCount:      channelCount,
		bufferSizeInBytes: bufferSizeInBytes,
		applicationName:   applicationName,
	}

	// Initializing a driver might take some time, so do it asynchronously.
	// PulseAudio is the default; if no server is reachable, fall back to ALSA.
	go func() {
		defer close(ctx.ready)
		b, err := ctx.open(deviceID)
		if err != nil && deviceID != "" {
			// The device chosen is not there: the default plays.
			b, err = ctx.open("")
		}
		if err != nil {
			ctx.err.Join(err)
			return
		}
		ctx.backend = b
	}()

	return ctx, ctx.ready, nil
}

// open starts a backend on the device.
func (c *context) open(deviceID string) (unixBackend, error) {
	switch {
	case strings.HasPrefix(deviceID, alsaPrefix):
		if newALSAContext == nil {
			return nil, errors.New("oto: ALSA is not available")
		}
		return newALSAContext(c.sampleRate, c.channelCount, c.mux, c.bufferSizeInBytes, strings.TrimPrefix(deviceID, alsaPrefix))
	case strings.HasPrefix(deviceID, pulsePrefix):
		return newPulseContext(c.sampleRate, c.channelCount, c.mux, c.bufferSizeInBytes, c.applicationName, strings.TrimPrefix(deviceID, pulsePrefix))
	}
	pc, err0 := newPulseContext(c.sampleRate, c.channelCount, c.mux, c.bufferSizeInBytes, c.applicationName, "")
	if err0 == nil {
		return pc, nil
	}
	if newALSAContext == nil {
		return nil, err0
	}
	ac, err1 := newALSAContext(c.sampleRate, c.channelCount, c.mux, c.bufferSizeInBytes, "")
	if err1 == nil {
		return ac, nil
	}
	return nil, fmt.Errorf("oto: initialization failed: PulseAudio: %w; ALSA: %w", err0, err1)
}

// SetDevice moves the sound to another device, which the mixer goes on
// feeding: what plays continues there after a moment of silence. When the
// device cannot be opened the one before is opened again, and the error
// returned.
func (c *context) SetDevice(deviceID string) error {
	<-c.ready
	c.switching.Lock()
	defer c.switching.Unlock()
	c.backendMu.Lock()
	old := c.backend
	c.backend = nil
	c.backendMu.Unlock()
	if old != nil {
		old.Close()
	}
	b, err := c.open(deviceID)
	if err != nil {
		var err2 error
		if b, err2 = c.open(""); err2 != nil {
			c.err.Join(err2)
		}
	}
	c.backendMu.Lock()
	c.backend = b
	c.backendMu.Unlock()
	return err
}

// current returns the backend, nil while it is being changed.
func (c *context) current() unixBackend {
	c.backendMu.Lock()
	defer c.backendMu.Unlock()
	return c.backend
}

func (c *context) Suspend() error {
	<-c.ready
	if b := c.current(); b != nil {
		return b.Suspend()
	}
	return nil
}

func (c *context) Resume() error {
	<-c.ready
	if b := c.current(); b != nil {
		return b.Resume()
	}
	return nil
}

func (c *context) Err() error {
	if err := c.err.Load(); err != nil {
		return err
	}

	select {
	case <-c.ready:
	default:
		return nil
	}

	if b := c.current(); b != nil {
		return b.Err()
	}
	return nil
}

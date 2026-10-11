// SPDX-License-Identifier: GPL-2.0-or-later

// Package audio defines the PCM streams used between hardware, files, and
// modem pipelines.
package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

var (
	ErrInvalidSpec      = errors.New("invalid PCM specification")
	ErrMisalignedBuffer = errors.New("sample buffer is not channel-aligned")
)

// SampleFormat identifies the representation of each interleaved PCM sample.
type SampleFormat uint8

const (
	// Signed16LE is signed 16-bit little-endian PCM. In memory it is represented
	// as native int16 values; byte order matters at device/file boundaries.
	Signed16LE SampleFormat = iota
)

// Spec describes interleaved signed 16-bit PCM.
type Spec struct {
	SampleRate int
	Channels   int
	Format     SampleFormat
}

// DeviceConfig describes a live PCM endpoint. BufferFrames is the requested
// hardware/software buffering in sample frames, where one frame contains one
// sample for every channel.
type DeviceConfig struct {
	Device       string
	PCM          Spec
	BufferFrames int
}

// Validate checks both the PCM specification and live buffering request.
func (c DeviceConfig) Validate() error {
	if err := c.PCM.Validate(); err != nil {
		return err
	}
	if c.Device == "" {
		return fmt.Errorf("%w: audio device is empty", ErrInvalidSpec)
	}
	if c.BufferFrames <= 0 {
		return fmt.Errorf("%w: buffer frames must be positive", ErrInvalidSpec)
	}
	return nil
}

// Validate checks the supported PCM limits.
func (s Spec) Validate() error {
	if s.SampleRate < 8000 || s.SampleRate > 192000 {
		return fmt.Errorf("%w: sample rate %d is outside 8000..192000", ErrInvalidSpec, s.SampleRate)
	}
	if s.Channels < 1 || s.Channels > 2 {
		return fmt.Errorf("%w: channel count %d is outside 1..2", ErrInvalidSpec, s.Channels)
	}
	if s.Format != Signed16LE {
		return fmt.Errorf("%w: unsupported sample format %d", ErrInvalidSpec, s.Format)
	}
	return nil
}

// Capture supplies interleaved signed 16-bit PCM samples.
type Capture interface {
	Spec() Spec
	ReadSamples(context.Context, []int16) (int, error)
	Close() error
}

// Playback consumes interleaved signed 16-bit PCM samples.
type Playback interface {
	Spec() Spec
	WriteSamples(context.Context, []int16) (int, error)
	Close() error
}

// PlaybackBus serializes complete playback operations that share one device.
// Callers should construct one bus per physical playback endpoint and share it
// across every channel routed to that endpoint.
type PlaybackBus struct {
	playback Playback
	permit   chan struct{}
}

// NewPlaybackBus wraps a playback endpoint for exclusive burst access.
func NewPlaybackBus(playback Playback) (*PlaybackBus, error) {
	if playback == nil {
		return nil, errors.New("audio playback is nil")
	}
	return &PlaybackBus{playback: playback, permit: make(chan struct{}, 1)}, nil
}

// Spec returns the wrapped endpoint's PCM specification.
func (b *PlaybackBus) Spec() Spec { return b.playback.Spec() }

// WithExclusive runs one complete transmission while excluding other users of
// this bus. Cancellation also interrupts waiting to acquire the device.
func (b *PlaybackBus) WithExclusive(ctx context.Context, transmit func(Playback) error) error {
	if transmit == nil {
		return errors.New("audio transmit operation is nil")
	}
	select {
	case b.permit <- struct{}{}:
		defer func() { <-b.permit }()
		return transmit(b.playback)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SliceCapture is a deterministic in-memory Capture used by tests and offline
// pipelines.
type SliceCapture struct {
	spec    Spec
	samples []int16
	offset  int
	closed  bool
	mu      sync.Mutex
}

// NewSliceCapture constructs a capture stream over a private copy of samples.
func NewSliceCapture(spec Spec, samples []int16) (*SliceCapture, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if len(samples)%spec.Channels != 0 {
		return nil, ErrMisalignedBuffer
	}
	return &SliceCapture{spec: spec, samples: append([]int16(nil), samples...)}, nil
}

func (c *SliceCapture) Spec() Spec { return c.spec }

func (c *SliceCapture) ReadSamples(ctx context.Context, destination []int16) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(destination)%c.spec.Channels != 0 {
		return 0, ErrMisalignedBuffer
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, io.ErrClosedPipe
	}
	if c.offset == len(c.samples) {
		return 0, io.EOF
	}
	n := copy(destination, c.samples[c.offset:])
	c.offset += n
	return n, nil
}

func (c *SliceCapture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// SlicePlayback is a deterministic in-memory Playback. Samples returns a copy
// of everything written so far.
type SlicePlayback struct {
	spec    Spec
	samples []int16
	closed  bool
	mu      sync.Mutex
}

// NewSlicePlayback constructs an empty in-memory playback stream.
func NewSlicePlayback(spec Spec) (*SlicePlayback, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &SlicePlayback{spec: spec}, nil
}

func (p *SlicePlayback) Spec() Spec { return p.spec }

func (p *SlicePlayback) WriteSamples(ctx context.Context, samples []int16) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(samples)%p.spec.Channels != 0 {
		return 0, ErrMisalignedBuffer
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	p.samples = append(p.samples, samples...)
	return len(samples), nil
}

func (p *SlicePlayback) Samples() []int16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int16(nil), p.samples...)
}

func (p *SlicePlayback) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

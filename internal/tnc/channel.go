// SPDX-License-Identifier: GPL-2.0-or-later

// Package tnc contains per-radio channel state and packet transmit/receive
// coordination.
package tnc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sparques/vulpes/internal/audio"
	"github.com/sparques/vulpes/internal/hdlc"
	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/ptt"
)

var (
	ErrQueueFull      = errors.New("transmit queue is full")
	ErrClosed         = errors.New("TNC channel is closed")
	ErrFrameTooLarge  = errors.New("frame exceeds configured maximum")
	ErrCarrierTimeout = errors.New("timed out waiting for a clear channel")
)

const defaultMaximumFrameBytes = 2048

// ChannelConfig contains the modem and bounded transmit-queue settings for a
// single radio channel.
type ChannelConfig struct {
	Name              string
	Modem             modem.Settings
	MaximumFrameBytes int
	QueueDepth        int
	TXDelayMillis     int
	TXTailMillis      int
	SlotMillis        int
	Persistence       int
	FullDuplex        bool
	AudioChannel      int
	VOXToneMillis     int
}

// Timing contains live per-channel KISS timing controls. Millisecond fields
// are stored in milliseconds; Persistence uses the KISS 0..255 range.
type Timing struct {
	TXDelayMillis int
	TXTailMillis  int
	SlotMillis    int
	Persistence   int
	FullDuplex    bool
}

// Channel owns independent modem state and a bounded packet transmit queue.
type Channel struct {
	name              string
	receiver          modem.Receiver
	transmitter       modem.Transmitter
	queue             chan []byte
	done              chan struct{}
	closeOnce         sync.Once
	mu                sync.Mutex
	timingMu          sync.RWMutex
	rxMu              sync.Mutex
	txMu              sync.Mutex
	playbackMu        sync.Mutex
	closed            bool
	maximumFrameBytes int
	leadingFlags      int
	trailingFlags     int
	symbolBits        int
	baud              int
	sampleRate        int
	audioChannel      int
	voxToneMillis     int
	txAmplitude       float64
	lastLevel         bool
	timing            Timing
	pending           []byte
	carrier           atomic.Bool
	subscribers       map[chan modem.Event]struct{}
	droppedEvents     atomic.Uint64
	audioChunks       atomic.Uint64
	audioSamples      atomic.Uint64
	receiveEvents     atomic.Uint64
	queueDrops        atomic.Uint64
}

// Metrics is a point-in-time operational snapshot for one channel.
type Metrics struct {
	AudioChunks, AudioSamples, ReceiveEvents uint64
	QueueDepth                               int
	QueueDrops, DroppedEvents                uint64
	CarrierDetected                          bool
	HDLC                                     hdlc.Stats
}

// NewChannel constructs modem endpoints and defaults an unspecified queue to
// 16 frames and an unspecified maximum payload to 2048 bytes.
func NewChannel(config ChannelConfig) (*Channel, error) {
	if config.Name == "" {
		return nil, errors.New("TNC channel name cannot be empty")
	}
	if config.MaximumFrameBytes <= 0 {
		config.MaximumFrameBytes = defaultMaximumFrameBytes
	}
	if config.QueueDepth <= 0 {
		config.QueueDepth = 16
	}
	if config.VOXToneMillis < 0 || config.VOXToneMillis > 30000 {
		return nil, errors.New("VOX lead-in tone duration must be between 0 and 30000 milliseconds")
	}
	receiver, err := modem.NewReceiver(config.Modem, config.MaximumFrameBytes)
	if err != nil {
		return nil, fmt.Errorf("create receiver for channel %q: %w", config.Name, err)
	}
	transmitter, err := modem.NewTransmitter(config.Modem)
	if err != nil && !errors.Is(err, modem.ErrReceiveOnly) {
		return nil, fmt.Errorf("create transmitter for channel %q: %w", config.Name, err)
	}
	baud, symbolBits := modemTiming(config.Modem.Mode)
	if baud == 0 {
		return nil, fmt.Errorf("unsupported channel mode %q", config.Modem.Mode)
	}
	leading := flagCount(config.TXDelayMillis, baud)
	trailing := flagCount(config.TXTailMillis, baud)
	return &Channel{
		name: config.Name, receiver: receiver, transmitter: transmitter,
		queue: make(chan []byte, config.QueueDepth), done: make(chan struct{}),
		maximumFrameBytes: config.MaximumFrameBytes,
		leadingFlags:      leading, trailingFlags: trailing, symbolBits: symbolBits, baud: baud,
		sampleRate: config.Modem.SampleRate, audioChannel: config.AudioChannel,
		voxToneMillis: config.VOXToneMillis, txAmplitude: config.Modem.Amplitude,
		timing: Timing{TXDelayMillis: config.TXDelayMillis, TXTailMillis: config.TXTailMillis,
			SlotMillis: config.SlotMillis, Persistence: config.Persistence, FullDuplex: config.FullDuplex},
		subscribers: make(map[chan modem.Event]struct{}),
	}, nil
}

// Name returns the configured radio-channel name.
func (c *Channel) Name() string { return c.name }

// Timing returns the channel's current KISS timing controls.
func (c *Channel) Timing() Timing {
	c.timingMu.RLock()
	defer c.timingMu.RUnlock()
	return c.timing
}

// SetTiming updates live timing controls. Values are bounded to the KISS
// single-byte representation; a zero slot time is retained for wire fidelity.
func (c *Channel) SetTiming(timing Timing) error {
	return c.UpdateTiming(func(Timing) Timing { return timing })
}

// UpdateTiming atomically applies a timing transformation and refreshes the
// transmit flag counts used for subsequent frames.
func (c *Channel) UpdateTiming(update func(Timing) Timing) error {
	if update == nil {
		return errors.New("timing update cannot be nil")
	}
	c.timingMu.Lock()
	defer c.timingMu.Unlock()
	timing := update(c.timing)
	if timing.TXDelayMillis < 0 || timing.TXDelayMillis > 2550 ||
		timing.TXTailMillis < 0 || timing.TXTailMillis > 2550 ||
		timing.SlotMillis < 0 || timing.SlotMillis > 2550 ||
		timing.Persistence < 0 || timing.Persistence > 255 {
		return errors.New("channel timing is outside KISS limits")
	}
	c.timing = timing
	c.leadingFlags = flagCount(timing.TXDelayMillis, c.baud)
	c.trailingFlags = flagCount(timing.TXTailMillis, c.baud)
	return nil
}

// ProcessAudio consumes one audio chunk and returns completed receive events.
func (c *Channel) ProcessAudio(samples []int16) []modem.Event {
	c.rxMu.Lock()
	defer c.rxMu.Unlock()
	select {
	case <-c.done:
		return nil
	default:
		c.audioChunks.Add(1)
		c.audioSamples.Add(uint64(len(samples)))
		events := c.receiver.Process(samples)
		c.receiveEvents.Add(uint64(len(events)))
		c.carrier.Store(c.receiver.CarrierDetected())
		c.publish(events)
		return events
	}
}

// SubscribeEvents registers a bounded, non-blocking event stream. When a
// subscriber falls behind, its newest event is dropped and counted rather
// than stalling modem processing or other subscribers.
func (c *Channel) SubscribeEvents(queueDepth int) (<-chan modem.Event, func(), error) {
	if queueDepth <= 0 {
		return nil, nil, errors.New("event queue depth must be positive")
	}
	events := make(chan modem.Event, queueDepth)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, nil, ErrClosed
	}
	c.subscribers[events] = struct{}{}
	c.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			c.mu.Lock()
			if _, exists := c.subscribers[events]; exists {
				delete(c.subscribers, events)
				close(events)
			}
			c.mu.Unlock()
		})
	}
	return events, cancel, nil
}

// DroppedEvents reports receive events discarded due to subscriber
// backpressure.
func (c *Channel) DroppedEvents() uint64 { return c.droppedEvents.Load() }

// Metrics returns counters used for diagnostics without resetting them.
func (c *Channel) Metrics() Metrics {
	metrics := Metrics{
		AudioChunks: c.audioChunks.Load(), AudioSamples: c.audioSamples.Load(), ReceiveEvents: c.receiveEvents.Load(),
		QueueDepth: c.QueueDepth(), QueueDrops: c.queueDrops.Load(), DroppedEvents: c.droppedEvents.Load(), CarrierDetected: c.carrier.Load(),
	}
	if receiver, ok := c.receiver.(interface{ Stats() hdlc.Stats }); ok {
		metrics.HDLC = receiver.Stats()
	}
	return metrics
}

// QueueDepth reports the number of frames waiting for transmission. It is a
// snapshot intended for operational metrics and protocol status queries.
func (c *Channel) QueueDepth() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	depth := len(c.queue)
	if c.pending != nil {
		depth++
	}
	return depth
}

func (c *Channel) publish(events []modem.Event) {
	if len(events) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, event := range events {
		for subscriber := range c.subscribers {
			copyOfEvent := event
			copyOfEvent.Payload = append([]byte(nil), event.Payload...)
			select {
			case subscriber <- copyOfEvent:
			default:
				c.droppedEvents.Add(1)
			}
		}
	}
}

// QueueFrame queues one decoded KISS/AGW payload without blocking. Payloads are
// copied before enqueueing so callers may immediately reuse their buffers.
func (c *Channel) QueueFrame(payload []byte) error {
	if len(payload) == 0 || len(payload) > c.maximumFrameBytes {
		return fmt.Errorf("%w: got %d bytes, limit %d", ErrFrameTooLarge, len(payload), c.maximumFrameBytes)
	}
	if c.transmitter == nil {
		return fmt.Errorf("%w: %s", modem.ErrReceiveOnly, c.name)
	}
	frame := append([]byte(nil), payload...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	select {
	case c.queue <- frame:
		return nil
	default:
		c.queueDrops.Add(1)
		return ErrQueueFull
	}
}

// NextTransmit waits for a queued payload and returns its modulated PCM.
// Cancellation and channel shutdown interrupt the wait.
func (c *Channel) NextTransmit(ctx context.Context) ([]int16, error) {
	c.txMu.Lock()
	defer c.txMu.Unlock()
	if c.transmitter == nil {
		return nil, modem.ErrReceiveOnly
	}
	select {
	case <-c.done:
		return nil, ErrClosed
	default:
	}
	var payload []byte
	if c.pending != nil {
		payload, c.pending = c.pending, nil
	} else {
		select {
		case payload = <-c.queue:
		case <-c.done:
			return nil, ErrClosed
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := c.waitForClearChannel(ctx); err != nil {
		if !errors.Is(err, ErrClosed) {
			c.pending = payload
		}
		return nil, err
	}
	select {
	case <-c.done:
		return nil, ErrClosed
	default:
		c.timingMu.RLock()
		leadingFlags, trailingFlags := c.leadingFlags, c.trailingFlags
		c.timingMu.RUnlock()
		levels, _ := hdlc.EncodeFrom(payload, leadingFlags, trailingFlags, c.lastLevel)
		levels = alignSymbolBoundary(levels, c.symbolBits)
		c.lastLevel = levels[len(levels)-1]
		return c.transmitter.Modulate(levels), nil
	}
}

// TransmitNext dequeues and modulates one frame, keys PTT after channel access
// is granted, writes the complete PCM burst, and unkeys on every exit path.
// TX delay and tail are represented by the leading and trailing HDLC flags.
func (c *Channel) TransmitNext(ctx context.Context, playback *audio.PlaybackBus, keyer *ptt.Keyer) error {
	c.playbackMu.Lock()
	defer c.playbackMu.Unlock()
	if playback == nil {
		return errors.New("audio playback is nil")
	}
	spec := playback.Spec()
	if err := spec.Validate(); err != nil {
		return err
	}
	if spec.SampleRate != c.sampleRate {
		return fmt.Errorf("playback sample rate %d does not match channel sample rate %d", spec.SampleRate, c.sampleRate)
	}
	if c.audioChannel < 0 || c.audioChannel >= spec.Channels {
		return fmt.Errorf("audio channel %d is unavailable on %d-channel playback", c.audioChannel, spec.Channels)
	}
	samples, err := c.NextTransmit(ctx)
	if err != nil {
		return err
	}
	return playback.WithExclusive(ctx, func(endpoint audio.Playback) (result error) {
		if keyer != nil {
			if err := keyer.Set(ctx, true); err != nil {
				return fmt.Errorf("key PTT: %w", err)
			}
			defer func() {
				if err := keyer.Set(context.WithoutCancel(ctx), false); err != nil {
					result = errors.Join(result, fmt.Errorf("unkey PTT: %w", err))
				}
			}()
		}
		if c.voxToneMillis > 0 {
			tone := voxTone(c.sampleRate, c.voxToneMillis, c.txAmplitude)
			if err := writeChannelSamples(ctx, endpoint, tone, spec.Channels, c.audioChannel); err != nil {
				return fmt.Errorf("write VOX lead-in tone: %w", err)
			}
		}
		return writeChannelSamples(ctx, endpoint, samples, spec.Channels, c.audioChannel)
	})
}

func writeChannelSamples(ctx context.Context, playback audio.Playback, samples []int16, channels, audioChannel int) error {
	if channels == 1 {
		return writeAll(ctx, playback, samples)
	}
	interleaved := make([]int16, len(samples)*channels)
	for i, sample := range samples {
		interleaved[i*channels+audioChannel] = sample
	}
	return writeAll(ctx, playback, interleaved)
}

func voxTone(sampleRate, durationMillis int, amplitude float64) []int16 {
	count := sampleRate * durationMillis / 1000
	samples := make([]int16, count)
	scale := amplitude * math.MaxInt16
	for i := range samples {
		samples[i] = int16(math.Round(math.Sin(2*math.Pi*400*float64(i)/float64(sampleRate)) * scale))
	}
	return samples
}

func writeAll(ctx context.Context, playback audio.Playback, samples []int16) error {
	for len(samples) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := playback.WriteSamples(ctx, samples)
		if n < 0 || n > len(samples) {
			return fmt.Errorf("audio playback returned invalid sample count %d", n)
		}
		samples = samples[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}

func (c *Channel) waitForClearChannel(ctx context.Context) error {
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	for {
		timing := c.Timing()
		if timing.FullDuplex {
			return nil
		}
		if c.carrier.Load() {
			if err := waitTimer(ctx, c.done, 10*time.Millisecond, deadline.C); err != nil {
				return err
			}
			continue
		}
		slot := time.Duration(timing.SlotMillis) * time.Millisecond
		if slot <= 0 {
			slot = time.Millisecond
		}
		if err := waitTimer(ctx, c.done, slot, deadline.C); err != nil {
			return err
		}
		if c.carrier.Load() {
			continue
		}
		if rand.Intn(256) <= timing.Persistence {
			return nil
		}
	}
}

func waitTimer(ctx context.Context, done <-chan struct{}, duration time.Duration, deadline <-chan time.Time) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return ErrClosed
	case <-deadline:
		return ErrCarrierTimeout
	}
}

// Close rejects future frames and interrupts pending transmit waits.
func (c *Channel) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		close(c.done)
		for subscriber := range c.subscribers {
			delete(c.subscribers, subscriber)
			close(subscriber)
		}
		c.mu.Unlock()
	})
	return nil
}

func alignSymbolBoundary(levels []bool, symbolBits int) []bool {
	for flags := 0; flags < symbolBits; flags++ {
		if (len(levels)+flags*8)%symbolBits == 0 {
			return appendFlags(levels, flags)
		}
	}
	level := false
	if len(levels) != 0 {
		level = levels[len(levels)-1]
	}
	padding := (symbolBits - len(levels)%symbolBits) % symbolBits
	for range padding {
		// An NRZI one is an idle bit: preserve the line level after the
		// closing flag while completing the final PSK symbol.
		levels = append(levels, level)
	}
	return levels
}

func appendFlags(levels []bool, count int) []bool {
	level := false
	if len(levels) != 0 {
		level = levels[len(levels)-1]
	}
	for range count {
		for bit := range 8 {
			if 0x7e&(1<<bit) == 0 {
				level = !level
			}
			levels = append(levels, level)
		}
	}
	return levels
}

func flagCount(milliseconds, baud int) int {
	if milliseconds <= 0 {
		return 1
	}
	bits := (milliseconds*baud + 999) / 1000
	flags := (bits + 7) / 8
	if flags < 1 {
		return 1
	}
	return flags
}

func modemTiming(mode string) (baud, symbolBits int) {
	switch mode {
	case "afsk300":
		return 300, 1
	case "afsk1200":
		return 1200, 1
	case "psk2400":
		return 2400, 2
	case "psk4800":
		return 4800, 3
	case "g3ruh9600", "ais":
		return 9600, 1
	case "eas":
		return 521, 1
	default:
		return 0, 1
	}
}

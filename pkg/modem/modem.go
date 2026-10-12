// SPDX-License-Identifier: GPL-2.0-or-later

// Package modem exposes Vulpes' streaming radio modem codecs for firmware and
// other applications that provide their own audio transport. Samples are
// signed 16-bit mono PCM; this package does not open sound devices or perform
// I/O. A Modem instance is stateful and must not be used concurrently.
package modem

import (
	"errors"
	"fmt"

	"github.com/sparques/vulpes/internal/hdlc"
	core "github.com/sparques/vulpes/internal/modem"
)

const (
	ModeAFSK300   = "afsk300"
	ModeAFSK1200  = "afsk1200"
	ModePSK2400   = "psk2400"
	ModePSK4800   = "psk4800"
	ModeG3RUH9600 = "g3ruh9600"
	ModeAIS       = "ais"
	ModeEAS       = "eas"

	DefaultMaximumFrameBytes = 2048
)

var (
	ErrReceiveOnly   = core.ErrReceiveOnly
	ErrInvalidFrame  = errors.New("frame must contain 1 or more bytes")
	ErrFrameTooLarge = errors.New("frame exceeds configured maximum")
)

// Settings configures one streaming modem. Amplitude defaults to 0.5,
// MaximumFrameBytes to 2048, and transmit delay/tail to 300/100 ms when zero.
// Mode defaults follow the Vulpes YAML defaults: A/9-slicer for AFSK, R/V26B
// for PSK2400, and V for PSK4800. AIS and EAS are receive-only.
type Settings struct {
	Mode              string
	SampleRate        int
	MarkHz            float64
	SpaceHz           float64
	Amplitude         float64
	Profile           string
	Variant           string
	Slicers           int
	Upsample          int
	MaximumFrameBytes int
	TXDelayMillis     int
	TXTailMillis      int
}

// EventKind identifies the received data family.
type EventKind uint8

const (
	EventFrame EventKind = iota + 1
	EventAIS
	EventEAS
)

// Event is a completed receive result. Payload contains the FCS-verified
// packet bytes for EventFrame and EventAIS. NMEA is populated for EventAIS;
// Text is populated for EventEAS SAME messages. Quality is 0..100.
type Event struct {
	Kind            EventKind
	Payload         []byte
	NMEA            string
	Text            string
	Quality         int
	CarrierDetected bool
}

// Modem owns streaming receive and transmit state for one configured mode.
// Keep one instance for the lifetime of the audio stream so demodulator timing
// and transmitter phase/NRZI state continue across calls.
type Modem struct {
	receiver      core.Receiver
	transmitter   core.Transmitter
	maximumFrame  int
	leadingFlags  int
	trailingFlags int
	symbolBits    int
	lastLevel     bool
}

// New constructs a modem for the requested mode and sample rate.
func New(settings Settings) (*Modem, error) {
	settings = applyDefaults(settings)
	maximumFrame := settings.MaximumFrameBytes
	if maximumFrame <= 0 {
		maximumFrame = DefaultMaximumFrameBytes
	}
	coreSettings := core.Settings{
		Mode: settings.Mode, SampleRate: settings.SampleRate,
		MarkHz: settings.MarkHz, SpaceHz: settings.SpaceHz,
		Amplitude: settings.Amplitude, Profile: settings.Profile,
		Variant: settings.Variant, Slicers: settings.Slicers,
		Upsample: settings.Upsample,
	}
	receiver, err := core.NewReceiver(coreSettings, maximumFrame)
	if err != nil {
		return nil, fmt.Errorf("create modem receiver: %w", err)
	}
	transmitter, txErr := core.NewTransmitter(coreSettings)
	if txErr != nil && !errors.Is(txErr, core.ErrReceiveOnly) {
		return nil, fmt.Errorf("create modem transmitter: %w", txErr)
	}
	baud, symbolBits := modeTiming(settings.Mode)
	if baud == 0 {
		return nil, fmt.Errorf("unsupported modem mode %q", settings.Mode)
	}
	return &Modem{
		receiver: receiver, transmitter: transmitter,
		maximumFrame:  maximumFrame,
		leadingFlags:  flagCount(settings.TXDelayMillis, baud),
		trailingFlags: flagCount(settings.TXTailMillis, baud),
		symbolBits:    symbolBits,
	}, nil
}

// ProcessAudio incrementally decodes signed 16-bit mono PCM samples and
// returns all complete messages recovered from this chunk. Pass chunks in
// order without resetting the modem between I2S reads.
func (m *Modem) ProcessAudio(samples []int16) []Event {
	internalEvents := m.receiver.Process(samples)
	events := make([]Event, 0, len(internalEvents))
	for _, event := range internalEvents {
		kind := EventFrame
		switch event.Kind {
		case core.EventAIS:
			kind = EventAIS
		case core.EventEAS:
			kind = EventEAS
		}
		events = append(events, Event{
			Kind: kind, Payload: event.Payload, NMEA: event.NMEA, Text: event.Text,
			Quality: event.Quality, CarrierDetected: event.CarrierDetected,
		})
	}
	return events
}

// Encode adds HDLC flags, bit stuffing, and FCS to a payload, then modulates it
// into signed 16-bit mono PCM samples. The returned slice can be sent to an
// I2S/DAC output. Consecutive calls preserve line/modulator state.
func (m *Modem) Encode(payload []byte) ([]int16, error) {
	if len(payload) == 0 {
		return nil, ErrInvalidFrame
	}
	if len(payload) > m.maximumFrame {
		return nil, fmt.Errorf("%w: %d bytes (limit %d)", ErrFrameTooLarge, len(payload), m.maximumFrame)
	}
	if m.transmitter == nil {
		return nil, ErrReceiveOnly
	}
	levels, finalLevel := hdlc.EncodeFrom(payload, m.leadingFlags, m.trailingFlags, m.lastLevel)
	levels = alignSymbolBoundary(levels, m.symbolBits)
	if len(levels) > 0 {
		m.lastLevel = levels[len(levels)-1]
	} else {
		m.lastLevel = finalLevel
	}
	return m.transmitter.Modulate(levels), nil
}

// CarrierDetected reports whether the receive modem currently detects a
// carrier. It can be used to drive a firmware's squelch or channel-busy logic.
func (m *Modem) CarrierDetected() bool { return m.receiver.CarrierDetected() }

// Reset clears receive, transmit, and NRZI history while retaining settings.
func (m *Modem) Reset() {
	m.receiver.Reset()
	if m.transmitter != nil {
		m.transmitter.Reset()
	}
	m.lastLevel = false
}

func applyDefaults(settings Settings) Settings {
	if settings.Amplitude == 0 {
		settings.Amplitude = 0.5
	}
	switch settings.Mode {
	case ModeAFSK300, ModeAFSK1200:
		if settings.Profile == "" {
			settings.Profile = "A"
		}
		if settings.Slicers == 0 {
			settings.Slicers = 9
		}
	case ModePSK2400:
		if settings.Profile == "" {
			settings.Profile = "R"
		}
		if settings.Variant == "" {
			settings.Variant = "V26B"
		}
	case ModePSK4800:
		if settings.Profile == "" {
			settings.Profile = "V"
		}
	}
	if settings.MaximumFrameBytes == 0 {
		settings.MaximumFrameBytes = DefaultMaximumFrameBytes
	}
	if settings.TXDelayMillis == 0 {
		settings.TXDelayMillis = 300
	}
	if settings.TXTailMillis == 0 {
		settings.TXTailMillis = 100
	}
	return settings
}

func modeTiming(mode string) (baud, symbolBits int) {
	switch mode {
	case ModeAFSK300:
		return 300, 1
	case ModeAFSK1200:
		return 1200, 1
	case ModePSK2400:
		return 2400, 2
	case ModePSK4800:
		return 4800, 3
	case ModeG3RUH9600, ModeAIS:
		return 9600, 1
	case ModeEAS:
		return 521, 1
	default:
		return 0, 1
	}
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

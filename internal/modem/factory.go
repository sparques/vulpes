// SPDX-License-Identifier: GPL-2.0-or-later

// Package modem provides mode selection and common streaming interfaces for
// the individual waveform implementations.
package modem

import (
	"errors"
	"fmt"

	"github.com/sparques/vulpes/internal/modem/afsk"
	"github.com/sparques/vulpes/internal/modem/ais"
	"github.com/sparques/vulpes/internal/modem/eas"
	"github.com/sparques/vulpes/internal/modem/g3ruh"
	"github.com/sparques/vulpes/internal/modem/psk"
)

var ErrUnsupportedMode = errors.New("unsupported modem mode")
var ErrReceiveOnly = errors.New("modem mode is receive-only")

// Settings is the subset of channel configuration used to construct a modem.
type Settings struct {
	Mode       string
	SampleRate int
	MarkHz     float64
	SpaceHz    float64
	Amplitude  float64
	Profile    string
	Variant    string
	Slicers    int
	Upsample   int
}

// EventKind identifies the output family carried by an Event.
type EventKind string

const (
	EventFrame EventKind = "frame"
	EventAIS   EventKind = "ais"
	EventEAS   EventKind = "eas"
)

// Event is a completed receiver output. Exactly one of Payload, NMEA, or Text
// is normally populated, according to Kind.
type Event struct {
	Kind            EventKind
	Payload         []byte
	NMEA            string
	Text            string
	Quality         int
	CarrierDetected bool
}

// Receiver consumes PCM incrementally and produces completed modem events.
type Receiver interface {
	Process(samples []int16) []Event
	CarrierDetected() bool
	Reset()
}

// Transmitter converts HDLC/NRZI line-level bits into PCM samples.
type Transmitter interface {
	Modulate(bits []bool) []int16
	Reset()
}

// NewReceiver constructs the configured receiver. maximumFrameBytes applies
// to HDLC modes and defaults according to the underlying decoder when zero.
func NewReceiver(settings Settings, maximumFrameBytes int) (Receiver, error) {
	switch settings.Mode {
	case "afsk300", "afsk1200":
		baud, mark, space := afskParameters(settings)
		receiver, err := afsk.NewReceiver(afsk.Config{
			SampleRate: settings.SampleRate, Baud: baud, Mark: mark, Space: space,
			Amplitude: settings.Amplitude, Profile: settings.Profile, Slicers: settings.Slicers,
		}, maximumFrameBytes)
		if err != nil {
			return nil, err
		}
		return receiverAdapter{
			process: func(samples []int16) []Event {
				frames := receiver.Process(samples)
				events := make([]Event, 0, len(frames))
				for _, frame := range frames {
					events = append(events, Event{Kind: EventFrame, Payload: frame.Payload, Quality: frame.Quality, CarrierDetected: frame.CarrierDetected})
				}
				return events
			}, carrier: receiver.CarrierDetected, reset: receiver.Reset,
		}, nil
	case "psk2400", "psk4800":
		mode, err := pskMode(settings)
		if err != nil {
			return nil, err
		}
		receiver, err := psk.NewReceiver(psk.Config{SampleRate: settings.SampleRate, Mode: mode, Amplitude: settings.Amplitude, Profile: settings.Profile}, maximumFrameBytes)
		if err != nil {
			return nil, err
		}
		return receiverAdapter{
			process: func(samples []int16) []Event {
				frames := receiver.Process(samples)
				events := make([]Event, 0, len(frames))
				for _, frame := range frames {
					events = append(events, Event{Kind: EventFrame, Payload: frame.Payload, Quality: frame.Quality, CarrierDetected: frame.CarrierDetected})
				}
				return events
			}, carrier: receiver.CarrierDetected, reset: receiver.Reset,
		}, nil
	case "g3ruh9600":
		receiver, err := g3ruh.NewReceiver(g3ruh.Config{SampleRate: settings.SampleRate, Amplitude: settings.Amplitude, Upsample: settings.Upsample}, maximumFrameBytes)
		if err != nil {
			return nil, err
		}
		return receiverAdapter{
			process: func(samples []int16) []Event {
				frames := receiver.Process(samples)
				events := make([]Event, 0, len(frames))
				for _, frame := range frames {
					events = append(events, Event{Kind: EventFrame, Payload: frame.Payload, Quality: frame.Quality, CarrierDetected: frame.CarrierDetected})
				}
				return events
			}, carrier: receiver.CarrierDetected, reset: receiver.Reset,
		}, nil
	case "ais":
		receiver, err := ais.NewReceiver(ais.Config{SampleRate: settings.SampleRate, Upsample: settings.Upsample}, maximumFrameBytes)
		if err != nil {
			return nil, err
		}
		return receiverAdapter{
			process: func(samples []int16) []Event {
				messages := receiver.Process(samples)
				events := make([]Event, 0, len(messages))
				for _, message := range messages {
					events = append(events, Event{Kind: EventAIS, Payload: message.Payload, NMEA: message.NMEA, Quality: message.Quality, CarrierDetected: message.CarrierDetected})
				}
				return events
			}, carrier: receiver.CarrierDetected, reset: receiver.Reset,
		}, nil
	case "eas":
		receiver, err := eas.NewReceiver(eas.Config{SampleRate: settings.SampleRate, Mark: settings.MarkHz, Space: settings.SpaceHz})
		if err != nil {
			return nil, err
		}
		return receiverAdapter{
			process: func(samples []int16) []Event {
				messages := receiver.Process(samples)
				events := make([]Event, 0, len(messages))
				for _, message := range messages {
					events = append(events, Event{Kind: EventEAS, Text: message.Text, Quality: message.Quality, CarrierDetected: message.CarrierDetected})
				}
				return events
			}, carrier: receiver.CarrierDetected, reset: receiver.Reset,
		}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedMode, settings.Mode)
	}
}

// NewTransmitter constructs a packet-mode transmitter. AIS and EAS are
// receive-only in the Vulpes scope.
func NewTransmitter(settings Settings) (Transmitter, error) {
	switch settings.Mode {
	case "afsk300", "afsk1200":
		baud, mark, space := afskParameters(settings)
		return afsk.NewModulator(afsk.Config{SampleRate: settings.SampleRate, Baud: baud, Mark: mark, Space: space, Amplitude: settings.Amplitude})
	case "psk2400", "psk4800":
		mode, err := pskMode(settings)
		if err != nil {
			return nil, err
		}
		return psk.NewModulator(psk.Config{SampleRate: settings.SampleRate, Mode: mode, Amplitude: settings.Amplitude, Profile: settings.Profile})
	case "g3ruh9600":
		return g3ruh.NewModulator(g3ruh.Config{SampleRate: settings.SampleRate, Amplitude: settings.Amplitude})
	case "ais", "eas":
		return nil, fmt.Errorf("%w: %s", ErrReceiveOnly, settings.Mode)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedMode, settings.Mode)
	}
}

type receiverAdapter struct {
	process func([]int16) []Event
	carrier func() bool
	reset   func()
}

func (r receiverAdapter) Process(samples []int16) []Event { return r.process(samples) }
func (r receiverAdapter) CarrierDetected() bool           { return r.carrier() }
func (r receiverAdapter) Reset()                          { r.reset() }

func afskParameters(settings Settings) (int, float64, float64) {
	baud, mark, space := 1200, 1200.0, 2200.0
	if settings.Mode == "afsk300" {
		baud, mark, space = 300, 1600, 1800
	}
	if settings.MarkHz != 0 {
		mark = settings.MarkHz
	}
	if settings.SpaceHz != 0 {
		space = settings.SpaceHz
	}
	return baud, mark, space
}

func pskMode(settings Settings) (psk.Mode, error) {
	if settings.Mode == "psk4800" {
		return psk.V274800, nil
	}
	switch settings.Variant {
	case "", "V26B":
		return psk.V26B2400, nil
	case "V26A":
		return psk.V26A2400, nil
	default:
		return 0, fmt.Errorf("unsupported PSK 2400 variant %q", settings.Variant)
	}
}

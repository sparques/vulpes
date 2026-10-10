// SPDX-License-Identifier: GPL-2.0-or-later

package psk

import (
	"errors"
	"math"
)

const carrierFrequency = 1800.0

var ErrInvalidConfig = errors.New("invalid PSK configuration")

// Config describes a Dire Wolf-compatible differential PSK waveform.
type Config struct {
	SampleRate int
	Mode       Mode
	Amplitude  float64
	Profile    string
}

// Validate checks the sample rate, mode, and output amplitude.
func (c Config) Validate() error {
	if c.SampleRate < 8000 || c.SampleRate > 192000 || carrierFrequency >= float64(c.SampleRate)/2 {
		return ErrInvalidConfig
	}
	if _, err := bitsPerSymbol(c.Mode); err != nil {
		return ErrInvalidConfig
	}
	if c.Amplitude < 0 || c.Amplitude > 1 || math.IsNaN(c.Amplitude) {
		return ErrInvalidConfig
	}
	if c.Profile != "" && !((c.Mode == V274800 && c.Profile == "V") || (c.Mode != V274800 && c.Profile == "R")) {
		return ErrInvalidConfig
	}
	return nil
}

// Modulator groups bits into symbols and generates Dire Wolf's phase-jump PSK
// waveform. Carrier and fractional symbol timing state span calls.
type Modulator struct {
	config         Config
	encoder        *Encoder
	carrierPhase   uint32
	carrierStep    uint32
	symbolRate     int
	symbols        int64
	emittedSamples int64
}

// NewModulator constructs a PSK modulator.
func NewModulator(config Config) (*Modulator, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	encoder, err := NewEncoder(config.Mode)
	if err != nil {
		return nil, err
	}
	width, _ := bitsPerSymbol(config.Mode)
	initialPhase := uint32(0)
	if config.Mode == V26A2400 || config.Mode == V26B2400 {
		initialPhase = 1 << 29
	}
	return &Modulator{
		config:       config,
		encoder:      encoder,
		carrierPhase: initialPhase,
		carrierStep:  uint32(math.Round(math.Ldexp(1, 32) * carrierFrequency / float64(config.SampleRate))),
		symbolRate:   bitRate(config.Mode) / width,
	}, nil
}

// Modulate consumes serial bits and emits samples for every complete symbol.
// Incomplete bit groups remain buffered for the next call.
func (m *Modulator) Modulate(bits []bool) []int16 {
	capacity := (len(bits)*m.config.SampleRate + bitRate(m.config.Mode) - 1) / bitRate(m.config.Mode)
	output := make([]int16, 0, capacity)
	for _, bit := range bits {
		symbol, ready := m.encoder.Push(bit)
		if !ready {
			continue
		}
		m.carrierPhase += uint32(symbol.PhaseDelta) << 29
		m.symbols++
		target := (m.symbols*int64(m.config.SampleRate) + int64(m.symbolRate) - 1) / int64(m.symbolRate)
		for range target - m.emittedSamples {
			m.carrierPhase += m.carrierStep
			angle := float64(m.carrierPhase>>24) * 2 * math.Pi / 256
			output = append(output, int16(math.Round(math.Sin(angle)*m.config.Amplitude*32767)))
		}
		m.emittedSamples = target
	}
	return output
}

// Reset discards pending bits and restores initial carrier/symbol timing.
func (m *Modulator) Reset() {
	m.encoder.Reset()
	m.carrierPhase = 0
	if m.config.Mode == V26A2400 || m.config.Mode == V26B2400 {
		m.carrierPhase = 1 << 29
	}
	m.symbols, m.emittedSamples = 0, 0
}

func bitRate(mode Mode) int {
	if mode == V274800 {
		return 4800
	}
	return 2400
}

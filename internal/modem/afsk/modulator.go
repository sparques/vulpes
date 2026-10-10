// SPDX-License-Identifier: GPL-2.0-or-later

// Package afsk implements audio-frequency-shift-keying modulation and
// demodulation.
package afsk

import (
	"errors"
	"fmt"
	"math"

	"github.com/sparques/vulpes/internal/dsp"
)

var ErrInvalidConfig = errors.New("invalid AFSK configuration")

// Config describes a two-tone AFSK signal. Mark is emitted for a true NRZI
// level and Space for a false level.
type Config struct {
	SampleRate int
	Baud       int
	Mark       float64
	Space      float64
	Amplitude  float64
	Profile    string
	Slicers    int
}

// Validate checks whether a configuration can be represented at its sample
// rate.
func (c Config) Validate() error {
	if c.SampleRate < 8000 || c.SampleRate > 192000 || c.Baud <= 0 || c.SampleRate/c.Baud < 2 {
		return ErrInvalidConfig
	}
	if c.Mark <= 0 || c.Space <= 0 || c.Mark >= float64(c.SampleRate)/2 || c.Space >= float64(c.SampleRate)/2 || c.Mark == c.Space {
		return ErrInvalidConfig
	}
	if c.Amplitude < 0 || c.Amplitude > 1 || math.IsNaN(c.Amplitude) {
		return ErrInvalidConfig
	}
	if c.Slicers < 0 || c.Slicers > maxSlicers {
		return ErrInvalidConfig
	}
	if c.Profile != "" && c.Profile != "A" && c.Profile != "B" {
		return ErrInvalidConfig
	}
	return nil
}

// Modulator converts NRZI line levels to phase-continuous signed 16-bit PCM.
// It retains carrier phase and fractional symbol timing across calls.
type Modulator struct {
	config         Config
	oscillator     *dsp.Oscillator
	symbols        int64
	emittedSamples int64
}

// NewModulator constructs a zero-phase modulator.
func NewModulator(config Config) (*Modulator, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %+v", err, config)
	}
	oscillator, err := dsp.NewOscillator(float64(config.SampleRate), config.Mark, 0)
	if err != nil {
		return nil, err
	}
	return &Modulator{config: config, oscillator: oscillator}, nil
}

// Modulate generates samples for levels and advances modulator state.
func (m *Modulator) Modulate(levels []bool) []int16 {
	capacity := (len(levels)*m.config.SampleRate + m.config.Baud - 1) / m.config.Baud
	output := make([]int16, 0, capacity)
	for _, level := range levels {
		frequency := m.config.Space
		if level {
			frequency = m.config.Mark
		}
		// Frequencies were validated at construction.
		_ = m.oscillator.SetFrequency(frequency)
		m.symbols++
		targetSamples := (m.symbols*int64(m.config.SampleRate) + int64(m.config.Baud) - 1) / int64(m.config.Baud)
		count := targetSamples - m.emittedSamples
		for range count {
			value := m.oscillator.Next() * m.config.Amplitude * 32767
			output = append(output, int16(math.Round(value)))
		}
		m.emittedSamples = targetSamples
	}
	return output
}

// Reset starts a new signal with zero carrier phase and symbol timing.
func (m *Modulator) Reset() {
	oscillator, _ := dsp.NewOscillator(float64(m.config.SampleRate), m.config.Mark, 0)
	m.oscillator = oscillator
	m.symbols = 0
	m.emittedSamples = 0
}

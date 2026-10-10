// SPDX-License-Identifier: GPL-2.0-or-later

package dsp

import (
	"errors"
	"math"
)

var ErrInvalidOscillator = errors.New("invalid oscillator parameters")

// Oscillator is a phase-continuous sine oscillator.
type Oscillator struct {
	sampleRate float64
	frequency  float64
	phase      float64
	step       float64
}

// NewOscillator constructs an oscillator. phase is measured in radians.
func NewOscillator(sampleRate, frequency, phase float64) (*Oscillator, error) {
	if sampleRate <= 0 || frequency < 0 || frequency >= sampleRate/2 || math.IsNaN(phase) || math.IsInf(phase, 0) {
		return nil, ErrInvalidOscillator
	}
	oscillator := &Oscillator{sampleRate: sampleRate, phase: wrapPhase(phase)}
	if err := oscillator.SetFrequency(frequency); err != nil {
		return nil, err
	}
	return oscillator, nil
}

// SetFrequency changes frequency without resetting phase.
func (o *Oscillator) SetFrequency(frequency float64) error {
	if frequency < 0 || frequency >= o.sampleRate/2 || math.IsNaN(frequency) || math.IsInf(frequency, 0) {
		return ErrInvalidOscillator
	}
	o.frequency = frequency
	o.step = 2 * math.Pi * frequency / o.sampleRate
	return nil
}

// Next returns the current sine value and advances by one sample.
func (o *Oscillator) Next() float64 {
	value := math.Sin(o.phase)
	o.phase = wrapPhase(o.phase + o.step)
	return value
}

// Phase returns the current phase in [0, 2*pi).
func (o *Oscillator) Phase() float64 { return o.phase }

func wrapPhase(phase float64) float64 {
	phase = math.Mod(phase, 2*math.Pi)
	if phase < 0 {
		phase += 2 * math.Pi
	}
	return phase
}

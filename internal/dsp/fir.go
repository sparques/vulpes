// SPDX-License-Identifier: GPL-2.0-or-later

// Package dsp contains reusable streaming digital-signal-processing building
// blocks for the Vulpes modems.
package dsp

import (
	"errors"
	"fmt"
	"math"
)

var ErrInvalidFilter = errors.New("invalid FIR filter parameters")

// Window selects the window applied to an ideal FIR impulse response.
type Window uint8

const (
	Hamming Window = iota
	Blackman
)

// LowPass designs an odd-length, unity-DC-gain low-pass FIR.
func LowPass(sampleRate, cutoff float64, taps int, window Window) ([]float64, error) {
	if sampleRate <= 0 || cutoff <= 0 || cutoff >= sampleRate/2 || taps < 3 || taps%2 == 0 {
		return nil, ErrInvalidFilter
	}
	if window != Hamming && window != Blackman {
		return nil, ErrInvalidFilter
	}
	coefficients := make([]float64, taps)
	middle := float64(taps-1) / 2
	normalized := cutoff / sampleRate
	var sum float64
	for i := range coefficients {
		x := float64(i) - middle
		ideal := 2 * normalized
		if x != 0 {
			ideal = math.Sin(2*math.Pi*normalized*x) / (math.Pi * x)
		}
		coefficients[i] = ideal * windowValue(window, i, taps)
		sum += coefficients[i]
	}
	if math.Abs(sum) < 1e-15 {
		return nil, fmt.Errorf("%w: zero filter gain", ErrInvalidFilter)
	}
	for i := range coefficients {
		coefficients[i] /= sum
	}
	return coefficients, nil
}

// HighPass designs an odd-length high-pass FIR by spectral inversion.
func HighPass(sampleRate, cutoff float64, taps int, window Window) ([]float64, error) {
	coefficients, err := LowPass(sampleRate, cutoff, taps, window)
	if err != nil {
		return nil, err
	}
	for i := range coefficients {
		coefficients[i] = -coefficients[i]
	}
	coefficients[len(coefficients)/2]++
	return coefficients, nil
}

// BandPass designs an odd-length band-pass FIR.
func BandPass(sampleRate, lowCutoff, highCutoff float64, taps int, window Window) ([]float64, error) {
	if lowCutoff <= 0 || lowCutoff >= highCutoff {
		return nil, ErrInvalidFilter
	}
	high, err := LowPass(sampleRate, highCutoff, taps, window)
	if err != nil {
		return nil, err
	}
	low, err := LowPass(sampleRate, lowCutoff, taps, window)
	if err != nil {
		return nil, err
	}
	for i := range high {
		high[i] -= low[i]
	}
	return high, nil
}

func windowValue(window Window, index, count int) float64 {
	phase := 2 * math.Pi * float64(index) / float64(count-1)
	switch window {
	case Hamming:
		return 0.54 - 0.46*math.Cos(phase)
	case Blackman:
		return 0.42 - 0.5*math.Cos(phase) + 0.08*math.Cos(2*phase)
	default:
		return math.NaN()
	}
}

// FIR applies a finite impulse response to a streaming signal.
type FIR struct {
	coefficients []float64
	delay        []float64
	position     int
}

// NewFIR constructs a filter with zero initial state.
func NewFIR(coefficients []float64) (*FIR, error) {
	if len(coefficients) == 0 {
		return nil, ErrInvalidFilter
	}
	for _, coefficient := range coefficients {
		if math.IsNaN(coefficient) || math.IsInf(coefficient, 0) {
			return nil, ErrInvalidFilter
		}
	}
	return &FIR{
		coefficients: append([]float64(nil), coefficients...),
		delay:        make([]float64, len(coefficients)),
	}, nil
}

// Process adds one sample and returns one filtered sample.
func (f *FIR) Process(sample float64) float64 {
	f.delay[f.position] = sample
	index := f.position
	var output float64
	for _, coefficient := range f.coefficients {
		output += coefficient * f.delay[index]
		index--
		if index < 0 {
			index = len(f.delay) - 1
		}
	}
	f.position++
	if f.position == len(f.delay) {
		f.position = 0
	}
	return output
}

// Reset clears all delayed samples.
func (f *FIR) Reset() {
	clear(f.delay)
	f.position = 0
}

// Magnitude returns the magnitude response of coefficients at frequency.
func Magnitude(coefficients []float64, sampleRate, frequency float64) float64 {
	var realPart, imaginaryPart float64
	for i, coefficient := range coefficients {
		phase := -2 * math.Pi * frequency * float64(i) / sampleRate
		realPart += coefficient * math.Cos(phase)
		imaginaryPart += coefficient * math.Sin(phase)
	}
	return math.Hypot(realPart, imaginaryPart)
}

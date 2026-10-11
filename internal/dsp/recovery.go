// SPDX-License-Identifier: GPL-2.0-or-later

package dsp

import (
	"errors"
	"math"
)

var ErrInvalidRecovery = errors.New("invalid recovery parameters")

// AGC tracks asymmetric peaks and maps their midpoint and span to zero and
// unit amplitude. Attack and decay are fractions in (0,1].
type AGC struct {
	attack, decay float64
	peak, valley  float64
}

func NewAGC(attack, decay float64) (*AGC, error) {
	if attack <= 0 || attack > 1 || decay <= 0 || decay > 1 || math.IsNaN(attack) || math.IsNaN(decay) {
		return nil, ErrInvalidRecovery
	}
	return &AGC{attack: attack, decay: decay}, nil
}

func (a *AGC) Process(sample float64) float64 {
	peakRate := a.decay
	if sample >= a.peak {
		peakRate = a.attack
	}
	a.peak += (sample - a.peak) * peakRate
	valleyRate := a.decay
	if sample <= a.valley {
		valleyRate = a.attack
	}
	a.valley += (sample - a.valley) * valleyRate
	span := a.peak - a.valley
	if span <= 1e-15 {
		return 0
	}
	return 2 * (sample - (a.peak+a.valley)/2) / span
}

func (a *AGC) Reset() { a.peak, a.valley = 0, 0 }

// Slicer converts samples into one decision per threshold. It supports the
// independent multi-slicer strategy used by Dire Wolf's A+ profiles.
type Slicer struct{ thresholds []float64 }

func NewSlicer(thresholds []float64) (*Slicer, error) {
	if len(thresholds) == 0 {
		return nil, ErrInvalidRecovery
	}
	for _, threshold := range thresholds {
		if math.IsNaN(threshold) || math.IsInf(threshold, 0) {
			return nil, ErrInvalidRecovery
		}
	}
	return &Slicer{thresholds: append([]float64(nil), thresholds...)}, nil
}

func (s *Slicer) Process(sample float64, decisions []bool) []bool {
	if cap(decisions) < len(s.thresholds) {
		decisions = make([]bool, len(s.thresholds))
	} else {
		decisions = decisions[:len(s.thresholds)]
	}
	for i, threshold := range s.thresholds {
		decisions[i] = sample >= threshold
	}
	return decisions
}

// SymbolPLL is a transition-corrected symbol clock. Advance returns true at
// the center of each symbol. Transition applies a bounded phase correction;
// locked loops normally use a smaller inertia than acquisition.
type SymbolPLL struct {
	step, phase, gain float64
}

func NewSymbolPLL(sampleRate, symbolRate, gain float64) (*SymbolPLL, error) {
	if sampleRate <= 0 || symbolRate <= 0 || symbolRate > sampleRate || gain < 0 || gain > 1 {
		return nil, ErrInvalidRecovery
	}
	return &SymbolPLL{step: symbolRate / sampleRate, gain: gain}, nil
}

func (p *SymbolPLL) Advance() bool {
	previous := p.phase
	p.phase += p.step
	if p.phase >= 1 {
		p.phase -= 1
	}
	return previous < .5 && p.phase >= .5 || previous > p.phase && (previous < .5 || p.phase >= .5)
}

func (p *SymbolPLL) Transition() {
	errorFromBoundary := p.phase
	if errorFromBoundary > .5 {
		errorFromBoundary -= 1
	}
	p.phase -= errorFromBoundary * p.gain
	if p.phase < 0 {
		p.phase++
	} else if p.phase >= 1 {
		p.phase--
	}
}

func (p *SymbolPLL) Reset() { p.phase = 0 }

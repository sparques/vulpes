// SPDX-License-Identifier: GPL-2.0-or-later

// Package psk implements the differential symbol and waveform processing used
// by Dire Wolf's 2400 bit/s V.26 and 4800 bit/s V.27 modems.
package psk

import "errors"

// Mode identifies the over-the-air differential PSK format.
type Mode uint8

const (
	V26A2400 Mode = iota
	V26B2400
	V274800
)

var ErrInvalidMode = errors.New("invalid PSK mode")

var (
	grayToPhaseV26 = [...]uint8{0, 1, 3, 2}
	grayToPhaseV27 = [...]uint8{1, 0, 2, 3, 6, 7, 5, 4}
	phaseToGrayV26 = [...]uint8{0, 1, 3, 2}
	phaseToGrayV27 = [...]uint8{1, 0, 2, 3, 7, 6, 4, 5}
)

// Symbol describes one completed symbol. PhaseDelta and AbsolutePhase are in
// 45-degree octants, modulo eight. Bits contains the source bits MSB first.
type Symbol struct {
	Bits          uint8
	PhaseDelta    uint8
	AbsolutePhase uint8
}

// Encoder groups a serial bit stream and applies Dire Wolf's differential
// Gray-to-phase mapping.
type Encoder struct {
	mode    Mode
	wanted  int
	pending uint8
	count   int
	phase   uint8
}

// NewEncoder constructs a differential symbol encoder. V.26 starts at Dire
// Wolf's arbitrary 45-degree initial phase; only phase differences carry data.
func NewEncoder(mode Mode) (*Encoder, error) {
	wanted, err := bitsPerSymbol(mode)
	if err != nil {
		return nil, err
	}
	phase := uint8(0)
	if mode == V26A2400 || mode == V26B2400 {
		phase = 1
	}
	return &Encoder{mode: mode, wanted: wanted, phase: phase}, nil
}

// Push consumes one bit and returns a symbol whenever its bit group completes.
func (e *Encoder) Push(bit bool) (Symbol, bool) {
	e.pending <<= 1
	if bit {
		e.pending |= 1
	}
	e.count++
	if e.count != e.wanted {
		return Symbol{}, false
	}
	bits := e.pending
	var delta uint8
	if e.wanted == 2 {
		delta = 2 * grayToPhaseV26[bits]
		if e.mode == V26B2400 {
			delta++
		}
	} else {
		delta = grayToPhaseV27[bits]
	}
	e.phase = (e.phase + delta) & 7
	e.pending, e.count = 0, 0
	return Symbol{Bits: bits, PhaseDelta: delta, AbsolutePhase: e.phase}, true
}

// Reset discards an incomplete symbol and restores the initial phase.
func (e *Encoder) Reset() {
	e.pending, e.count, e.phase = 0, 0, 0
	if e.mode == V26A2400 || e.mode == V26B2400 {
		e.phase = 1
	}
}

// DecodePhaseDelta reverses the differential mapping and returns source bits
// in transmission order (most significant first).
func DecodePhaseDelta(mode Mode, phaseDelta uint8) ([]bool, error) {
	wanted, err := bitsPerSymbol(mode)
	if err != nil {
		return nil, err
	}
	phaseDelta &= 7
	var value uint8
	if wanted == 2 {
		if mode == V26B2400 {
			phaseDelta = (phaseDelta - 1) & 7
		}
		value = phaseToGrayV26[(phaseDelta/2)&3]
	} else {
		value = phaseToGrayV27[phaseDelta]
	}
	result := make([]bool, wanted)
	for i := range result {
		result[i] = value&(1<<uint(wanted-1-i)) != 0
	}
	return result, nil
}

func bitsPerSymbol(mode Mode) (int, error) {
	switch mode {
	case V26A2400, V26B2400:
		return 2, nil
	case V274800:
		return 3, nil
	default:
		return 0, ErrInvalidMode
	}
}

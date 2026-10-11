// SPDX-License-Identifier: GPL-2.0-or-later

// Package eas receives Emergency Alert System SAME audio messages.
package eas

import (
	"math"

	"github.com/sparques/vulpes/internal/modem/afsk"
)

const (
	baud      = 521 // Dire Wolf uses 521 for filtering and 520.83 for the PLL.
	maxLength = 268
)

// Config describes the fixed EAS SAME AFSK receive mode. Zero tone frequencies
// select the Dire Wolf defaults (2083.3 Hz mark and 1562.5 Hz space).
type Config struct {
	SampleRate int
	Mark       float64
	Space      float64
}

// Message is a complete SAME header or end-of-message marker.
type Message struct {
	Text            string
	Quality         int
	CarrierDetected bool
}

// Receiver demodulates EAS tones and assembles SAME messages.
type Receiver struct {
	demodulator  *afsk.Demodulator
	decoder      decoder
	qualitySum   float64
	qualityCount int
}

// NewReceiver constructs the Dire Wolf profile-A EAS demodulator.
func NewReceiver(config Config) (*Receiver, error) {
	if config.Mark == 0 {
		config.Mark = 2083.33333333333
	}
	if config.Space == 0 {
		config.Space = 1562.5
	}
	demodulator, err := afsk.NewDemodulator(afsk.Config{
		SampleRate: config.SampleRate,
		Baud:       baud,
		Mark:       config.Mark,
		Space:      config.Space,
		Profile:    "A",
	})
	if err != nil {
		return nil, err
	}
	return &Receiver{demodulator: demodulator}, nil
}

// Process consumes PCM samples and returns complete SAME messages.
func (r *Receiver) Process(samples []int16) []Message {
	var messages []Message
	for _, decision := range r.demodulator.Process(samples) {
		r.qualitySum += math.Abs(decision.Confidence)
		r.qualityCount++
		if text, done := r.decoder.feed(decision.Level); done {
			quality := 0
			if r.qualityCount > 0 {
				quality = int(100 * r.qualitySum / float64(r.qualityCount))
			}
			messages = append(messages, Message{
				Text:            text,
				Quality:         quality,
				CarrierDetected: r.demodulator.CarrierDetected(),
			})
			r.qualitySum, r.qualityCount = 0, 0
		}
	}
	return messages
}

// CarrierDetected reports the AFSK transition-quality DCD state.
func (r *Receiver) CarrierDetected() bool { return r.demodulator.CarrierDetected() }

// Reset clears demodulation and SAME assembly state.
func (r *Receiver) Reset() {
	r.demodulator.Reset()
	r.decoder = decoder{}
	r.qualitySum, r.qualityCount = 0, 0
}

type decoder struct {
	acc             uint64
	bitCount        int
	frame           []byte
	gathering       bool
	plussign        bool
	dashesAfterPlus int
}

const (
	preambleZCZC = uint64(0x435a435aabababab)
	preambleNNNN = uint64(0x4e4e4e4eabababab)
)

func (d *decoder) feed(bit bool) (string, bool) {
	d.acc >>= 1
	if bit {
		d.acc |= 1 << 63
	}
	switch d.acc {
	case preambleZCZC:
		d.gathering = true
		d.plussign = false
		d.dashesAfterPlus = 0
		d.bitCount = 0
		d.frame = append(d.frame[:0], "ZCZC"...)
		return "", false
	case preambleNNNN:
		d.gathering = false
		d.bitCount = 0
		return "NNNN", true
	}
	if !d.gathering {
		return "", false
	}
	d.bitCount++
	if d.bitCount != 8 {
		return "", false
	}
	d.bitCount = 0
	ch := byte(d.acc >> 56)
	if !((ch >= ' ' && ch <= 0x7f) || ch == '\r' || ch == '\n') || len(d.frame) >= maxLength {
		d.gathering = false
		return "", false
	}
	d.frame = append(d.frame, ch)
	if ch == '+' {
		d.plussign = true
		d.dashesAfterPlus = 0
	} else if d.plussign && ch == '-' {
		d.dashesAfterPlus++
		if d.dashesAfterPlus == 3 {
			d.gathering = false
			return string(d.frame), true
		}
	}
	return "", false
}

// SPDX-License-Identifier: GPL-2.0-or-later

package g3ruh

import (
	"errors"
	"math"
)

const bitRate = 9600

var ErrInvalidModemConfig = errors.New("invalid G3RUH modem configuration")

// Config describes Dire Wolf-compatible 9600 bit/s G3RUH audio.
type Config struct {
	SampleRate int
	Amplitude  float64
	Upsample   int
}

func (c Config) Validate() error {
	if c.SampleRate < 8000 || c.SampleRate > 192000 || c.SampleRate < bitRate/2 {
		return ErrInvalidModemConfig
	}
	if c.Amplitude < 0 || c.Amplitude > 1 || math.IsNaN(c.Amplitude) {
		return ErrInvalidModemConfig
	}
	if c.Upsample < 0 || c.Upsample > 4 {
		return ErrInvalidModemConfig
	}
	return nil
}

// Modulator scrambles NRZI levels and creates Dire Wolf's baud/2 baseband
// waveform. Fractional samples per bit and scrambler/phase state span calls.
type Modulator struct {
	config         Config
	scrambler      Scrambler
	phase          uint32
	phaseStep      uint32
	previous       bool
	bits           int64
	emittedSamples int64
}

func NewModulator(config Config) (*Modulator, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Modulator{config: config, phaseStep: uint32(math.Round(math.Ldexp(1, 32) * float64(bitRate/2) / float64(config.SampleRate)))}, nil
}

// Modulate emits samples for the supplied NRZI line levels.
func (m *Modulator) Modulate(levels []bool) []int16 {
	output := make([]int16, 0, (len(levels)*m.config.SampleRate+bitRate-1)/bitRate)
	for _, level := range levels {
		dat := m.scrambler.Bit(level)
		m.bits++
		target := (m.bits*int64(m.config.SampleRate) + bitRate - 1) / bitRate
		for range target - m.emittedSamples {
			if dat != m.previous {
				m.phase += m.phaseStep
			} else if m.phase&0x80000000 != 0 {
				m.phase = 0xc0000000
			} else {
				m.phase = 0x40000000
			}
			angle := float64(m.phase>>24) * 2 * math.Pi / 256
			output = append(output, int16(math.Round(math.Sin(angle)*m.config.Amplitude*32767)))
		}
		m.emittedSamples = target
		m.previous = dat
	}
	return output
}

func (m *Modulator) Reset() {
	m.scrambler = Scrambler{}
	m.phase = 0
	m.previous = false
	m.bits, m.emittedSamples = 0, 0
}

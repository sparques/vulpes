// SPDX-License-Identifier: GPL-2.0-or-later

package g3ruh

import (
	"math"
	"math/bits"
)

// Decision is one sliced G3RUH NRZI line level and its normalized confidence.
type Decision struct {
	Level      bool
	Confidence float64
}

type clockState struct {
	pll, previousPLL        int32
	previousOutput          float64
	goodFlag, badFlag       bool
	goodHistory, badHistory uint32
	scoreHistory            uint32
	locked                  bool
}

// Demodulator closely follows Dire Wolf's demod_9600.c default receive path,
// including cosine-window FIR, polyphase upsampling, AGC, and transition PLL.
type Demodulator struct {
	config       Config
	upsample     int
	filterSize   int
	polyphase    [][]float64
	history      []float64
	peak, valley float64
	pllStep      uint32
	clock        clockState
}

func NewDemodulator(config Config) (*Demodulator, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	upsample := config.Upsample
	if upsample == 0 {
		upsample = defaultUpsample(config.SampleRate)
	}
	filterSize := int(float64(config.SampleRate)/bitRate + 0.5)
	totalTaps := filterSize * upsample
	cutoff := float64(bitRate) / float64(config.SampleRate*upsample)
	filter := make([]float64, totalTaps)
	center := float64(totalTaps-1) / 2
	var gain float64
	for i := range filter {
		x := float64(i) - center
		if x == 0 {
			filter[i] = 2 * cutoff
		} else {
			filter[i] = math.Sin(2*math.Pi*cutoff*x) / (math.Pi * x)
		}
		filter[i] *= math.Cos(x / float64(totalTaps) * math.Pi)
		gain += filter[i]
	}
	for i := range filter {
		filter[i] /= gain
	}
	polyphase := make([][]float64, upsample)
	for phase := range upsample {
		polyphase[phase] = make([]float64, filterSize)
		for tap := range filterSize {
			polyphase[phase][tap] = filter[tap*upsample+phase]
		}
	}
	return &Demodulator{
		config: config, upsample: upsample, filterSize: filterSize, polyphase: polyphase,
		history: make([]float64, filterSize),
		pllStep: uint32(math.Round(math.Ldexp(1, 32) * bitRate / float64(config.SampleRate*upsample))),
	}, nil
}

func (d *Demodulator) Process(samples []int16) []Decision {
	result := make([]Decision, 0, len(samples)*d.upsample*bitRate/d.config.SampleRate+1)
	for _, sample := range samples {
		copy(d.history[1:], d.history)
		d.history[0] = float64(sample) / 16384
		for _, filter := range d.polyphase {
			filtered := 0.0
			for i, coefficient := range filter {
				filtered += coefficient * d.history[i]
			}
			value := automaticGain(filtered, &d.peak, &d.valley)
			if decision, ready := d.processClock(value); ready {
				result = append(result, decision)
			}
		}
	}
	return result
}

func (d *Demodulator) CarrierDetected() bool { return d.clock.locked }

func (d *Demodulator) Reset() {
	clear(d.history)
	d.peak, d.valley = 0, 0
	d.clock = clockState{}
}

func (d *Demodulator) processClock(value float64) (Decision, bool) {
	c := &d.clock
	c.previousPLL = c.pll
	c.pll = int32(uint32(c.pll) + d.pllStep)
	ready := c.previousPLL > 1_000_000_000 && c.pll < -1_000_000_000
	decision := Decision{Level: value > 0, Confidence: math.Min(math.Abs(value), 1)}
	if ready {
		d.updateDCD()
	}
	if (c.previousOutput < 0 && value > 0) || (c.previousOutput > 0 && value < 0) {
		if c.pll > -dcdGoodWidthG3RUH && c.pll < dcdGoodWidthG3RUH {
			c.goodFlag = true
		} else {
			c.badFlag = true
		}
		target := float64(d.pllStep) * value / (value - c.previousOutput)
		inertia := 0.67
		if c.locked {
			inertia = 0.89
		}
		c.pll = int32(float64(c.pll)*inertia + target*(1-inertia))
	}
	c.previousOutput = value
	return decision, ready
}

const (
	dcdGoodWidthG3RUH    = int32(512 * 1024 * 1024)
	dcdThresholdOnG3RUH  = 30
	dcdThresholdOffG3RUH = 6
)

func (d *Demodulator) updateDCD() {
	c := &d.clock
	c.goodHistory = c.goodHistory<<1 | g3ruhBoolBit(c.goodFlag)
	c.badHistory = c.badHistory<<1 | g3ruhBoolBit(c.badFlag)
	c.goodFlag, c.badFlag = false, false
	c.scoreHistory <<= 1
	if bits.OnesCount32(c.goodHistory)-bits.OnesCount32(c.badHistory) >= 2 {
		c.scoreHistory |= 1
	}
	score := bits.OnesCount32(c.scoreHistory)
	if score >= dcdThresholdOnG3RUH {
		c.locked = true
	} else if score <= dcdThresholdOffG3RUH {
		c.locked = false
	}
}

func automaticGain(input float64, peak, valley *float64) float64 {
	if input >= *peak {
		*peak = input*0.080 + *peak*(1-0.080)
	} else {
		*peak = input*0.00012 + *peak*(1-0.00012)
	}
	if input <= *valley {
		*valley = input*0.080 + *valley*(1-0.080)
	} else {
		*valley = input*0.00012 + *valley*(1-0.00012)
	}
	if *peak <= *valley {
		return 0
	}
	return (input - 0.5*(*peak+*valley)) / (*peak - *valley)
}

func defaultUpsample(sampleRate int) int {
	ratio := float64(sampleRate) / bitRate
	switch {
	case ratio < 4:
		return 4
	case ratio < 10:
		return 3
	case ratio < 15:
		return 2
	default:
		return 1
	}
}

func g3ruhBoolBit(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

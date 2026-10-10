// SPDX-License-Identifier: GPL-2.0-or-later

package psk

import (
	"math"
	"math/bits"
)

// BitDecision is one recovered line-level bit and Dire Wolf-style confidence.
type BitDecision struct {
	Level      bool
	Confidence int
}

// Demodulator translates Dire Wolf's local-oscillator profile R (V.26) and V
// (V.27) receive paths.
type Demodulator struct {
	config                    Config
	width, symbolRate         int
	lowpass                   []float64
	inPhase, quadrature       sampleHistory
	phaseDelay                sampleHistory
	delay                     int
	loPhase, loStep, pllStep  uint32
	pll, previousPLL          int32
	previousSymbol            uint8
	locked, goodFlag, badFlag bool
	goodHistory, badHistory   uint32
	scoreHistory              uint32
	lockedInertia             float64
}

// NewDemodulator constructs the default Dire Wolf LO-based PSK receiver.
func NewDemodulator(config Config) (*Demodulator, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	width, _ := bitsPerSymbol(config.Mode)
	symbolRate := bitRate(config.Mode) / width
	filterWidth, cutoff := 1.007, 0.70
	window := pskWindowTruncated
	if config.Mode == V274800 {
		filterWidth, cutoff, window = 0.844, 0.85, pskWindowCosine
	}
	taps := int(math.Round(filterWidth * float64(config.SampleRate) / float64(symbolRate)))
	delayTaps := int(math.Round(1.25 * float64(config.SampleRate) / float64(symbolRate)))
	delay := int(math.Round(float64(config.SampleRate) / float64(symbolRate)))
	return &Demodulator{
		config: config, width: width, symbolRate: symbolRate,
		lowpass: pskLowpass(cutoff*float64(symbolRate)/float64(config.SampleRate), taps, window),
		inPhase: newSampleHistory(taps), quadrature: newSampleHistory(taps),
		phaseDelay: newSampleHistory(delayTaps), delay: delay,
		loStep:  phaseStep(carrierFrequency, config.SampleRate),
		pllStep: phaseStep(float64(symbolRate), config.SampleRate), lockedInertia: 0.925,
	}, nil
}

// Process consumes PCM and returns recovered bits in transmission order.
func (d *Demodulator) Process(samples []int16) []BitDecision {
	result := make([]BitDecision, 0, len(samples)*bitRate(d.config.Mode)/d.config.SampleRate+1)
	for _, sample := range samples {
		value := float64(sample) / 16384
		angle := float64(d.loPhase>>24) * 2 * math.Pi / 256
		d.inPhase.push(value * math.Cos(angle))
		d.quadrature.push(value * math.Sin(angle))
		inPhase := d.inPhase.convolve(d.lowpass)
		quadrature := d.quadrature.convolve(d.lowpass)
		phase := 0.0
		if inPhase != 0 || quadrature != 0 {
			phase = math.Atan2(inPhase, quadrature)
		}
		d.phaseDelay.push(phase)
		delta := phase - d.phaseDelay.values[d.delay]
		if d.config.Mode == V26B2400 {
			delta -= math.Pi / 4
		}
		symbol, quality := phaseShiftToSymbol(delta, d.width)
		result = append(result, d.advancePLL(symbol, quality)...)
		d.loPhase += d.loStep
	}
	return result
}

// CarrierDetected reports the hysteretic transition-quality DCD state.
func (d *Demodulator) CarrierDetected() bool { return d.locked }

// Reset clears all streaming receive state.
func (d *Demodulator) Reset() {
	d.inPhase.clear()
	d.quadrature.clear()
	d.phaseDelay.clear()
	d.loPhase, d.pll, d.previousPLL, d.previousSymbol = 0, 0, 0, 0
	d.locked, d.goodFlag, d.badFlag = false, false, false
	d.goodHistory, d.badHistory, d.scoreHistory = 0, 0, 0
}

func (d *Demodulator) advancePLL(symbol uint8, quality []int) []BitDecision {
	d.previousPLL = d.pll
	d.pll = int32(uint32(d.pll) + d.pllStep)
	var decisions []BitDecision
	if d.pll < 0 && d.previousPLL >= 0 {
		decisions = make([]BitDecision, d.width)
		for i := range decisions {
			bit := d.width - 1 - i
			decisions[i] = BitDecision{Level: symbol&(1<<bit) != 0, Confidence: quality[bit]}
		}
		d.updateDCD()
	}
	if symbol != d.previousSymbol {
		if d.pll > -dcdGoodWidthPSK && d.pll < dcdGoodWidthPSK {
			d.goodFlag = true
		} else {
			d.badFlag = true
		}
		inertia := 0.50
		if d.locked {
			inertia = d.lockedInertia
		}
		d.pll = int32(math.Floor(float64(d.pll) * inertia))
	}
	d.previousSymbol = symbol
	return decisions
}

const dcdGoodWidthPSK = int32(512 * 1024 * 1024)

func (d *Demodulator) updateDCD() {
	d.goodHistory = d.goodHistory<<1 | pskBoolBit(d.goodFlag)
	d.badHistory = d.badHistory<<1 | pskBoolBit(d.badFlag)
	d.goodFlag, d.badFlag = false, false
	d.scoreHistory <<= 1
	if bits.OnesCount32(d.goodHistory)-bits.OnesCount32(d.badHistory) >= 2 {
		d.scoreHistory |= 1
	}
	score := bits.OnesCount32(d.scoreHistory)
	if score >= 30 {
		d.locked = true
	} else if score <= 6 {
		d.locked = false
	}
}

func phaseShiftToSymbol(phase float64, width int) (uint8, []int) {
	states := 1 << width
	a := phase * float64(states) / (2 * math.Pi)
	for a >= float64(states) {
		a -= float64(states)
	}
	for a < 0 {
		a += float64(states)
	}
	i := int(a)
	if i == states {
		i = states - 1
	}
	fraction := a - float64(i)
	table := phaseToGrayV27[:]
	if width == 2 {
		table = phaseToGrayV26[:]
	}
	var result uint8
	quality := make([]int, width)
	for bit := range width {
		level := float64((table[i]>>bit)&1)*(1-fraction) + float64((table[(i+1)%states]>>bit)&1)*fraction
		if level >= 0.5 {
			result |= 1 << bit
		}
		quality[bit] = int(math.Round(200 * math.Abs(level-0.5)))
	}
	return result, quality
}

type sampleHistory struct{ values []float64 }

func newSampleHistory(size int) sampleHistory { return sampleHistory{values: make([]float64, size)} }
func (h *sampleHistory) push(value float64)   { copy(h.values[1:], h.values); h.values[0] = value }
func (h *sampleHistory) convolve(filter []float64) float64 {
	sum := 0.0
	for i, c := range filter {
		sum += c * h.values[i]
	}
	return sum
}
func (h *sampleHistory) clear() { clear(h.values) }

type pskWindow uint8

const (
	pskWindowTruncated pskWindow = iota
	pskWindowCosine
)

func pskLowpass(cutoff float64, size int, window pskWindow) []float64 {
	filter := make([]float64, size)
	center := 0.5 * float64(size-1)
	gain := 0.0
	for i := range filter {
		x := float64(i) - center
		if x == 0 {
			filter[i] = 2 * cutoff
		} else {
			filter[i] = math.Sin(2*math.Pi*cutoff*x) / (math.Pi * x)
		}
		if window == pskWindowCosine {
			filter[i] *= math.Cos(x / float64(size) * math.Pi)
		}
		gain += filter[i]
	}
	for i := range filter {
		filter[i] /= gain
	}
	return filter
}
func phaseStep(frequency float64, sampleRate int) uint32 {
	return uint32(math.Round(math.Ldexp(1, 32) * frequency / float64(sampleRate)))
}
func pskBoolBit(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

// SPDX-License-Identifier: GPL-2.0-or-later

package afsk

import (
	"math"
	"math/bits"
)

// Decision is one recovered NRZI level and its normalized mark-versus-space confidence.
type Decision struct {
	Level      bool
	Confidence float64
	Slice      int
}

type slicerState struct {
	pll, previousPLL        int32
	previousLevel, locked   bool
	goodFlag, badFlag       bool
	goodHistory, badHistory uint32
	scoreHistory            uint32
}

// Demodulator closely translates Dire Wolf's AFSK profile A and B receive
// paths, including their filters, discriminators, slicers, DCD, and DPLL.
type Demodulator struct {
	config                              Config
	profile                             string
	prefilter, lowpass                  []float64
	raw, markI, markQ                   firHistory
	spaceI, spaceQ                      firHistory
	markPhase, markDelta                uint32
	spacePhase, spaceDelta              uint32
	centerPhase, centerDelta            uint32
	previousCarrierPhase, normalizeRate float64
	markPeak, markValley                float64
	spacePeak, spaceValley              float64
	pllStep                             uint32
	slicers                             []slicerState
}

const (
	agcFastAttack       = 0.70
	agcSlowDecay        = 0.000090
	pllLockedInertia    = 0.74
	pllSearchingInertia = 0.50
	dcdGoodWidth        = int32(512 * 1024 * 1024)
	dcdThresholdOn      = 30
	dcdThresholdOff     = 6
	maxSlicers          = 9
	minimumSpaceGain    = 0.5
	maximumSpaceGain    = 4.0
)

// NewDemodulator constructs a Dire Wolf profile A or B AFSK demodulator.
func NewDemodulator(config Config) (*Demodulator, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	profile := config.Profile
	if profile == "" {
		profile = "A"
	}
	prefilterBaud, prefilterLength := 0.87, 1.857
	prefilterWindow := windowCosine
	if config.Baud > 600 {
		if profile == "B" {
			prefilterBaud, prefilterLength = 0.19, 8.163
		} else {
			prefilterBaud = 0.155
			prefilterLength = 383 * 1200.0 / 44100.0
		}
		prefilterWindow = windowTruncated
	}
	preTaps := int(prefilterLength*float64(config.SampleRate)/float64(config.Baud)) | 1
	rrcWidth, rrcRolloff := 2.80, 0.20
	if profile == "B" {
		rrcWidth, rrcRolloff = 2.00, 0.40
	}
	lpTaps := int(rrcWidth*float64(config.SampleRate)/float64(config.Baud)) | 1
	lower := math.Min(config.Mark, config.Space) - prefilterBaud*float64(config.Baud)
	upper := math.Max(config.Mark, config.Space) + prefilterBaud*float64(config.Baud)
	slicerCount := config.Slicers
	if slicerCount == 0 {
		slicerCount = 1
	}
	return &Demodulator{
		config:    config,
		profile:   profile,
		prefilter: bandpass(lower/float64(config.SampleRate), upper/float64(config.SampleRate), preTaps, prefilterWindow),
		lowpass:   rootRaisedCosine(lpTaps, rrcRolloff, float64(config.SampleRate)/float64(config.Baud)),
		raw:       newFIRHistory(preTaps), markI: newFIRHistory(lpTaps), markQ: newFIRHistory(lpTaps),
		spaceI: newFIRHistory(lpTaps), spaceQ: newFIRHistory(lpTaps),
		markDelta: phaseDelta(config.Mark, config.SampleRate), spaceDelta: phaseDelta(config.Space, config.SampleRate),
		centerDelta:   phaseDelta(0.5*(config.Mark+config.Space), config.SampleRate),
		normalizeRate: float64(config.SampleRate) / (math.Abs(config.Mark-config.Space) * math.Pi),
		pllStep:       phaseDelta(float64(config.Baud), config.SampleRate), slicers: make([]slicerState, slicerCount),
	}, nil
}

// Process consumes PCM and returns every decision made when the DPLL wraps.
func (d *Demodulator) Process(samples []int16) []Decision {
	decisions := make([]Decision, 0, (len(samples)*d.config.Baud/d.config.SampleRate+1)*len(d.slicers))
	for _, sample := range samples {
		value := float64(sample) / 16384
		d.raw.push(value)
		value = d.raw.convolve(d.prefilter)
		if d.profile == "B" {
			d.processProfileB(value, &decisions)
			continue
		}
		mc, ms := oscillator(d.markPhase)
		d.markPhase += d.markDelta
		d.markI.push(value * mc)
		d.markQ.push(value * ms)
		sc, ss := oscillator(d.spacePhase)
		d.spacePhase += d.spaceDelta
		d.spaceI.push(value * sc)
		d.spaceQ.push(value * ss)
		markAmplitude := math.Hypot(d.markI.convolve(d.lowpass), d.markQ.convolve(d.lowpass))
		spaceAmplitude := math.Hypot(d.spaceI.convolve(d.lowpass), d.spaceQ.convolve(d.lowpass))
		if len(d.slicers) == 1 {
			value = automaticGain(markAmplitude, &d.markPeak, &d.markValley) - automaticGain(spaceAmplitude, &d.spacePeak, &d.spaceValley)
			if decision, ok := d.advancePLL(0, value, 1); ok {
				decisions = append(decisions, decision)
			}
			continue
		}
		automaticGain(markAmplitude, &d.markPeak, &d.markValley)
		automaticGain(spaceAmplitude, &d.spacePeak, &d.spaceValley)
		gainStep := math.Pow(maximumSpaceGain/minimumSpaceGain, 1.0/(maxSlicers-1))
		spaceGain := minimumSpaceGain
		for slice := range d.slicers {
			value = markAmplitude - spaceAmplitude*spaceGain
			amplitude := 0.5 * (d.markPeak - d.markValley + (d.spacePeak-d.spaceValley)*spaceGain)
			if amplitude < 0.0000001 {
				amplitude = 1
			}
			if decision, ok := d.advancePLL(slice, value, amplitude); ok {
				decisions = append(decisions, decision)
			}
			spaceGain *= gainStep
		}
	}
	return decisions
}

func (d *Demodulator) processProfileB(value float64, decisions *[]Decision) {
	carrierCos, carrierSin := oscillator(d.centerPhase)
	d.centerPhase += d.centerDelta
	d.markI.push(value * carrierCos)
	d.markQ.push(value * carrierSin)
	inPhase := d.markI.convolve(d.lowpass)
	quadrature := d.markQ.convolve(d.lowpass)
	phase := math.Atan2(quadrature, inPhase)
	rate := phase - d.previousCarrierPhase
	if rate > math.Pi {
		rate -= 2 * math.Pi
	} else if rate < -math.Pi {
		rate += 2 * math.Pi
	}
	d.previousCarrierPhase = phase
	normalizedRate := rate * d.normalizeRate
	if len(d.slicers) == 1 {
		if decision, ok := d.advancePLL(0, normalizedRate, 1); ok {
			*decisions = append(*decisions, decision)
		}
		return
	}
	for slice := range d.slicers {
		offset := -0.5 + float64(slice)/float64(len(d.slicers)-1)
		if decision, ok := d.advancePLL(slice, normalizedRate+offset, 1); ok {
			*decisions = append(*decisions, decision)
		}
	}
}

// CarrierDetected reports whether Dire Wolf's transition-quality DCD score is
// currently above its lock threshold. The value has hysteresis and therefore
// remains stable through occasional poor transitions.
func (d *Demodulator) CarrierDetected() bool {
	for i := range d.slicers {
		if d.slicers[i].locked {
			return true
		}
	}
	return false
}

// Reset discards receive state while retaining generated filters.
func (d *Demodulator) Reset() {
	d.raw.clear()
	d.markI.clear()
	d.markQ.clear()
	d.spaceI.clear()
	d.spaceQ.clear()
	d.markPhase, d.spacePhase = 0, 0
	d.centerPhase, d.previousCarrierPhase = 0, 0
	d.markPeak, d.markValley, d.spacePeak, d.spaceValley = 0, 0, 0, 0
	d.pllStep = phaseDelta(float64(d.config.Baud), d.config.SampleRate)
	clear(d.slicers)
}

func (d *Demodulator) advancePLL(slice int, value, amplitude float64) (Decision, bool) {
	state := &d.slicers[slice]
	state.previousPLL = state.pll
	state.pll = int32(uint32(state.pll) + d.pllStep)
	ready := state.pll < 0 && state.previousPLL > 0
	decision := Decision{}
	if ready {
		confidence := min(math.Abs(value)/amplitude, 1)
		decision = Decision{Level: value > 0, Confidence: math.Copysign(confidence, value), Slice: slice}
		d.updateDCD(state)
	}
	level := value > 0
	if level != state.previousLevel {
		if state.pll > -dcdGoodWidth && state.pll < dcdGoodWidth {
			state.goodFlag = true
		} else {
			state.badFlag = true
		}
		inertia := pllSearchingInertia
		if state.locked {
			inertia = pllLockedInertia
		}
		state.pll = int32(float64(state.pll) * inertia)
	}
	state.previousLevel = level
	return decision, ready
}

func (d *Demodulator) updateDCD(state *slicerState) {
	state.goodHistory = state.goodHistory<<1 | boolBit(state.goodFlag)
	state.badHistory = state.badHistory<<1 | boolBit(state.badFlag)
	state.goodFlag, state.badFlag = false, false
	state.scoreHistory <<= 1
	if bits.OnesCount32(state.goodHistory)-bits.OnesCount32(state.badHistory) >= 2 {
		state.scoreHistory |= 1
	}
	score := bits.OnesCount32(state.scoreHistory)
	if score >= dcdThresholdOn {
		state.locked = true
	} else if score <= dcdThresholdOff {
		state.locked = false
	}
}

func automaticGain(input float64, peak, valley *float64) float64 {
	if input >= *peak {
		*peak = input*agcFastAttack + *peak*(1-agcFastAttack)
	} else {
		*peak = input*agcSlowDecay + *peak*(1-agcSlowDecay)
	}
	if input <= *valley {
		*valley = input*agcFastAttack + *valley*(1-agcFastAttack)
	} else {
		*valley = input*agcSlowDecay + *valley*(1-agcSlowDecay)
	}
	clipped := min(max(input, *valley), *peak)
	if *peak <= *valley {
		return 0
	}
	return (clipped - 0.5*(*peak+*valley)) / (*peak - *valley)
}

type firHistory struct{ values []float64 }

func newFIRHistory(size int) firHistory  { return firHistory{values: make([]float64, size)} }
func (h *firHistory) push(value float64) { copy(h.values[1:], h.values); h.values[0] = value }
func (h *firHistory) convolve(filter []float64) float64 {
	var sum float64
	for i, c := range filter {
		sum += c * h.values[i]
	}
	return sum
}
func (h *firHistory) clear() { clear(h.values) }

type windowType uint8

const (
	windowTruncated windowType = iota
	windowCosine
)

func window(kind windowType, size, index int) float64 {
	if kind == windowCosine {
		center := 0.5 * float64(size-1)
		return math.Cos((float64(index) - center) / float64(size) * math.Pi)
	}
	return 1
}

func bandpass(lower, upper float64, size int, kind windowType) []float64 {
	filter := make([]float64, size)
	center := 0.5 * float64(size-1)
	for i := range filter {
		offset := float64(i) - center
		if offset == 0 {
			filter[i] = 2 * (upper - lower)
		} else {
			filter[i] = (math.Sin(2*math.Pi*upper*offset) - math.Sin(2*math.Pi*lower*offset)) / (math.Pi * offset)
		}
		filter[i] *= window(kind, size, i)
	}
	angular := 2 * math.Pi * (lower + upper) / 2
	gain := 0.0
	for i, c := range filter {
		gain += 2 * c * math.Cos((float64(i)-center)*angular)
	}
	for i := range filter {
		filter[i] /= gain
	}
	return filter
}

func rootRaisedCosine(size int, rolloff, samplesPerSymbol float64) []float64 {
	filter := make([]float64, size)
	gain := 0.0
	for i := range filter {
		t := (float64(i) - float64(size-1)/2) / samplesPerSymbol
		sinc := 1.0
		if t <= -0.001 || t >= 0.001 {
			sinc = math.Sin(math.Pi*t) / (math.Pi * t)
		}
		taper := 0.0
		if a := math.Abs(rolloff * t); a > 0.499 && a < 0.501 {
			taper = math.Pi / 4
		} else {
			taper = math.Cos(math.Pi*rolloff*t) / (1 - math.Pow(2*rolloff*t, 2))
		}
		filter[i] = sinc * taper
		gain += filter[i]
	}
	for i := range filter {
		filter[i] /= gain
	}
	return filter
}

func phaseDelta(frequency float64, sampleRate int) uint32 {
	return uint32(math.Round(math.Ldexp(1, 32) * frequency / float64(sampleRate)))
}
func oscillator(phase uint32) (float64, float64) {
	angle := float64(phase>>24) * 2 * math.Pi / 256
	return math.Cos(angle), math.Sin(angle)
}
func boolBit(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

// SPDX-License-Identifier: GPL-2.0-or-later

package afsk

import (
	"math"
	"testing"
)

func TestFractionalSamplesPerSymbol(t *testing.T) {
	modulator, err := NewModulator(Config{
		SampleRate: 44100,
		Baud:       1200,
		Mark:       1200,
		Space:      2200,
		Amplitude:  0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	levels := make([]bool, 1200)
	first := modulator.Modulate(levels[:137])
	second := modulator.Modulate(levels[137:])
	if got := len(first) + len(second); got != 44100 {
		t.Fatalf("sample count = %d, want 44100", got)
	}
}

func TestMarkAndSpaceFrequencies(t *testing.T) {
	const sampleRate = 48000
	config := Config{SampleRate: sampleRate, Baud: 1200, Mark: 1200, Space: 2200, Amplitude: 0.8}
	for _, test := range []struct {
		name      string
		level     bool
		frequency float64
	}{
		{name: "mark", level: true, frequency: config.Mark},
		{name: "space", level: false, frequency: config.Space},
	} {
		t.Run(test.name, func(t *testing.T) {
			modulator, err := NewModulator(config)
			if err != nil {
				t.Fatal(err)
			}
			levels := make([]bool, 120)
			for i := range levels {
				levels[i] = test.level
			}
			samples := modulator.Modulate(levels)
			atTone := correlation(samples, sampleRate, test.frequency)
			atOther := correlation(samples, sampleRate, config.Mark+config.Space-test.frequency)
			if atTone < 100*atOther {
				t.Fatalf("tone correlation %g is not dominant over %g", atTone, atOther)
			}
		})
	}
}

func TestPhaseIsContinuousAcrossFrequencyChange(t *testing.T) {
	modulator, err := NewModulator(Config{SampleRate: 48000, Baud: 1200, Mark: 1300, Space: 2200, Amplitude: 1})
	if err != nil {
		t.Fatal(err)
	}
	samples := modulator.Modulate([]bool{true, false})
	// There must be no reset-to-zero discontinuity at the 40-sample boundary.
	if samples[40] == 0 {
		t.Fatal("carrier phase appears to reset at a symbol boundary")
	}
}

func correlation(samples []int16, sampleRate int, frequency float64) float64 {
	var inPhase, quadrature float64
	for i, sample := range samples {
		phase := 2 * math.Pi * frequency * float64(i) / float64(sampleRate)
		inPhase += float64(sample) * math.Cos(phase)
		quadrature += float64(sample) * math.Sin(phase)
	}
	return math.Hypot(inPhase, quadrature)
}

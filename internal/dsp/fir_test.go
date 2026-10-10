// SPDX-License-Identifier: GPL-2.0-or-later

package dsp

import (
	"math"
	"testing"
)

func TestLowPassResponse(t *testing.T) {
	taps, err := LowPass(48000, 3000, 101, Hamming)
	if err != nil {
		t.Fatal(err)
	}
	if got := Magnitude(taps, 48000, 0); math.Abs(got-1) > 1e-12 {
		t.Fatalf("DC magnitude = %g, want 1", got)
	}
	if got := Magnitude(taps, 48000, 1000); got < 0.98 {
		t.Fatalf("passband magnitude = %g, want >= 0.98", got)
	}
	if got := Magnitude(taps, 48000, 8000); got > 0.002 {
		t.Fatalf("stopband magnitude = %g, want <= 0.002", got)
	}
	for i := range taps {
		if math.Abs(taps[i]-taps[len(taps)-1-i]) > 1e-15 {
			t.Fatalf("coefficient %d is not symmetric", i)
		}
	}
}

func TestHighPassResponse(t *testing.T) {
	taps, err := HighPass(48000, 3000, 101, Blackman)
	if err != nil {
		t.Fatal(err)
	}
	if got := Magnitude(taps, 48000, 0); got > 1e-12 {
		t.Fatalf("DC magnitude = %g, want approximately zero", got)
	}
	if got := Magnitude(taps, 48000, 10000); got < 0.99 {
		t.Fatalf("passband magnitude = %g, want >= 0.99", got)
	}
}

func TestBandPassResponse(t *testing.T) {
	taps, err := BandPass(48000, 1000, 3000, 151, Blackman)
	if err != nil {
		t.Fatal(err)
	}
	if got := Magnitude(taps, 48000, 2000); got < 0.98 {
		t.Fatalf("passband magnitude = %g, want >= 0.98", got)
	}
	for _, frequency := range []float64{0, 8000} {
		if got := Magnitude(taps, 48000, frequency); got > 0.002 {
			t.Fatalf("magnitude at %g Hz = %g, want <= 0.002", frequency, got)
		}
	}
}

func TestFIRImpulseAndReset(t *testing.T) {
	coefficients := []float64{0.25, 0.5, 0.25}
	filter, err := NewFIR(coefficients)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []float64{0.25, 0.5, 0.25, 0} {
		input := 0.0
		if i == 0 {
			input = 1
		}
		if got := filter.Process(input); got != want {
			t.Fatalf("output %d = %g, want %g", i, got, want)
		}
	}
	filter.Process(1)
	filter.Reset()
	if got := filter.Process(0); got != 0 {
		t.Fatalf("output after reset = %g", got)
	}
}

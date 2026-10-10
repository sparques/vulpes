// SPDX-License-Identifier: GPL-2.0-or-later

package dsp

import (
	"math"
	"testing"
)

func TestOscillatorPeriodAndPhaseContinuity(t *testing.T) {
	oscillator, err := NewOscillator(48000, 1200, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := oscillator.Next(); math.Abs(got) > 1e-15 {
		t.Fatalf("first sample = %g, want zero", got)
	}
	for range 39 {
		oscillator.Next()
	}
	if math.Abs(oscillator.Phase()) > 1e-12 && math.Abs(oscillator.Phase()-2*math.Pi) > 1e-12 {
		t.Fatalf("phase after one period = %g", oscillator.Phase())
	}
	before := oscillator.Phase()
	if err := oscillator.SetFrequency(2200); err != nil {
		t.Fatal(err)
	}
	if oscillator.Phase() != before {
		t.Fatal("changing frequency reset phase")
	}
}

func TestOscillatorRejectsNyquist(t *testing.T) {
	if _, err := NewOscillator(48000, 24000, 0); err == nil {
		t.Fatal("NewOscillator accepted Nyquist frequency")
	}
}

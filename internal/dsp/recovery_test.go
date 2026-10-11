// SPDX-License-Identifier: GPL-2.0-or-later

package dsp

import "testing"

func TestAGCAndSlicer(t *testing.T) {
	agc, err := NewAGC(.5, .01)
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		agc.Process(2)
		agc.Process(-2)
	}
	if got := agc.Process(2); got < .8 || got > 1.2 {
		t.Fatalf("normalized peak = %v", got)
	}
	slicer, err := NewSlicer([]float64{-.5, 0, .5})
	if err != nil {
		t.Fatal(err)
	}
	got := slicer.Process(.25, nil)
	want := []bool{true, true, false}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("decisions = %v, want %v", got, want)
		}
	}
}

func TestSymbolPLLRateAndCorrection(t *testing.T) {
	pll, err := NewSymbolPLL(48000, 1200, .25)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for sample := range 48000 {
		if sample%40 == 0 {
			pll.Transition()
		}
		if pll.Advance() {
			count++
		}
	}
	if count < 1199 || count > 1201 {
		t.Fatalf("symbol decisions = %d", count)
	}
}

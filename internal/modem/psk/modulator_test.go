// SPDX-License-Identifier: GPL-2.0-or-later

package psk

import (
	"slices"
	"testing"
)

func TestModulatorFractionalTimingAndChunking(t *testing.T) {
	for _, mode := range []Mode{V26A2400, V26B2400, V274800} {
		config := Config{SampleRate: 44100, Mode: mode, Amplitude: 0.5}
		bits := make([]bool, bitRate(mode))
		for i := range bits {
			bits[i] = i%3 != 0
		}
		whole, err := NewModulator(config)
		if err != nil {
			t.Fatal(err)
		}
		want := whole.Modulate(bits)
		if len(want) != config.SampleRate {
			t.Fatalf("mode %d sample count = %d, want %d", mode, len(want), config.SampleRate)
		}
		chunked, err := NewModulator(config)
		if err != nil {
			t.Fatal(err)
		}
		var got []int16
		for offset := 0; offset < len(bits); {
			end := min(offset+137, len(bits))
			got = append(got, chunked.Modulate(bits[offset:end])...)
			offset = end
		}
		if !slices.Equal(got, want) {
			t.Fatalf("mode %d chunked waveform differs", mode)
		}
	}
}

func TestModulatorRejectsInvalidConfig(t *testing.T) {
	if _, err := NewModulator(Config{SampleRate: 48000, Mode: 99, Amplitude: 0.5}); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

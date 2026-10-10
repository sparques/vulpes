// SPDX-License-Identifier: GPL-2.0-or-later

package psk

import (
	"errors"
	"testing"
)

func TestDireWolfPhaseMappings(t *testing.T) {
	tests := []struct {
		mode Mode
		want []uint8
	}{
		{mode: V26A2400, want: []uint8{0, 2, 6, 4}},
		{mode: V26B2400, want: []uint8{1, 3, 7, 5}},
		{mode: V274800, want: []uint8{1, 0, 2, 3, 6, 7, 5, 4}},
	}
	for _, test := range tests {
		encoder, err := NewEncoder(test.mode)
		if err != nil {
			t.Fatal(err)
		}
		width, _ := bitsPerSymbol(test.mode)
		for value, wantDelta := range test.want {
			var symbol Symbol
			for bit := width - 1; bit >= 0; bit-- {
				var ready bool
				symbol, ready = encoder.Push(value&(1<<bit) != 0)
				if ready != (bit == 0) {
					t.Fatalf("mode %d value %d readiness at bit %d = %v", test.mode, value, bit, ready)
				}
			}
			if symbol.Bits != uint8(value) || symbol.PhaseDelta != wantDelta {
				t.Fatalf("mode %d value %d symbol = %+v, want delta %d", test.mode, value, symbol, wantDelta)
			}
			decoded, err := DecodePhaseDelta(test.mode, symbol.PhaseDelta)
			if err != nil {
				t.Fatal(err)
			}
			for i, bit := range decoded {
				want := value&(1<<(width-1-i)) != 0
				if bit != want {
					t.Fatalf("mode %d value %d decoded bit %d = %v, want %v", test.mode, value, i, bit, want)
				}
			}
		}
	}
}

func TestInvalidMode(t *testing.T) {
	if _, err := NewEncoder(99); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("NewEncoder error = %v", err)
	}
	if _, err := DecodePhaseDelta(99, 0); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("DecodePhaseDelta error = %v", err)
	}
}

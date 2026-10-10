// SPDX-License-Identifier: GPL-2.0-or-later

package g3ruh

import (
	"math/rand"
	"testing"
)

func TestScramblerRoundTrip(t *testing.T) {
	random := rand.New(rand.NewSource(2))
	var scrambler Scrambler
	var descrambler Descrambler
	for i := 0; i < 100_000; i++ {
		input := random.Intn(2) != 0
		if got := descrambler.Bit(scrambler.Bit(input)); got != input {
			t.Fatalf("bit %d = %t, want %t", i, got, input)
		}
	}
}

func TestScramblerStateSpansChunks(t *testing.T) {
	input := make([]bool, 1000)
	for i := range input {
		input[i] = i%3 == 0 || i%17 == 0
	}

	var continuous Scrambler
	want := scramble(&continuous, input)
	var chunked Scrambler
	got := append(scramble(&chunked, input[:137]), scramble(&chunked, input[137:])...)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bit %d differs across chunk boundary", i)
		}
	}
}

func TestInitialElevenBitsPassThrough(t *testing.T) {
	input := []bool{true, false, true, true, false, false, true, false, true, false, true}
	var scrambler Scrambler
	got := scramble(&scrambler, input)
	for i := range input {
		if got[i] != input[i] {
			t.Fatalf("bit %d = %t, want %t", i, got[i], input[i])
		}
	}
}

func scramble(scrambler *Scrambler, input []bool) []bool {
	output := make([]bool, len(input))
	for i, bit := range input {
		output[i] = scrambler.Bit(bit)
	}
	return output
}

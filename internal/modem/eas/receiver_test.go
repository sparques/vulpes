// SPDX-License-Identifier: GPL-2.0-or-later

package eas

import "testing"

func TestDecoderHeaderAndEndMarker(t *testing.T) {
	var d decoder
	feedWord(t, &d, preambleZCZC, false)
	want := "ZCZC-EAS-RWT-012001+0030-1231200-N0CALL-"
	var got string
	for _, ch := range []byte(want[4:]) {
		for bit := 0; bit < 8; bit++ {
			if text, done := d.feed(ch&(1<<bit) != 0); done {
				got = text
			}
		}
	}
	if got != want {
		t.Fatalf("decoded header = %q, want %q", got, want)
	}

	d = decoder{}
	feedWord(t, &d, preambleNNNN, true)
}

func TestDecoderRejectsInvalidCharacter(t *testing.T) {
	var d decoder
	feedWord(t, &d, preambleZCZC, false)
	for bit := 0; bit < 8; bit++ {
		if _, done := d.feed(byte(1)&(1<<bit) != 0); done {
			t.Fatal("invalid character completed a message")
		}
	}
	if d.gathering {
		t.Fatal("decoder remained active after invalid character")
	}
}

func feedWord(t *testing.T, decoder *decoder, word uint64, expectDone bool) {
	t.Helper()
	var text string
	var done bool
	for bit := 0; bit < 64; bit++ {
		text, done = decoder.feed(word&(uint64(1)<<bit) != 0)
	}
	if done != expectDone {
		t.Fatalf("word %#x done = %v, want %v (text %q)", word, done, expectDone, text)
	}
	if expectDone && text != "NNNN" {
		t.Fatalf("end marker = %q", text)
	}
}

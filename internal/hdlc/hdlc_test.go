// SPDX-License-Identifier: GPL-2.0-or-later

package hdlc

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestFCSCheckVector(t *testing.T) {
	data := []byte("123456789")
	if got, want := FCS(data), uint16(0x906e); got != want {
		t.Fatalf("FCS = %#04x, want %#04x", got, want)
	}
	frame := AppendFCS(data)
	if got, want := frame[len(frame)-2:], []byte{0x6e, 0x90}; !bytes.Equal(got, want) {
		t.Fatalf("encoded FCS = %x, want %x", got, want)
	}
	if !ValidFCS(frame) {
		t.Fatal("ValidFCS rejected check vector")
	}
	frame[3] ^= 1
	if ValidFCS(frame) {
		t.Fatal("ValidFCS accepted corrupted data")
	}
}

func TestRoundTrip(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	for size := 0; size <= 512; size++ {
		payload := make([]byte, size)
		if _, err := random.Read(payload); err != nil {
			t.Fatal(err)
		}
		levels := Encode(payload, 2, 1)
		decoder := NewDecoder(1024)
		var frames [][]byte
		for _, level := range levels {
			frames = append(frames, decoder.Feed(level)...)
		}
		if len(frames) != 1 {
			t.Fatalf("size %d: decoded %d frames, stats %+v", size, len(frames), decoder.Stats())
		}
		if !bytes.Equal(frames[0], payload) {
			t.Fatalf("size %d: payload mismatch", size)
		}
	}
}

func TestBackToBackFrames(t *testing.T) {
	first := Encode([]byte{0xff, 0xf8, 0x7e}, 2, 1)
	second := Encode([]byte("second"), 1, 1)
	// Encode starts each standalone waveform from the low NRZI level. Preserve
	// line state when joining the independently generated waveforms.
	if first[len(first)-1] {
		for i := range second {
			second[i] = !second[i]
		}
	}
	levels := append(first, second...)

	decoder := NewDecoder(64)
	var frames [][]byte
	for _, level := range levels {
		frames = append(frames, decoder.Feed(level)...)
	}
	if len(frames) != 2 {
		t.Fatalf("decoded %d frames, want 2; stats %+v", len(frames), decoder.Stats())
	}
}

func TestCorruptionRejected(t *testing.T) {
	levels := Encode([]byte("corrupt me"), 2, 1)
	levels[len(levels)/2] = !levels[len(levels)/2]

	decoder := NewDecoder(64)
	var frames [][]byte
	for _, level := range levels {
		frames = append(frames, decoder.Feed(level)...)
	}
	if len(frames) != 0 {
		t.Fatalf("decoded corrupted frame: %x", frames[0])
	}
	stats := decoder.Stats()
	if stats.BadFCS+stats.Invalid == 0 {
		t.Fatalf("corruption was not counted: %+v", stats)
	}
}

func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte("vulpes"))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0x00})
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 2048 {
			t.Skip()
		}
		decoder := NewDecoder(2048)
		var frames [][]byte
		for _, level := range Encode(payload, 2, 2) {
			frames = append(frames, decoder.Feed(level)...)
		}
		if len(frames) != 1 || !bytes.Equal(frames[0], payload) {
			t.Fatalf("round trip failed: frames=%d stats=%+v", len(frames), decoder.Stats())
		}
	})
}

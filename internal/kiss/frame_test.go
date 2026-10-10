// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"bytes"
	"reflect"
	"testing"
)

func TestEncodeVector(t *testing.T) {
	frame := Frame{Port: 3, Command: DataFrame, Payload: []byte{1, FEND, 2, FESC, 3}}
	want := []byte{FEND, 0x30, 1, FESC, TFEND, 2, FESC, TFESC, 3, FEND}
	if got := Encode(frame); !bytes.Equal(got, want) {
		t.Fatalf("Encode = % x, want % x", got, want)
	}
}

func TestDecoderEveryFragmentation(t *testing.T) {
	want := Frame{Port: 15, Command: SetHardware, Payload: []byte{0, FEND, FESC, 0xff}}
	wire := Encode(want)
	for split := 0; split <= len(wire); split++ {
		decoder := NewDecoder(32)
		got := decoder.Feed(wire[:split])
		got = append(got, decoder.Feed(wire[split:])...)
		if !reflect.DeepEqual(got, []Frame{want}) {
			t.Fatalf("split %d: got %#v, want %#v", split, got, []Frame{want})
		}
	}
}

func TestDecoderRejectsMalformedAndRecovers(t *testing.T) {
	valid := Frame{Port: 1, Command: TXDelay, Payload: []byte{30}}
	wire := append([]byte{FEND, 0, FESC, 0x42, 1, FEND}, Encode(valid)...)
	decoder := NewDecoder(32)
	got := decoder.Feed(wire)
	if !reflect.DeepEqual(got, []Frame{valid}) {
		t.Fatalf("got %#v, want %#v", got, []Frame{valid})
	}
	if decoder.Stats().MalformedEscape != 1 {
		t.Fatalf("stats = %+v", decoder.Stats())
	}
}

func TestDecoderBoundsInput(t *testing.T) {
	decoder := NewDecoder(4)
	decoder.Feed([]byte{FEND, 0, 1, 2, 3, 4, FEND})
	if decoder.Stats().Oversize != 1 {
		t.Fatalf("stats = %+v", decoder.Stats())
	}
}

func FuzzRoundTrip(f *testing.F) {
	f.Add(byte(0), byte(DataFrame), []byte("vulpes"))
	f.Add(byte(15), byte(SetHardware), []byte{FEND, FESC})
	f.Fuzz(func(t *testing.T, port, command byte, payload []byte) {
		if len(payload) > 2048 {
			t.Skip()
		}
		want := Frame{Port: port & 0x0f, Command: Command(command & 0x0f), Payload: payload}
		decoder := NewDecoder(2049)
		got := decoder.Feed(Encode(want))
		if len(got) != 1 || got[0].Port != want.Port || got[0].Command != want.Command || !bytes.Equal(got[0].Payload, want.Payload) {
			t.Fatalf("got %#v, want %#v", got, []Frame{want})
		}
	})
}

func FuzzDecoder(f *testing.F) {
	f.Add([]byte{FEND, 0, FEND})
	f.Add([]byte{FEND, FESC, 0, FEND})
	f.Fuzz(func(t *testing.T, input []byte) {
		decoder := NewDecoder(2049)
		for _, frame := range decoder.Feed(input) {
			if frame.Port > 15 || frame.Command > 15 || len(frame.Payload) > 2048 {
				t.Fatalf("invalid decoded frame: %#v", frame)
			}
		}
	})
}

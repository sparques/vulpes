// SPDX-License-Identifier: GPL-2.0-or-later

package ax25

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestUIFrameWireVector(t *testing.T) {
	frame := Frame{
		Addresses: []Address{
			{Callsign: "APRS"},
			{Callsign: "N0CALL", SSID: 7, Flag: true},
			{Callsign: "WIDE1", SSID: 1, Flag: true},
		},
		Control:     0x03,
		PID:         []byte{0xf0},
		Information: []byte("test"),
	}
	want := []byte{
		0x82, 0xa0, 0xa4, 0xa6, 0x40, 0x40, 0x60,
		0x9c, 0x60, 0x86, 0x82, 0x98, 0x98, 0xee,
		0xae, 0x92, 0x88, 0x8a, 0x62, 0x40, 0xe3,
		0x03, 0xf0, 't', 'e', 's', 't',
	}
	encoded, err := frame.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("encoded frame:\n got % x\nwant % x", encoded, want)
	}
	decoded, err := Parse(want)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, frame) {
		t.Fatalf("decoded frame = %#v, want %#v", decoded, frame)
	}
}

func TestSupervisoryFrameHasNoPID(t *testing.T) {
	wire, err := (Frame{
		Addresses: []Address{{Callsign: "DEST"}, {Callsign: "SOURCE"}},
		Control:   0x01,
	}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Parse(wire)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.PID) != 0 || len(decoded.Information) != 0 {
		t.Fatalf("unexpected payload: %#v", decoded)
	}
}

func TestEscapedPID(t *testing.T) {
	frame := Frame{
		Addresses:   []Address{{Callsign: "DEST"}, {Callsign: "SOURCE"}},
		Control:     0x00,
		PID:         []byte{0xff, 0x42},
		Information: []byte{1, 2, 3},
	}
	wire, err := frame.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Parse(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, frame) {
		t.Fatalf("decoded frame = %#v, want %#v", decoded, frame)
	}
}

func TestParseRejectsMalformedFrames(t *testing.T) {
	valid, err := (Frame{
		Addresses: []Address{{Callsign: "DEST"}, {Callsign: "SOURCE"}},
		Control:   0x03,
		PID:       []byte{0xf0},
	}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		data []byte
		err  error
	}{
		{name: "short", data: valid[:14], err: ErrTooShort},
		{name: "bad shifted character", data: append([]byte(nil), valid...), err: ErrInvalidAddress},
		{name: "missing PID", data: valid[:15], err: ErrMissingPID},
	}
	tests[1].data[0] |= 1
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got := Parse(test.data)
			if !errors.Is(got, test.err) {
				t.Fatalf("error = %v, want %v", got, test.err)
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x82, 0xa0, 0xa4, 0xa6, 0x40, 0x40, 0x61})
	f.Fuzz(func(t *testing.T, data []byte) {
		frame, err := Parse(data)
		if err != nil {
			return
		}
		encoded, err := frame.MarshalBinary()
		if err != nil {
			t.Fatalf("parsed frame cannot be encoded: %v", err)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatalf("round trip differs:\n got % x\nwant % x", encoded, data)
		}
	})
}

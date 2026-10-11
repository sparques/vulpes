// SPDX-License-Identifier: GPL-2.0-or-later

package agw

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestCodecFragmentedAndCoalesced(t *testing.T) {
	want := []Frame{
		{Port: 2, Kind: 'K', PID: 0xf0, CallFrom: "N0CALL-1", CallTo: "APRS", User: 42, Payload: []byte{0, 0xc0, 0xdb}},
		{Kind: 'R', Payload: []byte("version")},
	}
	var wire []byte
	for _, frame := range want {
		encoded, err := Encode(frame)
		if err != nil {
			t.Fatal(err)
		}
		wire = append(wire, encoded...)
	}
	decoder := NewDecoder(32)
	var got []Frame
	for _, chunk := range [][]byte{wire[:3], wire[3:39], wire[39:]} {
		frames, err := decoder.Feed(chunk)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, frames...)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d frames, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Port != want[i].Port || got[i].Kind != want[i].Kind || got[i].CallFrom != want[i].CallFrom ||
			got[i].CallTo != want[i].CallTo || got[i].PID != want[i].PID || got[i].User != want[i].User || !bytes.Equal(got[i].Payload, want[i].Payload) {
			t.Fatalf("frame %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestDecoderRejectsOversizeBeforePayload(t *testing.T) {
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[28:32], 17)
	_, err := NewDecoder(16).Feed(header)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("got %v, want ErrPayloadTooLarge", err)
	}
}

func FuzzDecoder(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 'R'})
	f.Add(make([]byte, HeaderSize))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoder := NewDecoder(256)
		for offset := 0; offset < len(data); {
			n := offset%11 + 1
			if n > len(data)-offset {
				n = len(data) - offset
			}
			frames, _ := decoder.Feed(data[offset : offset+n])
			for _, frame := range frames {
				if len(frame.Payload) > 256 {
					t.Fatalf("decoded oversized payload: %d", len(frame.Payload))
				}
			}
			offset += n
		}
	})
}

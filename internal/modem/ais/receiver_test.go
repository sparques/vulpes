// SPDX-License-Identifier: GPL-2.0-or-later

package ais

import (
	"bytes"
	"encoding/hex"
	"math"
	"strings"
	"testing"

	"github.com/sparques/vulpes/internal/hdlc"
)

func TestPayloadLengths(t *testing.T) {
	tests := []struct {
		typ   byte
		bytes int
		valid bool
	}{
		{1, 21, true}, {1, 20, false}, {5, 53, true}, {5, 52, false},
		{14, 9, true}, {14, 22, true}, {0, 21, false}, {28, 21, false},
	}
	for _, test := range tests {
		payload := make([]byte, test.bytes)
		payload[0] = test.typ << 2
		if got := validPayloadLength(payload); got != test.valid {
			t.Errorf("type %d length %d valid = %v, want %v", test.typ, test.bytes, got, test.valid)
		}
	}
}

func TestReceiverKnownDireWolfDocumentationVector(t *testing.T) {
	// This payload is the bit-decoded form of the AIS sentence used by Dire
	// Wolf's AIS documentation/source examples:
	// !AIVDM,1,1,,A,35NO=dPOiAJriVDH@94E84AJ0000,0*4B
	payload, err := hex.DecodeString("0c579f36c81fc516bac6651840911520445a000000")
	if err != nil {
		t.Fatal(err)
	}
	if got := ToNMEA(payload); got != "!AIVDM,1,1,,A,35NO=dPOiAJriVDH@94E84AJ0000,0*4B" {
		t.Fatalf("NMEA = %q", got)
	}
	levels := hdlc.Encode(payload, 100, 20)
	samples := modulateAIS(levels, 48000)
	receiver, err := NewReceiver(Config{SampleRate: 48000}, 256)
	if err != nil {
		t.Fatal(err)
	}
	var messages []Message
	for offset := 0; offset < len(samples); {
		end := min(offset+1+offset%197, len(samples))
		messages = append(messages, receiver.Process(samples[offset:end])...)
		offset = end
	}
	if len(messages) != 1 || !bytes.Equal(messages[0].Payload, payload) {
		t.Fatalf("messages = %#v, stats=%+v", messages, receiver.Stats())
	}

	corrupted := append([]bool(nil), levels...)
	corrupted[100*8+24] = !corrupted[100*8+24]
	receiver.Reset()
	if got := receiver.Process(modulateAIS(corrupted, 48000)); len(got) != 0 {
		t.Fatalf("corrupt frame produced %d messages", len(got))
	}
	receiver.Reset()
	truncated := samples[:len(samples)/2]
	if got := receiver.Process(truncated); len(got) != 0 {
		t.Fatalf("truncated frame produced %d messages", len(got))
	}
}

func modulateAIS(levels []bool, sampleRate int) []int16 {
	const bitRate = 9600
	phaseStep := uint32(math.Round(math.Ldexp(1, 32) * float64(bitRate/2) / float64(sampleRate)))
	var phase uint32
	var previous bool
	var emitted int64
	output := make([]int16, 0, len(levels)*sampleRate/bitRate+1)
	for bit, level := range levels {
		target := (int64(bit+1)*int64(sampleRate) + bitRate - 1) / bitRate
		for range target - emitted {
			if level != previous {
				phase += phaseStep
			} else if phase&0x80000000 != 0 {
				phase = 0xc0000000
			} else {
				phase = 0x40000000
			}
			angle := float64(phase>>24) * 2 * math.Pi / 256
			output = append(output, int16(math.Round(math.Sin(angle)*.8*32767)))
		}
		emitted = target
		previous = level
	}
	return output
}

func TestToNMEAArmorAndChecksum(t *testing.T) {
	// AIS type 1, repeat indicator 0, MMSI 123456789 followed by zeroed fields.
	payload := []byte{0x04, 0x1d, 0x6f, 0x34, 0x54, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	sentence := ToNMEA(payload)
	if !strings.HasPrefix(sentence, "!AIVDM,1,1,,A,") {
		t.Fatalf("unexpected NMEA prefix: %q", sentence)
	}
	star := strings.LastIndexByte(sentence, '*')
	if star < 0 || star+3 != len(sentence) {
		t.Fatalf("missing checksum: %q", sentence)
	}
	var checksum byte
	for i := 1; i < star; i++ {
		checksum ^= sentence[i]
	}
	hex := "0123456789ABCDEF"
	if sentence[star+1] != hex[checksum>>4] || sentence[star+2] != hex[checksum&15] {
		t.Fatalf("bad checksum in %q", sentence)
	}
	if !validPayloadLength(payload) {
		t.Fatal("known type-1 payload failed length validation")
	}
}

func TestToNMEARejectsInvalidSize(t *testing.T) {
	if got := ToNMEA(nil); got != "" {
		t.Fatalf("empty payload = %q", got)
	}
	if got := ToNMEA(make([]byte, 134)); got != "" {
		t.Fatalf("oversized payload = %q", got)
	}
}

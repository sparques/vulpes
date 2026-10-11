// SPDX-License-Identifier: GPL-2.0-or-later

package ais

import (
	"strings"
	"testing"
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

// SPDX-License-Identifier: GPL-2.0-or-later

package modem

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/sparques/vulpes/internal/hdlc"
)

func TestPacketModeEndToEndMatrix(t *testing.T) {
	tests := []Settings{
		{Mode: "afsk300", Profile: "A", Slicers: 9},
		{Mode: "afsk1200", Profile: "A", Slicers: 9},
		{Mode: "psk2400", Variant: "V26A", Profile: "R"},
		{Mode: "psk2400", Variant: "V26B", Profile: "R"},
		{Mode: "psk4800", Profile: "V"},
		{Mode: "g3ruh9600"},
	}
	for _, settings := range tests {
		for _, sampleRate := range []int{44100, 48000} {
			settings := settings
			settings.SampleRate = sampleRate
			settings.Amplitude = .7
			name := fmt.Sprintf("%s-%s/%d", settings.Mode, settings.Variant, sampleRate)
			t.Run(name, func(t *testing.T) {
				transmitter, err := NewTransmitter(settings)
				if err != nil {
					t.Fatal(err)
				}
				receiver, err := NewReceiver(settings, 512)
				if err != nil {
					t.Fatal(err)
				}
				payload := []byte("Vulpes end-to-end mode matrix")
				samples := transmitter.Modulate(hdlc.Encode(payload, 160, 30))
				var events []Event
				for offset := 0; offset < len(samples); {
					end := min(offset+1+offset%251, len(samples))
					events = append(events, receiver.Process(samples[offset:end])...)
					offset = end
				}
				for _, event := range events {
					if event.Kind == EventFrame && bytes.Equal(event.Payload, payload) {
						return
					}
				}
				t.Fatalf("payload not recovered from %d events", len(events))
			})
		}
	}
}

// SPDX-License-Identifier: GPL-2.0-or-later

package psk

import (
	"bytes"
	"testing"

	"github.com/sparques/vulpes/internal/hdlc"
)

func TestPSKHDLCRoundTrip(t *testing.T) {
	for _, mode := range []Mode{V26A2400, V26B2400, V274800} {
		t.Run(modeName(mode), func(t *testing.T) {
			config := Config{SampleRate: 48000, Mode: mode, Amplitude: 0.6}
			payload := []byte("Dire Wolf PSK receive pipeline")
			modulator, err := NewModulator(config)
			if err != nil {
				t.Fatal(err)
			}
			samples := modulator.Modulate(hdlc.Encode(payload, 100, 10))
			receiver, err := NewReceiver(config, 128)
			if err != nil {
				t.Fatal(err)
			}
			var frames []ReceivedFrame
			for offset := 0; offset < len(samples); offset += 211 {
				end := min(offset+211, len(samples))
				frames = append(frames, receiver.Process(samples[offset:end])...)
			}
			if len(frames) != 1 || !bytes.Equal(frames[0].Payload, payload) {
				t.Fatalf("frames=%d stats=%+v", len(frames), receiver.Stats())
			}
			if frames[0].Quality <= 0 || !frames[0].CarrierDetected {
				t.Fatalf("metadata=%+v", frames[0])
			}
		})
	}
}

func modeName(mode Mode) string {
	switch mode {
	case V26A2400:
		return "V26A"
	case V26B2400:
		return "V26B"
	default:
		return "V27"
	}
}

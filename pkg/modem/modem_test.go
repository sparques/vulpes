// SPDX-License-Identifier: GPL-2.0-or-later

package modem

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncodeThenProcessAudioAcrossChunks(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
	}{
		{name: ModeAFSK300, settings: Settings{Mode: ModeAFSK300}},
		{name: ModeAFSK1200, settings: Settings{Mode: ModeAFSK1200}},
		{name: "psk2400-v26a", settings: Settings{Mode: ModePSK2400, Variant: "V26A"}},
		{name: "psk2400-v26b", settings: Settings{Mode: ModePSK2400, Variant: "V26B"}},
		{name: ModePSK4800, settings: Settings{Mode: ModePSK4800}},
		{name: ModeG3RUH9600, settings: Settings{Mode: ModeG3RUH9600}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.settings.SampleRate = 48000
			test.settings.Amplitude = .7
			codec, err := New(test.settings)
			if err != nil {
				t.Fatal(err)
			}
			payload := []byte("public streaming modem API")
			samples, err := codec.Encode(payload)
			if err != nil {
				t.Fatal(err)
			}
			var events []Event
			for offset := 0; offset < len(samples); {
				end := min(offset+1+offset%251, len(samples))
				events = append(events, codec.ProcessAudio(samples[offset:end])...)
				offset = end
			}
			for _, event := range events {
				if event.Kind == EventFrame && bytes.Equal(event.Payload, payload) {
					return
				}
			}
			t.Fatalf("encoded payload not recovered in %d events", len(events))
		})
	}
}

func TestReceiveOnlyAndFrameValidation(t *testing.T) {
	codec, err := New(Settings{Mode: ModeAIS, SampleRate: 48000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Encode([]byte{1}); !errors.Is(err, ErrReceiveOnly) {
		t.Fatalf("AIS Encode error = %v, want ErrReceiveOnly", err)
	}
	packet, err := New(Settings{Mode: ModeAFSK1200, SampleRate: 48000, MaximumFrameBytes: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := packet.Encode(nil); err != ErrInvalidFrame {
		t.Fatalf("empty Encode error = %v, want ErrInvalidFrame", err)
	}
	if _, err := packet.Encode([]byte{1, 2, 3}); err == nil {
		t.Fatal("oversize frame was accepted")
	}
}

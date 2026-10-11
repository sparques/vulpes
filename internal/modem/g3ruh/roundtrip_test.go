// SPDX-License-Identifier: GPL-2.0-or-later

package g3ruh

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/sparques/vulpes/internal/hdlc"
)

func TestRoundTripSampleRatesChunksAndPolarity(t *testing.T) {
	payload := []byte("G3RUH deterministic round trip")
	for _, sampleRate := range []int{44100, 48000} {
		for _, inverted := range []bool{false, true} {
			name := "normal"
			if inverted {
				name = "inverted"
			}
			t.Run(name+"/"+strconv.Itoa(sampleRate), func(t *testing.T) {
				modulator, err := NewModulator(Config{SampleRate: sampleRate, Amplitude: .8})
				if err != nil {
					t.Fatal(err)
				}
				levels := hdlc.Encode(payload, 100, 30)
				samples := modulator.Modulate(levels)
				if inverted {
					for i := range samples {
						samples[i] = -samples[i]
					}
				}
				receiver, err := NewReceiver(Config{SampleRate: sampleRate}, 2048)
				if err != nil {
					t.Fatal(err)
				}
				var frames []ReceivedFrame
				for offset := 0; offset < len(samples); {
					end := min(offset+137, len(samples))
					frames = append(frames, receiver.Process(samples[offset:end])...)
					offset = end
				}
				found := false
				for _, frame := range frames {
					found = found || bytes.Equal(frame.Payload, payload)
				}
				if !found {
					t.Fatalf("did not recover payload; got %d frames, stats %+v", len(frames), receiver.Stats())
				}
			})
		}
	}
}

func TestReceiverResetRecoversAfterSignalLoss(t *testing.T) {
	const sampleRate = 48000
	payload := []byte("after reset")
	modulator, _ := NewModulator(Config{SampleRate: sampleRate, Amplitude: .8})
	receiver, _ := NewReceiver(Config{SampleRate: sampleRate}, 2048)
	receiver.Process(make([]int16, sampleRate/4))
	receiver.Reset()
	frames := receiver.Process(modulator.Modulate(hdlc.Encode(payload, 100, 30)))
	for _, frame := range frames {
		if bytes.Equal(frame.Payload, payload) {
			return
		}
	}
	t.Fatalf("did not recover after reset; got %d frames", len(frames))
}

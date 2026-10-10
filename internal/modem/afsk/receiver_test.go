// SPDX-License-Identifier: GPL-2.0-or-later

package afsk

import (
	"bytes"
	"math"
	"math/rand"
	"testing"

	"github.com/sparques/vulpes/internal/hdlc"
)

func TestReceiverAcquiresArbitrarySymbolPhase(t *testing.T) {
	for _, test := range []struct {
		name  string
		baud  int
		mark  float64
		space float64
	}{
		{name: "300", baud: 300, mark: 1600, space: 1800},
		{name: "1200", baud: 1200, mark: 1200, space: 2200},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := Config{SampleRate: 48000, Baud: test.baud, Mark: test.mark, Space: test.space, Amplitude: 0.7}
			payload := []byte("arbitrary phase")
			modulator, err := NewModulator(config)
			if err != nil {
				t.Fatal(err)
			}
			signal := modulator.Modulate(hdlc.Encode(payload, 24, 2))
			prefix := make([]int16, 23)
			random := rand.New(rand.NewSource(5))
			for i := range prefix {
				prefix[i] = int16(random.Intn(401) - 200)
			}
			samples := append(prefix, signal...)
			receiver, err := NewReceiver(config, 128)
			if err != nil {
				t.Fatal(err)
			}
			var frames []ReceivedFrame
			for offset := 0; offset < len(samples); {
				count := 1 + offset%211
				if count > len(samples)-offset {
					count = len(samples) - offset
				}
				frames = append(frames, receiver.Process(samples[offset:offset+count])...)
				offset += count
			}
			if len(frames) != 1 {
				t.Fatalf("received %d frames, want 1", len(frames))
			}
			if !bytes.Equal(frames[0].Payload, payload) {
				t.Fatalf("payload = %x, want %x", frames[0].Payload, payload)
			}
			if frames[0].Quality <= 0 || !frames[0].CarrierDetected {
				t.Fatalf("frame metadata = %+v, want positive quality and DCD", frames[0])
			}
		})
	}
}

func TestReceiverMultiSlicerDeduplicates(t *testing.T) {
	config := Config{SampleRate: 48000, Baud: 1200, Mark: 1200, Space: 2200, Amplitude: 0.8, Slicers: 9}
	payload := []byte("Dire Wolf A+ multi-slicer")
	levels := hdlc.Encode(payload, 32, 3)
	modulator, err := NewModulator(config)
	if err != nil {
		t.Fatal(err)
	}
	samples := modulator.Modulate(levels)
	start := 0
	for symbol, level := range levels {
		end := ((symbol+1)*config.SampleRate + config.Baud - 1) / config.Baud
		gain := 1.0
		if level {
			gain = 0.35
		}
		for i := start; i < end; i++ {
			samples[i] = int16(math.Round(float64(samples[i]) * gain))
		}
		start = end
	}
	receiver, err := NewReceiver(config, 128)
	if err != nil {
		t.Fatal(err)
	}
	var frames []ReceivedFrame
	for offset := 0; offset < len(samples); offset += 193 {
		end := min(offset+193, len(samples))
		frames = append(frames, receiver.Process(samples[offset:end])...)
	}
	if len(frames) != 1 || !bytes.Equal(frames[0].Payload, payload) {
		t.Fatalf("received %d frames, want one de-duplicated frame; stats=%+v", len(frames), receiver.Stats())
	}
}

func TestReceiverProfileBRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name        string
		baud        int
		mark, space float64
	}{
		{name: "300", baud: 300, mark: 1600, space: 1800},
		{name: "1200", baud: 1200, mark: 1200, space: 2200},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := Config{SampleRate: 48000, Baud: test.baud, Mark: test.mark, Space: test.space, Amplitude: 0.7, Profile: "B", Slicers: 9}
			payload := []byte("profile B FM discriminator")
			modulator, err := NewModulator(config)
			if err != nil {
				t.Fatal(err)
			}
			samples := modulator.Modulate(hdlc.Encode(payload, 32, 3))
			receiver, err := NewReceiver(config, 128)
			if err != nil {
				t.Fatal(err)
			}
			var frames []ReceivedFrame
			for offset := 0; offset < len(samples); offset += 173 {
				end := min(offset+173, len(samples))
				frames = append(frames, receiver.Process(samples[offset:end])...)
			}
			if len(frames) != 1 || !bytes.Equal(frames[0].Payload, payload) {
				t.Fatalf("profile B received %d frames; stats=%+v", len(frames), receiver.Stats())
			}
		})
	}
}

func TestReceiverToleratesSmallSampleClockError(t *testing.T) {
	for _, test := range []struct {
		name        string
		baud        int
		mark, space float64
	}{
		{name: "300", baud: 300, mark: 1600, space: 1800},
		{name: "1200", baud: 1200, mark: 1200, space: 2200},
	} {
		t.Run(test.name, func(t *testing.T) {
			receiveConfig := Config{SampleRate: 48000, Baud: test.baud, Mark: test.mark, Space: test.space, Amplitude: 0.7}
			transmitConfig := receiveConfig
			transmitConfig.SampleRate = 48005
			payload := make([]byte, 128)
			if _, err := rand.New(rand.NewSource(6)).Read(payload); err != nil {
				t.Fatal(err)
			}
			modulator, err := NewModulator(transmitConfig)
			if err != nil {
				t.Fatal(err)
			}
			samples := modulator.Modulate(hdlc.Encode(payload, 24, 2))
			receiver, err := NewReceiver(receiveConfig, 256)
			if err != nil {
				t.Fatal(err)
			}
			var frames []ReceivedFrame
			for offset := 0; offset < len(samples); offset += 257 {
				end := min(offset+257, len(samples))
				frames = append(frames, receiver.Process(samples[offset:end])...)
			}
			if len(frames) != 1 || !bytes.Equal(frames[0].Payload, payload) {
				t.Fatalf("clock-offset receive failed: %d frames", len(frames))
			}
		})
	}
}

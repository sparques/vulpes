// SPDX-License-Identifier: GPL-2.0-or-later

package afsk

import (
	"bytes"
	"math"
	"math/rand"
	"testing"

	"github.com/sparques/vulpes/internal/hdlc"
)

func TestAFSKHDLCRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate int
		baud       int
		mark       float64
		space      float64
	}{
		{name: "300-44100", sampleRate: 44100, baud: 300, mark: 1600, space: 1800},
		{name: "300-48000", sampleRate: 48000, baud: 300, mark: 1600, space: 1800},
		{name: "1200-44100", sampleRate: 44100, baud: 1200, mark: 1200, space: 2200},
		{name: "1200-48000", sampleRate: 48000, baud: 1200, mark: 1200, space: 2200},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Config{
				SampleRate: test.sampleRate,
				Baud:       test.baud,
				Mark:       test.mark,
				Space:      test.space,
				Amplitude:  0.65,
			}
			payload := make([]byte, 256)
			if _, err := rand.New(rand.NewSource(3)).Read(payload); err != nil {
				t.Fatal(err)
			}
			levels := hdlc.Encode(payload, 20, 2)
			modulator, err := NewModulator(config)
			if err != nil {
				t.Fatal(err)
			}
			samples := modulator.Modulate(levels)
			decoded := demodulateInChunks(t, config, samples)
			decoder := hdlc.NewDecoder(512)
			var frames [][]byte
			for _, level := range decoded {
				frames = append(frames, decoder.Feed(level)...)
			}
			if len(frames) != 1 {
				t.Fatalf("decoded %d frames, HDLC stats %+v", len(frames), decoder.Stats())
			}
			if !bytes.Equal(frames[0], payload) {
				t.Fatal("decoded payload differs")
			}
		})
	}
}

func TestAFSKRoundTripWithNoiseAndFrequencyOffset(t *testing.T) {
	for _, test := range []struct {
		name        string
		baud        int
		mark, space float64
		offset      float64
	}{
		{name: "300", baud: 300, mark: 1600, space: 1800, offset: 10},
		{name: "1200", baud: 1200, mark: 1200, space: 2200, offset: 25},
	} {
		t.Run(test.name, func(t *testing.T) {
			receive := Config{SampleRate: 48000, Baud: test.baud, Mark: test.mark, Space: test.space, Amplitude: 0.4}
			transmit := receive
			transmit.Mark += test.offset
			transmit.Space += test.offset
			payload := []byte("vulpes AFSK impairment test")
			levels := hdlc.Encode(payload, 20, 2)
			modulator, err := NewModulator(transmit)
			if err != nil {
				t.Fatal(err)
			}
			samples := modulator.Modulate(levels)
			random := rand.New(rand.NewSource(4))
			for i := range samples {
				value := int(samples[i]) + random.Intn(6001) - 3000
				value = min(max(value, -32768), 32767)
				samples[i] = int16(value)
			}
			decoded := demodulateInChunks(t, receive, samples)
			decoder := hdlc.NewDecoder(128)
			var frames [][]byte
			for _, level := range decoded {
				frames = append(frames, decoder.Feed(level)...)
			}
			if len(frames) != 1 || !bytes.Equal(frames[0], payload) {
				t.Fatalf("impaired round trip failed: frames=%d stats=%+v", len(frames), decoder.Stats())
			}
		})
	}
}

func TestAFSKRoundTripWithAmplitudeImbalanceAndClipping(t *testing.T) {
	for _, test := range []struct {
		name        string
		baud        int
		mark, space float64
	}{
		{name: "300", baud: 300, mark: 1600, space: 1800},
		{name: "1200", baud: 1200, mark: 1200, space: 2200},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := Config{SampleRate: 48000, Baud: test.baud, Mark: test.mark, Space: test.space, Amplitude: 0.9}
			payload := []byte("unequal mark and space levels with clipped peaks")
			levels := hdlc.Encode(payload, 32, 2)
			modulator, err := NewModulator(config)
			if err != nil {
				t.Fatal(err)
			}
			samples := modulator.Modulate(levels)
			start := 0
			for symbol, level := range levels {
				end := ((symbol+1)*config.SampleRate + config.Baud - 1) / config.Baud
				gain := 2.0
				if level {
					gain = 0.55
				}
				for i := start; i < end; i++ {
					value := int(math.Round(float64(samples[i]) * gain))
					value = min(max(value, -32768), 32767)
					samples[i] = int16(value)
				}
				start = end
			}
			decoded := demodulateInChunks(t, config, samples)
			decoder := hdlc.NewDecoder(128)
			var frames [][]byte
			for _, level := range decoded {
				frames = append(frames, decoder.Feed(level)...)
			}
			if len(frames) != 1 || !bytes.Equal(frames[0], payload) {
				t.Fatalf("imbalanced/clipped round trip failed: frames=%d stats=%+v", len(frames), decoder.Stats())
			}
		})
	}
}

func demodulateInChunks(t *testing.T, config Config, samples []int16) []bool {
	t.Helper()
	demodulator, err := NewDemodulator(config)
	if err != nil {
		t.Fatal(err)
	}
	var levels []bool
	for offset := 0; offset < len(samples); {
		count := 1 + offset%137
		if count > len(samples)-offset {
			count = len(samples) - offset
		}
		for _, decision := range demodulator.Process(samples[offset : offset+count]) {
			levels = append(levels, decision.Level)
		}
		offset += count
	}
	return levels
}

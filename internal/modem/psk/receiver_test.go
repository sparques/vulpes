// SPDX-License-Identifier: GPL-2.0-or-later

package psk

import (
	"bytes"
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/sparques/vulpes/internal/hdlc"
)

func TestPSKHDLCRoundTrip(t *testing.T) {
	for _, mode := range []Mode{V26A2400, V26B2400, V274800} {
		for _, sampleRate := range []int{22050, 44100, 48000} {
			t.Run(modeName(mode)+"/"+strconv.Itoa(sampleRate), func(t *testing.T) {
				config := Config{SampleRate: sampleRate, Mode: mode, Amplitude: 0.6}
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
				for offset := 0; offset < len(samples); {
					end := min(offset+1+offset%211, len(samples))
					frames = append(frames, receiver.Process(samples[offset:end])...)
					offset = end
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
}

func TestPSKImpairmentRegressionFloor(t *testing.T) {
	for _, mode := range []Mode{V26A2400, V26B2400, V274800} {
		t.Run(modeName(mode), func(t *testing.T) {
			receive := Config{SampleRate: 48000, Mode: mode, Amplitude: 0.7}
			// A 20 Hz carrier error, 100 ppm clock error, and roughly 24 dB
			// peak SNR form the deliberately conservative regression floor.
			transmit := receive
			payload := []byte("Vulpes PSK deterministic impairment floor")
			modulator, err := NewModulator(transmit)
			if err != nil {
				t.Fatal(err)
			}
			samples := modulator.Modulate(hdlc.Encode(payload, 160, 20))
			samples = shiftPSKCarrier(samples, transmit.SampleRate, 20)
			samples = resamplePSK(samples, 1.0001)
			random := rand.New(rand.NewSource(23))
			for i := range samples {
				value := int(samples[i]) + random.Intn(3001) - 1500
				samples[i] = int16(min(max(value, -32768), 32767))
			}
			receiver, err := NewReceiver(receive, 256)
			if err != nil {
				t.Fatal(err)
			}
			var frames []ReceivedFrame
			for offset := 0; offset < len(samples); {
				end := min(offset+1+offset%173, len(samples))
				frames = append(frames, receiver.Process(samples[offset:end])...)
				offset = end
			}
			for _, frame := range frames {
				if bytes.Equal(frame.Payload, payload) {
					return
				}
			}
			t.Fatalf("impaired payload not recovered; frames=%d stats=%+v", len(frames), receiver.Stats())
		})
	}
}

func shiftPSKCarrier(samples []int16, sampleRate int, offset float64) []int16 {
	result := make([]int16, len(samples))
	phase := 0.0
	for i, sample := range samples {
		// Multiplication by a low-frequency complex oscillator is not
		// available for real PCM. This small-angle approximation adds a
		// deterministic quadrature component from the sample derivative.
		previous := int16(0)
		if i != 0 {
			previous = samples[i-1]
		}
		quadrature := float64(sample-previous) * float64(sampleRate) / (2 * math.Pi * carrierFrequency)
		value := float64(sample)*math.Cos(phase) - quadrature*math.Sin(phase)
		result[i] = int16(min(max(int(math.Round(value)), -32768), 32767))
		phase += 2 * math.Pi * offset / float64(sampleRate)
	}
	return result
}

func resamplePSK(input []int16, ratio float64) []int16 {
	output := make([]int16, int(float64(len(input))*ratio))
	for i := range output {
		position := float64(i) / ratio
		left := min(int(position), len(input)-1)
		right := min(left+1, len(input)-1)
		fraction := position - float64(left)
		output[i] = int16(math.Round(float64(input[left])*(1-fraction) + float64(input[right])*fraction))
	}
	return output
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

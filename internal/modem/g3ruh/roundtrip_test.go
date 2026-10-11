// SPDX-License-Identifier: GPL-2.0-or-later

package g3ruh

import (
	"bytes"
	"math"
	"math/rand"
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

func TestImpairmentRegressionFloor(t *testing.T) {
	const sampleRate = 48000
	payload := []byte("G3RUH deterministic impairment floor")
	modulator, err := NewModulator(Config{SampleRate: sampleRate, Amplitude: .8})
	if err != nil {
		t.Fatal(err)
	}
	samples := modulator.Modulate(hdlc.Encode(payload, 160, 40))
	// Model a bandwidth-limited audio path, 100 ppm sample clock error,
	// and about 28 dB of uniformly distributed peak noise.
	for pass := 0; pass < 2; pass++ {
		filtered := append([]int16(nil), samples...)
		for i := 1; i+1 < len(samples); i++ {
			filtered[i] = int16((int(samples[i-1]) + 2*int(samples[i]) + int(samples[i+1])) / 4)
		}
		samples = filtered
	}
	samples = resampleG3RUH(samples, 1.0001)
	random := rand.New(rand.NewSource(96))
	for i := range samples {
		value := int(samples[i]) + random.Intn(2001) - 1000
		samples[i] = int16(min(max(value, -32768), 32767))
	}
	receiver, err := NewReceiver(Config{SampleRate: sampleRate}, 256)
	if err != nil {
		t.Fatal(err)
	}
	var frames []ReceivedFrame
	for offset := 0; offset < len(samples); {
		end := min(offset+1+offset%149, len(samples))
		frames = append(frames, receiver.Process(samples[offset:end])...)
		offset = end
	}
	for _, frame := range frames {
		if bytes.Equal(frame.Payload, payload) {
			return
		}
	}
	t.Fatalf("impaired payload not recovered; frames=%d stats=%+v", len(frames), receiver.Stats())
}

func resampleG3RUH(input []int16, ratio float64) []int16 {
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

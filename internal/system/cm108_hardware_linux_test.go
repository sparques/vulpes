// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux && cgo

package system_test

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sparques/vulpes/internal/audio"
	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

// This opt-in test exercises the complete Vulpes modem path through a physical
// audio loopback. The selected device must have mono capture and stereo output
// connected together at a safe level.
func TestCM108AudioLoopback(t *testing.T) {
	captureDevice := os.Getenv("VULPES_TEST_CM108_CAPTURE")
	playbackDevice := os.Getenv("VULPES_TEST_CM108_PLAYBACK")
	if captureDevice == "" || playbackDevice == "" {
		t.Skip("set VULPES_TEST_CM108_CAPTURE and VULPES_TEST_CM108_PLAYBACK to test CM108 audio loopback")
	}

	for _, sampleRate := range []int{44100, 48000} {
		t.Run(fmt.Sprintf("%dHz", sampleRate), func(t *testing.T) {
			captureSpec := audio.Spec{SampleRate: sampleRate, Channels: 1, Format: audio.Signed16LE}
			playbackSpec := audio.Spec{SampleRate: sampleRate, Channels: 2, Format: audio.Signed16LE}
			frames := sampleRate / 10
			capture, err := audio.OpenALSACapture(audio.DeviceConfig{Device: captureDevice, PCM: captureSpec, BufferFrames: frames})
			if err != nil {
				t.Fatalf("open CM108 capture: %v", err)
			}
			recorder := &recordingCapture{Capture: capture}
			defer recorder.Close()
			playback, err := audio.OpenALSAPlayback(audio.DeviceConfig{Device: playbackDevice, PCM: playbackSpec, BufferFrames: frames})
			if err != nil {
				t.Fatalf("open CM108 playback: %v", err)
			}
			defer playback.Close()
			recorderPlayback := &recordingPlayback{Playback: playback}
			bus, err := audio.NewPlaybackBus(recorderPlayback)
			if err != nil {
				t.Fatal(err)
			}
			channel, err := tnc.NewChannel(tnc.ChannelConfig{
				Name: "cm108-loopback", Modem: modem.Settings{Mode: "afsk1200", SampleRate: sampleRate, Amplitude: 0.7, Profile: "A", Slicers: 1},
				AudioChannel: 0, QueueDepth: 2, FullDuplex: true, TXDelayMillis: 1000, TXTailMillis: 250,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer channel.Close()
			events, unsubscribe, err := channel.SubscribeEvents(4)
			if err != nil {
				t.Fatal(err)
			}
			defer unsubscribe()

			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			captureDone := make(chan error, 1)
			go func() {
				captureDone <- tnc.RunCapture(ctx, recorder, []tnc.AudioRoute{{Channel: channel, AudioChannel: 0}}, frames/4, 8)
			}()
			time.Sleep(150 * time.Millisecond)

			payload := []byte("CM108 physical audio loopback")
			if err := channel.QueueFrame(payload); err != nil {
				t.Fatal(err)
			}
			if err := channel.TransmitNext(ctx, bus, nil); err != nil {
				t.Fatalf("transmit through CM108: %v", err)
			}
			for {
				select {
				case event := <-events:
					if event.Kind == modem.EventFrame && bytes.Equal(event.Payload, payload) {
						cancel()
						<-captureDone
						return
					}
				case err := <-captureDone:
					if err != nil && ctx.Err() == nil {
						t.Fatalf("capture from CM108 loopback: %v", err)
					}
					t.Fatal("capture pipeline stopped before recovering the transmitted frame")
				case <-ctx.Done():
					samples := recorder.samplesSnapshot()
					var peak, sumSquares float64
					for _, sample := range samples {
						value := math.Abs(float64(sample))
						if value > peak {
							peak = value
						}
						sumSquares += value * value
					}
					rms := 0.0
					if len(samples) > 0 {
						rms = math.Sqrt(sumSquares / float64(len(samples)))
					}
					t.Fatalf("CM108 loopback did not recover frame: reads=%d captured=%d samples peak=%.1f rms=%.1f output=%d samples peak=%d metrics=%+v", recorder.reads.Load(), len(samples), peak, rms, recorderPlayback.samples.Load(), recorderPlayback.peak.Load(), channel.Metrics())
				}
			}
		})
	}
}

type recordingCapture struct {
	audio.Capture
	mu      sync.Mutex
	samples []int16
	reads   atomic.Uint64
}

type recordingPlayback struct {
	audio.Playback
	samples atomic.Uint64
	peak    atomic.Int64
}

func (p *recordingPlayback) WriteSamples(ctx context.Context, samples []int16) (int, error) {
	var peak int64
	for _, sample := range samples {
		value := int64(sample)
		if value < 0 {
			value = -value
		}
		if value > peak {
			peak = value
		}
	}
	for current := p.peak.Load(); peak > current && !p.peak.CompareAndSwap(current, peak); current = p.peak.Load() {
	}
	n, err := p.Playback.WriteSamples(ctx, samples)
	p.samples.Add(uint64(n))
	return n, err
}

func (c *recordingCapture) ReadSamples(ctx context.Context, destination []int16) (int, error) {
	c.reads.Add(1)
	n, err := c.Capture.ReadSamples(ctx, destination)
	if n > 0 {
		c.mu.Lock()
		c.samples = append(c.samples, destination[:n]...)
		c.mu.Unlock()
	}
	return n, err
}

func (c *recordingCapture) samplesSnapshot() []int16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int16(nil), c.samples...)
}

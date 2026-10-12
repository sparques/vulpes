// SPDX-License-Identifier: GPL-2.0-or-later

package tnc

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/sparques/vulpes/internal/audio"
	"github.com/sparques/vulpes/internal/modem"
)

func TestChannelQueueBoundsCopiesAndCloses(t *testing.T) {
	channel := testChannel(t, 1)
	payload := []byte{1, 2, 3}
	if err := channel.QueueFrame(payload); err != nil {
		t.Fatal(err)
	}
	payload[0] = 9
	if got := channel.QueueDepth(); got != 1 {
		t.Fatalf("queue depth = %d, want 1", got)
	}
	if err := channel.QueueFrame([]byte{4}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("queue overflow = %v, want ErrQueueFull", err)
	}
	if metrics := channel.Metrics(); metrics.QueueDepth != 1 || metrics.QueueDrops != 1 {
		t.Fatalf("metrics = %+v", metrics)
	}
	channel.carrier.Store(false)
	channel.timingMu.Lock()
	channel.timing.FullDuplex = true
	channel.timingMu.Unlock()
	pcm, err := channel.NextTransmit(context.Background())
	if err != nil || len(pcm) == 0 {
		t.Fatalf("NextTransmit = %d samples, %v", len(pcm), err)
	}
	if err := channel.Close(); err != nil {
		t.Fatal(err)
	}
	if err := channel.QueueFrame([]byte{1}); !errors.Is(err, ErrClosed) {
		t.Fatalf("queue after close = %v, want ErrClosed", err)
	}
}

func TestChannelConcurrentQueueSubscribersAndShutdown(t *testing.T) {
	channel := testChannel(t, 8)
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for sequence := 0; sequence < 100; sequence++ {
				err := channel.QueueFrame([]byte{byte(worker), byte(sequence)})
				if err != nil && !errors.Is(err, ErrQueueFull) && !errors.Is(err, ErrClosed) {
					t.Errorf("QueueFrame: %v", err)
				}
				_ = channel.QueueDepth()
				timing := channel.Timing()
				timing.Persistence = (worker*100 + sequence) & 255
				if err := channel.SetTiming(timing); err != nil {
					t.Errorf("SetTiming: %v", err)
				}
			}
		}(worker)
	}
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				events, unsubscribe, err := channel.SubscribeEvents(1)
				if err != nil {
					if !errors.Is(err, ErrClosed) {
						t.Errorf("SubscribeEvents: %v", err)
					}
					return
				}
				select {
				case <-events:
				default:
				}
				unsubscribe()
			}
		}()
	}
	workers.Wait()
	if err := channel.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestChannelTimingAndSymbolAlignment(t *testing.T) {
	channel := testChannel(t, 2)
	if err := channel.SetTiming(Timing{TXDelayMillis: 100, TXTailMillis: 50, SlotMillis: 20, Persistence: 255}); err != nil {
		t.Fatal(err)
	}
	if got := channel.Timing(); got.TXDelayMillis != 100 || got.Persistence != 255 {
		t.Fatalf("timing = %#v", got)
	}
	if err := channel.SetTiming(Timing{Persistence: 256}); err == nil {
		t.Fatal("accepted invalid persistence")
	}
	for bits := 1; bits <= 3; bits++ {
		got := alignSymbolBoundary([]bool{true, false, true, true}, bits)
		if len(got)%bits != 0 {
			t.Fatalf("%d-bit symbols left %d bits", bits, len(got))
		}
	}
}

func TestTransmitNextPrependsVOXTone(t *testing.T) {
	channel, err := NewChannel(ChannelConfig{
		Name: "vox", QueueDepth: 1, TXDelayMillis: 1, TXTailMillis: 1,
		SlotMillis: 1, Persistence: 255, FullDuplex: true, AudioChannel: 1, VOXToneMillis: 100,
		Modem: modem.Settings{Mode: "afsk1200", SampleRate: 48000, Amplitude: .5, Profile: "A", Slicers: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	playback, err := audio.NewSlicePlayback(audio.Spec{SampleRate: 48000, Channels: 2, Format: audio.Signed16LE})
	if err != nil {
		t.Fatal(err)
	}
	bus, err := audio.NewPlaybackBus(playback)
	if err != nil {
		t.Fatal(err)
	}
	if err := channel.QueueFrame([]byte("vox")); err != nil {
		t.Fatal(err)
	}
	if err := channel.TransmitNext(context.Background(), bus, nil); err != nil {
		t.Fatal(err)
	}
	pcm := playback.Samples()
	toneFrames := 4800
	if len(pcm) <= toneFrames*2 {
		t.Fatalf("transmission has %d interleaved samples, expected VOX tone plus frame", len(pcm))
	}
	for frame := 0; frame < toneFrames; frame++ {
		if pcm[frame*2] != 0 {
			t.Fatalf("left channel sample %d = %d, want silence", frame, pcm[frame*2])
		}
		want := int16(math.Round(math.Sin(2*math.Pi*400*float64(frame)/48000) * .5 * 32767))
		if pcm[frame*2+1] != want {
			t.Fatalf("VOX tone sample %d = %d, want 400 Hz sample %d", frame, pcm[frame*2+1], want)
		}
	}
	frameHasSignal := false
	for _, sample := range pcm[toneFrames*2:] {
		if sample != 0 {
			frameHasSignal = true
			break
		}
	}
	if !frameHasSignal {
		t.Fatal("modulated frame did not follow VOX tone")
	}
}

func testChannel(t *testing.T, depth int) *Channel {
	t.Helper()
	channel, err := NewChannel(ChannelConfig{
		Name: "test", QueueDepth: depth, TXDelayMillis: 1, TXTailMillis: 1,
		SlotMillis: 1, Persistence: 255, FullDuplex: true,
		Modem: modem.Settings{Mode: "afsk1200", SampleRate: 48000, Amplitude: .5, Profile: "A", Slicers: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = channel.Close() })
	return channel
}

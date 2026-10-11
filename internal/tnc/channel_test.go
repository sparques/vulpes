// SPDX-License-Identifier: GPL-2.0-or-later

package tnc

import (
	"context"
	"errors"
	"testing"

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

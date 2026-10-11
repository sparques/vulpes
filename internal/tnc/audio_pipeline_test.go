// SPDX-License-Identifier: GPL-2.0-or-later

package tnc

import (
	"context"
	"testing"

	"github.com/sparques/vulpes/internal/audio"
	"github.com/sparques/vulpes/internal/modem"
)

func TestRunCaptureRoutesStereoInArbitraryChunks(t *testing.T) {
	spec := audio.Spec{SampleRate: 48000, Channels: 2}
	capture, err := audio.NewSliceCapture(spec, []int16{100, -100, 200, -200, 300, -300})
	if err != nil {
		t.Fatal(err)
	}
	channels := make([]*Channel, 2)
	for i := range channels {
		channels[i], err = NewChannel(ChannelConfig{Name: string(rune('a' + i)), Modem: modem.Settings{Mode: "afsk1200", SampleRate: 48000, Amplitude: .5, Profile: "A", Slicers: 1}, SlotMillis: 1, Persistence: 255})
		if err != nil {
			t.Fatal(err)
		}
		defer channels[i].Close()
	}
	if err := RunCapture(context.Background(), capture, []AudioRoute{{Channel: channels[0], AudioChannel: 0}, {Channel: channels[1], AudioChannel: 1}}, 1, 2); err != nil {
		t.Fatal(err)
	}
}

func TestRunCaptureRejectsInvalidRoute(t *testing.T) {
	capture, err := audio.NewSliceCapture(audio.Spec{SampleRate: 48000, Channels: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RunCapture(context.Background(), capture, []AudioRoute{{AudioChannel: 1}}, 64, 1); err == nil {
		t.Fatal("expected invalid route error")
	}
}

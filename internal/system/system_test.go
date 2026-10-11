// SPDX-License-Identifier: GPL-2.0-or-later

package system_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/sparques/vulpes/internal/agw"
	"github.com/sparques/vulpes/internal/audio"
	"github.com/sparques/vulpes/internal/kiss"
	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

type transportServer interface {
	Serve(context.Context) error
	Close() error
}

func TestEndToEndModeAudioTransportMatrix(t *testing.T) {
	tests := []struct {
		mode, variant, profile, transport string
		rate, audioChannel                int
	}{
		{mode: "afsk300", profile: "A", transport: "kiss", rate: 44100, audioChannel: 0},
		{mode: "afsk300", profile: "A", transport: "agw", rate: 48000, audioChannel: 1},
		{mode: "afsk1200", profile: "A", transport: "agw", rate: 44100, audioChannel: 0},
		{mode: "afsk1200", profile: "B", transport: "kiss", rate: 48000, audioChannel: 1},
		{mode: "psk2400", variant: "V26A", profile: "R", transport: "kiss", rate: 44100, audioChannel: 0},
		{mode: "psk2400", variant: "V26A", profile: "S", transport: "agw", rate: 48000, audioChannel: 1},
		{mode: "psk2400", variant: "V26B", profile: "R", transport: "agw", rate: 44100, audioChannel: 0},
		{mode: "psk2400", variant: "V26B", profile: "S", transport: "kiss", rate: 48000, audioChannel: 1},
		{mode: "psk4800", profile: "V", transport: "kiss", rate: 44100, audioChannel: 0},
		{mode: "psk4800", profile: "W", transport: "agw", rate: 48000, audioChannel: 1},
		{mode: "g3ruh9600", transport: "agw", rate: 44100, audioChannel: 0},
		{mode: "g3ruh9600", transport: "kiss", rate: 48000, audioChannel: 1},
	}
	for _, test := range tests {
		test := test
		name := fmt.Sprintf("%s-%s/%d/channel-%d/%s", test.mode, test.variant, test.rate, test.audioChannel, test.transport)
		t.Run(name, func(t *testing.T) {
			settings := modem.Settings{
				Mode: test.mode, Variant: test.variant, Profile: test.profile,
				SampleRate: test.rate, Amplitude: .7, Slicers: 9,
			}
			channel, err := tnc.NewChannel(tnc.ChannelConfig{
				Name: "radio", Modem: settings, QueueDepth: 4, FullDuplex: true,
				TXDelayMillis: 1000, TXTailMillis: 250, AudioChannel: test.audioChannel,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer channel.Close()
			events, unsubscribe, err := channel.SubscribeEvents(8)
			if err != nil {
				t.Fatal(err)
			}
			defer unsubscribe()

			payload := []byte("Vulpes system matrix " + name)
			queueThroughTransport(t, test.transport, channel, payload)

			spec := audio.Spec{SampleRate: test.rate, Channels: 2}
			playback, err := audio.NewSlicePlayback(spec)
			if err != nil {
				t.Fatal(err)
			}
			bus, err := audio.NewPlaybackBus(playback)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := channel.TransmitNext(ctx, bus, nil); err != nil {
				t.Fatal(err)
			}
			interleaved := playback.Samples()
			if len(interleaved) == 0 {
				t.Fatal("transmit produced no PCM")
			}
			for frame := 0; frame < len(interleaved)/2; frame++ {
				if interleaved[frame*2+1-test.audioChannel] != 0 {
					t.Fatalf("PCM leaked into stereo channel %d", 1-test.audioChannel)
				}
			}
			capture, err := audio.NewSliceCapture(spec, interleaved)
			if err != nil {
				t.Fatal(err)
			}
			if err := tnc.RunCapture(ctx, capture, []tnc.AudioRoute{{Channel: channel, AudioChannel: test.audioChannel}}, 137, 4); err != nil {
				t.Fatal(err)
			}
			for {
				select {
				case event := <-events:
					if event.Kind == modem.EventFrame && bytes.Equal(event.Payload, payload) {
						return
					}
				default:
					t.Fatal("transmitted frame was not recovered through capture routing")
				}
			}
		})
	}
}

func queueThroughTransport(t *testing.T, kind string, channel *tnc.Channel, payload []byte) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("TCP loopback is unavailable: %v", err)
		}
		t.Fatal(err)
	}
	var server transportServer
	var wire []byte
	switch kind {
	case "kiss":
		server, err = kiss.NewTCPServer(listener, kiss.TCPConfig{Ports: map[uint8]*tnc.Channel{0: channel}})
		wire = kiss.Encode(kiss.Frame{Command: kiss.DataFrame, Payload: payload})
	case "agw":
		server, err = agw.NewServer(listener, map[uint8]*tnc.Channel{0: channel}, 4)
		wire, err = agw.Encode(agw.Frame{Kind: 'K', Payload: append([]byte{0}, payload...)})
	default:
		t.Fatalf("unknown transport %q", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(wire); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for channel.QueueDepth() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	_ = connection.Close()
	cancel()
	_ = server.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if channel.QueueDepth() != 1 {
		t.Fatal("transport did not queue the frame")
	}
}

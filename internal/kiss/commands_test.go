// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"testing"

	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

func TestCommandsAndHardwareQueries(t *testing.T) {
	channel, err := tnc.NewChannel(tnc.ChannelConfig{Name: "radio", QueueDepth: 4, SlotMillis: 100, Persistence: 63, Modem: modem.Settings{Mode: "afsk1200", SampleRate: 48000, Amplitude: .5, Profile: "A", Slicers: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	for _, frame := range []Frame{
		{Command: TXDelay, Payload: []byte{30}}, {Command: Persistence, Payload: []byte{127}},
		{Command: SlotTime, Payload: []byte{10}}, {Command: TXTail, Payload: []byte{5}}, {Command: FullDuplex, Payload: []byte{1}},
	} {
		if response, ok := applyFrame(frame, channel); !ok || response != nil {
			t.Fatalf("command %#v was rejected", frame)
		}
	}
	if got := channel.Timing(); got.TXDelayMillis != 300 || got.Persistence != 127 || got.SlotMillis != 100 || got.TXTailMillis != 50 || !got.FullDuplex {
		t.Fatalf("timing = %+v", got)
	}
	for query, want := range map[string]string{"TNC:": "VULPES dev", "TXBUF:": "TXBUF:0"} {
		response, ok := applyFrame(Frame{Port: 3, Command: SetHardware, Payload: []byte(query)}, channel)
		if !ok || response == nil || response.Port != 3 || response.Command != SetHardware || string(response.Payload) != want {
			t.Fatalf("query %q response = %#v, ok %v", query, response, ok)
		}
	}
	if _, ok := applyFrame(Frame{Command: SetHardware, Payload: []byte("UNKNOWN:")}, channel); ok {
		t.Fatal("unknown hardware query was accepted")
	}
}

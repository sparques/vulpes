// SPDX-License-Identifier: GPL-2.0-or-later

package agw

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/sparques/vulpes/internal/ax25"
	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

func newLinkTestClient(t *testing.T) (*client, *tnc.Channel) {
	t.Helper()
	channel, err := tnc.NewChannel(tnc.ChannelConfig{
		Name: "radio", QueueDepth: 16, FullDuplex: true,
		Modem: modem.Settings{Mode: "afsk1200", SampleRate: 48000, Amplitude: .5, Profile: "A", Slicers: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = channel.Close() })
	server := &Server{ports: map[uint8]*tnc.Channel{0: channel}, names: map[uint8]string{0: "radio"}, clients: make(map[*client]struct{})}
	c := &client{server: server, out: make(chan Frame, 32), done: make(chan struct{}), calls: make(map[string]struct{}), links: make(map[linkKey]*linkState)}
	server.clients[c] = struct{}{}
	return c, channel
}

func drainTransmit(t *testing.T, channel *tnc.Channel) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := channel.NextTransmit(ctx); err != nil {
		t.Fatal(err)
	}
}

func wirePacket(t *testing.T, frame ax25.Frame) []byte {
	t.Helper()
	data, err := frame.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestOutgoingConnectedModeLifecycle(t *testing.T) {
	c, channel := newLinkTestClient(t)
	if !c.handle(Frame{Port: 0, Kind: 'C', CallFrom: "N0CALL", CallTo: "REMOTE"}) {
		t.Fatal("connect request rejected")
	}
	if channel.QueueDepth() != 1 {
		t.Fatalf("SABM queue depth = %d, want 1", channel.QueueDepth())
	}
	drainTransmit(t, channel)

	c.server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: wirePacket(t, ax25.Frame{
		Addresses: []ax25.Address{{Callsign: "N0CALL"}, {Callsign: "REMOTE"}}, Control: controlUA,
	})})
	connected := <-c.out
	if connected.Kind != 'C' || connected.CallFrom != "REMOTE" || connected.CallTo != "N0CALL" {
		t.Fatalf("connected event = %#v", connected)
	}

	if !c.handle(Frame{Port: 0, Kind: 'D', PID: 0xf0, CallFrom: "N0CALL", CallTo: "REMOTE", Payload: []byte("hello")}) {
		t.Fatal("connected data rejected")
	}
	if channel.QueueDepth() != 1 {
		t.Fatalf("I frame queue depth = %d, want 1", channel.QueueDepth())
	}
	if !c.handle(Frame{Port: 0, Kind: 'Y', CallFrom: "N0CALL", CallTo: "REMOTE"}) {
		t.Fatal("outstanding query rejected")
	}
	if reply := <-c.out; reply.Kind != 'Y' || binary.LittleEndian.Uint32(reply.Payload) != 1 {
		t.Fatalf("outstanding reply = %#v, want 1", reply)
	}
	drainTransmit(t, channel)

	c.server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: wirePacket(t, ax25.Frame{
		Addresses: []ax25.Address{{Callsign: "N0CALL"}, {Callsign: "REMOTE"}}, Control: 0x21,
	})})
	if !c.handle(Frame{Port: 0, Kind: 'Y', CallFrom: "N0CALL", CallTo: "REMOTE"}) {
		t.Fatal("second outstanding query rejected")
	}
	if reply := <-c.out; binary.LittleEndian.Uint32(reply.Payload) != 0 {
		t.Fatalf("outstanding reply = %#v, want 0", reply)
	}

	if !c.handle(Frame{Port: 0, Kind: 'd', CallFrom: "N0CALL", CallTo: "REMOTE"}) {
		t.Fatal("disconnect request rejected")
	}
	drainTransmit(t, channel)
	c.server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: wirePacket(t, ax25.Frame{
		Addresses: []ax25.Address{{Callsign: "N0CALL"}, {Callsign: "REMOTE"}}, Control: controlUA,
	})})
	if reply := <-c.out; reply.Kind != 'd' {
		t.Fatalf("disconnect event = %#v", reply)
	}
}

func TestIncomingConnectedModeData(t *testing.T) {
	c, channel := newLinkTestClient(t)
	if !c.handle(Frame{Port: 0, Kind: 'X', CallFrom: "N0CALL"}) {
		t.Fatal("registration rejected")
	}
	<-c.out
	c.server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: wirePacket(t, ax25.Frame{
		Addresses: []ax25.Address{{Callsign: "N0CALL"}, {Callsign: "REMOTE"}}, Control: controlSABM,
	})})
	if reply := <-c.out; reply.Kind != 'C' || reply.CallFrom != "REMOTE" {
		t.Fatalf("incoming connect event = %#v", reply)
	}
	drainTransmit(t, channel) // UA

	c.server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: wirePacket(t, ax25.Frame{
		Addresses: []ax25.Address{{Callsign: "N0CALL"}, {Callsign: "REMOTE"}}, Control: 0x00,
		PID: []byte{0xcf}, Information: []byte{0, 1, 2},
	})})
	if reply := <-c.out; reply.Kind != 'D' || reply.PID != 0xcf || string(reply.Payload) != "\x00\x01\x02" {
		t.Fatalf("connected data event = %#v", reply)
	}
	if channel.QueueDepth() != 1 {
		t.Fatalf("RR queue depth = %d, want 1", channel.QueueDepth())
	}
}

func TestConnectViaValidation(t *testing.T) {
	c, _ := newLinkTestClient(t)
	valid := append([]byte{1}, []byte("WIDE1-1\x00\x00\x00")...)
	if !c.handle(Frame{Port: 0, Kind: 'v', CallFrom: "N0CALL", CallTo: "REMOTE", Payload: valid}) {
		t.Fatal("valid connect-via rejected")
	}
	if c.handle(Frame{Port: 0, Kind: 'v', CallFrom: "N0CALL", CallTo: "OTHER", Payload: []byte{8}}) {
		t.Fatal("invalid connect-via accepted")
	}
}

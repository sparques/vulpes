// SPDX-License-Identifier: GPL-2.0-or-later

package agw

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/sparques/vulpes/internal/ax25"
	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

func TestServerQueriesTransmitAndMonitor(t *testing.T) {
	channel, err := tnc.NewChannel(tnc.ChannelConfig{
		Name: "radio", QueueDepth: 4, TXDelayMillis: 300, TXTailMillis: 100,
		SlotMillis: 100, Persistence: 63,
		Modem: modem.Settings{Mode: "afsk1200", SampleRate: 48000, Amplitude: .5, Profile: "A", Slicers: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = channel.Close() })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("TCP loopback is unavailable: %v", err)
		}
		t.Fatal(err)
	}
	server, err := NewServer(listener, map[uint8]*tnc.Channel{0: channel}, 4)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = server.Close()
		<-done
	})
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	sendAGW(t, connection, Frame{Kind: 'R'})
	reply := readAGW(t, connection)
	if reply.Kind != 'R' || len(reply.Payload) != 8 || binary.LittleEndian.Uint32(reply.Payload) != 2005 {
		t.Fatalf("bad version reply: %#v", reply)
	}
	sendAGW(t, connection, Frame{Kind: 'G'})
	reply = readAGW(t, connection)
	if string(reply.Payload) != "1;Port1 radio;\x00" {
		t.Fatalf("bad port reply %q", reply.Payload)
	}

	payload := []byte{0, 1, 2, 3}
	sendAGW(t, connection, Frame{Port: 0, Kind: 'K', Payload: payload})
	deadline := time.Now().Add(time.Second)
	for channel.QueueDepth() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if channel.QueueDepth() != 1 {
		t.Fatal("raw frame was not queued")
	}
	sendAGW(t, connection, Frame{Kind: 'k'})
	time.Sleep(10 * time.Millisecond)
	server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: []byte{4, 5}})
	reply = readAGW(t, connection)
	if reply.Kind != 'K' || string(reply.Payload) != "\x00\x04\x05" {
		t.Fatalf("bad monitor reply: %#v", reply)
	}
}

func TestClientCommandsWithoutNetwork(t *testing.T) {
	channel, err := tnc.NewChannel(tnc.ChannelConfig{
		Name: "radio", QueueDepth: 4, TXDelayMillis: 300, TXTailMillis: 100,
		SlotMillis: 100, Persistence: 63,
		Modem: modem.Settings{Mode: "afsk1200", SampleRate: 48000, Amplitude: .5, Profile: "A", Slicers: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	server := &Server{
		ports: map[uint8]*tnc.Channel{0: channel}, names: map[uint8]string{0: "radio"},
		clients: make(map[*client]struct{}),
	}
	c := &client{server: server, out: make(chan Frame, 8), done: make(chan struct{}), calls: make(map[string]struct{})}
	server.clients[c] = struct{}{}

	if !c.handle(Frame{Kind: 'R'}) {
		t.Fatal("version command rejected")
	}
	if reply := <-c.out; reply.Kind != 'R' || binary.LittleEndian.Uint32(reply.Payload) != 2005 {
		t.Fatalf("bad version reply: %#v", reply)
	}
	if !c.handle(Frame{Kind: 'G'}) {
		t.Fatal("port query rejected")
	}
	if reply := <-c.out; string(reply.Payload) != "1;Port1 radio;\x00" {
		t.Fatalf("bad port reply: %q", reply.Payload)
	}
	if !c.handle(Frame{Kind: 'X', CallFrom: "n0call"}) || !c.handle(Frame{Kind: 'x', CallFrom: "N0CALL"}) {
		t.Fatal("callsign registration command rejected")
	}
	<-c.out
	if reply := <-c.out; len(reply.Payload) != 1 || reply.Payload[0] != 1 {
		t.Fatalf("bad unregister reply: %#v", reply)
	}
	if !c.handle(Frame{Kind: 'K', Payload: []byte{0, 1, 2, 3}}) || channel.QueueDepth() != 1 {
		t.Fatal("raw transmit was not queued")
	}
	if !c.handle(Frame{Kind: 'k'}) {
		t.Fatal("raw monitor enable rejected")
	}
	server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: []byte{4, 5}})
	if reply := <-c.out; reply.Kind != 'K' || string(reply.Payload) != "\x00\x04\x05" {
		t.Fatalf("bad monitor event: %#v", reply)
	}
}

func TestUnprotoTransmit(t *testing.T) {
	tests := []struct {
		name    string
		frame   Frame
		path    []string
		payload string
	}{
		{name: "M", frame: Frame{Kind: 'M', CallFrom: "N0CALL-1", CallTo: "APRS", PID: 0xf0, Payload: []byte("hello")}, path: []string{"APRS", "N0CALL-1"}, payload: "hello"},
		{name: "V", frame: Frame{Kind: 'V', CallFrom: "N0CALL", CallTo: "APRS", PID: 0xcf, Payload: append([]byte{1}, append([]byte("WIDE1-1\x00\x00\x00"), []byte{0, 1, 2}...)...)}, path: []string{"APRS", "N0CALL", "WIDE1-1"}, payload: "\x00\x01\x02"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := unprotoFrame(test.frame)
			if err != nil {
				t.Fatal(err)
			}
			packet, err := ax25.Parse(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if len(packet.Addresses) != len(test.path) {
				t.Fatalf("got %d addresses, want %d", len(packet.Addresses), len(test.path))
			}
			for i, want := range test.path {
				got := packet.Addresses[i].Callsign
				if packet.Addresses[i].SSID != 0 {
					got += fmt.Sprintf("-%d", packet.Addresses[i].SSID)
				}
				if got != want {
					t.Fatalf("address %d = %q, want %q", i, got, want)
				}
			}
			if string(packet.Information) != test.payload || packet.PID[0] != test.frame.PID {
				t.Fatalf("decoded packet = %#v", packet)
			}
		})
	}
}

func sendAGW(t *testing.T, writer io.Writer, frame Frame) {
	t.Helper()
	data, err := Encode(frame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
}

func readAGW(t *testing.T, reader io.Reader) Frame {
	t.Helper()
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(reader, header); err != nil {
		t.Fatal(err)
	}
	length := binary.LittleEndian.Uint32(header[28:32])
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	frames, err := NewDecoder(defaultMaximumPayload).Feed(append(header, payload...))
	if err != nil || len(frames) != 1 {
		t.Fatalf("decode reply: %v, %d frames", err, len(frames))
	}
	return frames[0]
}

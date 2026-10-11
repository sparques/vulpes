// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

func TestTCPMultipleClientsAndDisconnect(t *testing.T) {
	channel := testKISSChannel(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("TCP loopback unavailable: %v", err)
		}
		t.Fatal(err)
	}
	server, err := NewTCPServer(listener, TCPConfig{Ports: map[uint8]*tnc.Channel{0: channel}, WriteQueue: 2})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	clients := make([]net.Conn, 2)
	for i := range clients {
		clients[i], err = net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer clients[i].Close()
	}
	if _, err := clients[0].Write(Encode(Frame{Command: DataFrame, Payload: []byte("input")})); err != nil {
		t.Fatal(err)
	}
	waitQueueDepth(t, channel, 1)
	time.Sleep(10 * time.Millisecond)
	server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: []byte{FEND, FESC}})
	for _, client := range clients {
		if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, 32)
		count, err := client.Read(buffer)
		if err != nil {
			t.Fatal(err)
		}
		frames := NewDecoder(32).Feed(buffer[:count])
		if len(frames) != 1 || !bytes.Equal(frames[0].Payload, []byte{FEND, FESC}) {
			t.Fatalf("broadcast = %#v", frames)
		}
	}
	_ = clients[0].Close()
	server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: []byte("after disconnect")})
	buffer := make([]byte, 64)
	count, err := clients[1].Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if frames := NewDecoder(64).Feed(buffer[:count]); len(frames) != 1 || string(frames[0].Payload) != "after disconnect" {
		t.Fatalf("post-disconnect broadcast = %#v", frames)
	}
	cancel()
	_ = server.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTCPClientSoakClosesAllGoroutines(t *testing.T) {
	channel := testKISSChannel(t)
	server := &TCPServer{
		ports: map[uint8]*tnc.Channel{0: channel}, portsByName: map[string]uint8{"radio": 0},
		writeQueue: 32, clients: make(map[*tcpClient]struct{}),
	}
	baseline := runtime.NumGoroutine()
	const clientCount = 8
	peers := make([]net.Conn, 0, clientCount)
	var drains sync.WaitGroup
	for range clientCount {
		serverEnd, peer := net.Pipe()
		client := &tcpClient{server: server, conn: serverEnd, out: make(chan []byte, 32), done: make(chan struct{})}
		server.clients[client] = struct{}{}
		server.clientWG.Add(1)
		go client.serve()
		peers = append(peers, peer)
		drains.Add(1)
		go func() {
			defer drains.Done()
			_, _ = io.Copy(io.Discard, peer)
		}()
	}
	for sequence := 0; sequence < 1000; sequence++ {
		server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: []byte{byte(sequence), byte(sequence >> 8)}})
	}
	for _, peer := range peers {
		_ = peer.Close()
	}
	server.clientWG.Wait()
	drains.Wait()
	waitFor(t, func() bool { return runtime.NumGoroutine() <= baseline+2 })
}

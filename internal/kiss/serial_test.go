// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

func TestSerialServerLoopbackAndRemoval(t *testing.T) {
	channel := testKISSChannel(t)
	serverEnd, clientEnd := net.Pipe()
	server, err := NewSerialServer(serverEnd, []string{"radio"}, map[string]*tnc.Channel{"radio": channel}, "", 4)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()

	wire := Encode(Frame{Command: DataFrame, Payload: []byte{1, FEND, FESC, 2}})
	for _, fragment := range [][]byte{wire[:2], wire[2:5], wire[5:]} {
		if _, err := clientEnd.Write(fragment); err != nil {
			t.Fatal(err)
		}
	}
	waitQueueDepth(t, channel, 1)

	server.Broadcast("radio", modem.Event{Kind: modem.EventFrame, Payload: []byte{3, FEND, 4}})
	if err := clientEnd.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 64)
	count, err := clientEnd.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	frames := NewDecoder(64).Feed(buffer[:count])
	if len(frames) != 1 || !bytes.Equal(frames[0].Payload, []byte{3, FEND, 4}) {
		t.Fatalf("serial output = %#v", frames)
	}
	_ = clientEnd.Close()
	if err := <-done; err == nil || !strings.Contains(err.Error(), "read KISS serial stream") {
		t.Fatalf("removal error = %v", err)
	}
	cancel()
}

func TestReconnectingSerialServerRetriesAndReconnects(t *testing.T) {
	channel := testKISSChannel(t)
	server, err := NewReconnectingSerialServer("fake", 9600, []string{"radio"}, map[string]*tnc.Channel{"radio": channel}, "")
	if err != nil {
		t.Fatal(err)
	}
	server.retry = time.Millisecond
	var attempts atomic.Int32
	var clientsMu sync.Mutex
	var clients []net.Conn
	server.opener = func(string, int, []string, map[string]*tnc.Channel, string) (*SerialServer, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("device absent")
		}
		left, right := net.Pipe()
		clientsMu.Lock()
		clients = append(clients, right)
		clientsMu.Unlock()
		return NewSerialServer(left, []string{"radio"}, map[string]*tnc.Channel{"radio": channel}, "", 4)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	waitFor(t, func() bool {
		clientsMu.Lock()
		defer clientsMu.Unlock()
		return attempts.Load() >= 2 && len(clients) >= 1
	})
	clientsMu.Lock()
	first := clients[0]
	clientsMu.Unlock()
	_ = first.Close()
	waitFor(t, func() bool { return attempts.Load() >= 3 })
	cancel()
	_ = server.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	clientsMu.Lock()
	for _, client := range clients {
		_ = client.Close()
	}
	clientsMu.Unlock()
}

func TestOpenSerialServerPTY(t *testing.T) {
	if _, err := exec.LookPath("socat"); err != nil {
		t.Skip("socat is not installed")
	}
	directory := t.TempDir()
	left := filepath.Join(directory, "left")
	right := filepath.Join(directory, "right")
	command := exec.Command("socat", "PTY,raw,echo=0,link="+left, "PTY,raw,echo=0,link="+right)
	if err := command.Start(); err != nil {
		t.Skipf("start PTY helper: %v", err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	waitFor(t, func() bool {
		_, leftErr := os.Stat(left)
		_, rightErr := os.Stat(right)
		return leftErr == nil && rightErr == nil
	})
	channel := testKISSChannel(t)
	server, err := OpenSerialServer(left, 9600, []string{"radio"}, map[string]*tnc.Channel{"radio": channel}, "")
	if err != nil {
		t.Fatal(err)
	}
	peer, err := os.OpenFile(right, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	if _, err := peer.Write(Encode(Frame{Command: DataFrame, Payload: []byte("pty")})); err != nil {
		t.Fatal(err)
	}
	waitQueueDepth(t, channel, 1)
	cancel()
	_ = server.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func testKISSChannel(t *testing.T) *tnc.Channel {
	t.Helper()
	channel, err := tnc.NewChannel(tnc.ChannelConfig{Name: "radio", QueueDepth: 8, SlotMillis: 10, Persistence: 255, Modem: modem.Settings{Mode: "afsk1200", SampleRate: 48000, Amplitude: .5, Profile: "A", Slicers: 1}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = channel.Close() })
	return channel
}

func waitQueueDepth(t *testing.T, channel *tnc.Channel, depth int) {
	t.Helper()
	waitFor(t, func() bool { return channel.QueueDepth() == depth })
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ready() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !ready() {
		t.Fatal("condition did not become ready")
	}
}

var _ io.ReadWriteCloser = (net.Conn)(nil)

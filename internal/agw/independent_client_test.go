// SPDX-License-Identifier: GPL-2.0-or-later

package agw

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

// TestIndependentPythonClient deliberately uses no Vulpes codec code on the
// client side. It catches shared-assumption errors in header layout and basic
// query semantics that an in-package loopback client could conceal.
func TestIndependentPythonClient(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	channel, err := tnc.NewChannel(tnc.ChannelConfig{
		Name: "radio", QueueDepth: 4, FullDuplex: true,
		Modem: modem.Settings{Mode: "afsk1200", SampleRate: 48000, Amplitude: .5, Profile: "A", Slicers: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
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
	defer func() {
		cancel()
		_ = server.Close()
		<-done
	}()

	script := `
import socket, struct, sys

def header(kind, port=0, source=b'', destination=b'', pid=0, size=0):
    return struct.pack('<BBBBBBBB10s10sII', port, 0, 0, 0, ord(kind), 0, pid, 0,
                       source.ljust(10, b'\0'), destination.ljust(10, b'\0'), size, 0)

def receive(sock):
    data = b''
    while len(data) < 36:
        part = sock.recv(36 - len(data))
        if not part: raise RuntimeError('short AGW header')
        data += part
    fields = struct.unpack('<BBBBBBBB10s10sII', data)
    payload = b''
    while len(payload) < fields[10]:
        part = sock.recv(fields[10] - len(payload))
        if not part: raise RuntimeError('short AGW payload')
        payload += part
    return chr(fields[4]), payload

sock = socket.create_connection((sys.argv[1], int(sys.argv[2])))
sock.sendall(header('R'))
kind, payload = receive(sock)
assert kind == 'R' and struct.unpack('<II', payload) == (2005, 127)
sock.sendall(header('G'))
kind, payload = receive(sock)
assert kind == 'G' and payload == b'1;Port1 radio;\0'
sock.sendall(header('X', source=b'N0CALL'))
kind, payload = receive(sock)
assert kind == 'X' and payload == b'\1'
sock.sendall(header('y'))
kind, payload = receive(sock)
assert kind == 'y' and struct.unpack('<I', payload) == (0,)
`
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "-c", script, host, port)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("independent AGW client: %v\n%s", err, strings.TrimSpace(string(output)))
	}
}

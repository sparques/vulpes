// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sparques/vulpes/internal/tnc"
)

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestDireWolfKissutilTCP(t *testing.T) {
	program, err := exec.LookPath("kissutil")
	if err != nil {
		t.Skip("Dire Wolf kissutil is not installed")
	}
	channel := testKISSChannel(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("TCP loopback unavailable: %v", err)
		}
		t.Fatal(err)
	}
	server, err := NewTCPServer(listener, TCPConfig{Ports: map[uint8]*tnc.Channel{0: channel}})
	if err != nil {
		t.Fatal(err)
	}
	serverContext, stopServer := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(serverContext) }()

	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	clientContext, stopClient := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopClient()
	command := exec.CommandContext(clientContext, program, "-h", host, "-p", port, "-v")
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	command.Stdin = inputReader
	var output synchronizedBuffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	inputErr := make(chan error, 1)
	go func() {
		_, err := io.WriteString(inputWriter, "h TNC:\nN0CALL>APRS:kissutil interoperability\n")
		inputErr <- err
	}()
	waitQueueDepth(t, channel, 1)
	// kissutil uses block-buffered stdout when it is redirected. Give it time to
	// receive the reply, then terminate it so the buffer is flushed by Wait.
	time.Sleep(100 * time.Millisecond)
	_ = inputWriter.Close()
	stopClient()
	_ = command.Wait() // kissutil remains connected until its input closes or the context expires.
	select {
	case err := <-inputErr:
		if err != nil && !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal(err)
		}
	default:
	}
	if !strings.Contains(output.String(), "VULPES dev") {
		t.Fatalf("kissutil did not display the TNC reply:\n%s", output.String())
	}

	stopServer()
	_ = server.Close()
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

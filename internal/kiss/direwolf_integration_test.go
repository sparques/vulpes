// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sparques/vulpes/internal/tnc"
)

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
	clientContext, stopClient := context.WithTimeout(context.Background(), time.Second)
	defer stopClient()
	command := exec.CommandContext(clientContext, program, "-h", host, "-p", port, "-v")
	command.Stdin = strings.NewReader("h TNC:\nN0CALL>APRS:kissutil interoperability\n")
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	_ = command.Run() // kissutil normally remains connected until the deadline.
	waitQueueDepth(t, channel, 1)
	if !strings.Contains(output.String(), "VULPES dev") {
		t.Fatalf("kissutil did not display the TNC reply:\n%s", output.String())
	}

	stopServer()
	_ = server.Close()
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

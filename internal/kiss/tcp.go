// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sparques/vulpes/internal/config"
	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

const (
	defaultClientQueue = 16
	maximumKISSPayload = 2048
	clientWriteTimeout = 10 * time.Second
)

// TCPConfig associates KISS port nibbles with channels. BoundChannel, when
// set, routes all client transmissions to that channel regardless of port.
type TCPConfig struct {
	Ports        map[uint8]*tnc.Channel
	BoundChannel string
	WriteQueue   int
}

// TCPCounters are cumulative transport-level backpressure and input errors.
type TCPCounters struct {
	DroppedOutbound   uint64
	RejectedInbound   uint64
	ClientDisconnects uint64
}

// TCPServer serves concurrent KISS/TCP clients on an existing listener.
type TCPServer struct {
	listener    net.Listener
	ports       map[uint8]*tnc.Channel
	portsByName map[string]uint8
	bound       *tnc.Channel
	writeQueue  int
	mu          sync.RWMutex
	clients     map[*tcpClient]struct{}
	closed      bool
	dropped     atomic.Uint64
	rejected    atomic.Uint64
	disconnects atomic.Uint64
	clientWG    sync.WaitGroup
}

type tcpClient struct {
	server *TCPServer
	conn   net.Conn
	out    chan []byte
	done   chan struct{}
	once   sync.Once
}

// NewTCPServer validates channel routing for an already-open listener.
func NewTCPServer(listener net.Listener, config TCPConfig) (*TCPServer, error) {
	if listener == nil {
		return nil, errors.New("KISS TCP listener cannot be nil")
	}
	if config.WriteQueue <= 0 {
		config.WriteQueue = defaultClientQueue
	}
	ports := make(map[uint8]*tnc.Channel, len(config.Ports))
	portsByName := make(map[string]uint8, len(config.Ports))
	portNumbers := make([]int, 0, len(config.Ports))
	for port := range config.Ports {
		portNumbers = append(portNumbers, int(port))
	}
	sort.Ints(portNumbers)
	for _, number := range portNumbers {
		port := uint8(number)
		channel := config.Ports[port]
		if port > 15 || channel == nil {
			return nil, fmt.Errorf("invalid KISS port mapping %d", port)
		}
		ports[port] = channel
		if _, exists := portsByName[channel.Name()]; !exists {
			portsByName[channel.Name()] = port
		}
	}
	var bound *tnc.Channel
	if config.BoundChannel != "" {
		for _, channel := range ports {
			if channel.Name() == config.BoundChannel {
				bound = channel
				break
			}
		}
		if bound == nil {
			return nil, fmt.Errorf("KISS listener is bound to unknown channel %q", config.BoundChannel)
		}
	}
	if len(ports) == 0 {
		return nil, errors.New("KISS TCP server requires at least one port mapping")
	}
	return &TCPServer{
		listener: listener, ports: ports, portsByName: portsByName, bound: bound,
		writeQueue: config.WriteQueue, clients: make(map[*tcpClient]struct{}),
	}, nil
}

// NewConfiguredTCPServer applies one validated YAML listener to the configured
// channel order (KISS port numbers are zero-based channel indexes).
func NewConfiguredTCPServer(listener net.Listener, configured config.TCPListener, channelOrder []string, channels map[string]*tnc.Channel) (*TCPServer, error) {
	if len(channelOrder) > 16 {
		return nil, errors.New("KISS supports at most 16 channel ports")
	}
	ports := make(map[uint8]*tnc.Channel, len(channelOrder))
	for index, name := range channelOrder {
		channel := channels[name]
		if channel == nil {
			return nil, fmt.Errorf("KISS channel order references unknown channel %q", name)
		}
		ports[uint8(index)] = channel
	}
	return NewTCPServer(listener, TCPConfig{Ports: ports, BoundChannel: configured.Channel})
}

// Serve accepts clients until the context is cancelled or the listener fails.
func (s *TCPServer) Serve(ctx context.Context) error {
	watcherDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-watcherDone:
		}
	}()
	var serveErr error
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() == nil && !s.isClosed() {
				serveErr = err
			}
			break
		}
		client := &tcpClient{server: s, conn: conn, out: make(chan []byte, s.writeQueue), done: make(chan struct{})}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		s.clients[client] = struct{}{}
		s.clientWG.Add(1)
		s.mu.Unlock()
		go client.serve()
	}
	_ = s.Close()
	close(watcherDone)
	s.clientWG.Wait()
	return serveErr
}

// Broadcast sends a completed AX.25 frame to eligible KISS clients. Each
// client has an independent bounded output queue; a slow client drops frames
// rather than blocking demodulation or other clients.
func (s *TCPServer) Broadcast(channelName string, event modem.Event) {
	if event.Kind != modem.EventFrame {
		return
	}
	port, ok := s.portsByName[channelName]
	if !ok {
		return
	}
	encoded := Encode(Frame{Port: port, Command: DataFrame, Payload: event.Payload})
	s.mu.RLock()
	defer s.mu.RUnlock()
	for client := range s.clients {
		select {
		case <-client.done:
		case client.out <- encoded:
		default:
			s.dropped.Add(1)
		}
	}
}

// Counters reports dropped outbound and rejected inbound frames.
func (s *TCPServer) Counters() TCPCounters {
	return TCPCounters{DroppedOutbound: s.dropped.Load(), RejectedInbound: s.rejected.Load(), ClientDisconnects: s.disconnects.Load()}
}

// Close stops accepting clients and closes every active connection.
func (s *TCPServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	clients := make([]*tcpClient, 0, len(s.clients))
	for client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.Unlock()
	listenerErr := s.listener.Close()
	for _, client := range clients {
		client.close()
	}
	if errors.Is(listenerErr, net.ErrClosed) {
		return nil
	}
	return listenerErr
}

func (s *TCPServer) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

func (s *TCPServer) handle(frame Frame) (*Frame, bool) {
	channel := s.bound
	if channel == nil {
		channel = s.ports[frame.Port]
	}
	response, ok := applyFrame(frame, channel)
	if !ok {
		s.rejected.Add(1)
	}
	return response, ok
}

func (c *tcpClient) serve() {
	defer c.server.clientWG.Done()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		c.writeLoop()
	}()
	decoder := NewDecoder(maximumKISSPayload + 1)
	buffer := make([]byte, 4096)
	for {
		count, err := c.conn.Read(buffer)
		if count > 0 {
			for _, frame := range decoder.Feed(buffer[:count]) {
				if response, ok := c.server.handle(frame); ok && response != nil {
					select {
					case c.out <- Encode(*response):
					default:
						c.server.dropped.Add(1)
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	c.close()
	<-writerDone
}

func (c *tcpClient) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case data := <-c.out:
			if err := c.conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout)); err != nil {
				c.close()
				return
			}
			for len(data) > 0 {
				count, err := c.conn.Write(data)
				if err != nil || count == 0 {
					c.close()
					return
				}
				data = data[count:]
			}
		}
	}
}

func (c *tcpClient) close() {
	c.once.Do(func() {
		c.server.disconnects.Add(1)
		close(c.done)
		_ = c.conn.Close()
		c.server.mu.Lock()
		delete(c.server.clients, c)
		c.server.mu.Unlock()
	})
}

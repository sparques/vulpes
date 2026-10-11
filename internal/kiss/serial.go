// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
	"go.bug.st/serial"
)

const serialReadTimeout = 200 * time.Millisecond

const serialReconnectDelay = time.Second

type serialOpener func(string, int, []string, map[string]*tnc.Channel, string) (*SerialServer, error)

// ReconnectingSerialServer keeps a configured KISS serial endpoint available
// across device removal, USB re-enumeration, and transient open failures.
type ReconnectingSerialServer struct {
	device, bound string
	baud          int
	channelOrder  []string
	channels      map[string]*tnc.Channel
	opener        serialOpener
	retry         time.Duration
	done          chan struct{}
	closeOnce     sync.Once
	mu            sync.RWMutex
	active        *SerialServer
}

// NewReconnectingSerialServer validates a configured serial endpoint. The
// device is opened by Run so startup and later reconnects share one path.
func NewReconnectingSerialServer(device string, baud int, channelOrder []string, channels map[string]*tnc.Channel, boundChannel string) (*ReconnectingSerialServer, error) {
	if device == "" || baud <= 0 {
		return nil, errors.New("KISS serial requires a device and positive baud rate")
	}
	if _, err := ChannelPortMap(channelOrder, channels); err != nil {
		return nil, err
	}
	if boundChannel != "" && channels[boundChannel] == nil {
		return nil, fmt.Errorf("KISS serial is bound to unknown channel %q", boundChannel)
	}
	return &ReconnectingSerialServer{
		device: device, baud: baud, channelOrder: append([]string(nil), channelOrder...), channels: channels,
		bound: boundChannel, opener: OpenSerialServer, retry: serialReconnectDelay, done: make(chan struct{}),
	}, nil
}

// Run polls for the configured device and reconnects whenever it disappears.
func (s *ReconnectingSerialServer) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.done:
			return nil
		default:
		}
		server, err := s.opener(s.device, s.baud, s.channelOrder, s.channels, s.bound)
		if err == nil {
			s.mu.Lock()
			s.active = server
			s.mu.Unlock()
			err = server.Run(ctx)
			s.mu.Lock()
			if s.active == server {
				s.active = nil
			}
			s.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-s.done:
			return nil
		case <-time.After(s.retry):
		}
	}
}

// Broadcast forwards receive frames to the currently connected device.
func (s *ReconnectingSerialServer) Broadcast(channelName string, event modem.Event) {
	s.mu.RLock()
	active := s.active
	s.mu.RUnlock()
	if active != nil {
		active.Broadcast(channelName, event)
	}
}

// Close stops polling and interrupts the current serial connection.
func (s *ReconnectingSerialServer) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.done)
		s.mu.RLock()
		active := s.active
		s.mu.RUnlock()
		if active != nil {
			closeErr = active.Close()
		}
	})
	return closeErr
}

// SerialServer provides KISS framing on an opened serial-like stream. The
// server owns and closes the stream after construction.
type SerialServer struct {
	port        io.ReadWriteCloser
	ports       map[uint8]*tnc.Channel
	portsByName map[string]uint8
	bound       *tnc.Channel
	out         chan []byte
	done        chan struct{}
	closeOnce   sync.Once
	rejected    atomic.Uint64
	dropped     atomic.Uint64
}

// OpenSerialServer opens a raw 8N1 KISS device and routes channel indexes in
// the same order as channelOrder. A non-empty boundChannel overrides input
// port selection.
func OpenSerialServer(device string, baud int, channelOrder []string, channels map[string]*tnc.Channel, boundChannel string) (*SerialServer, error) {
	if device == "" || baud <= 0 {
		return nil, errors.New("KISS serial requires a device and positive baud rate")
	}
	port, err := serial.Open(device, &serial.Mode{BaudRate: baud, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit})
	if err != nil {
		return nil, fmt.Errorf("open KISS serial device %q: %w", device, err)
	}
	if err := port.SetReadTimeout(serialReadTimeout); err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("set KISS serial read timeout: %w", err)
	}
	server, err := NewSerialServer(port, channelOrder, channels, boundChannel, defaultClientQueue)
	if err != nil {
		_ = port.Close()
		return nil, err
	}
	return server, nil
}

// NewSerialServer wraps a serial or PTY stream with KISS routing. writeQueue
// bounds pending receive frames; overflow drops the newest frame.
func NewSerialServer(port io.ReadWriteCloser, channelOrder []string, channels map[string]*tnc.Channel, boundChannel string, writeQueue int) (*SerialServer, error) {
	if port == nil {
		return nil, errors.New("KISS serial stream cannot be nil")
	}
	if len(channelOrder) == 0 || len(channelOrder) > 16 {
		return nil, errors.New("KISS serial requires between 1 and 16 channel ports")
	}
	if writeQueue <= 0 {
		writeQueue = defaultClientQueue
	}
	ports := make(map[uint8]*tnc.Channel, len(channelOrder))
	portsByName := make(map[string]uint8, len(channelOrder))
	var bound *tnc.Channel
	for index, name := range channelOrder {
		channel := channels[name]
		if channel == nil {
			return nil, fmt.Errorf("KISS serial channel order references unknown channel %q", name)
		}
		ports[uint8(index)] = channel
		if _, exists := portsByName[name]; !exists {
			portsByName[name] = uint8(index)
		}
		if name == boundChannel {
			bound = channel
		}
	}
	if boundChannel != "" && bound == nil {
		return nil, fmt.Errorf("KISS serial is bound to unknown channel %q", boundChannel)
	}
	return &SerialServer{port: port, ports: ports, portsByName: portsByName, bound: bound,
		out: make(chan []byte, writeQueue), done: make(chan struct{})}, nil
}

// Run reads and writes KISS frames until cancellation or a serial error.
func (s *SerialServer) Run(ctx context.Context) error {
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		s.writeLoop()
	}()
	watcherDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-watcherDone:
		}
	}()
	decoder := NewDecoder(maximumKISSPayload + 1)
	buffer := make([]byte, 2048)
	var runErr error
	for {
		if err := ctx.Err(); err != nil {
			break
		}
		count, err := s.port.Read(buffer)
		if count > 0 {
			for _, frame := range decoder.Feed(buffer[:count]) {
				s.handle(frame)
			}
		}
		if err != nil {
			if ctx.Err() == nil {
				runErr = fmt.Errorf("read KISS serial stream: %w", err)
			}
			break
		}
	}
	_ = s.Close()
	close(watcherDone)
	<-writerDone
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return runErr
}

// Broadcast queues an AX.25 frame for serial delivery without blocking the
// receiver. Other event types are not KISS data frames.
func (s *SerialServer) Broadcast(channelName string, event modem.Event) {
	if event.Kind != modem.EventFrame {
		return
	}
	port, ok := s.portsByName[channelName]
	if !ok {
		return
	}
	encoded := Encode(Frame{Port: port, Command: DataFrame, Payload: event.Payload})
	select {
	case <-s.done:
		return
	default:
	}
	select {
	case <-s.done:
	case s.out <- encoded:
	default:
		s.dropped.Add(1)
	}
}

// Counters reports dropped outbound and rejected inbound frames.
func (s *SerialServer) Counters() TCPCounters {
	return TCPCounters{DroppedOutbound: s.dropped.Load(), RejectedInbound: s.rejected.Load()}
}

// Close interrupts reads and writes and closes the underlying serial stream.
func (s *SerialServer) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.done)
		closeErr = s.port.Close()
	})
	return closeErr
}

func (s *SerialServer) handle(frame Frame) {
	channel := s.bound
	if channel == nil {
		channel = s.ports[frame.Port]
	}
	response, ok := applyFrame(frame, channel)
	if !ok {
		s.rejected.Add(1)
		return
	}
	if response != nil {
		select {
		case s.out <- Encode(*response):
		default:
			s.dropped.Add(1)
		}
	}
}

func (s *SerialServer) writeLoop() {
	for {
		select {
		case <-s.done:
			return
		case data := <-s.out:
			for len(data) > 0 {
				count, err := s.port.Write(data)
				if err != nil || count == 0 {
					_ = s.Close()
					return
				}
				data = data[count:]
			}
		}
	}
}

// ChannelPortMap creates the KISS port mapping for configured channel order.
func ChannelPortMap(channelOrder []string, channels map[string]*tnc.Channel) (map[uint8]*tnc.Channel, error) {
	if len(channelOrder) == 0 || len(channelOrder) > 16 {
		return nil, errors.New("KISS requires between 1 and 16 channel ports")
	}
	ports := make(map[uint8]*tnc.Channel, len(channelOrder))
	for index, name := range channelOrder {
		channel := channels[name]
		if channel == nil {
			return nil, fmt.Errorf("channel order references unknown channel %q", name)
		}
		ports[uint8(index)] = channel
	}
	return ports, nil
}

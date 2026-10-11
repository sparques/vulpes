// SPDX-License-Identifier: GPL-2.0-or-later

package agw

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sparques/vulpes/internal/ax25"
	"github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/tnc"
)

const (
	defaultWriteQueue = 16
	writeTimeout      = 10 * time.Second
)

// Server provides the modem-facing subset of the AGWPE TCP protocol. It owns
// the listener but not the channels.
type Server struct {
	listener     net.Listener
	ports        map[uint8]*tnc.Channel
	names        map[uint8]string
	queue        int
	mu           sync.RWMutex
	clients      map[*client]struct{}
	closed       bool
	wg           sync.WaitGroup
	dropped      atomic.Uint64
	rejected     atomic.Uint64
	disconnected atomic.Uint64
}

type client struct {
	server     *Server
	conn       net.Conn
	out        chan Frame
	done       chan struct{}
	once       sync.Once
	monitorRaw atomic.Bool
	monitor    atomic.Bool
	mu         sync.Mutex
	calls      map[string]struct{}
	links      map[linkKey]*linkState
}

// Counters summarizes input rejection and per-client output backpressure.
type Counters struct {
	DroppedOutbound   uint64
	RejectedInbound   uint64
	ClientDisconnects uint64
}

// NewServer constructs an AGWPE service. Port numbers are zero based and must
// fit in the protocol's one-byte port field.
func NewServer(listener net.Listener, ports map[uint8]*tnc.Channel, writeQueue int) (*Server, error) {
	if listener == nil {
		return nil, errors.New("AGW listener cannot be nil")
	}
	if len(ports) == 0 {
		return nil, errors.New("AGW server requires at least one port")
	}
	if writeQueue <= 0 {
		writeQueue = defaultWriteQueue
	}
	copyPorts := make(map[uint8]*tnc.Channel, len(ports))
	names := make(map[uint8]string, len(ports))
	for port, channel := range ports {
		if channel == nil {
			return nil, fmt.Errorf("AGW port %d has no channel", port)
		}
		copyPorts[port], names[port] = channel, channel.Name()
	}
	return &Server{listener: listener, ports: copyPorts, names: names, queue: writeQueue, clients: make(map[*client]struct{})}, nil
}

// Serve accepts clients until cancellation, Close, or an unexpected listener
// error. Slow clients cannot block other clients or modem receive processing.
func (s *Server) Serve(ctx context.Context) error {
	wake := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-wake:
		}
	}()
	var serveErr error
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() == nil && !s.isClosed() {
				serveErr = err
			}
			break
		}
		c := &client{server: s, conn: connection, out: make(chan Frame, s.queue), done: make(chan struct{}), calls: make(map[string]struct{}), links: make(map[linkKey]*linkState)}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = connection.Close()
			continue
		}
		s.clients[c] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go c.serve()
	}
	_ = s.Close()
	close(wake)
	s.wg.Wait()
	return serveErr
}

// Broadcast sends an FCS-free AX.25 frame to clients which enabled raw
// monitoring with the AGW 'k' command. The leading zero is the legacy AGW TNC
// selector retained by Dire Wolf's wire format.
func (s *Server) Broadcast(channelName string, event modem.Event) {
	if event.Kind != modem.EventFrame {
		return
	}
	var port uint8
	found := false
	for candidate, name := range s.names {
		if name == channelName {
			port, found = candidate, true
			break
		}
	}
	if !found {
		return
	}
	payload := make([]byte, len(event.Payload)+1)
	payload[0] = port << 4
	copy(payload[1:], event.Payload)
	frame := Frame{Port: port, Kind: 'K', Payload: payload}
	packet, packetOK := ax25.Parse(event.Payload)
	monitorFrame, monitorOK := makeMonitorFrame(port, event.Payload, false)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for c := range s.clients {
		if packetOK == nil {
			c.receiveConnected(port, packet)
		}
		if c.monitorRaw.Load() {
			s.queueClient(c, frame)
		}
		if c.monitor.Load() && monitorOK {
			s.queueClient(c, monitorFrame)
		}
	}
}

func (s *Server) queueClient(c *client, frame Frame) {
	select {
	case <-c.done:
	case c.out <- frame:
	default:
		s.dropped.Add(1)
	}
}

func makeMonitorFrame(port uint8, raw []byte, own bool) (Frame, bool) {
	packet, err := ax25.Parse(raw)
	if err != nil {
		return Frame{}, false
	}
	kind := byte('S')
	description := fmt.Sprintf("<%02X", packet.Control)
	if packet.Control&1 == 0 {
		kind = 'I'
		description = "<I"
	} else if packet.Control&0xef == 0x03 {
		kind = 'U'
		description = "<UI"
	}
	if own && kind == 'U' {
		kind = 'T'
	}
	if len(packet.PID) != 0 {
		description += fmt.Sprintf(" pid=%02X", packet.PID[len(packet.PID)-1])
	}
	description += fmt.Sprintf(" Len=%d >", len(packet.Information))
	addressText := fmt.Sprintf(" %d:Fm %s To %s", port+1, formatAddress(packet.Addresses[1]), formatAddress(packet.Addresses[0]))
	if len(packet.Addresses) > 2 {
		addressText += " Via "
		for i, address := range packet.Addresses[2:] {
			if i != 0 {
				addressText += ","
			}
			addressText += formatAddress(address)
			if address.Flag {
				addressText += "*"
			}
		}
	}
	payload := []byte(fmt.Sprintf("%s %s[%s]\r", addressText, description, time.Now().Format("15:04:05")))
	if len(packet.Information) != 0 {
		payload = append(payload, packet.Information...)
		payload = append(payload, '\r')
	}
	payload = append(payload, 0)
	return Frame{Port: port, Kind: kind, PID: firstByte(packet.PID), CallFrom: formatAddress(packet.Addresses[1]), CallTo: formatAddress(packet.Addresses[0]), Payload: payload}, true
}

func firstByte(value []byte) byte {
	if len(value) == 0 {
		return 0
	}
	return value[0]
}

func formatAddress(address ax25.Address) string {
	if address.SSID == 0 {
		return address.Callsign
	}
	return fmt.Sprintf("%s-%d", address.Callsign, address.SSID)
}

func (s *Server) Counters() Counters {
	return Counters{DroppedOutbound: s.dropped.Load(), RejectedInbound: s.rejected.Load(), ClientDisconnects: s.disconnected.Load()}
}

func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	err := s.listener.Close()
	for _, c := range clients {
		c.close()
	}
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (s *Server) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

func (c *client) serve() {
	defer c.server.wg.Done()
	written := make(chan struct{})
	go func() { defer close(written); c.writeLoop() }()
	linkDone := make(chan struct{})
	go func() {
		defer close(linkDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-c.done:
				return
			case now := <-ticker.C:
				c.serviceLinks(now)
			}
		}
	}()
	decoder := NewDecoder(defaultMaximumPayload)
	buffer := make([]byte, 4096)
	for {
		n, err := c.conn.Read(buffer)
		if n > 0 {
			frames, decodeErr := decoder.Feed(buffer[:n])
			if decodeErr != nil {
				c.server.rejected.Add(1)
				break
			}
			for _, frame := range frames {
				if !c.handle(frame) {
					c.server.rejected.Add(1)
				}
			}
		}
		if err != nil {
			break
		}
	}
	c.close()
	<-written
	<-linkDone
}

func (c *client) handle(frame Frame) bool {
	channel := c.server.ports[frame.Port]
	switch frame.Kind {
	case 'R':
		payload := make([]byte, 8)
		binary.LittleEndian.PutUint32(payload, 2005)
		binary.LittleEndian.PutUint32(payload[4:], 127)
		return c.reply(Frame{Kind: 'R', Payload: payload})
	case 'G':
		ports := make([]int, 0, len(c.server.ports))
		for port := range c.server.ports {
			ports = append(ports, int(port))
		}
		sort.Ints(ports)
		var info strings.Builder
		fmt.Fprintf(&info, "%d;", len(ports))
		for _, port := range ports {
			fmt.Fprintf(&info, "Port%d %s;", port+1, c.server.names[uint8(port)])
		}
		return c.reply(Frame{Kind: 'G', Payload: append([]byte(info.String()), 0)})
	case 'g':
		if channel == nil {
			return false
		}
		timing := channel.Timing()
		payload := make([]byte, 12)
		payload[1] = 1
		payload[2] = byte(timing.TXDelayMillis / 10)
		payload[3] = byte(timing.TXTailMillis / 10)
		payload[4] = byte(timing.Persistence)
		payload[5] = byte(timing.SlotMillis / 10)
		payload[6] = 7
		binary.LittleEndian.PutUint32(payload[8:], uint32(channel.QueueDepth()))
		return c.reply(Frame{Port: frame.Port, Kind: 'g', Payload: payload})
	case 'X':
		call := strings.ToUpper(strings.TrimSpace(frame.CallFrom))
		if call == "" {
			return false
		}
		c.mu.Lock()
		c.calls[call] = struct{}{}
		c.mu.Unlock()
		return c.reply(Frame{Port: frame.Port, Kind: 'X', CallFrom: frame.CallFrom, Payload: []byte{1}})
	case 'x':
		call := strings.ToUpper(strings.TrimSpace(frame.CallFrom))
		c.mu.Lock()
		_, ok := c.calls[call]
		delete(c.calls, call)
		c.mu.Unlock()
		value := byte(0)
		if ok {
			value = 1
		}
		return c.reply(Frame{Port: frame.Port, Kind: 'x', CallFrom: frame.CallFrom, Payload: []byte{value}})
	case 'y':
		if channel == nil {
			return false
		}
		payload := make([]byte, 4)
		binary.LittleEndian.PutUint32(payload, uint32(channel.QueueDepth()))
		return c.reply(Frame{Port: frame.Port, Kind: 'y', Payload: payload})
	case 'k':
		c.monitorRaw.Store(!c.monitorRaw.Load())
		return true
	case 'm':
		c.monitor.Store(!c.monitor.Load())
		return true
	case 'K':
		return channel != nil && len(frame.Payload) > 1 && channel.QueueFrame(frame.Payload[1:]) == nil
	case 'M', 'V':
		if channel == nil {
			return false
		}
		packet, err := unprotoFrame(frame)
		return err == nil && channel.QueueFrame(packet) == nil
	case 'C', 'v', 'c':
		return c.connect(frame)
	case 'D':
		return c.sendConnected(frame)
	case 'd':
		return c.disconnectLink(frame)
	case 'Y':
		return c.outstanding(frame)
	default:
		return false
	}
}

func unprotoFrame(frame Frame) ([]byte, error) {
	source, err := parseAddress(frame.CallFrom)
	if err != nil {
		return nil, fmt.Errorf("source callsign: %w", err)
	}
	destination, err := parseAddress(frame.CallTo)
	if err != nil {
		return nil, fmt.Errorf("destination callsign: %w", err)
	}
	addresses := []ax25.Address{destination, source}
	information := frame.Payload
	if frame.Kind == 'V' {
		if len(information) < 1 {
			return nil, errors.New("AGW V payload lacks repeater count")
		}
		count := int(information[0])
		if count > ax25.MaxAddresses-2 || len(information) < 1+count*10 {
			return nil, errors.New("AGW V repeater list is truncated or too long")
		}
		for i := range count {
			name := strings.TrimRight(string(information[1+i*10:1+(i+1)*10]), "\x00 ")
			repeater, err := parseAddress(name)
			if err != nil {
				return nil, fmt.Errorf("repeater %d: %w", i, err)
			}
			addresses = append(addresses, repeater)
		}
		information = information[1+count*10:]
	}
	pid := frame.PID
	if pid == 0 {
		pid = 0xf0
	}
	return (ax25.Frame{Addresses: addresses, Control: 0x03, PID: []byte{pid}, Information: information}).MarshalBinary()
}

func parseAddress(value string) (ax25.Address, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return ax25.Address{}, ax25.ErrInvalidAddress
	}
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return ax25.Address{}, ax25.ErrInvalidAddress
	}
	address := ax25.Address{Callsign: parts[0]}
	if len(parts) == 2 {
		var ssid int
		if _, err := fmt.Sscanf(parts[1], "%d", &ssid); err != nil || ssid < 0 || ssid > 15 {
			return ax25.Address{}, ax25.ErrInvalidAddress
		}
		address.SSID = uint8(ssid)
	}
	return address, nil
}

func (c *client) reply(frame Frame) bool {
	select {
	case <-c.done:
		return false
	case c.out <- frame:
		return true
	default:
		c.server.dropped.Add(1)
		return false
	}
}

func (c *client) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case frame := <-c.out:
			data, err := Encode(frame)
			if err != nil {
				c.close()
				return
			}
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				c.close()
				return
			}
			for len(data) > 0 {
				n, err := c.conn.Write(data)
				if err != nil || n == 0 {
					c.close()
					return
				}
				data = data[n:]
			}
		}
	}
}

func (c *client) close() {
	c.once.Do(func() {
		c.server.disconnected.Add(1)
		close(c.done)
		_ = c.conn.Close()
		c.server.mu.Lock()
		delete(c.server.clients, c)
		c.server.mu.Unlock()
	})
}

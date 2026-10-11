// SPDX-License-Identifier: GPL-2.0-or-later

package agw

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sparques/vulpes/internal/ax25"
)

const (
	controlSABM = 0x2f
	controlDISC = 0x43
	controlUA   = 0x63
	controlDM   = 0x0f
	linkRetry   = 3 * time.Second
	linkRetries = 3
	linkQueue   = 32
)

type linkKey struct {
	port        uint8
	local, peer string
}

type linkState struct {
	key         linkKey
	path        []ax25.Address
	pid         byte
	connected   bool
	closing     bool
	vs, vr      byte
	pending     [][]byte
	outstanding []byte
	deadline    time.Time
	retries     int
}

func normalizedCall(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }

func (c *client) connect(frame Frame) bool {
	channel := c.server.ports[frame.Port]
	if channel == nil {
		return false
	}
	local, err := parseAddress(frame.CallFrom)
	if err != nil {
		return false
	}
	peer, err := parseAddress(frame.CallTo)
	if err != nil {
		return false
	}
	path := []ax25.Address{peer, local}
	pid := frame.PID
	if pid == 0 {
		pid = 0xf0
	}
	if frame.Kind == 'v' {
		var repeaters []ax25.Address
		repeaters, err = parseVia(frame.Payload)
		if err != nil {
			return false
		}
		path = append(path, repeaters...)
	}
	key := linkKey{port: frame.Port, local: formatAddress(local), peer: formatAddress(peer)}
	link := &linkState{key: key, path: path, pid: pid, deadline: time.Now().Add(linkRetry)}
	c.mu.Lock()
	if _, exists := c.links[key]; exists {
		c.mu.Unlock()
		return false
	}
	c.links[key] = link
	c.mu.Unlock()
	if !queueAX25Control(channel, path, controlSABM) {
		c.mu.Lock()
		delete(c.links, key)
		c.mu.Unlock()
		return false
	}
	return true
}

func parseVia(payload []byte) ([]ax25.Address, error) {
	if len(payload) < 1 {
		return nil, errors.New("AGW connect-via payload lacks repeater count")
	}
	count := int(payload[0])
	if count < 1 || count > ax25.MaxAddresses-2 || len(payload) < 1+count*10 {
		return nil, errors.New("AGW connect-via repeater list is invalid")
	}
	result := make([]ax25.Address, 0, count)
	for i := range count {
		name := strings.TrimRight(string(payload[1+i*10:1+(i+1)*10]), "\x00 ")
		address, err := parseAddress(name)
		if err != nil {
			return nil, fmt.Errorf("repeater %d: %w", i, err)
		}
		result = append(result, address)
	}
	return result, nil
}

func queueAX25Control(channel frameQueue, path []ax25.Address, control byte) bool {
	packet, err := (ax25.Frame{Addresses: path, Control: control}).MarshalBinary()
	return err == nil && channel.QueueFrame(packet) == nil
}

type frameQueue interface{ QueueFrame([]byte) error }

func (c *client) sendConnected(frame Frame) bool {
	key := linkKey{port: frame.Port, local: normalizedCall(frame.CallFrom), peer: normalizedCall(frame.CallTo)}
	c.mu.Lock()
	link := c.links[key]
	if link == nil || !link.connected || link.closing || len(link.pending) >= linkQueue {
		c.mu.Unlock()
		return false
	}
	pid := frame.PID
	if pid == 0 {
		pid = link.pid
	}
	data := make([]byte, len(frame.Payload)+1)
	data[0] = pid
	copy(data[1:], frame.Payload)
	link.pending = append(link.pending, data)
	c.flushLinkLocked(link)
	c.mu.Unlock()
	return true
}

func (c *client) disconnectLink(frame Frame) bool {
	key := linkKey{port: frame.Port, local: normalizedCall(frame.CallFrom), peer: normalizedCall(frame.CallTo)}
	c.mu.Lock()
	link := c.links[key]
	if link == nil {
		c.mu.Unlock()
		return false
	}
	link.closing = true
	link.deadline = time.Now().Add(linkRetry)
	link.retries = 0
	path := append([]ax25.Address(nil), link.path...)
	c.mu.Unlock()
	return queueAX25Control(c.server.ports[frame.Port], path, controlDISC)
}

func (c *client) outstanding(frame Frame) bool {
	key := linkKey{port: frame.Port, local: normalizedCall(frame.CallFrom), peer: normalizedCall(frame.CallTo)}
	c.mu.Lock()
	link := c.links[key]
	count := 0
	if link != nil {
		count = len(link.pending)
		if link.outstanding != nil {
			count++
		}
	}
	c.mu.Unlock()
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, uint32(count))
	return c.reply(Frame{Port: frame.Port, Kind: 'Y', CallFrom: frame.CallFrom, CallTo: frame.CallTo, Payload: payload})
}

func (c *client) flushLinkLocked(link *linkState) {
	if !link.connected || link.outstanding != nil || len(link.pending) == 0 {
		return
	}
	data := link.pending[0]
	link.pending = link.pending[1:]
	control := link.vs<<1 | link.vr<<5
	packet, err := (ax25.Frame{Addresses: link.path, Control: control, PID: data[:1], Information: data[1:]}).MarshalBinary()
	if err != nil || c.server.ports[link.key.port].QueueFrame(packet) != nil {
		link.pending = append([][]byte{data}, link.pending...)
		return
	}
	link.outstanding = packet
	link.deadline = time.Now().Add(linkRetry)
	link.retries = 0
}

func reversePath(packet ax25.Frame) []ax25.Address {
	path := []ax25.Address{packet.Addresses[1], packet.Addresses[0]}
	for i := len(packet.Addresses) - 1; i >= 2; i-- {
		address := packet.Addresses[i]
		address.Flag = false
		path = append(path, address)
	}
	return path
}

func (c *client) receiveConnected(port uint8, packet ax25.Frame) bool {
	if len(packet.Addresses) < 2 {
		return false
	}
	local := formatAddress(packet.Addresses[0])
	peer := formatAddress(packet.Addresses[1])
	key := linkKey{port: port, local: local, peer: peer}
	c.mu.Lock()
	link := c.links[key]
	if link == nil && packet.Control&0xef == controlSABM {
		if _, registered := c.calls[local]; !registered {
			c.mu.Unlock()
			return false
		}
		link = &linkState{key: key, path: reversePath(packet), pid: 0xf0, connected: true}
		c.links[key] = link
		path := append([]ax25.Address(nil), link.path...)
		c.mu.Unlock()
		if !queueAX25Control(c.server.ports[port], path, controlUA) {
			return false
		}
		return c.reply(Frame{Port: port, Kind: 'C', CallFrom: peer, CallTo: local, Payload: []byte("*** CONNECTED To Station " + peer + "\r")})
	}
	if link == nil {
		c.mu.Unlock()
		return false
	}
	control := packet.Control
	switch {
	case control&0xef == controlSABM:
		link.connected = true
		link.closing = false
		link.vs, link.vr = 0, 0
		link.pending, link.outstanding = nil, nil
		link.deadline = time.Time{}
		path := append([]ax25.Address(nil), link.path...)
		c.mu.Unlock()
		return queueAX25Control(c.server.ports[port], path, controlUA)
	case control&0xef == controlUA && !link.connected && !link.closing:
		link.connected = true
		link.deadline = time.Time{}
		c.mu.Unlock()
		return c.reply(Frame{Port: port, Kind: 'C', CallFrom: peer, CallTo: local, Payload: []byte("*** CONNECTED With Station " + peer + "\r")})
	case control&0xef == controlUA && link.closing:
		delete(c.links, key)
		c.mu.Unlock()
		return c.reply(Frame{Port: port, Kind: 'd', CallFrom: peer, CallTo: local, Payload: []byte("*** DISCONNECTED From Station " + peer + "\r")})
	case control&0xef == controlDISC:
		path := append([]ax25.Address(nil), link.path...)
		delete(c.links, key)
		c.mu.Unlock()
		_ = queueAX25Control(c.server.ports[port], path, controlUA)
		return c.reply(Frame{Port: port, Kind: 'd', CallFrom: peer, CallTo: local, Payload: []byte("*** DISCONNECTED From Station " + peer + "\r")})
	case control&0xef == controlDM:
		delete(c.links, key)
		c.mu.Unlock()
		return c.reply(Frame{Port: port, Kind: 'd', CallFrom: peer, CallTo: local, Payload: []byte("*** DISCONNECTED From Station " + peer + "\r")})
	case control&1 == 0:
		ns, nr := (control>>1)&7, (control>>5)&7
		if link.outstanding != nil && nr == (link.vs+1)&7 {
			link.outstanding = nil
			link.vs = nr
		}
		if ns != link.vr {
			c.flushLinkLocked(link)
			path, vr := append([]ax25.Address(nil), link.path...), link.vr
			c.mu.Unlock()
			return queueAX25Control(c.server.ports[port], path, 0x01|vr<<5)
		}
		link.vr = (link.vr + 1) & 7
		c.flushLinkLocked(link)
		path, vr := append([]ax25.Address(nil), link.path...), link.vr
		c.mu.Unlock()
		_ = queueAX25Control(c.server.ports[port], path, 0x01|vr<<5)
		pid := byte(0)
		if len(packet.PID) != 0 {
			pid = packet.PID[0]
		}
		return c.reply(Frame{Port: port, Kind: 'D', PID: pid, CallFrom: peer, CallTo: local, Payload: packet.Information})
	case control&3 == 1:
		nr := (control >> 5) & 7
		if link.outstanding != nil && nr == (link.vs+1)&7 {
			link.outstanding = nil
			link.vs = nr
			c.flushLinkLocked(link)
		}
		c.mu.Unlock()
		return true
	default:
		c.mu.Unlock()
		return false
	}
}

func (c *client) serviceLinks(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, link := range c.links {
		if link.deadline.IsZero() || now.Before(link.deadline) {
			continue
		}
		if link.retries >= linkRetries {
			delete(c.links, key)
			c.replyLocked(Frame{Port: key.port, Kind: 'd', CallFrom: key.peer, CallTo: key.local, Payload: []byte("*** DISCONNECTED RETRY LIMIT\r")})
			continue
		}
		var packet []byte
		if link.outstanding != nil {
			packet = link.outstanding
		} else {
			control := byte(controlSABM)
			if link.closing {
				control = controlDISC
			}
			packet, _ = (ax25.Frame{Addresses: link.path, Control: control}).MarshalBinary()
		}
		if c.server.ports[key.port].QueueFrame(packet) == nil {
			link.retries++
			link.deadline = now.Add(linkRetry)
		}
	}
}

func (c *client) replyLocked(frame Frame) bool {
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

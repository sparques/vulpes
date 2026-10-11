// SPDX-License-Identifier: GPL-2.0-or-later

// Package agw implements the bounded AGWPE wire protocol used by Dire Wolf's
// modem-facing TCP interface.
package agw

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const HeaderSize = 36

const defaultMaximumPayload = 2048

var ErrPayloadTooLarge = errors.New("AGW payload exceeds configured maximum")

// Frame is one AGWPE command or event. Kind is the one-byte data-kind
// command; the four reserved bytes are preserved for protocol compatibility.
type Frame struct {
	Port      uint8
	Kind      byte
	PID       byte
	CallFrom  string
	CallTo    string
	User      uint32
	Payload   []byte
	Reserved1 byte
	Reserved2 byte
	Reserved3 byte
	Reserved4 byte
	Reserved5 byte
}

// Encode serializes one AGWPE frame using Dire Wolf's 36-byte little-endian
// header layout and rejects payloads above the default bounded limit.
func Encode(frame Frame) ([]byte, error) {
	if len(frame.Payload) > defaultMaximumPayload {
		return nil, fmt.Errorf("%w: %d > %d", ErrPayloadTooLarge, len(frame.Payload), defaultMaximumPayload)
	}
	data := make([]byte, HeaderSize+len(frame.Payload))
	data[0] = frame.Port
	data[1], data[2], data[3] = frame.Reserved1, frame.Reserved2, frame.Reserved3
	data[4] = frame.Kind
	data[5], data[6], data[7] = frame.Reserved4, frame.PID, frame.Reserved5
	putCall(data[8:18], frame.CallFrom)
	putCall(data[18:28], frame.CallTo)
	binary.LittleEndian.PutUint32(data[28:32], uint32(len(frame.Payload)))
	binary.LittleEndian.PutUint32(data[32:36], frame.User)
	copy(data[HeaderSize:], frame.Payload)
	return data, nil
}

// Decoder accepts fragmented and coalesced AGWPE frames with a bounded
// payload size. Feed returns all complete frames and a terminal framing error.
type Decoder struct {
	buffer  []byte
	maximum int
}

// NewDecoder constructs a bounded decoder. Non-positive limits use 2048 bytes.
func NewDecoder(maximumPayload int) *Decoder {
	if maximumPayload <= 0 {
		maximumPayload = defaultMaximumPayload
	}
	return &Decoder{maximum: maximumPayload}
}

func (d *Decoder) Feed(input []byte) ([]Frame, error) {
	d.buffer = append(d.buffer, input...)
	frames := make([]Frame, 0, 1)
	for len(d.buffer) >= HeaderSize {
		length := binary.LittleEndian.Uint32(d.buffer[28:32])
		if length > uint32(d.maximum) {
			d.buffer = nil
			return frames, fmt.Errorf("%w: %d > %d", ErrPayloadTooLarge, length, d.maximum)
		}
		frameSize := HeaderSize + int(length)
		if len(d.buffer) < frameSize {
			break
		}
		header := d.buffer[:HeaderSize]
		frames = append(frames, Frame{
			Port: header[0], Reserved1: header[1], Reserved2: header[2], Reserved3: header[3],
			Kind: header[4], Reserved4: header[5], PID: header[6], Reserved5: header[7],
			CallFrom: getCall(header[8:18]), CallTo: getCall(header[18:28]),
			Payload: append([]byte(nil), d.buffer[HeaderSize:frameSize]...),
			User:    binary.LittleEndian.Uint32(header[32:36]),
		})
		d.buffer = d.buffer[frameSize:]
	}
	return frames, nil
}

func putCall(destination []byte, call string) {
	copy(destination, call)
}

func getCall(source []byte) string {
	return strings.TrimRight(string(source), "\x00 ")
}

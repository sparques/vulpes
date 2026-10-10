// SPDX-License-Identifier: GPL-2.0-or-later

// Package kiss implements the KISS TNC wire protocol.
package kiss

const (
	FEND  byte = 0xc0
	FESC  byte = 0xdb
	TFEND byte = 0xdc
	TFESC byte = 0xdd
)

// Command is the low nibble of a KISS type indicator.
type Command uint8

const (
	DataFrame   Command = 0
	TXDelay     Command = 1
	Persistence Command = 2
	SlotTime    Command = 3
	TXTail      Command = 4
	FullDuplex  Command = 5
	SetHardware Command = 6
	EndKISS     Command = 15
)

// Frame is one decoded KISS message.
type Frame struct {
	Port    uint8
	Command Command
	Payload []byte
}

// Encode serializes one KISS frame including delimiters and escaping.
func Encode(frame Frame) []byte {
	result := make([]byte, 0, len(frame.Payload)+3)
	result = append(result, FEND)
	result = appendEscaped(result, (frame.Port&0x0f)<<4|uint8(frame.Command)&0x0f)
	for _, value := range frame.Payload {
		result = appendEscaped(result, value)
	}
	return append(result, FEND)
}

func appendEscaped(output []byte, value byte) []byte {
	switch value {
	case FEND:
		return append(output, FESC, TFEND)
	case FESC:
		return append(output, FESC, TFESC)
	default:
		return append(output, value)
	}
}

// Stats contains cumulative malformed-input counters.
type Stats struct {
	Frames          uint64
	MalformedEscape uint64
	Oversize        uint64
}

// Decoder incrementally parses a KISS byte stream.
type Decoder struct {
	maximum int
	started bool
	escaped bool
	discard bool
	buffer  []byte
	stats   Stats
}

// NewDecoder constructs a decoder. maximum is the largest unescaped message,
// including its type indicator. A non-positive value selects 2049 bytes.
func NewDecoder(maximum int) *Decoder {
	if maximum <= 0 {
		maximum = 2049
	}
	return &Decoder{maximum: maximum}
}

// Feed consumes bytes and returns all complete frames found in input.
func (d *Decoder) Feed(input []byte) []Frame {
	var frames []Frame
	for _, value := range input {
		if value == FEND {
			if d.escaped {
				d.stats.MalformedEscape++
				d.discard = true
			}
			if d.started && !d.discard && len(d.buffer) != 0 {
				typeIndicator := d.buffer[0]
				frames = append(frames, Frame{
					Port:    typeIndicator >> 4,
					Command: Command(typeIndicator & 0x0f),
					Payload: append([]byte(nil), d.buffer[1:]...),
				})
				d.stats.Frames++
			}
			d.started = true
			d.escaped = false
			d.discard = false
			d.buffer = d.buffer[:0]
			continue
		}
		if !d.started || d.discard {
			continue
		}
		if d.escaped {
			switch value {
			case TFEND:
				value = FEND
			case TFESC:
				value = FESC
			default:
				d.stats.MalformedEscape++
				d.escaped = false
				d.discard = true
				d.buffer = d.buffer[:0]
				continue
			}
			d.escaped = false
		} else if value == FESC {
			d.escaped = true
			continue
		}
		d.buffer = append(d.buffer, value)
		if len(d.buffer) > d.maximum {
			d.stats.Oversize++
			d.discard = true
			d.buffer = d.buffer[:0]
		}
	}
	return frames
}

// Stats returns a snapshot of cumulative decoder counters.
func (d *Decoder) Stats() Stats {
	return d.stats
}

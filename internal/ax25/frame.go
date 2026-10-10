// SPDX-License-Identifier: GPL-2.0-or-later

// Package ax25 parses and constructs binary AX.25 frames. It intentionally
// does not interpret APRS or other application payloads.
package ax25

import (
	"errors"
	"fmt"
	"strings"
)

const (
	MinAddresses = 2
	MaxAddresses = 10
	MaxInfoLen   = 2048
)

var (
	ErrTooShort       = errors.New("AX.25 frame is too short")
	ErrTooManyAddress = errors.New("AX.25 frame has too many addresses")
	ErrInvalidAddress = errors.New("invalid AX.25 address")
	ErrMissingPID     = errors.New("AX.25 information frame is missing its PID")
)

// Address is one AX.25 destination, source, or repeater address. Flag is the
// raw high bit in the SSID octet: command/response for the first two addresses
// and has-been-repeated for repeater addresses.
type Address struct {
	Callsign string
	SSID     uint8
	Flag     bool
}

// Frame is a modulo-8 AX.25 frame without its HDLC FCS. PID contains zero,
// one, or (when escaped by 0xff) two protocol identifier octets.
type Frame struct {
	Addresses   []Address
	Control     byte
	PID         []byte
	Information []byte
}

// Parse decodes a binary AX.25 frame without an FCS.
func Parse(data []byte) (Frame, error) {
	if len(data) < MinAddresses*7+1 {
		return Frame{}, ErrTooShort
	}

	var frame Frame
	offset := 0
	for {
		if len(data)-offset < 7 {
			return Frame{}, ErrTooShort
		}
		if len(frame.Addresses) == MaxAddresses {
			return Frame{}, ErrTooManyAddress
		}
		address, last, err := decodeAddress(data[offset : offset+7])
		if err != nil {
			return Frame{}, fmt.Errorf("address %d: %w", len(frame.Addresses), err)
		}
		frame.Addresses = append(frame.Addresses, address)
		offset += 7
		if last {
			break
		}
	}
	if len(frame.Addresses) < MinAddresses || offset >= len(data) {
		return Frame{}, ErrTooShort
	}

	frame.Control = data[offset]
	offset++
	if hasPID(frame.Control) {
		if offset >= len(data) {
			return Frame{}, ErrMissingPID
		}
		frame.PID = append(frame.PID, data[offset])
		offset++
		if frame.PID[0] == 0xff {
			if offset >= len(data) {
				return Frame{}, ErrMissingPID
			}
			frame.PID = append(frame.PID, data[offset])
			offset++
		}
	}
	frame.Information = append([]byte(nil), data[offset:]...)
	if len(frame.Information) > MaxInfoLen {
		return Frame{}, fmt.Errorf("information length %d exceeds %d", len(frame.Information), MaxInfoLen)
	}
	return frame, nil
}

// MarshalBinary encodes a frame without an HDLC FCS.
func (f Frame) MarshalBinary() ([]byte, error) {
	if len(f.Addresses) < MinAddresses {
		return nil, fmt.Errorf("need at least %d addresses", MinAddresses)
	}
	if len(f.Addresses) > MaxAddresses {
		return nil, ErrTooManyAddress
	}
	if len(f.Information) > MaxInfoLen {
		return nil, fmt.Errorf("information length %d exceeds %d", len(f.Information), MaxInfoLen)
	}
	wantsPID := hasPID(f.Control)
	if wantsPID && (len(f.PID) < 1 || len(f.PID) > 2 || (len(f.PID) == 2) != (f.PID[0] == 0xff)) {
		return nil, ErrMissingPID
	}
	if !wantsPID && len(f.PID) != 0 {
		return nil, fmt.Errorf("control %#02x does not carry a PID", f.Control)
	}

	result := make([]byte, 0, len(f.Addresses)*7+1+len(f.PID)+len(f.Information))
	for i, address := range f.Addresses {
		encoded, err := encodeAddress(address, i == len(f.Addresses)-1)
		if err != nil {
			return nil, fmt.Errorf("address %d: %w", i, err)
		}
		result = append(result, encoded[:]...)
	}
	result = append(result, f.Control)
	result = append(result, f.PID...)
	result = append(result, f.Information...)
	return result, nil
}

func hasPID(control byte) bool {
	return control&1 == 0 || control&0xef == 0x03
}

func decodeAddress(data []byte) (Address, bool, error) {
	var callsign [6]byte
	for i, value := range data[:6] {
		if value&1 != 0 {
			return Address{}, false, ErrInvalidAddress
		}
		character := value >> 1
		if character != ' ' && (character < '0' || character > '9') && (character < 'A' || character > 'Z') {
			return Address{}, false, ErrInvalidAddress
		}
		callsign[i] = character
	}
	ssid := data[6]
	if ssid&0x60 != 0x60 {
		return Address{}, false, ErrInvalidAddress
	}
	name := strings.TrimRight(string(callsign[:]), " ")
	if name == "" {
		return Address{}, false, ErrInvalidAddress
	}
	return Address{
		Callsign: name,
		SSID:     (ssid >> 1) & 0x0f,
		Flag:     ssid&0x80 != 0,
	}, ssid&1 != 0, nil
}

func encodeAddress(address Address, last bool) ([7]byte, error) {
	var result [7]byte
	callsign := strings.ToUpper(address.Callsign)
	if len(callsign) < 1 || len(callsign) > 6 || address.SSID > 15 {
		return result, ErrInvalidAddress
	}
	for i := range 6 {
		character := byte(' ')
		if i < len(callsign) {
			character = callsign[i]
		}
		if character != ' ' && (character < '0' || character > '9') && (character < 'A' || character > 'Z') {
			return result, ErrInvalidAddress
		}
		result[i] = character << 1
	}
	result[6] = 0x60 | address.SSID<<1
	if address.Flag {
		result[6] |= 0x80
	}
	if last {
		result[6] |= 1
	}
	return result, nil
}

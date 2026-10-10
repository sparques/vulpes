// SPDX-License-Identifier: GPL-2.0-or-later

// Package g3ruh implements the self-synchronizing scrambler used by the
// G3RUH/K9NG 9600 bit/s packet-radio modem.
package g3ruh

// Scrambler applies the G3RUH polynomial 1 + x^12 + x^17. Its zero value is
// ready for use. State is retained across calls.
type Scrambler struct {
	state uint32
}

// Bit scrambles one input bit.
func (s *Scrambler) Bit(input bool) bool {
	in := boolBit(input)
	out := (in ^ (s.state >> 16) ^ (s.state >> 11)) & 1
	s.state = (s.state << 1) | out
	return out != 0
}

// Descrambler reverses Scrambler. Its zero value is ready for use. Because the
// code is self-synchronizing, it retains the received scrambled bits.
type Descrambler struct {
	state uint32
}

// Bit descrambles one received bit.
func (d *Descrambler) Bit(input bool) bool {
	in := boolBit(input)
	out := (in ^ (d.state >> 16) ^ (d.state >> 11)) & 1
	d.state = (d.state << 1) | in
	return out != 0
}

func boolBit(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

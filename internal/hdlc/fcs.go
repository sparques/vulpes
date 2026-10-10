// SPDX-License-Identifier: GPL-2.0-or-later

// Package hdlc implements the bit-oriented framing used by AX.25.
package hdlc

const goodResidue = 0xf0b8

// FCS returns the CRC-16/X.25 frame check sequence for data. AX.25 transmits
// the low byte first.
func FCS(data []byte) uint16 {
	return ^updateFCS(0xffff, data)
}

// AppendFCS returns a copy of data followed by its little-endian FCS.
func AppendFCS(data []byte) []byte {
	result := make([]byte, len(data)+2)
	copy(result, data)
	fcs := FCS(data)
	result[len(data)] = byte(fcs)
	result[len(data)+1] = byte(fcs >> 8)
	return result
}

// ValidFCS reports whether frame, including its final two FCS bytes, has the
// expected CRC-16/X.25 residue.
func ValidFCS(frame []byte) bool {
	return len(frame) >= 2 && updateFCS(0xffff, frame) == goodResidue
}

func updateFCS(fcs uint16, data []byte) uint16 {
	for _, value := range data {
		fcs ^= uint16(value)
		for range 8 {
			if fcs&1 != 0 {
				fcs = (fcs >> 1) ^ 0x8408
			} else {
				fcs >>= 1
			}
		}
	}
	return fcs
}

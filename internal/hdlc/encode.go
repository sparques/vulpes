// SPDX-License-Identifier: GPL-2.0-or-later

package hdlc

const flagByte = 0x7e

// Encode returns an NRZI line-level stream containing payload, its FCS, bit
// stuffing, and the requested number of leading and trailing flags. At least
// one leading and trailing flag is always emitted.
func Encode(payload []byte, leadingFlags, trailingFlags int) []bool {
	levels, _ := EncodeFrom(payload, leadingFlags, trailingFlags, false)
	return levels
}

// EncodeFrom returns an NRZI stream and its final line level, starting from
// initialLevel. It is useful when successive frames share a continuous modem
// stream.
func EncodeFrom(payload []byte, leadingFlags, trailingFlags int, initialLevel bool) ([]bool, bool) {
	if leadingFlags < 1 {
		leadingFlags = 1
	}
	if trailingFlags < 1 {
		trailingFlags = 1
	}

	raw := make([]bool, 0, (leadingFlags+trailingFlags)*8+(len(payload)+2)*10)
	for range leadingFlags {
		raw = appendByteBits(raw, flagByte)
	}

	ones := 0
	for _, value := range AppendFCS(payload) {
		for bit := range 8 {
			set := value&(1<<bit) != 0
			raw = append(raw, set)
			if set {
				ones++
				if ones == 5 {
					raw = append(raw, false)
					ones = 0
				}
			} else {
				ones = 0
			}
		}
	}
	for range trailingFlags {
		raw = appendByteBits(raw, flagByte)
	}

	levels := make([]bool, len(raw))
	level := initialLevel
	for i, bit := range raw {
		if !bit {
			level = !level
		}
		levels[i] = level
	}
	return levels, level
}

func appendByteBits(bits []bool, value byte) []bool {
	for bit := range 8 {
		bits = append(bits, value&(1<<bit) != 0)
	}
	return bits
}

// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import "github.com/sparques/vulpes/internal/tnc"

// applyFrame handles one KISS command. It returns false for malformed,
// unsupported, or unroutable command payloads.
func applyFrame(frame Frame, channel *tnc.Channel) bool {
	if channel == nil {
		return false
	}
	if frame.Command == EndKISS {
		return true
	}
	if frame.Command == DataFrame {
		return len(frame.Payload) > 0 && len(frame.Payload) <= maximumKISSPayload && channel.QueueFrame(frame.Payload) == nil
	}
	if len(frame.Payload) != 1 {
		return false
	}
	value := int(frame.Payload[0])
	if frame.Command < TXDelay || frame.Command > FullDuplex {
		// SET_HARDWARE is TNC-specific and intentionally not interpreted.
		return false
	}
	return channel.UpdateTiming(func(timing tnc.Timing) tnc.Timing {
		switch frame.Command {
		case TXDelay:
			timing.TXDelayMillis = value * 10
		case Persistence:
			timing.Persistence = value
		case SlotTime:
			timing.SlotMillis = value * 10
		case TXTail:
			timing.TXTailMillis = value * 10
		case FullDuplex:
			timing.FullDuplex = value != 0
		}
		return timing
	}) == nil
}

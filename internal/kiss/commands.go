// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"fmt"

	"github.com/sparques/vulpes/internal/tnc"
)

// applyFrame handles one KISS command. It returns false for malformed,
// unsupported, or unroutable command payloads.
func applyFrame(frame Frame, channel *tnc.Channel) (*Frame, bool) {
	if channel == nil {
		return nil, false
	}
	if frame.Command == EndKISS {
		return nil, true
	}
	if frame.Command == DataFrame {
		return nil, len(frame.Payload) > 0 && len(frame.Payload) <= maximumKISSPayload && channel.QueueFrame(frame.Payload) == nil
	}
	if frame.Command == SetHardware {
		var payload []byte
		switch string(frame.Payload) {
		case "TNC:":
			payload = []byte("VULPES dev")
		case "TXBUF:":
			payload = []byte(fmt.Sprintf("TXBUF:%d", channel.QueueDepth()))
		default:
			return nil, false
		}
		return &Frame{Port: frame.Port, Command: SetHardware, Payload: payload}, true
	}
	if len(frame.Payload) != 1 {
		return nil, false
	}
	value := int(frame.Payload[0])
	if frame.Command < TXDelay || frame.Command > FullDuplex {
		// SET_HARDWARE is TNC-specific and intentionally not interpreted.
		return nil, false
	}
	err := channel.UpdateTiming(func(timing tnc.Timing) tnc.Timing {
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
	})
	return nil, err == nil
}

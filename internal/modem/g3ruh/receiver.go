// SPDX-License-Identifier: GPL-2.0-or-later

package g3ruh

import "github.com/sparques/vulpes/internal/hdlc"

// ReceivedFrame is an FCS-verified AX.25 payload recovered from G3RUH audio.
type ReceivedFrame struct {
	Payload         []byte
	Quality         int
	CarrierDetected bool
}

// Receiver connects the G3RUH descrambler and demodulator to HDLC recovery.
type Receiver struct {
	demodulator  *Demodulator
	descrambler  Descrambler
	hdlc         *hdlc.Decoder
	maximumFrame int
	qualitySum   float64
	qualityCount int
}

func NewReceiver(config Config, maximumFrameBytes int) (*Receiver, error) {
	demodulator, err := NewDemodulator(config)
	if err != nil {
		return nil, err
	}
	return &Receiver{demodulator: demodulator, hdlc: hdlc.NewDecoder(maximumFrameBytes), maximumFrame: maximumFrameBytes}, nil
}

func (r *Receiver) Process(samples []int16) []ReceivedFrame {
	var frames []ReceivedFrame
	for _, decision := range r.demodulator.Process(samples) {
		lineLevel := r.descrambler.Bit(decision.Level)
		r.qualitySum += decision.Confidence
		r.qualityCount++
		for _, payload := range r.hdlc.Feed(lineLevel) {
			quality := 0
			if r.qualityCount > 0 {
				quality = int(100 * r.qualitySum / float64(r.qualityCount))
			}
			frames = append(frames, ReceivedFrame{Payload: payload, Quality: quality, CarrierDetected: r.demodulator.CarrierDetected()})
			r.qualitySum, r.qualityCount = 0, 0
		}
	}
	return frames
}

func (r *Receiver) CarrierDetected() bool { return r.demodulator.CarrierDetected() }
func (r *Receiver) Stats() hdlc.Stats     { return r.hdlc.Stats() }
func (r *Receiver) Reset() {
	r.demodulator.Reset()
	r.descrambler = Descrambler{}
	r.hdlc = hdlc.NewDecoder(r.maximumFrame)
	r.qualitySum, r.qualityCount = 0, 0
}

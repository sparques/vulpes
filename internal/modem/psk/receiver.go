// SPDX-License-Identifier: GPL-2.0-or-later

package psk

import "github.com/sparques/vulpes/internal/hdlc"

// ReceivedFrame is an FCS-verified HDLC payload recovered from PSK.
type ReceivedFrame struct {
	Payload         []byte
	Quality         int
	CarrierDetected bool
}

// Receiver connects the PSK demodulator to streaming HDLC recovery.
type Receiver struct {
	demodulator  *Demodulator
	hdlc         *hdlc.Decoder
	maximumFrame int
	qualitySum   int
	qualityCount int
}

// NewReceiver constructs a PSK/HDLC receiver.
func NewReceiver(config Config, maximumFrameBytes int) (*Receiver, error) {
	demodulator, err := NewDemodulator(config)
	if err != nil {
		return nil, err
	}
	return &Receiver{demodulator: demodulator, hdlc: hdlc.NewDecoder(maximumFrameBytes), maximumFrame: maximumFrameBytes}, nil
}

// Process consumes PCM and returns completed frames.
func (r *Receiver) Process(samples []int16) []ReceivedFrame {
	var received []ReceivedFrame
	for _, decision := range r.demodulator.Process(samples) {
		r.qualitySum += decision.Confidence
		r.qualityCount++
		for _, payload := range r.hdlc.Feed(decision.Level) {
			quality := 0
			if r.qualityCount != 0 {
				quality = r.qualitySum / r.qualityCount
			}
			received = append(received, ReceivedFrame{Payload: payload, Quality: quality, CarrierDetected: r.demodulator.CarrierDetected()})
			r.qualitySum, r.qualityCount = 0, 0
		}
	}
	return received
}

func (r *Receiver) CarrierDetected() bool { return r.demodulator.CarrierDetected() }
func (r *Receiver) Stats() hdlc.Stats     { return r.hdlc.Stats() }
func (r *Receiver) Reset() {
	r.demodulator.Reset()
	r.hdlc = hdlc.NewDecoder(r.maximumFrame)
	r.qualitySum, r.qualityCount = 0, 0
}

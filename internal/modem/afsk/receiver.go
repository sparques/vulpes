// SPDX-License-Identifier: GPL-2.0-or-later

package afsk

import (
	"math"

	"github.com/sparques/vulpes/internal/hdlc"
)

// ReceivedFrame is an FCS-verified HDLC payload recovered from AFSK.
type ReceivedFrame struct {
	Payload         []byte
	Quality         int
	CarrierDetected bool
	Slice           int
}

type sliceReceiver struct {
	hdlc         *hdlc.Decoder
	qualitySum   float64
	qualityCount int
}

// Receiver incrementally demodulates AFSK and decodes HDLC. Timing acquisition
// is performed by the Dire Wolf-derived DPLL.
type Receiver struct {
	demodulator  *Demodulator
	slices       []sliceReceiver
	maximumFrame int
	symbols      uint64
	lastFrameAt  map[string]uint64
}

// NewReceiver constructs a streaming AFSK/HDLC receiver.
func NewReceiver(config Config, maximumFrameBytes int) (*Receiver, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	demodulator, err := NewDemodulator(config)
	if err != nil {
		return nil, err
	}
	slicerCount := config.Slicers
	if slicerCount == 0 {
		slicerCount = 1
	}
	receiver := &Receiver{
		demodulator:  demodulator,
		maximumFrame: maximumFrameBytes,
		slices:       make([]sliceReceiver, slicerCount),
		lastFrameAt:  make(map[string]uint64),
	}
	for i := range receiver.slices {
		receiver.slices[i].hdlc = hdlc.NewDecoder(maximumFrameBytes)
	}
	return receiver, nil
}

// Process consumes PCM and returns newly recovered frames. Quality is the
// average absolute discriminator confidence since the preceding valid frame,
// scaled from 0 to 100.
func (r *Receiver) Process(samples []int16) []ReceivedFrame {
	var received []ReceivedFrame
	for _, decision := range r.demodulator.Process(samples) {
		if decision.Slice == 0 {
			r.symbols++
		}
		slice := &r.slices[decision.Slice]
		slice.qualitySum += math.Abs(decision.Confidence)
		slice.qualityCount++
		for _, payload := range slice.hdlc.Feed(decision.Level) {
			quality := 0
			if slice.qualityCount != 0 {
				quality = int(math.Round(100 * slice.qualitySum / float64(slice.qualityCount)))
				quality = min(quality, 100)
			}
			slice.qualitySum, slice.qualityCount = 0, 0
			key := string(payload)
			if previous, duplicate := r.lastFrameAt[key]; duplicate && r.symbols-previous <= 16 {
				continue
			}
			r.lastFrameAt[key] = r.symbols
			received = append(received, ReceivedFrame{Payload: payload, Quality: quality,
				CarrierDetected: r.demodulator.CarrierDetected(), Slice: decision.Slice})
		}
	}
	return received
}

// CarrierDetected reports the demodulator's current DCD state.
func (r *Receiver) CarrierDetected() bool {
	return r.demodulator.CarrierDetected()
}

// Stats returns cumulative HDLC decoder counters.
func (r *Receiver) Stats() hdlc.Stats {
	var total hdlc.Stats
	for i := range r.slices {
		stats := r.slices[i].hdlc.Stats()
		total.Frames += stats.Frames
		total.BadFCS += stats.BadFCS
		total.Invalid += stats.Invalid
		total.Oversize += stats.Oversize
		total.Aborts += stats.Aborts
	}
	return total
}

// Reset discards demodulator, decoder, and frame-quality state.
func (r *Receiver) Reset() {
	r.demodulator.Reset()
	for i := range r.slices {
		r.slices[i] = sliceReceiver{hdlc: hdlc.NewDecoder(r.maximumFrame)}
	}
	r.symbols = 0
	clear(r.lastFrameAt)
}

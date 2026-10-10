// SPDX-License-Identifier: GPL-2.0-or-later

package hdlc

const (
	defaultMaxFrameBytes = 2048
	flagPattern          = 0x7e
)

// Stats contains cumulative decoder outcomes.
type Stats struct {
	Frames   uint64
	BadFCS   uint64
	Invalid  uint64
	Oversize uint64
	Aborts   uint64
}

// Decoder incrementally decodes NRZI line levels into FCS-verified frames.
// A decoder is stateful and must not be shared between input streams.
type Decoder struct {
	raw      RawDecoder
	previous bool
	havePrev bool
}

// NewDecoder constructs a decoder. A non-positive maximum selects 2048 bytes.
func NewDecoder(maxFrameBytes int) *Decoder {
	return &Decoder{raw: *NewRawDecoder(maxFrameBytes)}
}

// Feed consumes one NRZI line level and returns zero or one completed payloads.
// The initial level establishes NRZI state and does not itself represent a bit.
func (d *Decoder) Feed(level bool) [][]byte {
	if !d.havePrev {
		d.previous = level
		d.havePrev = true
		return nil
	}
	bit := level == d.previous
	d.previous = level
	return d.raw.Feed(bit)
}

// Stats returns a snapshot of cumulative decoder counters.
func (d *Decoder) Stats() Stats {
	return d.raw.Stats()
}

// RawDecoder incrementally decodes post-NRZI HDLC bits.
type RawDecoder struct {
	active     bool
	candidate  []bool
	shift      byte
	shifted    int
	maxRawBits int
	stats      Stats
}

// NewRawDecoder constructs a post-NRZI bit decoder.
func NewRawDecoder(maxFrameBytes int) *RawDecoder {
	if maxFrameBytes <= 0 {
		maxFrameBytes = defaultMaxFrameBytes
	}
	// Allow for two FCS bytes and worst-case stuffing, rounded up generously.
	return &RawDecoder{maxRawBits: (maxFrameBytes + 2) * 10}
}

// Feed consumes one post-NRZI bit and returns zero or one completed payloads.
func (d *RawDecoder) Feed(bit bool) [][]byte {
	d.shift >>= 1
	if bit {
		d.shift |= 0x80
	}
	if d.shifted < 8 {
		d.shifted++
	}

	if d.active {
		d.candidate = append(d.candidate, bit)
		if len(d.candidate) > d.maxRawBits+8 {
			d.stats.Oversize++
			d.resetFrame()
		}
	}

	if d.shifted == 8 && d.shift == flagPattern {
		var frames [][]byte
		if d.active && len(d.candidate) > 8 {
			if frame, ok := d.finish(d.candidate[:len(d.candidate)-8]); ok {
				frames = append(frames, frame)
			}
		}
		d.active = true
		d.candidate = d.candidate[:0]
		return frames
	}

	if d.shifted == 8 && d.shift == 0x7f {
		d.stats.Aborts++
		d.resetFrame()
	}
	return nil
}

// Stats returns a snapshot of cumulative decoder counters.
func (d *RawDecoder) Stats() Stats {
	return d.stats
}

func (d *RawDecoder) resetFrame() {
	d.active = false
	d.candidate = d.candidate[:0]
}

func (d *RawDecoder) finish(stuffed []bool) ([]byte, bool) {
	bits := make([]bool, 0, len(stuffed))
	ones := 0
	for _, bit := range stuffed {
		if bit {
			ones++
			if ones > 5 {
				d.stats.Invalid++
				return nil, false
			}
			bits = append(bits, true)
			continue
		}
		if ones == 5 {
			ones = 0
			continue
		}
		ones = 0
		bits = append(bits, false)
	}
	if len(bits)%8 != 0 || len(bits) < 16 {
		d.stats.Invalid++
		return nil, false
	}

	frame := make([]byte, len(bits)/8)
	for i, bit := range bits {
		if bit {
			frame[i/8] |= 1 << (i % 8)
		}
	}
	if !ValidFCS(frame) {
		d.stats.BadFCS++
		return nil, false
	}
	d.stats.Frames++
	return append([]byte(nil), frame[:len(frame)-2]...), true
}

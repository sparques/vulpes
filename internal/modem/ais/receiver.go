// SPDX-License-Identifier: GPL-2.0-or-later

// Package ais receives AIS HDLC frames carried by 9600 bit/s GMSK audio.
package ais

import (
	"fmt"

	"github.com/sparques/vulpes/internal/hdlc"
	"github.com/sparques/vulpes/internal/modem/g3ruh"
)

// Config uses the Dire Wolf-compatible 9600 bit/s baseband receive settings.
type Config = g3ruh.Config

// Message is an FCS-verified AIS payload and its armored NMEA sentence.
type Message struct {
	Payload         []byte
	NMEA            string
	Quality         int
	CarrierDetected bool
}

// Receiver decodes AIS audio. Unlike G3RUH packet mode, AIS data is not
// descrambled; its NRZI levels feed HDLC directly.
type Receiver struct {
	demodulator  *g3ruh.Demodulator
	hdlc         *hdlc.Decoder
	maximumFrame int
	qualitySum   float64
	qualityCount int
}

// NewReceiver constructs an AIS/GMSK receiver.
func NewReceiver(config Config, maximumFrameBytes int) (*Receiver, error) {
	demodulator, err := g3ruh.NewDemodulator(config)
	if err != nil {
		return nil, err
	}
	if maximumFrameBytes <= 0 {
		maximumFrameBytes = 2048
	}
	return &Receiver{
		demodulator:  demodulator,
		hdlc:         hdlc.NewDecoder(maximumFrameBytes),
		maximumFrame: maximumFrameBytes,
	}, nil
}

// Process consumes PCM samples and returns AIS messages with valid HDLC FCS
// and a Dire Wolf-style payload-length check.
func (r *Receiver) Process(samples []int16) []Message {
	var messages []Message
	for _, decision := range r.demodulator.Process(samples) {
		r.qualitySum += decision.Confidence
		r.qualityCount++
		for _, payload := range r.hdlc.Feed(decision.Level) {
			quality := 0
			if r.qualityCount > 0 {
				quality = int(100 * r.qualitySum / float64(r.qualityCount))
			}
			r.qualitySum, r.qualityCount = 0, 0
			if !validPayloadLength(payload) {
				continue
			}
			messages = append(messages, Message{
				Payload:         payload,
				NMEA:            ToNMEA(payload),
				Quality:         quality,
				CarrierDetected: r.demodulator.CarrierDetected(),
			})
		}
	}
	return messages
}

// CarrierDetected reports the GMSK timing-loop carrier state.
func (r *Receiver) CarrierDetected() bool { return r.demodulator.CarrierDetected() }

// Stats returns HDLC receive statistics.
func (r *Receiver) Stats() hdlc.Stats { return r.hdlc.Stats() }

// Reset drops audio, HDLC, and quality history while retaining configuration.
func (r *Receiver) Reset() {
	r.demodulator.Reset()
	r.hdlc = hdlc.NewDecoder(r.maximumFrame)
	r.qualitySum, r.qualityCount = 0, 0
}

var aisLengthBits = [28][2]int{
	{}, {168, 168}, {168, 168}, {168, 168}, {168, 168}, {424, 424},
	{72, 1008}, {72, 168}, {168, 1008}, {168, 168}, {72, 72},
	{168, 168}, {72, 1008}, {72, 168}, {40, 1008}, {88, 160},
	{96, 114}, {80, 816}, {168, 168}, {312, 312}, {72, 160},
	{272, 360}, {168, 168}, {160, 160}, {160, 168}, {40, 168},
	{60, 1064}, {96, 168},
}

func validPayloadLength(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	typ := int((payload[0] >> 2) & 0x3f)
	if typ < 1 || typ >= len(aisLengthBits) {
		return false
	}
	length := len(payload) * 8
	return length >= aisLengthBits[typ][0] && length <= aisLengthBits[typ][1]
}

// ToNMEA converts AIS payload bytes into the armored !AIVDM sentence used by
// Dire Wolf. Payloads should be length-validated before conversion.
func ToNMEA(payload []byte) string {
	if len(payload) == 0 || len(payload) > 133 {
		return ""
	}
	sextets := (len(payload)*8 + 5) / 6
	armored := make([]byte, sextets)
	for i := range sextets {
		value := aisField(payload, i*6, 6)
		if value < 40 {
			armored[i] = byte('0' + value)
		} else {
			armored[i] = byte('`' + value - 40)
		}
	}
	pad := sextets*6 - len(payload)*8
	body := fmt.Sprintf("AIVDM,1,1,,A,%s,%d", armored, pad)
	var checksum byte
	for i := 0; i < len(body); i++ {
		checksum ^= body[i]
	}
	return fmt.Sprintf("!%s*%02X", body, checksum)
}

// aisField reads AIS fields most-significant-bit first, matching Dire Wolf's
// bit-vector conversion of its HDLC payload bytes.
func aisField(payload []byte, start, length int) int {
	value := 0
	for bit := start; bit < start+length; bit++ {
		value <<= 1
		if bit < len(payload)*8 {
			value |= int((payload[bit/8] >> (7 - bit%8)) & 1)
		}
	}
	return value
}

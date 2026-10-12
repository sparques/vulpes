// SPDX-License-Identifier: GPL-2.0-or-later

package tnc

import (
	"fmt"

	"github.com/sparques/vulpes/internal/config"
	"github.com/sparques/vulpes/internal/modem"
)

// NewConfiguredChannels constructs one independent runtime channel per
// validated YAML channel, using the sample rate of its referenced audio
// device. The returned map is keyed by channel name.
func NewConfiguredChannels(configuration config.Config) (map[string]*Channel, error) {
	sampleRates := make(map[string]int, len(configuration.Audio))
	for _, device := range configuration.Audio {
		sampleRates[device.Name] = device.SampleRate
	}
	channels := make(map[string]*Channel, len(configuration.Channels))
	for _, configured := range configuration.Channels {
		voxToneMillis := 0
		if configured.PTT.Type == "vox" {
			voxToneMillis = configured.TX.VOXToneMillis
		}
		channel, err := NewChannel(ChannelConfig{
			Name: configured.Name,
			Modem: modem.Settings{
				Mode: configured.Modem.Mode, SampleRate: sampleRates[configured.Audio],
				MarkHz: configured.Modem.Mark, SpaceHz: configured.Modem.Space,
				Amplitude: configured.Modem.Amplitude, Profile: configured.Modem.Profile,
				Variant: configured.Modem.Variant, Slicers: configured.Modem.Slicers,
				Upsample: configured.Modem.Upsample,
			},
			MaximumFrameBytes: defaultMaximumFrameBytes, QueueDepth: 16,
			TXDelayMillis: configured.TX.DelayMillis, TXTailMillis: configured.TX.TailMillis,
			SlotMillis: configured.TX.SlotMillis, Persistence: configured.TX.Persistence,
			FullDuplex:    configured.TX.FullDuplex,
			AudioChannel:  configured.AudioChannel,
			VOXToneMillis: voxToneMillis,
		})
		if err != nil {
			for _, opened := range channels {
				_ = opened.Close()
			}
			return nil, fmt.Errorf("configure channel %q: %w", configured.Name, err)
		}
		channels[configured.Name] = channel
	}
	return channels, nil
}

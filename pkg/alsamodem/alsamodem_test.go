// SPDX-License-Identifier: GPL-2.0-or-later

package alsamodem

import (
	"strings"
	"testing"

	publicmodem "github.com/sparques/vulpes/pkg/modem"
)

func TestNormalizeConfigDefaultsAndCopiesSlices(t *testing.T) {
	input := Config{
		Audio:    []AudioConfig{{Name: "radio", Capture: "capture", Playback: "playback"}},
		Channels: []ChannelConfig{{Name: "vhf", Audio: "radio", Modem: publicmodem.Settings{Mode: publicmodem.ModeAFSK1200}, PTT: PTTConfig{Type: PTTVOX}}},
	}
	got, err := normalizeConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	if input.Audio[0].SampleRate != 0 || input.Channels[0].Modem.SampleRate != 0 {
		t.Fatal("normalizeConfig mutated caller-owned slices")
	}
	if got.Audio[0].SampleRate != 48000 || got.Audio[0].CaptureChannels != 1 || got.Audio[0].PlaybackChannels != 1 || got.Audio[0].BufferMillis != 100 {
		t.Fatalf("unexpected audio defaults: %+v", got.Audio[0])
	}
	channel := got.Channels[0]
	if channel.Modem.SampleRate != 48000 || channel.Modem.Amplitude != 0.5 || channel.Modem.Profile != "A" || channel.Modem.Slicers != 9 {
		t.Fatalf("unexpected modem defaults: %+v", channel.Modem)
	}
	if channel.TX.DelayMillis != 300 || channel.TX.TailMillis != 100 || channel.TX.SlotMillis != 100 || channel.TX.Persistence != 63 || channel.TX.VOXToneMillis != 2000 {
		t.Fatalf("unexpected TX defaults: %+v", channel.TX)
	}
	if got.CaptureQueueDepth != 4 || got.EventQueueDepth != 32 || channel.MaximumFrameBytes != 2048 || channel.TransmitQueueSize != 16 {
		t.Fatalf("unexpected queue/frame defaults: config=%+v channel=%+v", got, channel)
	}
}

func TestNormalizeConfigRejectsInvalidReferencesAndRates(t *testing.T) {
	base := Config{
		Audio:    []AudioConfig{{Name: "radio", Capture: "capture", Playback: "playback"}},
		Channels: []ChannelConfig{{Name: "vhf", Audio: "radio", Modem: publicmodem.Settings{Mode: publicmodem.ModeAFSK1200}}},
	}
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"unknown audio", func(c *Config) { c.Channels[0].Audio = "missing" }, "audio reference"},
		{"mismatched sample rate", func(c *Config) { c.Channels[0].Modem.SampleRate = 44100 }, "sample rate"},
		{"invalid PTT combination", func(c *Config) {
			c.Channels[0].PTT = PTTConfig{Type: PTTSerial, Serial: SerialPTTConfig{Device: "/dev/ttyS0", RTS: true}, GPIO: GPIOPTTConfig{Chip: "/dev/gpiochip0"}}
		}, "cannot include GPIO"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Config{Audio: append([]AudioConfig(nil), base.Audio...), Channels: append([]ChannelConfig(nil), base.Channels...)}
			test.edit(&config)
			_, err := normalizeConfig(config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeConfig error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidatePTT(t *testing.T) {
	tests := []struct {
		name   string
		config PTTConfig
		valid  bool
	}{
		{"none", PTTConfig{Type: PTTNone}, true},
		{"vox", PTTConfig{Type: PTTVOX}, true},
		{"serial", PTTConfig{Type: PTTSerial, Serial: SerialPTTConfig{Device: "/dev/ttyS0", RTS: true}}, true},
		{"gpio", PTTConfig{Type: PTTGPIO, GPIO: GPIOPTTConfig{Chip: "/dev/gpiochip0"}}, true},
		{"cm108 default gpio", PTTConfig{Type: PTTCM108, CM108: CM108PTTConfig{Device: "/dev/hidraw0"}}, true},
		{"cm108 invalid gpio", PTTConfig{Type: PTTCM108, CM108: CM108PTTConfig{Device: "/dev/hidraw0", GPIO: 9}}, false},
		{"missing CM108 device", PTTConfig{Type: PTTCM108}, false},
		{"unknown", PTTConfig{Type: "other"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePTT(test.config)
			if (err == nil) != test.valid {
				t.Fatalf("validatePTT(%+v) = %v, valid=%v", test.config, err, test.valid)
			}
		})
	}
}

// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestShippedExamples(t *testing.T) {
	for _, name := range []string{"vulpes.yaml", "all-modes.yaml"} {
		file, err := os.Open("../../examples/" + name)
		if err != nil {
			t.Fatal(err)
		}
		_, decodeErr := Decode(name, file)
		closeErr := file.Close()
		if decodeErr != nil {
			t.Errorf("%s: %v", name, decodeErr)
		}
		if closeErr != nil {
			t.Error(closeErr)
		}
	}
}

const minimalConfig = `
version: 1
audio:
  - name: radio
    capture: hw:CARD=Radio,DEV=0
    playback: hw:CARD=Radio,DEV=0
channels:
  - name: vhf
    audio: radio
    modem:
      mode: afsk1200
interfaces:
  kiss_tcp:
    - listen: 127.0.0.1:8001
      channel: vhf
  agw_tcp:
    - listen: 127.0.0.1:8000
`

func TestDecodeDefaults(t *testing.T) {
	configuration, err := Decode("test.yaml", strings.NewReader(minimalConfig))
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Logging.Level != "info" {
		t.Fatalf("logging level = %q", configuration.Logging.Level)
	}
	if got := configuration.Audio[0]; got.SampleRate != 48000 || got.Channels != 1 ||
		got.CaptureChannels != 1 || got.PlaybackChannels != 1 || got.BufferMillis != 100 {
		t.Fatalf("audio defaults = %+v", got)
	}
	channel := configuration.Channels[0]
	if channel.Modem.Amplitude != 0.5 || channel.Modem.Profile != "A" || channel.Modem.Slicers != 9 || channel.PTT.Type != "none" || channel.TX.DelayMillis != 300 || channel.TX.Persistence != 63 {
		t.Fatalf("channel defaults = %+v", channel)
	}
}

func TestDecodeDirectionSpecificAudioChannels(t *testing.T) {
	input := strings.Replace(minimalConfig,
		"    playback: hw:CARD=Radio,DEV=0\n",
		"    playback: hw:CARD=Radio,DEV=0\n    capture_channels: 1\n    playback_channels: 2\n", 1)
	configuration, err := Decode("duplex.yaml", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	device := configuration.Audio[0]
	if device.CaptureChannels != 1 || device.PlaybackChannels != 2 {
		t.Fatalf("direction-specific audio channels = %+v", device)
	}
}

func TestDecodeVOXToneDuration(t *testing.T) {
	input := strings.Replace(minimalConfig,
		"    audio: radio\n", "    audio: radio\n    ptt:\n      type: vox\n", 1)
	configuration, err := Decode("vox.yaml", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if got := configuration.Channels[0].TX.VOXToneMillis; got != 2000 {
		t.Fatalf("default VOX tone duration = %d ms, want 2000", got)
	}

	input = strings.Replace(input,
		"      type: vox\n", "      type: vox\n    tx:\n      vox_tone_ms: 750\n", 1)
	configuration, err = Decode("vox-custom.yaml", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if got := configuration.Channels[0].TX.VOXToneMillis; got != 750 {
		t.Fatalf("configured VOX tone duration = %d ms, want 750", got)
	}
}

func TestDecodeRejectsUnknownAndDuplicateFields(t *testing.T) {
	tests := []string{
		strings.Replace(minimalConfig, "version: 1", "version: 1\nunknown: true", 1),
		strings.Replace(minimalConfig, "version: 1", "version: 1\nversion: 1", 1),
		minimalConfig + "---\nversion: 1\n",
	}
	for i, input := range tests {
		if _, err := Decode("strict.yaml", strings.NewReader(input)); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d error = %v, want ErrInvalid", i, err)
		}
	}
}

func TestDecodeValidatesReferences(t *testing.T) {
	input := strings.Replace(minimalConfig, "audio: radio", "audio: missing", 1)
	if _, err := Decode("reference.yaml", strings.NewReader(input)); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "valid audio reference") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeAcceptsAFSKProfileB(t *testing.T) {
	input := strings.Replace(minimalConfig, "      mode: afsk1200", "      mode: afsk1200\n      profile: B\n      slicers: 3", 1)
	configuration, err := Decode("profile-b.yaml", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	modem := configuration.Channels[0].Modem
	if modem.Profile != "B" || modem.Slicers != 3 {
		t.Fatalf("modem = %+v", modem)
	}
}

func TestDecodePSKDefaultsAndVariants(t *testing.T) {
	for _, test := range []struct {
		mode, profile, variant string
	}{
		{mode: "psk2400", profile: "R", variant: "V26B"},
		{mode: "psk4800", profile: "V"},
	} {
		input := strings.Replace(minimalConfig, "afsk1200", test.mode, 1)
		configuration, err := Decode("psk.yaml", strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		modem := configuration.Channels[0].Modem
		if modem.Profile != test.profile || modem.Variant != test.variant || modem.Slicers != 0 {
			t.Fatalf("mode %s defaults = %+v", test.mode, modem)
		}
	}
	input := strings.Replace(minimalConfig, "      mode: afsk1200", "      mode: psk2400\n      variant: V26A", 1)
	configuration, err := Decode("v26a.yaml", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Channels[0].Modem.Variant != "V26A" {
		t.Fatalf("V26A selection lost: %+v", configuration.Channels[0].Modem)
	}
}

func TestDecodeRejectsMalformedRangesAndConflicts(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "malformed", input: "version: ["},
		{name: "sample rate", input: strings.Replace(minimalConfig, "playback: hw:CARD=Radio,DEV=0", "playback: hw:CARD=Radio,DEV=0\n    sample_rate: 7", 1)},
		{name: "conflicting PTT", input: strings.Replace(minimalConfig, "    modem:\n      mode: afsk1200", "    modem:\n      mode: afsk1200\n    ptt:\n      type: none\n      serial:\n        device: /dev/ttyS0\n        rts: true", 1)},
		{name: "unsupported AFSK profile", input: strings.Replace(minimalConfig, "      mode: afsk1200", "      mode: afsk1200\n      profile: Z", 1)},
		{name: "too many slicers", input: strings.Replace(minimalConfig, "      mode: afsk1200", "      mode: afsk1200\n      slicers: 10", 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode("invalid.yaml", strings.NewReader(test.input)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestDecodePTTBackends(t *testing.T) {
	tests := []string{
		"type: serial\n      serial:\n        device: /dev/ttyUSB0\n        rts: true",
		"type: gpio\n      gpio:\n        chip: /dev/gpiochip0\n        line: 17\n        active: low",
		"type: cm108\n      cm108:\n        device: /dev/hidraw2\n        gpio: 3",
	}
	for _, ptt := range tests {
		input := strings.Replace(minimalConfig, "    modem:\n      mode: afsk1200", "    modem:\n      mode: afsk1200\n    ptt:\n      "+ptt, 1)
		if _, err := Decode("ptt.yaml", strings.NewReader(input)); err != nil {
			t.Errorf("PTT %q: %v", ptt, err)
		}
	}
}

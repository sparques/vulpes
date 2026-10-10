// SPDX-License-Identifier: GPL-2.0-or-later

// Package config decodes and validates the versioned Vulpes YAML schema.
package config

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"go.yaml.in/yaml/v3"
)

const SchemaVersion = 1

var ErrInvalid = errors.New("invalid configuration")

// Config is the root of the Vulpes YAML configuration.
type Config struct {
	Version    int        `yaml:"version"`
	Logging    Logging    `yaml:"logging,omitempty"`
	Audio      []Audio    `yaml:"audio"`
	Channels   []Channel  `yaml:"channels"`
	Interfaces Interfaces `yaml:"interfaces,omitempty"`
}

type Logging struct {
	Level string `yaml:"level,omitempty"`
	File  string `yaml:"file,omitempty"`
}

// SlogLevel returns the validated structured logging level.
func (l Logging) SlogLevel() slog.Level {
	level, _ := parseLevel(l.Level)
	return level
}

type Audio struct {
	Name         string `yaml:"name"`
	Capture      string `yaml:"capture"`
	Playback     string `yaml:"playback"`
	SampleRate   int    `yaml:"sample_rate,omitempty"`
	Channels     int    `yaml:"channels,omitempty"`
	BufferMillis int    `yaml:"buffer_ms,omitempty"`
}

type Channel struct {
	Name         string `yaml:"name"`
	Audio        string `yaml:"audio"`
	AudioChannel int    `yaml:"audio_channel,omitempty"`
	Modem        Modem  `yaml:"modem"`
	PTT          PTT    `yaml:"ptt,omitempty"`
	TX           TX     `yaml:"tx,omitempty"`
}

type Modem struct {
	Mode      string  `yaml:"mode"`
	Mark      float64 `yaml:"mark_hz,omitempty"`
	Space     float64 `yaml:"space_hz,omitempty"`
	Amplitude float64 `yaml:"amplitude,omitempty"`
	Profile   string  `yaml:"profile,omitempty"`
	Variant   string  `yaml:"variant,omitempty"`
	Slicers   int     `yaml:"slicers,omitempty"`
}

type PTT struct {
	Type   string    `yaml:"type,omitempty"`
	Serial SerialPTT `yaml:"serial,omitempty"`
	GPIO   GPIOPTT   `yaml:"gpio,omitempty"`
	CM108  CM108PTT  `yaml:"cm108,omitempty"`
}

type SerialPTT struct {
	Device    string `yaml:"device"`
	RTS       bool   `yaml:"rts,omitempty"`
	DTR       bool   `yaml:"dtr,omitempty"`
	RTSActive string `yaml:"rts_active,omitempty"`
	DTRActive string `yaml:"dtr_active,omitempty"`
}

type GPIOPTT struct {
	Chip   string `yaml:"chip"`
	Line   int    `yaml:"line"`
	Active string `yaml:"active,omitempty"`
}

type CM108PTT struct {
	Device string `yaml:"device,omitempty"`
	GPIO   int    `yaml:"gpio,omitempty"`
	Active string `yaml:"active,omitempty"`
}

type TX struct {
	DelayMillis int  `yaml:"delay_ms,omitempty"`
	TailMillis  int  `yaml:"tail_ms,omitempty"`
	SlotMillis  int  `yaml:"slot_ms,omitempty"`
	Persistence int  `yaml:"persistence,omitempty"`
	FullDuplex  bool `yaml:"full_duplex,omitempty"`
}

type Interfaces struct {
	KISSTCP    []TCPListener `yaml:"kiss_tcp,omitempty"`
	KISSSerial []KISSSerial  `yaml:"kiss_serial,omitempty"`
	AGWTCP     []TCPListener `yaml:"agw_tcp,omitempty"`
}

type TCPListener struct {
	Listen  string `yaml:"listen"`
	Channel string `yaml:"channel,omitempty"`
}

type KISSSerial struct {
	Device  string `yaml:"device"`
	Baud    int    `yaml:"baud,omitempty"`
	Channel string `yaml:"channel,omitempty"`
}

// Decode strictly decodes one YAML document, applies defaults, and validates
// all references and ranges. source is included in diagnostics.
func Decode(source string, reader io.Reader) (Config, error) {
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	var configuration Config
	if err := decoder.Decode(&configuration); err != nil {
		return Config{}, fmt.Errorf("%s: %w: %v", source, ErrInvalid, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("%s: %w: multiple YAML documents are not allowed", source, ErrInvalid)
		}
		return Config{}, fmt.Errorf("%s: %w: %v", source, ErrInvalid, err)
	}
	configuration.applyDefaults()
	if err := configuration.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w: %v", source, ErrInvalid, err)
	}
	return configuration, nil
}

func (c *Config) applyDefaults() {
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	for i := range c.Audio {
		if c.Audio[i].SampleRate == 0 {
			c.Audio[i].SampleRate = 48000
		}
		if c.Audio[i].Channels == 0 {
			c.Audio[i].Channels = 1
		}
		if c.Audio[i].BufferMillis == 0 {
			c.Audio[i].BufferMillis = 100
		}
	}
	for i := range c.Channels {
		channel := &c.Channels[i]
		if channel.Modem.Amplitude == 0 {
			channel.Modem.Amplitude = 0.5
		}
		if channel.Modem.Mode == "afsk300" || channel.Modem.Mode == "afsk1200" {
			if channel.Modem.Profile == "" {
				channel.Modem.Profile = "A"
			}
			if channel.Modem.Slicers == 0 {
				channel.Modem.Slicers = 9
			}
		} else if channel.Modem.Mode == "psk2400" {
			if channel.Modem.Profile == "" {
				channel.Modem.Profile = "R"
			}
			if channel.Modem.Variant == "" {
				channel.Modem.Variant = "V26B"
			}
		} else if channel.Modem.Mode == "psk4800" && channel.Modem.Profile == "" {
			channel.Modem.Profile = "V"
		}
		if channel.PTT.Type == "" {
			channel.PTT.Type = "none"
		}
		if channel.TX.DelayMillis == 0 {
			channel.TX.DelayMillis = 300
		}
		if channel.TX.TailMillis == 0 {
			channel.TX.TailMillis = 100
		}
		if channel.TX.SlotMillis == 0 {
			channel.TX.SlotMillis = 100
		}
		if channel.TX.Persistence == 0 {
			channel.TX.Persistence = 63
		}
		switch channel.PTT.Type {
		case "serial":
			if channel.PTT.Serial.RTSActive == "" {
				channel.PTT.Serial.RTSActive = "high"
			}
			if channel.PTT.Serial.DTRActive == "" {
				channel.PTT.Serial.DTRActive = "high"
			}
		case "gpio":
			if channel.PTT.GPIO.Active == "" {
				channel.PTT.GPIO.Active = "high"
			}
		case "cm108":
			if channel.PTT.CM108.Active == "" {
				channel.PTT.CM108.Active = "high"
			}
			if channel.PTT.CM108.GPIO == 0 {
				channel.PTT.CM108.GPIO = 3
			}
		}
	}
	for i := range c.Interfaces.KISSSerial {
		if c.Interfaces.KISSSerial[i].Baud == 0 {
			c.Interfaces.KISSSerial[i].Baud = 9600
		}
	}
}

func (c Config) Validate() error {
	if c.Version != SchemaVersion {
		return fmt.Errorf("version must be %d", SchemaVersion)
	}
	if _, err := parseLevel(c.Logging.Level); err != nil {
		return err
	}
	if len(c.Audio) == 0 {
		return errors.New("at least one audio device is required")
	}
	audio := make(map[string]Audio, len(c.Audio))
	for i, device := range c.Audio {
		path := fmt.Sprintf("audio[%d]", i)
		if device.Name == "" || device.Capture == "" || device.Playback == "" {
			return fmt.Errorf("%s: name, capture, and playback are required", path)
		}
		if _, exists := audio[device.Name]; exists {
			return fmt.Errorf("%s: duplicate name %q", path, device.Name)
		}
		if device.SampleRate < 8000 || device.SampleRate > 192000 || device.Channels < 1 || device.Channels > 2 || device.BufferMillis < 1 || device.BufferMillis > 5000 {
			return fmt.Errorf("%s: invalid PCM or buffer settings", path)
		}
		audio[device.Name] = device
	}
	if len(c.Channels) == 0 {
		return errors.New("at least one modem channel is required")
	}
	channels := make(map[string]struct{}, len(c.Channels))
	for i, channel := range c.Channels {
		path := fmt.Sprintf("channels[%d]", i)
		device, exists := audio[channel.Audio]
		if channel.Name == "" || !exists {
			return fmt.Errorf("%s: name and a valid audio reference are required", path)
		}
		if _, exists := channels[channel.Name]; exists {
			return fmt.Errorf("%s: duplicate name %q", path, channel.Name)
		}
		channels[channel.Name] = struct{}{}
		if channel.AudioChannel < 0 || channel.AudioChannel >= device.Channels {
			return fmt.Errorf("%s: audio_channel %d is unavailable on %q", path, channel.AudioChannel, channel.Audio)
		}
		if err := validateModem(path+".modem", channel.Modem, device.SampleRate); err != nil {
			return err
		}
		if err := validatePTT(path+".ptt", channel.PTT); err != nil {
			return err
		}
		if channel.TX.DelayMillis < 0 || channel.TX.TailMillis < 0 || channel.TX.SlotMillis < 1 || channel.TX.Persistence < 0 || channel.TX.Persistence > 255 {
			return fmt.Errorf("%s.tx: timing or persistence is out of range", path)
		}
	}
	for i, listener := range append(append([]TCPListener(nil), c.Interfaces.KISSTCP...), c.Interfaces.AGWTCP...) {
		if listener.Listen == "" {
			return fmt.Errorf("interfaces TCP listener %d: listen is required", i)
		}
		if listener.Channel != "" {
			if _, exists := channels[listener.Channel]; !exists {
				return fmt.Errorf("interfaces TCP listener %d: unknown channel %q", i, listener.Channel)
			}
		}
	}
	for i, serial := range c.Interfaces.KISSSerial {
		if serial.Device == "" || serial.Baud <= 0 {
			return fmt.Errorf("interfaces.kiss_serial[%d]: device and positive baud are required", i)
		}
		if serial.Channel != "" {
			if _, exists := channels[serial.Channel]; !exists {
				return fmt.Errorf("interfaces.kiss_serial[%d]: unknown channel %q", i, serial.Channel)
			}
		}
	}
	return nil
}

func validateModem(path string, modem Modem, sampleRate int) error {
	validModes := map[string]bool{"afsk300": true, "afsk1200": true, "psk2400": true, "psk4800": true, "g3ruh9600": true, "ais": true, "eas": true}
	if !validModes[modem.Mode] {
		return fmt.Errorf("%s: unsupported mode %q", path, modem.Mode)
	}
	if modem.Amplitude <= 0 || modem.Amplitude > 1 {
		return fmt.Errorf("%s: amplitude must be greater than 0 and at most 1", path)
	}
	if modem.Mark < 0 || modem.Space < 0 || modem.Mark >= float64(sampleRate)/2 || modem.Space >= float64(sampleRate)/2 {
		return fmt.Errorf("%s: tone frequency is outside the usable sample rate", path)
	}
	if (modem.Mark == 0) != (modem.Space == 0) || (modem.Mark != 0 && modem.Mark == modem.Space) {
		return fmt.Errorf("%s: mark_hz and space_hz must be distinct and specified together", path)
	}
	isAFSK := modem.Mode == "afsk300" || modem.Mode == "afsk1200"
	if isAFSK {
		if modem.Profile != "A" && modem.Profile != "B" {
			return fmt.Errorf("%s: AFSK profile must be A or B", path)
		}
		if modem.Slicers < 1 || modem.Slicers > 9 {
			return fmt.Errorf("%s: slicers must be between 1 and 9", path)
		}
		if modem.Variant != "" {
			return fmt.Errorf("%s: variant does not apply to AFSK", path)
		}
	} else if modem.Mode == "psk2400" {
		if (modem.Profile != "P" && modem.Profile != "Q" && modem.Profile != "R" && modem.Profile != "S") || (modem.Variant != "V26A" && modem.Variant != "V26B") || modem.Slicers != 0 {
			return fmt.Errorf("%s: PSK 2400 requires profile P, Q, R, or S, variant V26A or V26B, and no slicers", path)
		}
	} else if modem.Mode == "psk4800" {
		if (modem.Profile != "T" && modem.Profile != "U" && modem.Profile != "V" && modem.Profile != "W") || modem.Variant != "" || modem.Slicers != 0 {
			return fmt.Errorf("%s: PSK 4800 requires profile T, U, V, or W and no variant or slicers", path)
		}
	} else if modem.Profile != "" || modem.Variant != "" || modem.Slicers != 0 {
		return fmt.Errorf("%s: profile, variant, and slicers do not apply to this mode", path)
	}
	return nil
}

func validatePTT(path string, ptt PTT) error {
	active := func(value string) bool { return value == "high" || value == "low" }
	hasSerial := ptt.Serial.Device != "" || ptt.Serial.RTS || ptt.Serial.DTR
	hasGPIO := ptt.GPIO.Chip != "" || ptt.GPIO.Line != 0 || ptt.GPIO.Active != ""
	hasCM108 := ptt.CM108.Device != "" || ptt.CM108.GPIO != 0 || ptt.CM108.Active != ""
	switch ptt.Type {
	case "none", "vox":
		if hasSerial || hasGPIO || hasCM108 {
			return fmt.Errorf("%s: %s PTT cannot contain hardware backend settings", path, ptt.Type)
		}
	case "serial":
		if hasGPIO || hasCM108 || ptt.Serial.Device == "" || (!ptt.Serial.RTS && !ptt.Serial.DTR) || !active(ptt.Serial.RTSActive) || !active(ptt.Serial.DTRActive) {
			return fmt.Errorf("%s.serial: device, RTS and/or DTR, and valid polarity are required", path)
		}
	case "gpio":
		if hasSerial || hasCM108 || ptt.GPIO.Chip == "" || ptt.GPIO.Line < 0 || !active(ptt.GPIO.Active) {
			return fmt.Errorf("%s.gpio: chip, non-negative line, and valid polarity are required", path)
		}
	case "cm108":
		if hasSerial || hasGPIO || ptt.CM108.GPIO < 1 || ptt.CM108.GPIO > 8 || !active(ptt.CM108.Active) {
			return fmt.Errorf("%s.cm108: GPIO must be 1..8 with valid polarity", path)
		}
	default:
		return fmt.Errorf("%s: unsupported type %q", path, ptt.Type)
	}
	return nil
}

func parseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logging.level: unsupported level %q", value)
	}
}

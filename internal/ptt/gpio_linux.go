// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux

package ptt

import (
	"fmt"

	"github.com/warthog618/go-gpiocdev"
)

// GPIOConfig selects one Linux GPIO character-device line. ActiveLow changes
// the physical active level while Driver.Set continues to use logical PTT
// state. Consumer is reported by GPIO inspection tools.
type GPIOConfig struct {
	Chip      string
	Line      int
	ActiveLow bool
	Consumer  string
}

// OpenGPIO exclusively requests an output line in its inactive state.
func OpenGPIO(config GPIOConfig) (Driver, error) {
	if config.Chip == "" || config.Line < 0 {
		return nil, fmt.Errorf("invalid GPIO PTT chip %q or line %d", config.Chip, config.Line)
	}
	if config.Consumer == "" {
		config.Consumer = "vulpes-ptt"
	}
	options := []gpiocdev.LineReqOption{
		gpiocdev.WithConsumer(config.Consumer),
		gpiocdev.AsOutput(0),
	}
	if config.ActiveLow {
		options = append(options, gpiocdev.AsActiveLow)
	} else {
		options = append(options, gpiocdev.AsActiveHigh)
	}
	line, err := gpiocdev.RequestLine(config.Chip, config.Line, options...)
	if err != nil {
		return nil, fmt.Errorf("request GPIO PTT %s line %d: %w", config.Chip, config.Line, err)
	}
	return newGPIODriver(line), nil
}

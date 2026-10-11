// SPDX-License-Identifier: GPL-2.0-or-later

package ptt

import (
	"context"
	"fmt"

	"github.com/sparques/vulpes/internal/config"
)

// OpenConfigured opens and safely initializes one validated YAML PTT backend.
func OpenConfigured(ctx context.Context, configured config.PTT) (*Keyer, error) {
	var (
		driver Driver
		err    error
	)
	switch configured.Type {
	case "", "none", "vox":
		driver = Noop{}
	case "serial":
		driver, err = OpenSerial(SerialConfig{
			Device: configured.Serial.Device, RTS: configured.Serial.RTS, DTR: configured.Serial.DTR,
			RTSActiveHigh: configured.Serial.RTSActive != "low", DTRActiveHigh: configured.Serial.DTRActive != "low",
		})
	case "gpio":
		driver, err = OpenGPIO(GPIOConfig{Chip: configured.GPIO.Chip, Line: configured.GPIO.Line, ActiveLow: configured.GPIO.Active == "low", Consumer: "vulpes-ptt"})
	case "cm108":
		driver, err = OpenCM108(CM108Config{Device: configured.CM108.Device, GPIO: configured.CM108.GPIO, ActiveLow: configured.CM108.Active == "low"})
	default:
		return nil, fmt.Errorf("unsupported PTT type %q", configured.Type)
	}
	if err != nil {
		return nil, err
	}
	return New(ctx, driver)
}

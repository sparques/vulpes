// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !linux

package ptt

import "errors"

type GPIOConfig struct {
	Chip      string
	Line      int
	ActiveLow bool
	Consumer  string
}

func OpenGPIO(GPIOConfig) (Driver, error) {
	return nil, errors.New("GPIO PTT is supported only on Linux")
}

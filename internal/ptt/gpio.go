// SPDX-License-Identifier: GPL-2.0-or-later

package ptt

import (
	"context"
	"errors"
)

type gpioLine interface {
	SetValue(int) error
	Close() error
}

type gpioDriver struct {
	line gpioLine
}

func newGPIODriver(line gpioLine) Driver {
	return &gpioDriver{line: line}
}

func (d *gpioDriver) Set(ctx context.Context, keyed bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value := 0
	if keyed {
		value = 1
	}
	return d.line.SetValue(value)
}

func (d *gpioDriver) Close() error {
	return errors.Join(d.line.SetValue(0), d.line.Close())
}

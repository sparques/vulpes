// SPDX-License-Identifier: GPL-2.0-or-later

package ptt

import (
	"context"
	"errors"
	"fmt"

	"go.bug.st/serial"
)

// SerialConfig selects RTS and/or DTR control lines. ActiveHigh means the
// serial library's asserted state keys the transmitter; false inverts it as in
// Dire Wolf's -RTS/-DTR configuration.
type SerialConfig struct {
	Device        string
	RTS           bool
	DTR           bool
	RTSActiveHigh bool
	DTRActiveHigh bool
}

type serialPort interface {
	SetRTS(bool) error
	SetDTR(bool) error
	Close() error
}

type serialDriver struct {
	port   serialPort
	config SerialConfig
}

// OpenSerial opens a serial device exclusively for PTT control and initializes
// selected control lines inactive.
func OpenSerial(config SerialConfig) (Driver, error) {
	if config.Device == "" || (!config.RTS && !config.DTR) {
		return nil, errors.New("serial PTT needs a device and RTS and/or DTR")
	}
	port, err := serial.Open(config.Device, &serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
		InitialStatusBits: &serial.ModemOutputBits{
			RTS: inactiveLevel(config.RTSActiveHigh),
			DTR: inactiveLevel(config.DTRActiveHigh),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open serial PTT %q: %w", config.Device, err)
	}
	driver := &serialDriver{port: port, config: config}
	if err := driver.Set(context.Background(), false); err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("initialize serial PTT %q: %w", config.Device, err)
	}
	return driver, nil
}

func newSerialDriver(port serialPort, config SerialConfig) Driver {
	return &serialDriver{port: port, config: config}
}

func (d *serialDriver) Set(ctx context.Context, keyed bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if d.config.RTS {
		err = errors.Join(err, d.port.SetRTS(lineLevel(keyed, d.config.RTSActiveHigh)))
	}
	if d.config.DTR {
		err = errors.Join(err, d.port.SetDTR(lineLevel(keyed, d.config.DTRActiveHigh)))
	}
	if err != nil && keyed {
		if d.config.RTS {
			err = errors.Join(err, d.port.SetRTS(inactiveLevel(d.config.RTSActiveHigh)))
		}
		if d.config.DTR {
			err = errors.Join(err, d.port.SetDTR(inactiveLevel(d.config.DTRActiveHigh)))
		}
	}
	return err
}

func (d *serialDriver) Close() error {
	return errors.Join(d.Set(context.Background(), false), d.port.Close())
}

func lineLevel(keyed, activeHigh bool) bool { return keyed == activeHigh }
func inactiveLevel(activeHigh bool) bool    { return !activeHigh }

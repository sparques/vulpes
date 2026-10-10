// SPDX-License-Identifier: GPL-2.0-or-later

// Package ptt controls transmitter push-to-talk outputs.
package ptt

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrClosed = errors.New("PTT controller is closed")

// Driver is one hardware-specific PTT output. Set must be safe to call with
// keyed=false during error recovery even if a prior Set call failed.
type Driver interface {
	Set(context.Context, bool) error
	Close() error
}

// Keyer serializes access to a Driver, suppresses redundant writes, and
// fail-safely unkeys before closing the hardware.
type Keyer struct {
	driver Driver
	keyed  bool
	closed bool
	mu     sync.Mutex
}

// New constructs an unkeyed PTT controller and explicitly drives its backend
// to the safe state.
func New(ctx context.Context, driver Driver) (*Keyer, error) {
	if driver == nil {
		return nil, errors.New("PTT driver is nil")
	}
	if err := driver.Set(ctx, false); err != nil {
		_ = driver.Close()
		return nil, fmt.Errorf("initialize PTT unkeyed: %w", err)
	}
	return &Keyer{driver: driver}, nil
}

// Set changes the requested transmitter state. Redundant requests do not touch
// hardware.
func (k *Keyer) Set(ctx context.Context, keyed bool) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return ErrClosed
	}
	if k.keyed == keyed {
		return nil
	}
	if err := k.driver.Set(ctx, keyed); err != nil {
		if keyed {
			// A partially successful hardware operation must be driven safe.
			_ = k.driver.Set(context.WithoutCancel(ctx), false)
		}
		k.keyed = false
		return err
	}
	k.keyed = keyed
	return nil
}

// Keyed reports the last successfully applied state.
func (k *Keyer) Keyed() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.keyed
}

// Close attempts to unkey even when ctx is canceled, then closes the driver.
// Both errors are preserved when unkey and close fail.
func (k *Keyer) Close(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil
	}
	k.closed = true
	unkeyErr := k.driver.Set(context.WithoutCancel(ctx), false)
	k.keyed = false
	closeErr := k.driver.Close()
	return errors.Join(unkeyErr, closeErr)
}

// Noop is a PTT driver for VOX operation and tests without PTT hardware.
type Noop struct{}

func (Noop) Set(context.Context, bool) error { return nil }
func (Noop) Close() error                    { return nil }

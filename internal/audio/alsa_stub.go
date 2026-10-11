// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !linux || !cgo

package audio

import "errors"

var errALSAUnavailable = errors.New("ALSA support requires Linux and cgo")

type ALSADevice struct {
	Name        string
	Description string
	Input       bool
	Output      bool
}

func ListALSADevices() ([]ALSADevice, error)          { return nil, errALSAUnavailable }
func OpenALSACapture(DeviceConfig) (Capture, error)   { return nil, errALSAUnavailable }
func OpenALSAPlayback(DeviceConfig) (Playback, error) { return nil, errALSAUnavailable }

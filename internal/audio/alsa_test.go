// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux && cgo

package audio

import "testing"

func TestALSADeviceEnumeration(t *testing.T) {
	devices, err := ListALSADevices()
	if err != nil {
		t.Skipf("ALSA enumeration is unavailable: %v", err)
	}
	for _, device := range devices {
		if device.Name == "" || !device.Input && !device.Output {
			t.Fatalf("invalid ALSA device: %#v", device)
		}
	}
}

// SPDX-License-Identifier: GPL-2.0-or-later

package ptt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CM108Config selects a CM108-family HID GPIO. GPIO defaults to the commonly
// used GPIO 3. If Device is empty, exactly one compatible hidraw device must be
// discoverable.
type CM108Config struct {
	Device    string
	GPIO      int
	ActiveLow bool
}

type cm108Driver struct {
	device    io.WriteCloser
	name      string
	mask      byte
	activeLow bool
}

// OpenCM108 opens a compatible hidraw device for GPIO PTT.
func OpenCM108(config CM108Config) (Driver, error) {
	if config.GPIO == 0 {
		config.GPIO = 3
	}
	if config.GPIO < 1 || config.GPIO > 8 {
		return nil, fmt.Errorf("CM108 GPIO %d is outside 1..8", config.GPIO)
	}
	device := config.Device
	if device == "" {
		devices, err := discoverCM108("/sys/class/hidraw", "/dev")
		if err != nil {
			return nil, err
		}
		switch len(devices) {
		case 0:
			return nil, errors.New("no compatible CM108-family HID GPIO device found")
		case 1:
			device = devices[0]
		default:
			return nil, fmt.Errorf("multiple compatible CM108-family HID GPIO devices found; select one explicitly: %s", strings.Join(devices, ", "))
		}
	}
	file, err := os.OpenFile(device, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open CM108 GPIO device %q for writing: %w (verify hidraw permissions and audio-group membership)", device, err)
	}
	driver := &cm108Driver{device: file, name: device, mask: 1 << (config.GPIO - 1), activeLow: config.ActiveLow}
	if err := driver.Set(context.Background(), false); err != nil {
		_ = file.Close()
		return nil, err
	}
	return driver, nil
}

func newCM108Driver(device io.WriteCloser, name string, gpio int, activeLow bool) Driver {
	return &cm108Driver{device: device, name: name, mask: 1 << (gpio - 1), activeLow: activeLow}
}

func (d *cm108Driver) Set(ctx context.Context, keyed bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	physicalHigh := keyed != d.activeLow
	data := byte(0)
	if physicalHigh {
		data = d.mask
	}
	// This five-byte HID report and byte order match Dire Wolf cm108_write:
	// report ID, reserved, output data, output mask, reserved.
	report := []byte{0, 0, data, d.mask, 0}
	written, err := d.device.Write(report)
	if err != nil {
		return fmt.Errorf("write CM108 GPIO report to %q: %w", d.name, err)
	}
	if written != len(report) {
		return fmt.Errorf("write CM108 GPIO report to %q: %w", d.name, io.ErrShortWrite)
	}
	return nil
}

func (d *cm108Driver) Close() error {
	return errors.Join(d.Set(context.Background(), false), d.device.Close())
}

func discoverCM108(sysClass, devRoot string) ([]string, error) {
	entries, err := filepath.Glob(filepath.Join(sysClass, "hidraw*"))
	if err != nil {
		return nil, fmt.Errorf("enumerate hidraw devices: %w", err)
	}
	var devices []string
	for _, entry := range entries {
		devicePath, err := filepath.EvalSymlinks(filepath.Join(entry, "device"))
		if err != nil {
			continue
		}
		vendor, product, ok := findUSBID(devicePath)
		if ok && compatibleCM108(vendor, product) {
			devices = append(devices, filepath.Join(devRoot, filepath.Base(entry)))
		}
	}
	return devices, nil
}

func findUSBID(path string) (uint16, uint16, bool) {
	for {
		vendorText, vendorErr := os.ReadFile(filepath.Join(path, "idVendor"))
		productText, productErr := os.ReadFile(filepath.Join(path, "idProduct"))
		if vendorErr == nil && productErr == nil {
			vendor, vendorParseErr := strconv.ParseUint(strings.TrimSpace(string(vendorText)), 16, 16)
			product, productParseErr := strconv.ParseUint(strings.TrimSpace(string(productText)), 16, 16)
			if vendorParseErr == nil && productParseErr == nil {
				return uint16(vendor), uint16(product), true
			}
			return 0, 0, false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, 0, false
		}
		path = parent
	}
}

func compatibleCM108(vendor, product uint16) bool {
	if vendor == 0x0d8c {
		return product >= 0x0008 && product <= 0x000f || product == 0x0012 || product == 0x0013 || product == 0x0139 || product == 0x013a || product == 0x013c
	}
	if vendor == 0x0c76 {
		return product == 0x1605 || product == 0x1607 || product == 0x160b
	}
	return vendor == 0x1209 && product == 0x7388
}

// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux

package ptt

import (
	"context"
	"os"
	"strconv"
	"testing"
)

// Hardware tests are opt-in because they deliberately toggle physical output
// lines. Each skips cleanly unless its exact device is supplied.
func TestSerialHardware(t *testing.T) {
	device := os.Getenv("VULPES_TEST_SERIAL_PTT")
	if device == "" {
		t.Skip("set VULPES_TEST_SERIAL_PTT to a serial adapter to test RTS/DTR")
	}
	driver, err := OpenSerial(SerialConfig{Device: device, RTS: true, DTR: true, RTSActiveHigh: true, DTRActiveHigh: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = driver.Close() })
	if err := driver.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := driver.Set(context.Background(), false); err != nil {
		t.Fatal(err)
	}
}

func TestGPIOHardware(t *testing.T) {
	chip := os.Getenv("VULPES_TEST_GPIO_CHIP")
	lineText := os.Getenv("VULPES_TEST_GPIO_LINE")
	if chip == "" || lineText == "" {
		t.Skip("set VULPES_TEST_GPIO_CHIP and VULPES_TEST_GPIO_LINE to test GPIO PTT")
	}
	line, err := strconv.Atoi(lineText)
	if err != nil {
		t.Fatalf("invalid VULPES_TEST_GPIO_LINE: %v", err)
	}
	driver, err := OpenGPIO(GPIOConfig{Chip: chip, Line: line, Consumer: "vulpes-hardware-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = driver.Close() })
	if err := driver.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := driver.Set(context.Background(), false); err != nil {
		t.Fatal(err)
	}
}

func TestCM108Hardware(t *testing.T) {
	device := os.Getenv("VULPES_TEST_CM108")
	if device == "" {
		t.Skip("set VULPES_TEST_CM108 to a compatible hidraw device to test CM108 PTT")
	}
	driver, err := OpenCM108(CM108Config{Device: device, GPIO: 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = driver.Close() })
	if err := driver.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := driver.Set(context.Background(), false); err != nil {
		t.Fatal(err)
	}
}

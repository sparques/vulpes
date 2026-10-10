// SPDX-License-Identifier: GPL-2.0-or-later

package ptt

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCM108ReportsAndPolarity(t *testing.T) {
	for _, test := range []struct {
		name      string
		activeLow bool
		want      [][]byte
	}{
		{name: "active high", want: [][]byte{{0, 0, 4, 4, 0}, {0, 0, 0, 4, 0}}},
		{name: "active low", activeLow: true, want: [][]byte{{0, 0, 0, 4, 0}, {0, 0, 4, 4, 0}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			device := &fakeHID{}
			driver := newCM108Driver(device, "/dev/hidraw-test", 3, test.activeLow)
			if err := driver.Set(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if err := driver.Set(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(device.reports, test.want) {
				t.Fatalf("reports = %v, want %v", device.reports, test.want)
			}
		})
	}
}

func TestCM108CloseUnkeys(t *testing.T) {
	device := &fakeHID{}
	driver := newCM108Driver(device, "test", 1, false)
	if err := driver.Close(); err != nil {
		t.Fatal(err)
	}
	if !device.closed || !bytes.Equal(device.reports[0], []byte{0, 0, 0, 1, 0}) {
		t.Fatalf("device = %+v", device)
	}
}

func TestDiscoverCM108(t *testing.T) {
	root := t.TempDir()
	sysClass := filepath.Join(root, "sys", "class", "hidraw")
	usbDevice := filepath.Join(root, "sys", "devices", "usb1", "1-1")
	hidDevice := filepath.Join(usbDevice, "1-1:1.3", "0003:0D8C:000C.0001")
	if err := os.MkdirAll(hidDevice, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(usbDevice, "idVendor"), []byte("0d8c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(usbDevice, "idProduct"), []byte("000c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(sysClass, "hidraw7")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hidDevice, filepath.Join(entry, "device")); err != nil {
		t.Fatal(err)
	}
	devices, err := discoverCM108(sysClass, filepath.Join(root, "dev"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "dev", "hidraw7")}
	if !reflect.DeepEqual(devices, want) {
		t.Fatalf("devices = %v, want %v", devices, want)
	}
}

func TestCompatibleCM108IDs(t *testing.T) {
	for _, id := range [][2]uint16{{0x0d8c, 0x0008}, {0x0d8c, 0x0139}, {0x0c76, 0x1607}, {0x1209, 0x7388}} {
		if !compatibleCM108(id[0], id[1]) {
			t.Errorf("%04x:%04x should be compatible", id[0], id[1])
		}
	}
	if compatibleCM108(0x1234, 0x5678) {
		t.Fatal("unknown device marked compatible")
	}
}

type fakeHID struct {
	reports [][]byte
	closed  bool
}

func (d *fakeHID) Write(report []byte) (int, error) {
	d.reports = append(d.reports, append([]byte(nil), report...))
	return len(report), nil
}

func (d *fakeHID) Close() error {
	d.closed = true
	return nil
}

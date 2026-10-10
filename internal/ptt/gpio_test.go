// SPDX-License-Identifier: GPL-2.0-or-later

package ptt

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestGPIODriver(t *testing.T) {
	line := &fakeGPIOLine{}
	driver := newGPIODriver(line)
	if err := driver.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := driver.Set(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if err := driver.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := line.values, []int{1, 0, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("values = %v, want %v", got, want)
	}
	if !line.closed {
		t.Fatal("line was not closed")
	}
}

func TestGPIODriverHonorsCanceledContext(t *testing.T) {
	line := &fakeGPIOLine{}
	driver := newGPIODriver(line)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := driver.Set(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("Set error = %v", err)
	}
	if len(line.values) != 0 {
		t.Fatalf("canceled Set wrote values %v", line.values)
	}
}

type fakeGPIOLine struct {
	values []int
	closed bool
}

func (l *fakeGPIOLine) SetValue(value int) error {
	l.values = append(l.values, value)
	return nil
}

func (l *fakeGPIOLine) Close() error {
	l.closed = true
	return nil
}

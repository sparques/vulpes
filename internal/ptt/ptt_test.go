// SPDX-License-Identifier: GPL-2.0-or-later

package ptt

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestKeyerLifecycle(t *testing.T) {
	driver := &fakeDriver{}
	keyer, err := New(context.Background(), driver)
	if err != nil {
		t.Fatal(err)
	}
	if err := keyer.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := keyer.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := keyer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got, want := driver.states, []bool{false, true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
	if !driver.closed {
		t.Fatal("driver was not closed")
	}
	if err := keyer.Set(context.Background(), true); !errors.Is(err, ErrClosed) {
		t.Fatalf("Set after close error = %v", err)
	}
}

func TestFailedKeyAttemptsUnkey(t *testing.T) {
	driver := &fakeDriver{failKey: true}
	keyer, err := New(context.Background(), driver)
	if err != nil {
		t.Fatal(err)
	}
	if err := keyer.Set(context.Background(), true); err == nil {
		t.Fatal("key succeeded unexpectedly")
	}
	if got, want := driver.states, []bool{false, true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
	if keyer.Keyed() {
		t.Fatal("failed key left logical state keyed")
	}
}

func TestClosePreservesBothErrors(t *testing.T) {
	unkeyErr := errors.New("unkey")
	closeErr := errors.New("close")
	driver := &fakeDriver{failUnkeyAfter: 1, unkeyErr: unkeyErr, closeErr: closeErr}
	keyer, err := New(context.Background(), driver)
	if err != nil {
		t.Fatal(err)
	}
	err = keyer.Close(context.Background())
	if !errors.Is(err, unkeyErr) || !errors.Is(err, closeErr) {
		t.Fatalf("Close error = %v", err)
	}
}

type fakeDriver struct {
	states         []bool
	closed         bool
	failKey        bool
	failUnkeyAfter int
	unkeyCount     int
	unkeyErr       error
	closeErr       error
}

func (d *fakeDriver) Set(_ context.Context, keyed bool) error {
	d.states = append(d.states, keyed)
	if keyed && d.failKey {
		return errors.New("key failed")
	}
	if !keyed {
		d.unkeyCount++
		if d.unkeyCount > d.failUnkeyAfter && d.failUnkeyAfter != 0 {
			return d.unkeyErr
		}
	}
	return nil
}

func (d *fakeDriver) Close() error {
	d.closed = true
	return d.closeErr
}

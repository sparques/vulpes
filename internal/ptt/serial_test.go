// SPDX-License-Identifier: GPL-2.0-or-later

package ptt

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSerialDriverLinesAndPolarity(t *testing.T) {
	port := &fakeSerialPort{}
	driver := newSerialDriver(port, SerialConfig{RTS: true, DTR: true, RTSActiveHigh: true, DTRActiveHigh: false})
	if err := driver.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := driver.Set(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if got, want := port.rts, []bool{true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RTS = %v, want %v", got, want)
	}
	if got, want := port.dtr, []bool{false, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DTR = %v, want %v", got, want)
	}
}

func TestSerialDriverRollsBackPartialKey(t *testing.T) {
	port := &fakeSerialPort{failDTR: true}
	driver := newSerialDriver(port, SerialConfig{RTS: true, DTR: true, RTSActiveHigh: true, DTRActiveHigh: true})
	if err := driver.Set(context.Background(), true); err == nil {
		t.Fatal("Set succeeded unexpectedly")
	}
	if got, want := port.rts, []bool{true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RTS = %v, want rollback %v", got, want)
	}
	if got, want := port.dtr, []bool{true, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DTR = %v, want rollback %v", got, want)
	}
}

func TestSerialDriverCloseUnkeys(t *testing.T) {
	port := &fakeSerialPort{}
	driver := newSerialDriver(port, SerialConfig{RTS: true, RTSActiveHigh: true})
	if err := driver.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := port.rts, []bool{false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RTS = %v, want %v", got, want)
	}
	if !port.closed {
		t.Fatal("port was not closed")
	}
}

type fakeSerialPort struct {
	rts     []bool
	dtr     []bool
	failDTR bool
	closed  bool
}

func (p *fakeSerialPort) SetRTS(value bool) error {
	p.rts = append(p.rts, value)
	return nil
}

func (p *fakeSerialPort) SetDTR(value bool) error {
	p.dtr = append(p.dtr, value)
	if p.failDTR && value {
		return errors.New("DTR failed")
	}
	return nil
}

func (p *fakeSerialPort) Close() error {
	p.closed = true
	return nil
}

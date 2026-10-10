// SPDX-License-Identifier: GPL-2.0-or-later

package audio

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestSliceCapturePlayback(t *testing.T) {
	spec := Spec{SampleRate: 48000, Channels: 2}
	want := []int16{-32768, 32767, -1, 1, 0, 42}
	capture, err := NewSliceCapture(spec, want)
	if err != nil {
		t.Fatal(err)
	}
	playback, err := NewSlicePlayback(spec)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]int16, 4)
	for {
		n, readErr := capture.ReadSamples(context.Background(), buffer)
		if n != 0 {
			if _, err := playback.WriteSamples(context.Background(), buffer[:n]); err != nil {
				t.Fatal(err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
	}
	if got := playback.Samples(); !reflect.DeepEqual(got, want) {
		t.Fatalf("samples = %v, want %v", got, want)
	}
}

func TestMisalignedBuffers(t *testing.T) {
	spec := Spec{SampleRate: 44100, Channels: 2}
	capture, err := NewSliceCapture(spec, []int16{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capture.ReadSamples(context.Background(), make([]int16, 1)); !errors.Is(err, ErrMisalignedBuffer) {
		t.Fatalf("ReadSamples error = %v", err)
	}
}

// SPDX-License-Identifier: GPL-2.0-or-later

package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestWAVRoundTrip(t *testing.T) {
	spec := Spec{SampleRate: 44100, Channels: 2}
	want := []int16{-32768, 32767, -1234, 1234, 0, 1}
	output := newMemoryWriteSeeker()
	writer, err := NewWAVWriter(output, spec)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := writer.WriteSamples(context.Background(), want[:4]); err != nil || n != 4 {
		t.Fatalf("first write = %d, %v", n, err)
	}
	if n, err := writer.WriteSamples(context.Background(), want[4:]); err != nil || n != 2 {
		t.Fatalf("second write = %d, %v", n, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	data := output.Bytes()
	if got, wantSize := binary.LittleEndian.Uint32(data[4:8]), uint32(len(data)-8); got != wantSize {
		t.Fatalf("RIFF size = %d, want %d", got, wantSize)
	}
	reader, err := NewWAVReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := reader.Spec(); got != spec {
		t.Fatalf("spec = %+v, want %+v", got, spec)
	}
	var samples []int16
	buffer := make([]int16, 4)
	for {
		n, readErr := reader.ReadSamples(context.Background(), buffer)
		samples = append(samples, buffer[:n]...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
	}
	if !reflect.DeepEqual(samples, want) {
		t.Fatalf("samples = %v, want %v", samples, want)
	}
}

func TestWAVReaderSkipsUnknownChunk(t *testing.T) {
	output := newMemoryWriteSeeker()
	writer, err := NewWAVWriter(output, Spec{SampleRate: 48000, Channels: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteSamples(context.Background(), []int16{123}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	canonical := output.Bytes()
	withChunk := append([]byte(nil), canonical[:12]...)
	withChunk = append(withChunk, 'J', 'U', 'N', 'K', 3, 0, 0, 0, 1, 2, 3, 0)
	withChunk = append(withChunk, canonical[12:]...)
	binary.LittleEndian.PutUint32(withChunk[4:8], uint32(len(withChunk)-8))
	reader, err := NewWAVReader(bytes.NewReader(withChunk))
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]int16, 1)
	if n, err := reader.ReadSamples(context.Background(), buffer); err != nil || n != 1 || buffer[0] != 123 {
		t.Fatalf("read = %d, %v, sample %d", n, err, buffer[0])
	}
}

func TestWAVRejectsUnsupportedFormat(t *testing.T) {
	data := make([]byte, 44)
	copy(data[:], "RIFF")
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 3)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 48000)
	binary.LittleEndian.PutUint32(data[28:], 192000)
	binary.LittleEndian.PutUint16(data[32:], 4)
	binary.LittleEndian.PutUint16(data[34:], 32)
	copy(data[36:], "data")
	if _, err := NewWAVReader(bytes.NewReader(data)); !errors.Is(err, ErrInvalidWAV) {
		t.Fatalf("error = %v", err)
	}
}

type memoryWriteSeeker struct {
	data   []byte
	offset int64
}

func newMemoryWriteSeeker() *memoryWriteSeeker { return &memoryWriteSeeker{} }

func (w *memoryWriteSeeker) Write(data []byte) (int, error) {
	end := w.offset + int64(len(data))
	if end > int64(len(w.data)) {
		w.data = append(w.data, make([]byte, end-int64(len(w.data)))...)
	}
	copy(w.data[w.offset:end], data)
	w.offset = end
	return len(data), nil
}

func (w *memoryWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = w.offset + offset
	case io.SeekEnd:
		next = int64(len(w.data)) + offset
	}
	if next < 0 {
		return 0, errors.New("negative seek")
	}
	w.offset = next
	return next, nil
}

func (w *memoryWriteSeeker) Bytes() []byte { return append([]byte(nil), w.data...) }

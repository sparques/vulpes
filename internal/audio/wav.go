// SPDX-License-Identifier: GPL-2.0-or-later

package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidWAV = errors.New("invalid WAV file")

// WAVReader reads interleaved 16-bit PCM from a RIFF/WAVE stream.
type WAVReader struct {
	reader    io.Reader
	spec      Spec
	remaining uint32
	closed    bool
}

// NewWAVReader parses a WAV header and positions the stream at its data chunk.
func NewWAVReader(reader io.Reader) (*WAVReader, error) {
	var header [12]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, fmt.Errorf("%w: read RIFF header: %v", ErrInvalidWAV, err)
	}
	if string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return nil, fmt.Errorf("%w: expected RIFF/WAVE", ErrInvalidWAV)
	}

	var spec Spec
	foundFormat := false
	for {
		var chunk [8]byte
		if _, err := io.ReadFull(reader, chunk[:]); err != nil {
			return nil, fmt.Errorf("%w: find data chunk: %v", ErrInvalidWAV, err)
		}
		size := binary.LittleEndian.Uint32(chunk[4:])
		switch string(chunk[:4]) {
		case "fmt ":
			if size < 16 || size > 1<<20 {
				return nil, fmt.Errorf("%w: unsupported fmt chunk size %d", ErrInvalidWAV, size)
			}
			body := make([]byte, size)
			if _, err := io.ReadFull(reader, body); err != nil {
				return nil, fmt.Errorf("%w: read fmt chunk: %v", ErrInvalidWAV, err)
			}
			format := binary.LittleEndian.Uint16(body[0:2])
			channels := binary.LittleEndian.Uint16(body[2:4])
			sampleRate := binary.LittleEndian.Uint32(body[4:8])
			byteRate := binary.LittleEndian.Uint32(body[8:12])
			blockAlign := binary.LittleEndian.Uint16(body[12:14])
			bits := binary.LittleEndian.Uint16(body[14:16])
			if format != 1 || bits != 16 {
				return nil, fmt.Errorf("%w: only 16-bit integer PCM is supported", ErrInvalidWAV)
			}
			spec = Spec{SampleRate: int(sampleRate), Channels: int(channels)}
			if err := spec.Validate(); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalidWAV, err)
			}
			if blockAlign != uint16(spec.Channels*2) || byteRate != sampleRate*uint32(blockAlign) {
				return nil, fmt.Errorf("%w: inconsistent PCM rates", ErrInvalidWAV)
			}
			foundFormat = true
			if size&1 != 0 {
				if _, err := io.CopyN(io.Discard, reader, 1); err != nil {
					return nil, fmt.Errorf("%w: read fmt padding: %v", ErrInvalidWAV, err)
				}
			}
		case "data":
			if !foundFormat {
				return nil, fmt.Errorf("%w: data appears before fmt", ErrInvalidWAV)
			}
			if size%uint32(spec.Channels*2) != 0 {
				return nil, fmt.Errorf("%w: data is not frame-aligned", ErrInvalidWAV)
			}
			return &WAVReader{reader: reader, spec: spec, remaining: size}, nil
		default:
			padded := int64(size)
			if size&1 != 0 {
				padded++
			}
			if _, err := io.CopyN(io.Discard, reader, padded); err != nil {
				return nil, fmt.Errorf("%w: skip %q chunk: %v", ErrInvalidWAV, chunk[:4], err)
			}
		}
	}
}

func (r *WAVReader) Spec() Spec { return r.spec }

func (r *WAVReader) ReadSamples(ctx context.Context, destination []int16) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	if len(destination)%r.spec.Channels != 0 {
		return 0, ErrMisalignedBuffer
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	wanted := len(destination) * 2
	if uint32(wanted) > r.remaining {
		wanted = int(r.remaining)
	}
	bytes := make([]byte, wanted)
	read, err := io.ReadFull(r.reader, bytes)
	if err != nil {
		return 0, fmt.Errorf("%w: truncated data chunk after %d bytes", ErrInvalidWAV, read)
	}
	r.remaining -= uint32(read)
	for i := 0; i < read/2; i++ {
		destination[i] = int16(binary.LittleEndian.Uint16(bytes[i*2:]))
	}
	return read / 2, nil
}

func (r *WAVReader) Close() error {
	r.closed = true
	return nil
}

// WAVWriter writes a canonical seekable 16-bit PCM RIFF/WAVE stream. Close
// finalizes sizes but does not close the underlying writer.
type WAVWriter struct {
	writer io.WriteSeeker
	spec   Spec
	base   int64
	bytes  uint32
	closed bool
	failed error
}

// NewWAVWriter writes a placeholder header at the writer's current position.
func NewWAVWriter(writer io.WriteSeeker, spec Spec) (*WAVWriter, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	base, err := writer.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("find WAV output offset: %w", err)
	}
	w := &WAVWriter{writer: writer, spec: spec, base: base}
	header := w.header(0)
	if _, err := writer.Write(header[:]); err != nil {
		return nil, fmt.Errorf("write WAV header: %w", err)
	}
	return w, nil
}

func (w *WAVWriter) Spec() Spec { return w.spec }

func (w *WAVWriter) WriteSamples(ctx context.Context, samples []int16) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	if w.failed != nil {
		return 0, w.failed
	}
	if len(samples)%w.spec.Channels != 0 {
		return 0, ErrMisalignedBuffer
	}
	if uint64(w.bytes)+uint64(len(samples))*2 > uint64(^uint32(0))-36 {
		return 0, fmt.Errorf("WAV data exceeds RIFF size limit")
	}
	buffer := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(buffer[i*2:], uint16(sample))
	}
	written, err := w.writer.Write(buffer)
	w.bytes += uint32(written)
	if err == nil && written != len(buffer) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.failed = fmt.Errorf("write WAV samples: %w", err)
		return written / 2, w.failed
	}
	return len(samples), nil
}

func (w *WAVWriter) Close() error {
	if w.closed {
		return w.failed
	}
	w.closed = true
	if w.failed != nil {
		return w.failed
	}
	end, err := w.writer.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("find end of WAV output: %w", err)
	}
	if _, err := w.writer.Seek(w.base, io.SeekStart); err != nil {
		return fmt.Errorf("seek to WAV header: %w", err)
	}
	header := w.header(w.bytes)
	if _, err := w.writer.Write(header[:]); err != nil {
		return fmt.Errorf("finalize WAV header: %w", err)
	}
	if _, err := w.writer.Seek(end, io.SeekStart); err != nil {
		return fmt.Errorf("restore WAV output position: %w", err)
	}
	return nil
}

func (w *WAVWriter) header(dataBytes uint32) [44]byte {
	var header [44]byte
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataBytes)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], uint16(w.spec.Channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(w.spec.SampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(w.spec.SampleRate*w.spec.Channels*2))
	binary.LittleEndian.PutUint16(header[32:34], uint16(w.spec.Channels*2))
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataBytes)
	return header
}

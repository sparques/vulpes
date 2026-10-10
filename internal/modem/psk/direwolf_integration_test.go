// SPDX-License-Identifier: GPL-2.0-or-later

package psk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sparques/vulpes/internal/audio"
	"github.com/sparques/vulpes/internal/ax25"
	"github.com/sparques/vulpes/internal/hdlc"
)

func TestVulpesDecodesDireWolfPSKWAV(t *testing.T) {
	program, err := exec.LookPath("gen_packets")
	if err != nil {
		t.Skip("Dire Wolf gen_packets is not installed")
	}
	for _, test := range []struct {
		name string
		mode Mode
		args []string
	}{
		{name: "V26A-2400", mode: V26A2400, args: []string{"-j"}},
		{name: "V26B-2400", mode: V26B2400, args: []string{"-J"}},
		{name: "V27-4800", mode: V274800, args: []string{"-B", "4800"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "direwolf-psk.wav")
			args := append(append([]string(nil), test.args...), "-a", "50", "-r", "48000", "-o", path, "-")
			wantInfo := []byte("Dire Wolf PSK interoperability")
			command := exec.Command(program, args...)
			command.Stdin = bytes.NewReader(append([]byte("N0CALL>VULPES:"), wantInfo...))
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("gen_packets: %v\n%s", err, output)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			wav, err := audio.NewWAVReader(file)
			if err != nil {
				t.Fatal(err)
			}
			receiver, err := NewReceiver(Config{SampleRate: wav.Spec().SampleRate, Mode: test.mode, Amplitude: 1}, 2048)
			if err != nil {
				t.Fatal(err)
			}
			buffer := make([]int16, 4096)
			var frames []ReceivedFrame
			for {
				count, readErr := wav.ReadSamples(context.Background(), buffer)
				frames = append(frames, receiver.Process(buffer[:count])...)
				if errors.Is(readErr, io.EOF) {
					break
				}
				if readErr != nil {
					t.Fatal(readErr)
				}
			}
			if len(frames) != 1 {
				t.Fatalf("frames=%d stats=%+v", len(frames), receiver.Stats())
			}
			frame, err := ax25.Parse(frames[0].Payload)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(frame.Information, wantInfo) {
				t.Fatalf("information=%q", frame.Information)
			}
		})
	}
}

func TestDireWolfDecodesVulpesPSKWAV(t *testing.T) {
	program, err := exec.LookPath("atest")
	if err != nil {
		t.Skip("Dire Wolf atest is not installed")
	}
	for _, test := range []struct {
		name string
		mode Mode
		args []string
	}{
		{name: "V26A-2400", mode: V26A2400, args: []string{"-j"}},
		{name: "V26B-2400", mode: V26B2400, args: []string{"-J"}},
		{name: "V27-4800", mode: V274800, args: []string{"-B", "4800"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			frame := ax25.Frame{
				Addresses:   []ax25.Address{{Callsign: "VULPES"}, {Callsign: "N0CALL"}},
				Control:     0x03,
				PID:         []byte{0xf0},
				Information: []byte("Vulpes PSK interoperability"),
			}
			payload, err := frame.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			modulator, err := NewModulator(Config{SampleRate: 48000, Mode: test.mode, Amplitude: 0.5})
			if err != nil {
				t.Fatal(err)
			}
			samples := modulator.Modulate(hdlc.Encode(payload, 100, 10))
			path := filepath.Join(t.TempDir(), "vulpes-psk.wav")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			wav, err := audio.NewWAVWriter(file, audio.Spec{SampleRate: 48000, Channels: 1})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := wav.WriteSamples(context.Background(), samples); err != nil {
				t.Fatal(err)
			}
			if err := wav.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			args := append(append([]string(nil), test.args...), "-L", "1", "-G", "1", path)
			command := exec.Command(program, args...)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("atest: %v\n%s", err, output)
			}
		})
	}
}

// SPDX-License-Identifier: GPL-2.0-or-later

package g3ruh

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

func TestVulpesDecodesDireWolf9600WAV(t *testing.T) {
	program, err := exec.LookPath("gen_packets")
	if err != nil {
		t.Skip("Dire Wolf gen_packets is not installed")
	}
	for _, sampleRate := range []string{"44100", "48000"} {
		t.Run(sampleRate, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "direwolf-9600.wav")
			wantInfo := []byte("Dire Wolf G3RUH interoperability")
			command := exec.Command(program, "-B", "9600", "-a", "80", "-r", sampleRate, "-o", path, "-")
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
			receiver, err := NewReceiver(Config{SampleRate: wav.Spec().SampleRate}, 2048)
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
			for _, received := range frames {
				frame, err := ax25.Parse(received.Payload)
				if err == nil && bytes.Equal(frame.Information, wantInfo) {
					return
				}
			}
			t.Fatalf("decoded %d frames; stats %+v", len(frames), receiver.Stats())
		})
	}
}

func TestDireWolfDecodesVulpes9600WAV(t *testing.T) {
	program, err := exec.LookPath("atest")
	if err != nil {
		t.Skip("Dire Wolf atest is not installed")
	}
	frame := ax25.Frame{Addresses: []ax25.Address{{Callsign: "VULPES"}, {Callsign: "N0CALL"}}, Control: 0x03, PID: []byte{0xf0}, Information: []byte("Vulpes G3RUH interoperability")}
	payload, err := frame.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	for _, sampleRate := range []int{44100, 48000} {
		modulator, _ := NewModulator(Config{SampleRate: sampleRate, Amplitude: .5})
		samples := modulator.Modulate(hdlc.Encode(payload, 100, 20))
		path := filepath.Join(t.TempDir(), "vulpes-9600.wav")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		wav, _ := audio.NewWAVWriter(file, audio.Spec{SampleRate: sampleRate, Channels: 1})
		if _, err := wav.WriteSamples(context.Background(), samples); err != nil {
			t.Fatal(err)
		}
		if err := wav.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		command := exec.Command(program, "-B", "9600", "-L", "1", "-G", "1", path)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("atest at %d Hz: %v\n%s", sampleRate, err, output)
		}
	}
}

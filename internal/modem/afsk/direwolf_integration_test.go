// SPDX-License-Identifier: GPL-2.0-or-later

package afsk

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

func TestDecodeDireWolfGenPacketsWAV(t *testing.T) {
	program, err := exec.LookPath("gen_packets")
	if err != nil {
		t.Skip("Dire Wolf gen_packets is not installed")
	}
	for _, test := range []struct {
		name        string
		baud        string
		mark, space float64
		profile     string
		slicers     int
	}{
		{name: "300-A", baud: "300", mark: 1600, space: 1800, profile: "A", slicers: 9},
		{name: "300-B", baud: "300", mark: 1600, space: 1800, profile: "B", slicers: 9},
		{name: "1200-A", baud: "1200", mark: 1200, space: 2200, profile: "A", slicers: 9},
		{name: "1200-B", baud: "1200", mark: 1200, space: 2200, profile: "B", slicers: 9},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "direwolf.wav")
			command := exec.Command(program, "-a", "50", "-r", "48000", "-B", test.baud, "-o", path, "-")
			wantInfo := []byte("Dire Wolf to Vulpes interoperability")
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
			config := Config{SampleRate: wav.Spec().SampleRate, Baud: testBaud(t, test.baud), Mark: test.mark, Space: test.space, Amplitude: 1, Profile: test.profile, Slicers: test.slicers}
			receiver, err := NewReceiver(config, 2048)
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
				t.Fatalf("decoded %d frames, want 1; HDLC stats %+v", len(frames), receiver.Stats())
			}
			frame, err := ax25.Parse(frames[0].Payload)
			if err != nil {
				t.Fatalf("parse generated AX.25 frame: %v", err)
			}
			if !bytes.Equal(frame.Information, wantInfo) {
				t.Fatalf("information = %q, want %q", frame.Information, wantInfo)
			}
		})
	}
}

func TestDireWolfDecodesVulpesWAV(t *testing.T) {
	program, err := exec.LookPath("atest")
	if err != nil {
		t.Skip("Dire Wolf atest is not installed")
	}
	for _, test := range []struct {
		name        string
		baud        int
		mark, space float64
	}{
		{name: "300", baud: 300, mark: 1600, space: 1800},
		{name: "1200", baud: 1200, mark: 1200, space: 2200},
	} {
		t.Run(test.name, func(t *testing.T) {
			frame := ax25.Frame{
				Addresses:   []ax25.Address{{Callsign: "VULPES"}, {Callsign: "N0CALL"}},
				Control:     0x03,
				PID:         []byte{0xf0},
				Information: []byte("Vulpes to Dire Wolf interoperability"),
			}
			payload, err := frame.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			config := Config{SampleRate: 48000, Baud: test.baud, Mark: test.mark, Space: test.space, Amplitude: 0.5}
			modulator, err := NewModulator(config)
			if err != nil {
				t.Fatal(err)
			}
			samples := modulator.Modulate(hdlc.Encode(payload, 50, 5))
			path := filepath.Join(t.TempDir(), "vulpes.wav")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			wav, err := audio.NewWAVWriter(file, audio.Spec{SampleRate: config.SampleRate, Channels: 1})
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
			command := exec.Command(program, "-B", testBaudString(test.baud), "-L", "1", "-G", "1", path)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("atest: %v\n%s", err, output)
			}
		})
	}
}

func testBaud(t *testing.T, value string) int {
	t.Helper()
	switch value {
	case "300":
		return 300
	case "1200":
		return 1200
	default:
		t.Fatalf("unsupported test baud %q", value)
		return 0
	}
}

func testBaudString(value int) string {
	if value == 300 {
		return "300"
	}
	return "1200"
}

// SPDX-License-Identifier: GPL-2.0-or-later

package eas

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sparques/vulpes/internal/audio"
)

func TestDecodeDireWolfCheckModemEAS(t *testing.T) {
	program, err := exec.LookPath("gen_packets")
	if err != nil {
		t.Skip("Dire Wolf gen_packets is not installed")
	}
	path := filepath.Join(t.TempDir(), "direwolf-eas.wav")
	command := exec.Command(program, "-B", "EAS", "-r", "48000", "-o", path)
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
	receiver, err := NewReceiver(Config{SampleRate: wav.Spec().SampleRate})
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]int16, 997)
	var messages []Message
	for {
		count, readErr := wav.ReadSamples(context.Background(), buffer)
		messages = append(messages, receiver.Process(buffer[:count])...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
	}
	var headers, endings int
	for _, message := range messages {
		if strings.HasPrefix(message.Text, "ZCZC-WXR-RWT-") {
			headers++
		}
		if message.Text == "NNNN" {
			endings++
		}
	}
	if headers != 3 || endings != 3 {
		t.Fatalf("decoded %d headers and %d endings from %d messages", headers, endings, len(messages))
	}
}

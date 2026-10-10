// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(context.Background(), []string{"-version"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "vulpes dev\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunLogsToStdout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout bytes.Buffer
	if err := run(ctx, []string{"-config", writeTestConfig(t, "")}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"vulpes started", "vulpes stopped"} {
		if !strings.Contains(stdout.String(), message) {
			t.Errorf("stdout does not contain %q: %q", message, stdout.String())
		}
	}
}

func TestRunLogsToConfiguredFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "vulpes.log")
	configPath := writeTestConfig(t, "logging:\n  level: info\n  file: "+logPath+"\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout bytes.Buffer
	if err := run(ctx, []string{"-config", configPath}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "vulpes started") || !strings.Contains(string(content), "vulpes stopped") {
		t.Fatalf("log file = %q", content)
	}
}

func writeTestConfig(t *testing.T, logging string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vulpes.yaml")
	content := "version: 1\n" + logging + `audio:
  - name: test
    capture: memory
    playback: memory
channels:
  - name: radio
    audio: test
    modem:
      mode: afsk1200
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

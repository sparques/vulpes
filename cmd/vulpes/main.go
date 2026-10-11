// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/sparques/vulpes/internal/config"
)

const version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("vulpes", flag.ContinueOnError)
	flags.SetOutput(stderr)
	showVersion := flags.Bool("version", false, "print version and exit")
	configFile := flags.String("config", "vulpes.yaml", "read configuration from this YAML file")
	logFile := flags.String("log-file", "", "write logs to this file instead of stdout")
	audioCapture := flags.String("audio-capture", "", "override the first ALSA capture device")
	audioPlayback := flags.String("audio-playback", "", "override the first ALSA playback device")
	mode := flags.String("mode", "", "override the first channel modem mode")
	sampleRate := flags.Int("sample-rate", 0, "override the first audio device sample rate")
	debug := flags.String("debug", "", "enable comma-separated debug categories")
	wavInput := flags.String("wav-input", "", "read the first audio device from a deterministic WAV file")
	wavOutput := flags.String("wav-output", "", "write first-device transmissions to a WAV file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *showVersion {
		fmt.Fprintln(stdout, "vulpes "+version)
		return nil
	}

	file, err := os.Open(*configFile)
	if err != nil {
		return fmt.Errorf("open config file %q: %w", *configFile, err)
	}
	configuration, decodeErr := config.Decode(*configFile, file)
	closeErr := file.Close()
	if decodeErr != nil {
		return decodeErr
	}
	if closeErr != nil {
		return fmt.Errorf("close config file %q: %w", *configFile, closeErr)
	}
	if *audioCapture != "" {
		configuration.Audio[0].Capture = *audioCapture
	}
	if *audioPlayback != "" {
		configuration.Audio[0].Playback = *audioPlayback
	}
	if *sampleRate != 0 {
		configuration.Audio[0].SampleRate = *sampleRate
	}
	if *mode != "" {
		applyModeOverride(&configuration.Channels[0].Modem, *mode)
	}
	if *debug != "" {
		configuration.Logging.Level = "debug"
	}
	if err := configuration.Validate(); err != nil {
		return fmt.Errorf("command-line overrides: %w", err)
	}

	logOutput := stdout
	selectedLogFile := configuration.Logging.File
	if *logFile != "" {
		selectedLogFile = *logFile
	}
	var openedLog *os.File
	if selectedLogFile != "" {
		file, err := os.OpenFile(selectedLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("open log file %q: %w", selectedLogFile, err)
		}
		logOutput = file
		openedLog = file
	}

	logger := slog.New(slog.NewTextHandler(logOutput, &slog.HandlerOptions{Level: configuration.Logging.SlogLevel()}))
	logger.Info("vulpes started", "version", version, "debug", *debug)
	runtimeErr := runRuntime(ctx, configuration, runtimeOverrides{wavInput: *wavInput, wavOutput: *wavOutput}, logger)
	logger.Info("vulpes stopped")
	if openedLog != nil {
		if err := openedLog.Close(); err != nil {
			return fmt.Errorf("close log file %q: %w", selectedLogFile, err)
		}
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		if runtimeErr != nil {
			return runtimeErr
		}
		return ctx.Err()
	}
	return runtimeErr
}

func applyModeOverride(modem *config.Modem, mode string) {
	*modem = config.Modem{Mode: mode, Amplitude: modem.Amplitude}
	switch mode {
	case "afsk300", "afsk1200":
		modem.Profile, modem.Slicers = "A", 9
	case "psk2400":
		modem.Profile, modem.Variant = "R", "V26B"
	case "psk4800":
		modem.Profile = "V"
	}
}

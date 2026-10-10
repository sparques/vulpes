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
	logger.Info("vulpes started", "version", version)
	<-ctx.Done()
	logger.Info("vulpes stopped")
	if openedLog != nil {
		if err := openedLog.Close(); err != nil {
			return fmt.Errorf("close log file %q: %w", selectedLogFile, err)
		}
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	return nil
}

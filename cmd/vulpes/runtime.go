// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/sparques/vulpes/internal/agw"
	"github.com/sparques/vulpes/internal/audio"
	"github.com/sparques/vulpes/internal/config"
	"github.com/sparques/vulpes/internal/kiss"
	"github.com/sparques/vulpes/internal/ptt"
	"github.com/sparques/vulpes/internal/tnc"
)

type runtimeOverrides struct {
	wavInput  string
	wavOutput string
}

type runtimeState struct {
	ctx      context.Context
	cancel   context.CancelFunc
	logger   *slog.Logger
	channels map[string]*tnc.Channel
	order    []string
	keyers   map[string]*ptt.Keyer
	inputs   map[string]audio.Capture
	outputs  map[string]*audio.PlaybackBus
	closers  []io.Closer
	wait     sync.WaitGroup
	errors   chan error
}

func runRuntime(ctx context.Context, configuration config.Config, overrides runtimeOverrides, logger *slog.Logger) error {
	runtimeCtx, cancel := context.WithCancel(ctx)
	state := &runtimeState{ctx: runtimeCtx, cancel: cancel, logger: logger, keyers: make(map[string]*ptt.Keyer), inputs: make(map[string]audio.Capture), outputs: make(map[string]*audio.PlaybackBus), errors: make(chan error, 64)}
	defer state.close()
	var err error
	state.channels, err = tnc.NewConfiguredChannels(configuration)
	if err != nil {
		return err
	}
	for _, channel := range configuration.Channels {
		state.order = append(state.order, channel.Name)
		keyer, err := ptt.OpenConfigured(runtimeCtx, channel.PTT)
		if err != nil {
			return fmt.Errorf("open PTT for channel %q: %w", channel.Name, err)
		}
		state.keyers[channel.Name] = keyer
	}
	if err := state.openAudio(configuration, overrides); err != nil {
		return err
	}
	if err := state.startInterfaces(configuration); err != nil {
		return err
	}
	state.startAudio(configuration)

	select {
	case <-ctx.Done():
		return nil
	case err := <-state.errors:
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
}

func (s *runtimeState) openAudio(configuration config.Config, overrides runtimeOverrides) error {
	for index, device := range configuration.Audio {
		captureSpec := audio.Spec{SampleRate: device.SampleRate, Channels: device.CaptureChannels, Format: audio.Signed16LE}
		playbackSpec := audio.Spec{SampleRate: device.SampleRate, Channels: device.PlaybackChannels, Format: audio.Signed16LE}
		frames := device.SampleRate * device.BufferMillis / 1000
		captureName, playbackName := device.Capture, device.Playback
		if index == 0 && overrides.wavInput != "" {
			captureName = "wav:" + overrides.wavInput
		}
		if index == 0 && overrides.wavOutput != "" {
			playbackName = "wav:" + overrides.wavOutput
		}
		capture, err := openCapture(captureName, captureSpec, frames, &s.closers)
		if err != nil {
			return fmt.Errorf("open capture %q: %w", device.Name, err)
		}
		if capture.Spec() != captureSpec {
			return fmt.Errorf("capture %q negotiated %+v, configured %+v", device.Name, capture.Spec(), captureSpec)
		}
		playback, err := openPlayback(playbackName, playbackSpec, frames, &s.closers)
		if err != nil {
			return fmt.Errorf("open playback %q: %w", device.Name, err)
		}
		if playback.Spec() != playbackSpec {
			return fmt.Errorf("playback %q negotiated %+v, configured %+v", device.Name, playback.Spec(), playbackSpec)
		}
		bus, err := audio.NewPlaybackBus(playback)
		if err != nil {
			return err
		}
		s.inputs[device.Name], s.outputs[device.Name] = capture, bus
	}
	return nil
}

func openCapture(name string, spec audio.Spec, frames int, closers *[]io.Closer) (audio.Capture, error) {
	if name == "memory" {
		return audio.NewSliceCapture(spec, nil)
	}
	if len(name) > 4 && name[:4] == "wav:" {
		file, err := os.Open(name[4:])
		if err != nil {
			return nil, err
		}
		*closers = append(*closers, file)
		reader, err := audio.NewWAVReader(file)
		if err != nil {
			return nil, err
		}
		*closers = append(*closers, reader)
		return reader, nil
	}
	return audio.OpenALSACapture(audio.DeviceConfig{Device: name, PCM: spec, BufferFrames: frames})
}

func openPlayback(name string, spec audio.Spec, frames int, closers *[]io.Closer) (audio.Playback, error) {
	if name == "memory" {
		return audio.NewSlicePlayback(spec)
	}
	if len(name) > 4 && name[:4] == "wav:" {
		file, err := os.Create(name[4:])
		if err != nil {
			return nil, err
		}
		*closers = append(*closers, file)
		writer, err := audio.NewWAVWriter(file, spec)
		if err != nil {
			return nil, err
		}
		*closers = append(*closers, writer)
		return writer, nil
	}
	return audio.OpenALSAPlayback(audio.DeviceConfig{Device: name, PCM: spec, BufferFrames: frames})
}

func (s *runtimeState) startAudio(configuration config.Config) {
	for _, device := range configuration.Audio {
		var routes []tnc.AudioRoute
		for _, configured := range configuration.Channels {
			if configured.Audio == device.Name {
				routes = append(routes, tnc.AudioRoute{Channel: s.channels[configured.Name], AudioChannel: configured.AudioChannel})
			}
		}
		capture := s.inputs[device.Name]
		frames := device.SampleRate * device.BufferMillis / 1000
		s.goRun(func() error { return tnc.RunCapture(s.ctx, capture, routes, frames, 4) })
	}
	for _, configured := range configuration.Channels {
		channel, playback, keyer := s.channels[configured.Name], s.outputs[configured.Audio], s.keyers[configured.Name]
		s.goRun(func() error {
			for {
				if err := channel.TransmitNext(s.ctx, playback, keyer); err != nil {
					return err
				}
			}
		})
	}
}

func (s *runtimeState) startInterfaces(configuration config.Config) error {
	for _, configured := range configuration.Interfaces.KISSTCP {
		listener, err := net.Listen("tcp", configured.Listen)
		if err != nil {
			return fmt.Errorf("listen for KISS on %q: %w", configured.Listen, err)
		}
		server, err := kiss.NewConfiguredTCPServer(listener, configured, s.order, s.channels)
		if err != nil {
			_ = listener.Close()
			return err
		}
		s.closers = append(s.closers, server)
		s.goRun(func() error { return server.Serve(s.ctx) })
		s.forward(server)
	}
	for _, configured := range configuration.Interfaces.KISSSerial {
		server, err := kiss.NewReconnectingSerialServer(configured.Device, configured.Baud, s.order, s.channels, configured.Channel)
		if err != nil {
			return err
		}
		s.closers = append(s.closers, server)
		s.goRun(func() error { return server.Run(s.ctx) })
		s.forward(server)
	}
	ports, err := kiss.ChannelPortMap(s.order, s.channels)
	if err != nil {
		return err
	}
	for _, configured := range configuration.Interfaces.AGWTCP {
		listener, err := net.Listen("tcp", configured.Listen)
		if err != nil {
			return fmt.Errorf("listen for AGW on %q: %w", configured.Listen, err)
		}
		server, err := agw.NewServer(listener, ports, 16)
		if err != nil {
			_ = listener.Close()
			return err
		}
		s.closers = append(s.closers, server)
		s.goRun(func() error { return server.Serve(s.ctx) })
		s.forward(server)
	}
	return nil
}

func (s *runtimeState) forward(destination kiss.EventBroadcaster) {
	for name, channel := range s.channels {
		name, channel := name, channel
		s.goRun(func() error { return kiss.ForwardEvents(s.ctx, name, channel, destination) })
	}
}

func (s *runtimeState) goRun(run func() error) {
	s.wait.Add(1)
	go func() {
		defer s.wait.Done()
		err := run()
		select {
		case s.errors <- err:
		default:
		}
	}()
}

func (s *runtimeState) close() {
	s.cancel()
	for _, channel := range s.channels {
		_ = channel.Close()
	}
	for i := len(s.closers) - 1; i >= 0; i-- {
		if err := s.closers[i].Close(); err != nil {
			s.logger.Error("close runtime resource", "error", err)
		}
	}
	for name, keyer := range s.keyers {
		if err := keyer.Close(context.Background()); err != nil {
			s.logger.Error("close PTT", "channel", name, "error", err)
		}
	}
	s.wait.Wait()
}

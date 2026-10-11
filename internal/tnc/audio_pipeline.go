// SPDX-License-Identifier: GPL-2.0-or-later

package tnc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/sparques/vulpes/internal/audio"
)

// AudioRoute sends one interleaved capture channel to one modem channel.
type AudioRoute struct {
	Channel      *Channel
	AudioChannel int
}

// RunCapture streams a capture endpoint through bounded per-modem queues.
// Backpressure is intentional: audio is never silently discarded when a
// decoder is slower than capture.
func RunCapture(ctx context.Context, capture audio.Capture, routes []AudioRoute, framesPerChunk, queueDepth int) error {
	if capture == nil {
		return errors.New("audio capture is nil")
	}
	spec := capture.Spec()
	if err := spec.Validate(); err != nil {
		return err
	}
	if framesPerChunk <= 0 || queueDepth <= 0 {
		return errors.New("capture chunk and queue sizes must be positive")
	}
	type worker struct {
		route AudioRoute
		input chan []int16
	}
	workers := make([]worker, len(routes))
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wait sync.WaitGroup
	for i, route := range routes {
		if route.Channel == nil || route.AudioChannel < 0 || route.AudioChannel >= spec.Channels {
			return fmt.Errorf("invalid audio route %d", i)
		}
		workers[i] = worker{route: route, input: make(chan []int16, queueDepth)}
		wait.Add(1)
		go func(w worker) {
			defer wait.Done()
			for samples := range w.input {
				if workerCtx.Err() != nil {
					return
				}
				w.route.Channel.ProcessAudio(samples)
			}
		}(workers[i])
	}
	defer func() {
		for _, worker := range workers {
			close(worker.input)
		}
		wait.Wait()
	}()
	buffer := make([]int16, framesPerChunk*spec.Channels)
	for {
		count, err := capture.ReadSamples(workerCtx, buffer)
		if count < 0 || count > len(buffer) || count%spec.Channels != 0 {
			return fmt.Errorf("audio capture returned invalid sample count %d", count)
		}
		if count > 0 {
			for _, worker := range workers {
				samples := make([]int16, count/spec.Channels)
				for frame := range samples {
					samples[frame] = buffer[frame*spec.Channels+worker.route.AudioChannel]
				}
				select {
				case worker.input <- samples:
				case <-workerCtx.Done():
					return workerCtx.Err()
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if count == 0 {
			return io.ErrNoProgress
		}
	}
}

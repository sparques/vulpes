// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux && cgo

package audio

/*
#cgo pkg-config: alsa
#include <alsa/asoundlib.h>
#include <stdlib.h>

static int vulpes_pcm_open(snd_pcm_t **pcm, const char *name, int capture) {
	return snd_pcm_open(pcm, name, capture ? SND_PCM_STREAM_CAPTURE : SND_PCM_STREAM_PLAYBACK, SND_PCM_NONBLOCK);
}

static int vulpes_pcm_configure(snd_pcm_t *pcm, unsigned int channels, unsigned int rate, unsigned int latency) {
	return snd_pcm_set_params(pcm, SND_PCM_FORMAT_S16_LE, SND_PCM_ACCESS_RW_INTERLEAVED,
		channels, rate, 1, latency);
}

static void **vulpes_pcm_hints(int *status) {
	void **hints = NULL;
	*status = snd_device_name_hint(-1, "pcm", &hints);
	return hints;
}

static int vulpes_free_hints(void *hints) {
	return snd_device_name_free_hint((void **)hints);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ALSADevice describes one PCM endpoint reported by ALSA's device hints.
type ALSADevice struct {
	Name        string
	Description string
	Input       bool
	Output      bool
}

// ListALSADevices enumerates ALSA PCM names without opening hardware.
func ListALSADevices() ([]ALSADevice, error) {
	var result C.int
	hints := C.vulpes_pcm_hints(&result)
	if result < 0 {
		return nil, alsaError("enumerate PCM devices", result)
	}
	defer C.vulpes_free_hints(unsafe.Pointer(hints))
	var devices []ALSADevice
	for _, hint := range unsafe.Slice((*unsafe.Pointer)(hints), 1<<20) {
		if hint == nil {
			break
		}
		name := hintString(hint, "NAME")
		if name == "" {
			continue
		}
		direction := hintString(hint, "IOID")
		devices = append(devices, ALSADevice{Name: name, Description: hintString(hint, "DESC"), Input: direction != "Output", Output: direction != "Input"})
	}
	return devices, nil
}

func hintString(hint unsafe.Pointer, key string) string {
	cKey := C.CString(key)
	defer C.free(unsafe.Pointer(cKey))
	value := C.snd_device_name_get_hint(hint, cKey)
	if value == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(value))
	return C.GoString(value)
}

type alsaPCM struct {
	pcm     *C.snd_pcm_t
	spec    Spec
	capture bool
	mu      sync.Mutex
	closed  bool
}

// OpenALSACapture opens a nonblocking interleaved S16_LE capture stream.
func OpenALSACapture(config DeviceConfig) (Capture, error) { return openALSA(config, true) }

// OpenALSAPlayback opens a nonblocking interleaved S16_LE playback stream.
func OpenALSAPlayback(config DeviceConfig) (Playback, error) { return openALSA(config, false) }

func openALSA(config DeviceConfig, capture bool) (*alsaPCM, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	name := C.CString(config.Device)
	defer C.free(unsafe.Pointer(name))
	var pcm *C.snd_pcm_t
	if result := C.vulpes_pcm_open(&pcm, name, boolInt(capture)); result < 0 {
		return nil, alsaError("open PCM "+config.Device, result)
	}
	latency := C.uint(1000000 * config.BufferFrames / config.PCM.SampleRate)
	if result := C.vulpes_pcm_configure(pcm, C.uint(config.PCM.Channels), C.uint(config.PCM.SampleRate), latency); result < 0 {
		C.snd_pcm_close(pcm)
		return nil, alsaError("configure PCM "+config.Device, result)
	}
	return &alsaPCM{pcm: pcm, spec: config.PCM, capture: capture}, nil
}

func boolInt(value bool) C.int {
	if value {
		return 1
	}
	return 0
}

func (p *alsaPCM) Spec() Spec { return p.spec }

func (p *alsaPCM) ReadSamples(ctx context.Context, samples []int16) (int, error) {
	if !p.capture {
		return 0, errors.New("ALSA playback stream cannot capture")
	}
	return p.transfer(ctx, samples, true)
}

func (p *alsaPCM) WriteSamples(ctx context.Context, samples []int16) (int, error) {
	if p.capture {
		return 0, errors.New("ALSA capture stream cannot play")
	}
	return p.transfer(ctx, samples, false)
}

func (p *alsaPCM) transfer(ctx context.Context, samples []int16, read bool) (int, error) {
	if len(samples)%p.spec.Channels != 0 {
		return 0, ErrMisalignedBuffer
	}
	if len(samples) == 0 {
		return 0, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return 0, io.ErrClosedPipe
		}
		frames := C.snd_pcm_uframes_t(len(samples) / p.spec.Channels)
		var result C.snd_pcm_sframes_t
		recovered := false
		if read {
			result = C.snd_pcm_readi(p.pcm, unsafe.Pointer(&samples[0]), frames)
		} else {
			result = C.snd_pcm_writei(p.pcm, unsafe.Pointer(&samples[0]), frames)
		}
		if result < 0 && result != -C.EAGAIN {
			result = C.snd_pcm_sframes_t(C.snd_pcm_recover(p.pcm, C.int(result), 1))
			recovered = result == 0
		}
		p.mu.Unlock()
		if recovered {
			continue
		}
		if result >= 0 {
			return int(result) * p.spec.Channels, nil
		}
		if result != -C.EAGAIN {
			return 0, alsaError("transfer PCM samples", C.int(result))
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, ctx.Err()
		case <-timer.C:
		}
	}
}

func (p *alsaPCM) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	result := C.snd_pcm_close(p.pcm)
	p.pcm = nil
	if result < 0 {
		return alsaError("close PCM", result)
	}
	return nil
}

func alsaError(operation string, result C.int) error {
	if result == -C.EACCES {
		return fmt.Errorf("%s: %s (check audio device permissions): %w", operation, C.GoString(C.snd_strerror(result)), syscall.EACCES)
	}
	return fmt.Errorf("%s: %s", operation, C.GoString(C.snd_strerror(result)))
}

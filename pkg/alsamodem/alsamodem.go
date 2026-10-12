// SPDX-License-Identifier: GPL-2.0-or-later

// Package alsamodem embeds Vulpes' ALSA-backed modem runtime in a Go program.
// It owns configured audio devices, modem channels, PTT controls, and their
// background capture/transmit workers. No CLI, KISS, AGW, or YAML is required.
package alsamodem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	audiopcm "github.com/sparques/vulpes/internal/audio"
	coremodem "github.com/sparques/vulpes/internal/modem"
	"github.com/sparques/vulpes/internal/ptt"
	"github.com/sparques/vulpes/internal/tnc"
	publicmodem "github.com/sparques/vulpes/pkg/modem"
)

var (
	ErrClosed         = tnc.ErrClosed
	ErrQueueFull      = tnc.ErrQueueFull
	ErrFrameTooLarge  = tnc.ErrFrameTooLarge
	ErrReceiveOnly    = coremodem.ErrReceiveOnly
	ErrUnknownChannel = errors.New("unknown modem channel")
)

const (
	PTTNone   = "none"
	PTTVOX    = "vox"
	PTTSerial = "serial"
	PTTGPIO   = "gpio"
	PTTCM108  = "cm108"
)

// Config describes ALSA endpoints and the modem channels routed through them.
// Every channel's Modem.SampleRate may be zero to inherit its audio device's
// rate. A single Modem owns all endpoints and channels listed here.
type Config struct {
	Audio             []AudioConfig
	Channels          []ChannelConfig
	CaptureQueueDepth int
	EventQueueDepth   int
}

// AudioConfig selects ALSA capture/playback PCMs and their S16_LE layouts.
type AudioConfig struct {
	Name             string
	Capture          string
	Playback         string
	SampleRate       int
	Channels         int
	CaptureChannels  int
	PlaybackChannels int
	BufferMillis     int
}

// ChannelConfig binds one modem to an audio endpoint and output/capture lane.
type ChannelConfig struct {
	Name              string
	Audio             string
	AudioChannel      int
	Modem             publicmodem.Settings
	PTT               PTTConfig
	TX                TXConfig
	MaximumFrameBytes int
	TransmitQueueSize int
}

// TXConfig controls transmit flags, channel access, and VOX lead-in audio.
type TXConfig struct {
	DelayMillis   int
	TailMillis    int
	SlotMillis    int
	Persistence   int
	FullDuplex    bool
	VOXToneMillis int
}

// PTTConfig selects no hardware, VOX, serial lines, Linux GPIO, or CM108 HID.
type PTTConfig struct {
	Type   string
	Serial SerialPTTConfig
	GPIO   GPIOPTTConfig
	CM108  CM108PTTConfig
}

type SerialPTTConfig struct {
	Device    string
	RTS       bool
	DTR       bool
	RTSActive string
	DTRActive string
}

type GPIOPTTConfig struct {
	Chip   string
	Line   int
	Active string
}

type CM108PTTConfig struct {
	Device string
	GPIO   int
	Active string
}

// Timing contains mutable per-channel transmit timing.
type Timing struct {
	DelayMillis int
	TailMillis  int
	SlotMillis  int
	Persistence int
	FullDuplex  bool
}

// Event is a decoded modem result. Packet and AIS payloads are available in
// Payload; AIS also includes NMEA and EAS SAME includes Text.
type Event = publicmodem.Event

// Metrics is a public snapshot of one channel's receive/transmit counters.
type Metrics struct {
	AudioChunks     uint64
	AudioSamples    uint64
	ReceiveEvents   uint64
	QueueDepth      int
	QueueDrops      uint64
	DroppedEvents   uint64
	CarrierDetected bool
	HDLCFrames      uint64
	BadFCS          uint64
	InvalidHDLC     uint64
	OversizeFrames  uint64
	HDLCAborts      uint64
}

type audioEndpoint struct {
	capture  audiopcm.Capture
	playback audiopcm.Playback
	bus      *audiopcm.PlaybackBus
	frames   int
}

// Modem manages ALSA endpoints and all configured modem channels.
type Modem struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
	errMu     sync.RWMutex
	err       error
	channels  map[string]*Channel
	ordered   []*Channel
	audio     map[string]*audioEndpoint
	keyers    map[string]*ptt.Keyer
	closers   []io.Closer
	queueSize int
}

// Channel provides direct frame, event, timing, and metrics access for one
// configured modem channel. Use Modem.Close to stop and release it.
type Channel struct {
	name   string
	inner  *tnc.Channel
	events chan Event
}

// Open validates the configuration, opens ALSA and PTT resources, then starts
// the receive/transmit workers. The returned Modem is ready for Send calls.
// Canceling ctx stops it just like Close.
func Open(ctx context.Context, config Config) (*Modem, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	runtimeCtx, cancel := context.WithCancel(ctx)
	m := &Modem{
		ctx: runtimeCtx, cancel: cancel, done: make(chan struct{}),
		channels:  make(map[string]*Channel, len(normalized.Channels)),
		audio:     make(map[string]*audioEndpoint, len(normalized.Audio)),
		keyers:    make(map[string]*ptt.Keyer, len(normalized.Channels)),
		queueSize: normalized.CaptureQueueDepth,
	}
	opened := false
	defer func() {
		if !opened {
			cancel()
			m.cleanup()
			m.wg.Wait()
		}
	}()

	for _, configured := range normalized.Channels {
		channel, err := tnc.NewChannel(tnc.ChannelConfig{
			Name: configured.Name, Modem: coreSettings(configured.Modem),
			MaximumFrameBytes: configured.MaximumFrameBytes, QueueDepth: configured.TransmitQueueSize,
			TXDelayMillis: configured.TX.DelayMillis, TXTailMillis: configured.TX.TailMillis,
			SlotMillis: configured.TX.SlotMillis, Persistence: configured.TX.Persistence,
			FullDuplex: configured.TX.FullDuplex, AudioChannel: configured.AudioChannel,
			VOXToneMillis: configured.TX.VOXToneMillis,
		})
		if err != nil {
			return nil, fmt.Errorf("configure channel %q: %w", configured.Name, err)
		}
		m.closers = append(m.closers, channel)
		publicChannel := &Channel{name: configured.Name, inner: channel}
		m.channels[configured.Name] = publicChannel
		m.ordered = append(m.ordered, publicChannel)
	}

	for _, device := range normalized.Audio {
		frames := device.SampleRate * device.BufferMillis / 1000
		captureSpec := audiopcm.Spec{SampleRate: device.SampleRate, Channels: device.CaptureChannels, Format: audiopcm.Signed16LE}
		playbackSpec := audiopcm.Spec{SampleRate: device.SampleRate, Channels: device.PlaybackChannels, Format: audiopcm.Signed16LE}
		capture, err := audiopcm.OpenALSACapture(audiopcm.DeviceConfig{Device: device.Capture, PCM: captureSpec, BufferFrames: frames})
		if err != nil {
			return nil, fmt.Errorf("open capture %q: %w", device.Name, err)
		}
		m.closers = append(m.closers, capture)
		playback, err := audiopcm.OpenALSAPlayback(audiopcm.DeviceConfig{Device: device.Playback, PCM: playbackSpec, BufferFrames: frames})
		if err != nil {
			return nil, fmt.Errorf("open playback %q: %w", device.Name, err)
		}
		m.closers = append(m.closers, playback)
		bus, err := audiopcm.NewPlaybackBus(playback)
		if err != nil {
			return nil, err
		}
		if capture.Spec() != captureSpec || playback.Spec() != playbackSpec {
			return nil, fmt.Errorf("ALSA device %q negotiated unexpected PCM settings", device.Name)
		}
		m.audio[device.Name] = &audioEndpoint{capture: capture, playback: playback, bus: bus, frames: frames}
	}

	type eventSource struct {
		channel     *Channel
		input       <-chan coremodem.Event
		unsubscribe func()
	}
	eventSources := make([]eventSource, 0, len(normalized.Channels))
	for _, configured := range normalized.Channels {
		channel := m.channels[configured.Name].inner
		keyer, err := openKeyer(runtimeCtx, configured.PTT)
		if err != nil {
			return nil, fmt.Errorf("open PTT for channel %q: %w", configured.Name, err)
		}
		m.keyers[configured.Name] = keyer
		input, unsubscribe, err := channel.SubscribeEvents(normalized.EventQueueDepth)
		if err != nil {
			return nil, fmt.Errorf("subscribe to channel %q: %w", configured.Name, err)
		}
		publicChannel := m.channels[configured.Name]
		publicChannel.events = make(chan Event, normalized.EventQueueDepth)
		eventSources = append(eventSources, eventSource{channel: publicChannel, input: input, unsubscribe: unsubscribe})
	}
	for _, source := range eventSources {
		source := source
		m.launch("events for channel "+source.channel.name, func() error {
			defer source.unsubscribe()
			defer close(source.channel.events)
			for event := range source.input {
				select {
				case source.channel.events <- publicEvent(event):
				case <-runtimeCtx.Done():
					return nil
				}
			}
			return nil
		})
	}

	for _, configured := range normalized.Channels {
		if isReceiveOnly(configured.Modem.Mode) {
			continue
		}
		channel := m.channels[configured.Name].inner
		endpoint := m.audio[configured.Audio]
		keyer := m.keyers[configured.Name]
		m.launch("transmit on channel "+configured.Name, func() error {
			for {
				if err := channel.TransmitNext(runtimeCtx, endpoint.bus, keyer); err != nil {
					return err
				}
			}
		})
	}

	for _, device := range normalized.Audio {
		var routes []tnc.AudioRoute
		for _, configured := range normalized.Channels {
			if configured.Audio == device.Name {
				routes = append(routes, tnc.AudioRoute{Channel: m.channels[configured.Name].inner, AudioChannel: configured.AudioChannel})
			}
		}
		endpoint := m.audio[device.Name]
		m.launch("capture on "+device.Name, func() error {
			return tnc.RunCapture(runtimeCtx, endpoint.capture, routes, endpoint.frames, m.queueSize)
		})
	}
	opened = true
	go m.supervise()
	return m, nil
}

// Channel returns a named channel and whether it exists.
func (m *Modem) Channel(name string) (*Channel, bool) {
	channel, exists := m.channels[name]
	return channel, exists
}

// Channels returns channels in configuration order.
func (m *Modem) Channels() []*Channel { return append([]*Channel(nil), m.ordered...) }

// Done closes after cancellation, audio/PTT shutdown, and worker termination.
func (m *Modem) Done() <-chan struct{} { return m.done }

// Err reports the first unexpected background capture/transmit error.
func (m *Modem) Err() error {
	m.errMu.RLock()
	defer m.errMu.RUnlock()
	return m.err
}

// Close cancels background work, releases ALSA/PTT resources, and waits for
// every worker to exit. It is safe to call more than once.
func (m *Modem) Close() error {
	m.closeOnce.Do(m.cancel)
	<-m.done
	return m.Err()
}

// Name returns the configured channel name.
func (c *Channel) Name() string { return c.name }

// Send queues one raw modem frame payload for transmission. QueueFull is
// returned when the bounded transmit queue is full; it does not wait for RF TX.
func (c *Channel) Send(payload []byte) error { return c.inner.QueueFrame(payload) }

// Events returns a bounded stream of decoded modem events. Drain it regularly;
// if consumers fall behind, events may be dropped and counted in Metrics.
func (c *Channel) Events() <-chan Event { return c.events }

// Timing returns the current KISS-compatible timing settings.
func (c *Channel) Timing() Timing {
	timing := c.inner.Timing()
	return Timing{DelayMillis: timing.TXDelayMillis, TailMillis: timing.TXTailMillis,
		SlotMillis: timing.SlotMillis, Persistence: timing.Persistence, FullDuplex: timing.FullDuplex}
}

// SetTiming atomically updates live transmit timing for subsequent frames.
func (c *Channel) SetTiming(timing Timing) error {
	return c.inner.SetTiming(tnc.Timing{TXDelayMillis: timing.DelayMillis, TXTailMillis: timing.TailMillis,
		SlotMillis: timing.SlotMillis, Persistence: timing.Persistence, FullDuplex: timing.FullDuplex})
}

// Metrics returns a point-in-time channel counter snapshot.
func (c *Channel) Metrics() Metrics {
	metrics := c.inner.Metrics()
	return Metrics{
		AudioChunks: metrics.AudioChunks, AudioSamples: metrics.AudioSamples,
		ReceiveEvents: metrics.ReceiveEvents, QueueDepth: metrics.QueueDepth,
		QueueDrops: metrics.QueueDrops, DroppedEvents: metrics.DroppedEvents,
		CarrierDetected: metrics.CarrierDetected, HDLCFrames: metrics.HDLC.Frames,
		BadFCS: metrics.HDLC.BadFCS, InvalidHDLC: metrics.HDLC.Invalid,
		OversizeFrames: metrics.HDLC.Oversize, HDLCAborts: metrics.HDLC.Aborts,
	}
}

func (m *Modem) launch(name string, run func() error) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := run(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, tnc.ErrClosed) {
			m.errMu.Lock()
			if m.err == nil {
				m.err = fmt.Errorf("%s: %w", name, err)
			}
			m.errMu.Unlock()
			m.cancel()
		}
	}()
}

func (m *Modem) supervise() {
	<-m.ctx.Done()
	for _, channel := range m.ordered {
		_ = channel.inner.Close()
	}
	for index := len(m.closers) - 1; index >= 0; index-- {
		_ = m.closers[index].Close()
	}
	for _, keyer := range m.keyers {
		if err := keyer.Close(context.Background()); err != nil {
			m.errMu.Lock()
			if m.err == nil {
				m.err = fmt.Errorf("close PTT: %w", err)
			}
			m.errMu.Unlock()
		}
	}
	m.wg.Wait()
	close(m.done)
}

func (m *Modem) cleanup() {
	for _, channel := range m.ordered {
		_ = channel.inner.Close()
	}
	for index := len(m.closers) - 1; index >= 0; index-- {
		_ = m.closers[index].Close()
	}
	for _, keyer := range m.keyers {
		_ = keyer.Close(context.Background())
	}
}

func normalizeConfig(config Config) (Config, error) {
	config.Audio = append([]AudioConfig(nil), config.Audio...)
	config.Channels = append([]ChannelConfig(nil), config.Channels...)
	if len(config.Audio) == 0 || len(config.Channels) == 0 {
		return Config{}, errors.New("at least one ALSA device and one modem channel are required")
	}
	if config.CaptureQueueDepth <= 0 {
		config.CaptureQueueDepth = 4
	}
	if config.EventQueueDepth <= 0 {
		config.EventQueueDepth = 32
	}
	audioByName := make(map[string]AudioConfig, len(config.Audio))
	for index, device := range config.Audio {
		if device.Name == "" || device.Capture == "" || device.Playback == "" {
			return Config{}, fmt.Errorf("audio[%d]: name, capture, and playback are required", index)
		}
		if _, exists := audioByName[device.Name]; exists {
			return Config{}, fmt.Errorf("audio[%d]: duplicate name %q", index, device.Name)
		}
		if device.SampleRate == 0 {
			device.SampleRate = 48000
		}
		if device.Channels == 0 {
			device.Channels = 1
		}
		if device.CaptureChannels == 0 {
			device.CaptureChannels = device.Channels
		}
		if device.PlaybackChannels == 0 {
			device.PlaybackChannels = device.Channels
		}
		if device.BufferMillis == 0 {
			device.BufferMillis = 100
		}
		if device.SampleRate < 8000 || device.SampleRate > 192000 || device.Channels < 1 || device.Channels > 2 ||
			device.CaptureChannels < 1 || device.CaptureChannels > 2 || device.PlaybackChannels < 1 || device.PlaybackChannels > 2 ||
			device.BufferMillis < 1 || device.BufferMillis > 5000 {
			return Config{}, fmt.Errorf("audio[%d]: invalid PCM or buffer settings", index)
		}
		config.Audio[index] = device
		audioByName[device.Name] = device
	}
	seenChannels := make(map[string]struct{}, len(config.Channels))
	for index := range config.Channels {
		channel := &config.Channels[index]
		device, exists := audioByName[channel.Audio]
		if channel.Name == "" || !exists {
			return Config{}, fmt.Errorf("channels[%d]: name and a valid audio reference are required", index)
		}
		if _, duplicate := seenChannels[channel.Name]; duplicate {
			return Config{}, fmt.Errorf("channels[%d]: duplicate name %q", index, channel.Name)
		}
		seenChannels[channel.Name] = struct{}{}
		if channel.AudioChannel < 0 || channel.AudioChannel >= device.CaptureChannels || channel.AudioChannel >= device.PlaybackChannels {
			return Config{}, fmt.Errorf("channels[%d]: audio channel is unavailable for capture or playback", index)
		}
		if channel.Modem.SampleRate != 0 && channel.Modem.SampleRate != device.SampleRate {
			return Config{}, fmt.Errorf("channels[%d]: modem sample rate must match audio device %q", index, device.Name)
		}
		channel.Modem.SampleRate = device.SampleRate
		channel.Modem = modemDefaults(channel.Modem)
		if channel.TX.DelayMillis == 0 {
			channel.TX.DelayMillis = 300
		}
		if channel.TX.TailMillis == 0 {
			channel.TX.TailMillis = 100
		}
		if channel.TX.SlotMillis == 0 {
			channel.TX.SlotMillis = 100
		}
		if channel.TX.Persistence == 0 {
			channel.TX.Persistence = 63
		}
		if channel.PTT.Type == "" {
			channel.PTT.Type = "none"
		}
		if channel.PTT.Type == "vox" && channel.TX.VOXToneMillis == 0 {
			channel.TX.VOXToneMillis = 2000
		}
		if channel.TX.DelayMillis < 0 || channel.TX.TailMillis < 0 || channel.TX.SlotMillis < 1 ||
			channel.TX.Persistence < 0 || channel.TX.Persistence > 255 || channel.TX.VOXToneMillis < 0 || channel.TX.VOXToneMillis > 30000 {
			return Config{}, fmt.Errorf("channels[%d]: invalid TX timing", index)
		}
		if channel.MaximumFrameBytes <= 0 {
			channel.MaximumFrameBytes = 2048
		}
		if channel.TransmitQueueSize <= 0 {
			channel.TransmitQueueSize = 16
		}
		if err := validatePTT(channel.PTT); err != nil {
			return Config{}, fmt.Errorf("channels[%d]: %w", index, err)
		}
	}
	return config, nil
}

func modemDefaults(settings publicmodem.Settings) publicmodem.Settings {
	if settings.Amplitude == 0 {
		settings.Amplitude = 0.5
	}
	switch settings.Mode {
	case publicmodem.ModeAFSK300, publicmodem.ModeAFSK1200:
		if settings.Profile == "" {
			settings.Profile = "A"
		}
		if settings.Slicers == 0 {
			settings.Slicers = 9
		}
	case publicmodem.ModePSK2400:
		if settings.Profile == "" {
			settings.Profile = "R"
		}
		if settings.Variant == "" {
			settings.Variant = "V26B"
		}
	case publicmodem.ModePSK4800:
		if settings.Profile == "" {
			settings.Profile = "V"
		}
	}
	return settings
}

func coreSettings(settings publicmodem.Settings) coremodem.Settings {
	return coremodem.Settings{
		Mode: settings.Mode, SampleRate: settings.SampleRate,
		MarkHz: settings.MarkHz, SpaceHz: settings.SpaceHz, Amplitude: settings.Amplitude,
		Profile: settings.Profile, Variant: settings.Variant, Slicers: settings.Slicers,
		Upsample: settings.Upsample,
	}
}

func validatePTT(config PTTConfig) error {
	active := func(value string) bool { return value == "high" || value == "low" }
	hasSerial := config.Serial.Device != "" || config.Serial.RTS || config.Serial.DTR || config.Serial.RTSActive != "" || config.Serial.DTRActive != ""
	hasGPIO := config.GPIO.Chip != "" || config.GPIO.Line != 0 || config.GPIO.Active != ""
	hasCM108 := config.CM108.Device != "" || config.CM108.GPIO != 0 || config.CM108.Active != ""
	switch config.Type {
	case PTTNone, PTTVOX:
		if hasSerial || hasGPIO || hasCM108 {
			return errors.New("none/vox PTT cannot include hardware settings")
		}
	case PTTSerial:
		if hasGPIO || hasCM108 {
			return errors.New("serial PTT cannot include GPIO or CM108 settings")
		}
		if config.Serial.Device == "" || (!config.Serial.RTS && !config.Serial.DTR) {
			return errors.New("serial PTT needs a device and RTS and/or DTR")
		}
		if config.Serial.RTSActive == "" {
			config.Serial.RTSActive = "high"
		}
		if config.Serial.DTRActive == "" {
			config.Serial.DTRActive = "high"
		}
		if !active(config.Serial.RTSActive) || !active(config.Serial.DTRActive) {
			return errors.New("serial PTT polarity must be high or low")
		}
	case PTTGPIO:
		if hasSerial || hasCM108 {
			return errors.New("GPIO PTT cannot include serial or CM108 settings")
		}
		if config.GPIO.Chip == "" || config.GPIO.Line < 0 || (config.GPIO.Active != "" && !active(config.GPIO.Active)) {
			return errors.New("GPIO PTT needs a chip, non-negative line, and high/low polarity")
		}
	case PTTCM108:
		if hasSerial || hasGPIO {
			return errors.New("CM108 PTT cannot include serial or GPIO settings")
		}
		if config.CM108.Device == "" {
			return errors.New("CM108 PTT needs a device")
		}
		if config.CM108.GPIO != 0 && (config.CM108.GPIO < 1 || config.CM108.GPIO > 8) ||
			(config.CM108.Active != "" && !active(config.CM108.Active)) {
			return errors.New("CM108 PTT needs GPIO 1..8 and high/low polarity")
		}
	default:
		return fmt.Errorf("unsupported PTT type %q", config.Type)
	}
	return nil
}

func openKeyer(ctx context.Context, config PTTConfig) (*ptt.Keyer, error) {
	var driver ptt.Driver
	var err error
	switch config.Type {
	case "none", "vox":
		driver = ptt.Noop{}
	case "serial":
		rtsActiveHigh := config.Serial.RTSActive != "low"
		dtrActiveHigh := config.Serial.DTRActive != "low"
		driver, err = ptt.OpenSerial(ptt.SerialConfig{Device: config.Serial.Device, RTS: config.Serial.RTS, DTR: config.Serial.DTR,
			RTSActiveHigh: rtsActiveHigh, DTRActiveHigh: dtrActiveHigh})
	case "gpio":
		driver, err = ptt.OpenGPIO(ptt.GPIOConfig{Chip: config.GPIO.Chip, Line: config.GPIO.Line,
			ActiveLow: config.GPIO.Active == "low", Consumer: "vulpes-embedded-ptt"})
	case "cm108":
		gpio := config.CM108.GPIO
		if gpio == 0 {
			gpio = 3
		}
		driver, err = ptt.OpenCM108(ptt.CM108Config{Device: config.CM108.Device, GPIO: gpio, ActiveLow: config.CM108.Active == "low"})
	default:
		return nil, fmt.Errorf("unsupported PTT type %q", config.Type)
	}
	if err != nil {
		return nil, err
	}
	return ptt.New(ctx, driver)
}

func publicEvent(event coremodem.Event) Event {
	kind := publicmodem.EventFrame
	switch event.Kind {
	case coremodem.EventAIS:
		kind = publicmodem.EventAIS
	case coremodem.EventEAS:
		kind = publicmodem.EventEAS
	}
	return Event{Kind: kind, Payload: event.Payload, NMEA: event.NMEA, Text: event.Text,
		Quality: event.Quality, CarrierDetected: event.CarrierDetected}
}

func isReceiveOnly(mode string) bool {
	return mode == publicmodem.ModeAIS || mode == publicmodem.ModeEAS
}

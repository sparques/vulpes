# Embedding Vulpes with `alsamodem`

`github.com/sparques/vulpes/pkg/alsamodem` runs Vulpes inside a Go process. It
owns ALSA capture/playback devices, modem channel workers, and configured PTT
controls. Unlike `pkg/modem`, it performs hardware I/O; unlike the `vulpes`
command, it does not read YAML or start KISS/AGW listeners.

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

runtime, err := alsamodem.Open(ctx, alsamodem.Config{
    Audio: []alsamodem.AudioConfig{{
        Name: "radio", Capture: "plughw:1,0", Playback: "plughw:1,0",
        SampleRate: 48000, Channels: 1,
    }},
    Channels: []alsamodem.ChannelConfig{{
        Name: "vhf", Audio: "radio", AudioChannel: 0,
        Modem: publicmodem.Settings{Mode: publicmodem.ModeAFSK1200},
        PTT: alsamodem.PTTConfig{Type: alsamodem.PTTNone},
    }},
})
if err != nil {
    return err
}
defer runtime.Close()

channel, ok := runtime.Channel("vhf")
if !ok {
    return errors.New("configured channel is missing")
}
if err := channel.Send(frameBytes); err != nil {
    return err // e.g. alsamodem.ErrQueueFull
}
for {
    select {
    case event, ok := <-channel.Events():
        if !ok {
            return runtime.Err()
        }
        handleReceived(event.Payload)
    case <-runtime.Done():
        return runtime.Err()
    }
}
```

The example uses `context`, `errors`, and `github.com/sparques/vulpes/pkg/modem`
as imports in addition to `github.com/sparques/vulpes/pkg/alsamodem`.

## Runtime configuration

- `Config.Audio` is a list of named ALSA endpoints. `Capture` and `Playback`
  are ALSA PCM names (for example `default`, `plughw:1,0`, or
  `hw:CARD=Device,DEV=0`). `SampleRate` defaults to 48000 Hz. `Channels`
  defaults to 1 and is used as the default for `CaptureChannels` and
  `PlaybackChannels`; each count must be 1 or 2. `BufferMillis` defaults to
  100 and must be 1 through 5000. The runtime uses signed 16-bit little-endian
  PCM and currently requires ALSA to negotiate the requested settings exactly.
- `Config.Channels` binds modem channels to an audio endpoint. Names must be
  unique; `Audio` must reference a configured endpoint. `AudioChannel` is the
  zero-based capture and playback lane and must exist on both sides.
- `ChannelConfig.Modem` is `pkg/modem.Settings`; supported mode constants are
  `ModeAFSK300`, `ModeAFSK1200`, `ModePSK2400`, `ModePSK4800`,
  `ModeG3RUH9600`, `ModeAIS`, and `ModeEAS`. A zero sample rate inherits the
  selected audio endpoint's rate. Mode-specific fields (profile, variant,
  slicers, upsample, tone frequencies, and amplitude) follow the public
  `pkg/modem` settings and Dire Wolf-derived modem defaults. AIS and EAS are
  receive-only; `Send` returns `alsamodem.ErrReceiveOnly` for those modes.
- `PTTConfig.Type` is `PTTNone`, `PTTVOX`, `PTTSerial`, `PTTGPIO`, or
  `PTTCM108`. The type-specific fields are `Serial` (`Device`, `RTS`, `DTR`,
  `RTSActive`, `DTRActive`), `GPIO` (`Chip`, zero-based `Line`, `Active`), or
  `CM108` (`Device`, `GPIO`, `Active`). Active polarity is `high` or `low`;
  omitted polarity defaults to high/active-high. Serial requires RTS and/or
  DTR. GPIO requires a chip and non-negative line. CM108 requires a device;
  GPIO defaults to 3 and otherwise must be 1 through 8. `none` and `vox` do
  not open hardware PTT controls. VOX activation audio is set with
  `TXConfig.VOXToneMillis`.
- `TXConfig` supports `DelayMillis` (default 300), `TailMillis` (100),
  `SlotMillis` (100), `Persistence` (63, range 0..255), `FullDuplex` (false),
  and `VOXToneMillis` (0 except default 2000 for PTT type `vox`; range
  0..30000). `Channel.SetTiming` updates delay, tail, slot, persistence, and
  duplex at runtime; the VOX lead-in remains a startup setting.
- `MaximumFrameBytes` defaults to 2048 and `TransmitQueueSize` to 16.
  `CaptureQueueDepth` defaults to 4 audio chunks per endpoint;
  `EventQueueDepth` defaults to 32 events per channel. The event stream is
  bounded, so consumers should drain it; dropped events are reflected in
  `Channel.Metrics().DroppedEvents`.

`Channel.Send` queues a raw frame payload and returns before radio transmission
completes. `Channel.Events` yields validated received frame/AIS/EAS messages.
`Channel.Timing` and `Channel.SetTiming` inspect and update channel-access
settings; `Channel.Metrics` provides receive and queue counters. `Modem.Err`
reports the first unexpected worker or shutdown error, and `Modem.Done` closes
when the runtime has stopped. `Close` is safe to call more than once and waits
for workers and hardware cleanup.

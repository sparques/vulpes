# Vulpes YAML configuration

Vulpes reads one strict YAML document. The top-level `version` must be `1`.
Unknown fields, duplicate keys, and multiple YAML documents are rejected.
Every example below corresponds to the schema accepted by the current
configuration decoder; use [examples/vulpes.yaml](../examples/vulpes.yaml) for
a single-channel starting point and [examples/all-modes.yaml](../examples/all-modes.yaml)
for a multi-channel example.

## Top-level fields

| Field | Required | Valid values and behavior |
|---|---:|---|
| `version` | Yes | Integer `1` only. |
| `logging` | No | Structured logging options; defaults to `level: info` and stdout. |
| `audio` | Yes | Non-empty list of named capture/playback devices. |
| `channels` | Yes | Non-empty list of modem channels. |
| `interfaces` | No | Optional KISS TCP, KISS serial, and AGW TCP listeners. |

## Logging

| Field | Default | Valid values and behavior |
|---|---|---|
| `logging.level` | `info` | `debug`, `info`, `warn`, or `error` (case-insensitive). |
| `logging.file` | omitted | Optional writable path. When set, logs append to this file instead of stdout. |

The command-line `-log-file FILE` overrides `logging.file`.

## Audio devices

Each `audio` item defines one shared capture/playback endpoint referenced by
channels through `channel.audio`.

| Field | Required/default | Valid values and behavior |
|---|---|---|
| `name` | Required | Non-empty and unique within `audio`; used by `channel.audio`. |
| `capture` | Required | Non-empty input device name. Common ALSA forms include `default`, `hw:3,0`, and `hw:CARD=Device,DEV=0`. The special value `memory` selects an in-memory source; `wav:PATH` reads a WAV file. |
| `playback` | Required | Non-empty output device name, with the same ALSA forms; `memory` selects in-memory playback and `wav:PATH` writes a WAV file. |
| `sample_rate` | `48000` | Integer from 8000 through 192000 Hz. The selected modem must also support that rate. |
| `channels` | `1` | Shared fallback channel count, integer 1 or 2. Used for capture and playback wherever their direction-specific value is omitted. |
| `capture_channels` | `channels` | Capture PCM channel count, integer 1 or 2. Set separately for asymmetric devices, such as mono capture/stereo playback. |
| `playback_channels` | `channels` | Playback PCM channel count, integer 1 or 2. |
| `buffer_ms` | `100` | ALSA audio buffer duration, integer 1 through 5000 milliseconds. |

The `audio_channel` value on a modem channel is zero-based and must exist in
both its referenced device's capture and playback layouts. For example, with
`capture_channels: 1` and `playback_channels: 2`, only `audio_channel: 0` is
available in both directions.

## Modem channels

Each item in `channels` describes one independent radio/modem channel. Channel
names must be non-empty and unique. `audio` must reference an `audio.name`.
The channel order defines zero-based KISS/AGW port numbers: the first channel
is port 0, the second is port 1, and so on. KISS supports at most 16 channels.

| Field | Required/default | Valid values and behavior |
|---|---|---|
| `name` | Required | Non-empty, unique channel name. |
| `audio` | Required | Name of an existing audio device. |
| `audio_channel` | `0` | Zero-based PCM channel index. It must be below both capture and playback channel counts. |
| `modem` | Required | Modem settings described below. |
| `ptt` | `type: none` | Push-to-talk backend described below. |
| `tx` | Defaults below | Transmit timing and VOX lead-in options. |

### `modem` fields and per-mode constraints

`mode` is required. Supported values and valid mode-specific fields are:

| `modem.mode` | Valid `profile` | Valid `variant` | Valid `slicers` | Valid `upsample` | Notes |
|---|---|---|---:|---:|---|
| `afsk300` | `A` or `B` (default `A`) | Must be omitted | 1–9 (default 9) | Must be omitted | 300-baud AFSK. Default tones are 1600/1800 Hz. |
| `afsk1200` | `A` or `B` (default `A`) | Must be omitted | 1–9 (default 9) | Must be omitted | 1200-baud AFSK. Default tones are 1200/2200 Hz. |
| `psk2400` | `P`, `Q`, `R`, or `S` (default `R`) | `V26A` or `V26B` (default `V26B`) | Must be omitted | Must be omitted | 2400-baud QPSK. |
| `psk4800` | `T`, `U`, `V`, or `W` (default `V`) | Must be omitted | Must be omitted | Must be omitted | 4800-baud 8PSK. |
| `g3ruh9600` | Must be omitted | Must be omitted | Must be omitted | 0–4 (default 0) | 9600-baud G3RUH/GMSK; 0 selects automatic receive interpolation. |
| `ais` | Must be omitted | Must be omitted | Must be omitted | Must be omitted | AIS/GMSK receive-only mode. |
| `eas` | Must be omitted | Must be omitted | Must be omitted | Must be omitted | EAS SAME receive-only mode. |

The remaining `modem` fields are:

| Field | Default | Valid values and behavior |
|---|---|---|
| `amplitude` | `0.5` | Number greater than 0 and at most 1. Sets transmit amplitude where applicable. |
| `mark_hz`, `space_hz` | Mode defaults | Optional custom tone pair for AFSK and EAS. If either is set, both must be set; they must be positive, distinct, and below half the configured sample rate. For AFSK300 the defaults are 1600/1800 Hz; for AFSK1200 they are 1200/2200 Hz; for EAS they are 2083.33/1562.5 Hz. These fields have no effect on PSK, G3RUH, or AIS and should be omitted there. |

EAS tones are listed as mark/space respectively. Profiles, variants, slicers,
and upsample settings not listed for a mode must be omitted; the strict schema
rejects unsupported combinations.

## Push-to-talk (`ptt`)

`ptt.type` selects exactly one backend. If omitted, it defaults to `none`.
Do not populate another backend's nested settings when selecting a type.

| `ptt.type` | Nested object | Valid settings and behavior |
|---|---|---|
| `none` | None | No hardware PTT control. |
| `vox` | None | No hardware control; audio opens the radio VOX. See `tx.vox_tone_ms`. |
| `serial` | `serial` | RTS and/or DTR control lines on a serial device. |
| `gpio` | `gpio` | Linux GPIO line controlled through gpiochip. |
| `cm108` | `cm108` | CM108-compatible USB sound-card HID GPIO. |

`ptt.serial` fields:

| Field | Required/default | Valid values |
|---|---|---|
| `device` | Required | Non-empty serial device path, such as `/dev/ttyUSB0`. |
| `rts` | `false` | Boolean; enable RTS control. At least one of `rts` and `dtr` must be `true`. |
| `dtr` | `false` | Boolean; enable DTR control. |
| `rts_active` | `high` | `high` or `low`; selects the electrical asserted/keyed polarity. |
| `dtr_active` | `high` | `high` or `low`; selects the electrical asserted/keyed polarity. |

`ptt.gpio` fields:

| Field | Required/default | Valid values |
|---|---|---|
| `chip` | Required | Non-empty gpiochip path, commonly `/dev/gpiochip0`. |
| `line` | `0` | Non-negative GPIO line offset; 0 is a valid offset and also the implicit default. |
| `active` | `high` | `high` or `low` electrical active polarity. |

`ptt.cm108` fields:

| Field | Required/default | Valid values |
|---|---|---|
| `device` | Auto-discovery | Optional hidraw path such as `/dev/hidraw3`; omit to discover a compatible device. |
| `gpio` | `3` | Integer 1 through 8, identifying the CM108 HID GPIO pin. |
| `active` | `high` | `high` or `low` electrical active polarity. |

## Transmit timing (`tx`)

| Field | Default | Valid values and behavior |
|---|---:|---|
| `delay_ms` | `300` | Non-negative milliseconds of preamble/leading flags. |
| `tail_ms` | `100` | Non-negative milliseconds of trailing flags. |
| `slot_ms` | `100` | Positive milliseconds between channel-access checks. |
| `persistence` | `63` | Integer 0–255; higher values increase the chance of transmitting when the channel is clear. |
| `full_duplex` | `false` | Boolean; when true, bypasses carrier-busy arbitration. |
| `vox_tone_ms` | `2000` for VOX; otherwise `0` | Integer 0–30000 milliseconds. With `ptt.type: vox`, a 400 Hz tone at modem amplitude precedes each frame. A zero or omitted value defaults to 2000 ms for VOX; the field is ignored by other PTT types. |

Zero-valued `delay_ms`, `tail_ms`, `slot_ms`, and `persistence` are replaced by
their defaults during configuration loading. To request the minimum one-flag
preamble/tail, use a positive delay/tail value small enough to round to one
flag (for example, 1 ms).

## Interfaces

All interface lists are optional and may contain multiple listeners/endpoints.
An omitted `channel` means use the KISS channel-port mapping or, for serial,
the port nibble. If a KISS `channel` is specified, that endpoint is bound to
the named configured channel.

### `interfaces.kiss_tcp`

Each item has:

| Field | Required/default | Valid values and behavior |
|---|---|---|
| `listen` | Required | Non-empty Go TCP listen address such as `127.0.0.1:8001`, `:8001`, or `0.0.0.0:8001`. Loopback addresses accept only local clients. |
| `channel` | Omitted | Optional configured channel name to bind this listener to; an unknown name is invalid. If omitted, KISS port numbers map to channel order (0–15). |

### `interfaces.kiss_serial`

Each item has:

| Field | Required/default | Valid values and behavior |
|---|---|---|
| `device` | Required | Non-empty serial device path. The server retries opening it after absence/removal. |
| `baud` | `9600` | Positive integer baud rate. |
| `channel` | Omitted | Optional configured channel name that binds this serial endpoint; unknown names are invalid. If omitted, the KISS port nibble selects the channel. |

### `interfaces.agw_tcp`

Each item has:

| Field | Required/default | Valid values and behavior |
|---|---|---|
| `listen` | Required | Non-empty Go TCP listen address, with the same forms as KISS TCP. |
| `channel` | Omitted | The shared listener schema accepts and validates an optional configured channel name, but AGW currently exposes all channels by order; omit this field. |

AGW and KISS may use different TCP ports, but two listeners cannot bind the
same address/port simultaneously.

## Command-line overrides

These are command-line flags, not YAML fields: `-config`, `-log-file`,
`-audio-capture`, `-audio-playback`, `-sample-rate`, `-mode`, `-debug`,
`-wav-input`, `-wav-output`, and `-version`. The audio and mode overrides
apply to the first configured device/channel. `-config` defaults to
`vulpes.yaml` in the current directory. WAV overrides provide offline audio
input/output and must match the configured sample rate and channel count.

There is deliberately no Dire Wolf configuration parser. Digipeating, IGate,
beacons, APRS interpretation, heard lists, GPS, telemetry, and other excluded
application features have no YAML equivalent.

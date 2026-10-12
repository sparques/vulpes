# Configuration and Dire Wolf migration

Vulpes reads one strict, versioned YAML document. Unknown and duplicate fields
are errors. Start with `examples/vulpes.yaml`; `examples/all-modes.yaml` shows
every modem mode. The first channel is KISS/AGW port 0, the second is port 1,
and so on.

Common Dire Wolf modem directives map as follows:

| Dire Wolf | Vulpes YAML |
|---|---|
| `ADEVICE capture playback` | `audio[].capture`, `audio[].playback` |
| Direction-specific ALSA channel counts | `audio[].capture_channels`, `audio[].playback_channels` (fall back to `channels`) |

Use the direction-specific counts when an interface has different capture and
playback layouts. For example, the C-Media CM108 tested here is mono capture
and stereo playback; set `capture_channels: 1` and `playback_channels: 2`.
The legacy `channels` field remains the shared fallback for both directions.
| `ARATE` | `audio[].sample_rate` |
| `ACHANNELS` | `audio[].channels` |
| `CHANNEL n` | `channels[n]` (zero-based order) |
| `MODEM 300` | `modem.mode: afsk300` |
| `MODEM 1200` | `modem.mode: afsk1200` |
| `MODEM 2400` | `modem.mode: psk2400` and `variant: V26A` or `V26B` |
| `MODEM 4800` | `modem.mode: psk4800` |
| `MODEM 9600` | `modem.mode: g3ruh9600` |
| `MODEM EAS` | `modem.mode: eas` |
| `TXDELAY`, `TXTAIL` | `tx.delay_ms`, `tx.tail_ms` |
| `PERSIST`, `SLOTTIME` | `tx.persistence`, `tx.slot_ms` |
| `FULLDUP` | `tx.full_duplex` |
| `PTT ... RTS/DTR` | `ptt.type: serial` and `ptt.serial` |
| `PTT GPIO` | `ptt.type: gpio` and `ptt.gpio` |
| `PTT CM108` | `ptt.type: cm108` and `ptt.cm108` |
| VOX lead-in tone | `ptt.type: vox` and `tx.vox_tone_ms` (400 Hz; defaults to 2000 ms) |
| `KISSPORT` | `interfaces.kiss_tcp[].listen` |
| `AGWPORT` | `interfaces.agw_tcp[].listen` |

There is deliberately no Dire Wolf configuration parser. Digipeating, IGate,
beacons, APRS interpretation, heard lists, GPS, telemetry, and other excluded
application features have no YAML equivalent.

Command-line overrides apply to the first configured audio device or channel:

```text
-config FILE
-audio-capture DEVICE
-audio-playback DEVICE
-sample-rate HZ
-mode MODE
-debug CATEGORIES
-log-file FILE
-wav-input FILE
-wav-output FILE
-version
```

`-wav-input` and `-wav-output` provide deterministic offline operation. WAV
input must exactly match the configured sample rate and channel count.

For VOX operation, Vulpes sends a 400 Hz audio tone before each modulated frame
to open the remote transmitter. Set `tx.vox_tone_ms` per channel to change the
duration; the default is 2000 ms. This setting is used only when
`ptt.type: vox`.

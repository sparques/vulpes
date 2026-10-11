# Configuration and Dire Wolf migration

Vulpes reads one strict, versioned YAML document. Unknown and duplicate fields
are errors. Start with `examples/vulpes.yaml`; `examples/all-modes.yaml` shows
every modem mode. The first channel is KISS/AGW port 0, the second is port 1,
and so on.

Common Dire Wolf modem directives map as follows:

| Dire Wolf | Vulpes YAML |
|---|---|
| `ADEVICE capture playback` | `audio[].capture`, `audio[].playback` |
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

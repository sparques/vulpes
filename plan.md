# Vulpes implementation plan

Vulpes is a Go reimplementation of the modem/TNC portion of Dire Wolf. The
`direwolf/` checkout is the behavioral reference and supplies interoperability
fixtures; it is not part of the Vulpes build.

This is a living plan. Check an item only after its implementation and the
listed verification are complete. When a design or compatibility decision
changes, update this file in the same change.

## Scope

### Included

- Linux command-line TNC with one or more mono/stereo audio channels.
- 300 bit/s AFSK (default 1600/1800 Hz) receive and transmit.
- 1200 bit/s Bell 202 AFSK (1200/2200 Hz) receive and transmit.
- 2400 bit/s QPSK, including Dire Wolf legacy/V.26A and MFJ-2400/V.26B
  compatibility, receive and transmit.
- 4800 bit/s 8PSK/V.27 receive and transmit.
- 9600 bit/s G3RUH/K9NG-style scrambled baseband (often called GMSK in radio
  documentation) receive and transmit.
- AIS 9600 bit/s GMSK receive and delivery of decoded NMEA/AIS payloads.
- EAS SAME AFSK receive and delivery of decoded SAME messages.
- AX.25 frame packing, HDLC flags, bit stuffing, NRZI, CRC-16/X.25/FCS, channel
  busy detection, and a transmit queue with KISS timing controls.
- ALSA capture/playback, deterministic WAV/file input and output, and mock
  audio devices for tests.
- KISS over TCP and a real or pseudo serial port.
- AGWPE-compatible TCP service for modem/TNC operations.
- A native YAML configuration file covering the features above, plus useful
  command-line overrides.
- PTT via serial-port RTS/DTR control lines, Linux GPIO using
  `github.com/warthog618/go-gpiocdev`, and CM108/CM109/CM119-compatible USB
  sound-card GPIO, plus a no-op/VOX mode for setups that do not key in software.

### Excluded

- Digipeating, connected APRS-IS IGate operation, APRS decoding/presentation,
  APRStt, beacon/tracker functions, GPS, waypoint output, speech, Morse, DTMF,
  telemetry, heard-station databases, packet filtering, and DNS-SD.
- FX.25, IL2P, higher-rate experimental modes, QAM, and non-audio SDR device
  integrations unless scope is expanded later.
- AIS-to-APRS object conversion and EAS interpretation beyond recognizing and
  reporting the SAME message.
- GUI and platform audio backends other than ALSA in the initial release.

## Compatibility and engineering rules

- Use `direwolf/src/demod_afsk.c`, `demod_psk.c`, `demod_9600.c`, `demod.c`,
  `dsp.c`, `gen_tone.c`, `hdlc_rec*.c`, `hdlc_send.c`, `ais.c`, and the network
  interface sources as the behavioral reference.
- Modem encode/decode algorithms must closely follow Dire Wolf's proven DSP
  algorithms, constants, profiles, state transitions, and framing behavior.
  Replace exploratory/provisional algorithms rather than allowing them to
  become a divergent implementation. Deviate only for a clear, documented,
  and notable Dire Wolf defect, and require regression evidence showing that
  the change preserves or improves interoperability and real-signal behavior;
  Dire Wolf's strong empirical performance is the default authority when the
  theoretical choice is ambiguous.
- Keep DSP state instance-local. Multiple channels and decoder profiles must
  not share mutable package globals.
- Separate byte/frame logic, bit/symbol logic, DSP, audio devices, transports,
  and orchestration so each layer can be tested without ALSA hardware.
- Stream audio through bounded buffers and propagate cancellation; do not let a
  slow TCP client block demodulation or other clients.
- Prefer Go's standard library. Evaluate `github.com/gen2brain/alsa` for ALSA
  PCM and a maintained Go serial package for KISS serial after small API and
  loopback probes; pin chosen versions in `go.mod`.
- Use `github.com/warthog618/go-gpiocdev` for Linux GPIO PTT. Model serial and
  CM108 PTT behavior on `direwolf/src/ptt.c` and `direwolf/src/cm108.c`, while
  keeping all hardware access behind testable interfaces.
- Use the standard library's `log/slog` for structured logging. Log to stdout
  by default and allow configuration/CLI selection of a log file instead.
- Use the YAML organization's maintained `go.yaml.in/yaml/v3` package and its
  strict known-field decoder for configuration files.
- Format every changed Go file with `goimports` (never `gofmt`/`go fmt`).
- Run Go builds/tests with `GOCACHE=/tmp`.
- Preserve Dire Wolf's GPL obligations if implementation is derived from its
  source. Add license and attribution before distributing binaries.

## Proposed package layout

```text
cmd/vulpes/          CLI, lifecycle, logging, signal handling
internal/config/     YAML schema, decoding, defaults, and validation
internal/audio/      PCM interfaces, ALSA backend, WAV/file and test backends
internal/dsp/        filters, oscillators, PLL/timing recovery, slicers
internal/modem/      AFSK, PSK, G3RUH, AIS, and EAS modulators/demodulators
internal/hdlc/       CRC/FCS, NRZI, stuffing, frame receive/transmit
internal/ax25/       binary frame validation and address/control helpers
internal/tnc/        channels, DCD, queues, CSMA timing, PTT, frame fan-out
internal/ptt/        serial-line, Linux GPIO, CM108 GPIO, and no-op backends
internal/kiss/       framing plus TCP and serial servers
internal/agw/        AGWPE wire protocol and TCP server
internal/testutil/   deterministic signals, impairment helpers, fixtures
testdata/            small licensed/generated WAV and protocol fixtures
```

Packages may be combined when that produces a clearer API, but dependencies
must flow from orchestration toward reusable lower layers, not back upward.

## Milestones and TODOs

### 0. Baseline and reference inventory

- [x] Inventory the relevant Dire Wolf modem, HDLC, audio, KISS, AGW, AIS, EAS,
  configuration, and regression-test sources.
- [x] Define the included/excluded feature boundary and initial architecture in
  this plan.
- [x] Record the exact Dire Wolf reference commit/version and licensing choice
  in `README.md` and the Vulpes license files.
- [x] Initialize `go.mod`, the package skeleton, `cmd/vulpes`, and a minimal
  build/test workflow.
- [x] Add repository-wide commands (Makefile or equivalent) for `goimports`,
  unit tests, race tests, vet, and builds, all using `GOCACHE=/tmp`.
- [x] Establish fixture provenance: generate our own frames/signals where
  possible and document any copied Dire Wolf fixtures.

Acceptance: a no-op Vulpes binary builds, starts, prints version/help, and all
empty-package checks run reproducibly.

### 1. Frame and bitstream foundation

- [x] Implement CRC-16/X.25 calculation and good/bad FCS verification with
  published check vectors and Dire Wolf parity vectors.
- [x] Implement streaming HDLC receive: flags, aborts, bit unstuffing, frame
  length limits, reset/recovery, and error counters.
- [x] Implement HDLC transmit: flags, bit stuffing, FCS append, and NRZI.
- [x] Implement G3RUH additive scrambler/descrambler as independently tested
  streaming components.
- [x] Implement the minimal AX.25 binary frame parser/builder required by AGW
  monitor/unproto operations without adding APRS interpretation.
- [x] Add fuzz tests for HDLC, AX.25, KISS, and later AGW parsers; malformed
  input must not panic or allocate without a bound.

Acceptance: randomized payloads survive encode/decode, single-bit corruption is
rejected, chunk boundaries do not affect streaming results, and vectors agree
with Dire Wolf.

### 2. Audio and DSP infrastructure

Implementation note: a small cgo binding to the stable ALSA PCM API was chosen
over a third-party wrapper. It keeps the negotiated format and recovery behavior
explicit, has a non-cgo stub, and avoids adding a second hardware abstraction.

- [x] Define capture/playback interfaces around interleaved signed PCM and
  explicit sample rate, channel count, sample format, and buffer duration.
- [x] Implement WAV input/output and in-memory audio backends before hardware
  access so every modem test is deterministic.
- [x] Probe and select the Go ALSA package; implement capture/playback, device
  enumeration/opening, negotiated parameter validation, underrun/overrun
  recovery, and clean shutdown.
- [x] Implement reusable DSP primitives needed by the reference algorithms:
  oscillators, FIR design/application, windowing, low/high/band-pass filters,
  AGC/normalization where required, symbol timing PLL, and data slicers.
- [x] Implement channel routing for mono/stereo PCM and bounded concurrent
  receive/playback pipelines.
- [x] Add sample-rate and chunk-boundary invariance tests at supported rates
  (at minimum 44.1 and 48 kHz; add 22.05 kHz where the mode permits).

Acceptance: synthetic tones and filter response tests meet numeric tolerances;
WAV round trips are sample-accurate; ALSA loopback smoke testing is documented
and skips cleanly when hardware is unavailable.

### 3. AFSK 300 and 1200

- [x] Implement the AFSK modulator with continuous phase and fractional
  samples-per-bit handling.
- [x] Replace the provisional symbol-correlation AFSK receiver with a close Go
  translation of Dire Wolf's `demod_afsk.c`/`demod.c` receive pipeline,
  including its selected filter profiles, discriminator, PLL, slicers, and DCD
  behavior; retain the provisional path only as an explicitly labeled test
  oracle if it remains useful.
- [x] Implement 300 bit/s AFSK demodulation with default/custom mark and space
  frequencies and Dire Wolf-equivalent receive filtering/timing recovery.
- [x] Implement Dire Wolf's `A+` receive strategy with up to nine independent
  gain slicers, PLL/DCD and HDLC state per slicer, and duplicate suppression.
- [x] Implement 1200 bit/s Bell 202 AFSK demodulation and configurable Dire
  Wolf profile A/tone-amplitude and profile B/FM discriminators with 1-9
  independent slicers.
- [x] Connect demodulated NRZI bits to HDLC/DCD and received-frame metadata.
- [x] Add clean, frequency-offset, amplitude-offset, noise, clipping, and
  sample-clock-offset tests for both modes.
- [x] Cross-test in both directions: Dire Wolf decodes Vulpes WAV output and
  Vulpes decodes Dire Wolf `gen_packets` output.
- [x] Establish and record regression thresholds using the Dire Wolf 300/1200
  test scripts or equivalent generated corpora.

Acceptance: both modes transmit and receive AX.25 frames interoperably, meet
recorded clean-signal expectations, and recover after malformed/noisy frames.

### 4. 2400/4800 PSK

- [x] Implement differential QPSK modulation at 2400 bit/s, including legacy
  V.26A and MFJ-compatible V.26B phase mappings.
- [x] Implement V.26A/V.26B QPSK demodulation at 2400 bit/s.
- [x] Implement differential 8PSK/V.27 modulation at 4800 bit/s.
- [x] Implement differential 8PSK/V.27 demodulation at 4800 bit/s.
- [x] Port/adapt carrier mixing, low-pass filtering, symbol timing, phase
  decision, and ambiguity handling from the reference design, with Dire Wolf
  receive profiles P/Q/R/S for V.26 and T/U/V/W for V.27.
- [x] Test symbol packing independently from waveform generation and recovery.
- [x] Cross-test each mapping/mode against Dire Wolf-generated WAVs and make
  Dire Wolf decode Vulpes-generated WAVs.
- [x] Establish noise/frequency/sample-clock impairment regression thresholds.

Acceptance: 2400 V.26A, 2400 V.26B, and 4800 V.27 each pass bidirectional clean
interoperability and their recorded impairment suites.

### 5. 9600 G3RUH/GMSK

- [x] Implement the scrambled NRZI/baseband waveform used by Dire Wolf's
  G3RUH/K9NG mode, including continuous phase and fractional sample timing.
- [x] Implement Dire Wolf's cosine-window receive filter, polyphase
  upsampling, AGC, transition-interpolating clock recovery, slicing,
  descrambling, and DCD.
- [x] Validate polarity inversion and recovery from loss of carrier/state.
- [x] Cross-test Vulpes and Dire Wolf WAV generation/reception in both
  directions.
- [x] Establish clean and impaired regression thresholds, including audio-path
  bandwidth limitations documented for this mode.

Acceptance: 9600 AX.25 frames interoperate with Dire Wolf fixtures at 44.1 and
48 kHz and sustain long randomized round trips without decoder state drift.

### 6. AIS and EAS SAME receive modes

Partial implementation note: `internal/modem/ais` now reuses the Dire
Wolf-compatible 9600 bit/s GMSK receive filter/PLL, skips G3RUH descrambling,
checks AIS payload lengths after HDLC/FCS recovery, and creates armored
`!AIVDM` sentences. This remains unchecked until payload vectors and audio
interoperability are validated.

`internal/modem/eas` now uses the Dire Wolf-derived profile-A AFSK detector,
including the 520.83 bit/s timing correction, and gathers repeated SAME
`ZCZC`/`NNNN` messages after the 64-bit alternating-bit preamble. Dire Wolf's
generated `check-modemeas` signal reproduces all three headers and end markers.
AIS uses the payload from Dire Wolf's documented type-3 example as an external
vector and locally generates its GMSK audio so fixture provenance stays exact;
both modes reject corrupted and truncated transmissions in regression tests.

- [x] Implement AIS GMSK demodulation, training/sync detection, NRZI/HDLC
  framing, de-stuffing, and FCS validation according to the Dire Wolf reference
  and AIS framing rules.
- [x] Preserve raw AIS payloads and produce validated `!AIVDM`/`!AIVDO`-style
  output without converting positions into APRS objects.
- [x] Implement EAS SAME AFSK demodulation, preamble/sync detection, repeated
  header/end-marker gathering, character decoding, and confidence/error
  handling.
- [x] Deliver EAS messages as raw/structured SAME events without alert policy,
  geographic lookup, or APRS conversion.
- [x] Validate against Dire Wolf AIS documentation/fixtures and the
  `check-modemeas` signal, plus corrupted and truncated transmissions.

Acceptance: known AIS frames pass FCS and reproduce their payload/NMEA output;
known EAS broadcasts reproduce the expected SAME text with duplicate handling.

### 7. TNC runtime and transmit control

Partial implementation note: `internal/modem.NewReceiver` and
`internal/modem.NewTransmitter` provide mode selection and common streaming
interfaces, and `internal/tnc` builds per-channel modem endpoints from YAML,
emits received modem events through bounded subscriber queues, and provides
bounded frame queues and on-demand PCM encoding. Transmit dequeue now uses DCD-aware 10 ms carrier polling,
SLOTTIME intervals, the Dire Wolf `random <= persistence` decision, and
full-duplex bypass, with cancellation and a 60-second acquisition timeout.
The CLI now owns ALSA/WAV pipelines and KISS/AGW application lifecycle. A channel transmit operation now keys
PTT after CSMA grants access, routes mono output to the configured stereo
channel, writes the full burst, and fail-safely unkeys on cancellation/errors.

- [x] Implement channel configuration, lifecycle, frame event fan-out, metrics,
  backpressure policy, and graceful shutdown.
- [x] Implement bounded per-channel transmit queues and serialized access to a
  shared stereo playback device using a shared `audio.PlaybackBus` per endpoint.
- [x] Add bounded receive-event subscriptions and a KISS forwarding loop for
  channel frame events; AIS/EAS events remain available to non-KISS consumers.
- [x] Implement DCD-aware p-persistence CSMA with `TXDELAY`, `PERSIST`,
  `SLOTTIME`, `TXTAIL`, and full-duplex settings in KISS units.
- [x] Implement preamble/tail flag calculation correctly for PSK symbols as
  well as one-bit-per-symbol modes (avoiding Dire Wolf's noted timing shortcut).
- [x] Define a PTT interface with key/unkey sequencing and fail-safe unkey on
  cancellation or playback/device error; channel close interrupts pending
  waits and controller shutdown unkeys hardware.
- [x] Implement serial-port PTT compatible with Dire Wolf: selectable RTS and/or
  DTR control lines, independent active-high/active-low polarity, and safe
  initial/final line state.
- [x] Implement Linux GPIO PTT using `github.com/warthog618/go-gpiocdev`, with
  configurable gpiochip, line offset, active polarity, consumer name, exclusive
  line ownership, and deterministic release on shutdown.
- [x] Implement CM108-family USB sound-card GPIO PTT following Dire Wolf's HID
  behavior: compatible-device discovery, explicit hidraw device selection,
  GPIO 1-8 selection (default GPIO 3), polarity inversion, permission/error
  diagnostics, and the same single-pin output-mask behavior as Dire Wolf.
- [x] Add fake-device unit tests for every PTT backend and Linux hardware tests
  that skip cleanly when serial, gpiochip, or compatible CM108 hardware is not
  present.
- [x] Exercise concurrent receive, multiple clients, queue saturation, client
  disconnects, and shutdown under `go test -race`.

Acceptance: frames from any supported client are queued, carrier arbitration is
honored, PTT/audio sequencing is correct, and received frames reach all eligible
clients without one client stalling another.

### 8. KISS over TCP and serial

Implementation note: `internal/kiss.TCPServer` supports
concurrent TCP clients, port-nibble routing, optional listener channel binding,
bounded per-client outbound queues, frame broadcast, and nonblocking handling
of inbound data frames. `internal/kiss.SerialServer` now provides the same
framing/routing with raw 8N1 port opening, bounded output, read timeout, and
shutdown interruption. TXDELAY, PERSISTENCE, SLOTTIME, TXTAIL, and FULLDUPLEX
commands now update per-channel timing state atomically, and `TNC:`/`TXBUF:`
SET_HARDWARE queries reply on the originating connection. Serial devices are
opened raw 8N1 and reconnect after absence or removal; PTY and external Dire
Wolf `kissutil` interoperability tests exercise the configured transport.

- [x] Implement streaming KISS FEND/FESC framing and escaping with independent
  parser state per connection.
- [x] Support data, TXDELAY, PERSISTENCE, SLOTTIME, TXTAIL, FULLDUPLEX, and the
  useful Dire Wolf SET_HARDWARE queries/commands; reject unsupported commands
  safely.
- [x] Implement channel/port nibble routing and enforce payload/channel limits.
- [x] Implement concurrent TCP KISS clients, optional per-listener channel
  binding, and receive broadcast semantics.
- [x] Implement KISS on configured serial devices and PTYs with baud/raw-mode
  setup, reconnect/poll behavior, and clean device removal handling.
- [x] Add byte-vector, fragmented-stream, escaping, malformed-frame, TCP
  loopback, and PTY loopback tests.
- [x] Interoperate with Dire Wolf `kissutil` and at least one independent KISS
  client.

Acceptance: TCP and serial clients can independently send and receive arbitrary
binary AX.25 frames, change timing parameters, reconnect, and coexist.

### 9. AGWPE over TCP

Implementation note: golden header vectors, fragmented/coalesced decoding,
malformed-length rejection, allocation bounds, and independent multi-client
backpressure tests are automated. Connected mode targets Dire Wolf's AGW
semantics and implements a conservative modulo-8, one-frame-window AX.25 link
state machine with acknowledgements, bounded pending data, retransmission,
retry limits, incoming registered calls, connect-via paths, and disconnects.

- [x] Implement the Dire Wolf-compatible 36-byte little-endian AGW header,
  bounded payload decoding, and fixed-width callsigns.
- [x] Add safe per-client connection state and graceful TCP shutdown.
- [x] Implement core query/control messages: `R` version, `G` port list, `g`
  port capabilities, `X`/`x` callsign registration, and `y` queue depth.
- [x] Implement raw frame receive/transmit `k`/`K`, monitor enable/output
  `m` plus `U`/`I`/`S`/`T`, and unproto transmit `M`/`V`.
- [x] Implement connected-mode commands/events `C`/`v`/`c`, `D`, `d`, and `Y`
  only to the extent required for Dire Wolf AGW modem compatibility; keep this
  isolated from and do not expand it into APRS/application behavior.
- [x] Document deliberately unsupported commands such as heard-list `H` when
  their backing excluded feature would violate scope.
- [x] Add golden wire vectors, fragmentation/coalescing tests, malformed length
  tests, multiple-client tests, and Dire Wolf/independent-client interop tests.

Acceptance: common AGW clients can enumerate ports, monitor frames, and send raw
or unproto frames; supported connected-mode exchanges pass scripted loopback
tests; malformed clients cannot affect modem operation.

### 10. Configuration and CLI

- [x] Design a versioned YAML schema backed by typed Go configuration with
  explicit defaults and validation; support multiple devices/channels without
  mirroring Dire Wolf's global structures.
- [x] Strictly decode YAML, rejecting unknown and duplicate fields, type
  mismatches, invalid ranges, conflicting options, and excluded features with
  diagnostics that identify the source file and field where possible.
- [x] Cover audio device/rate/channel settings, modem modes and profiles, audio
  levels, KISS TCP/serial, AGW TCP, logging, transmit timing, and PTT in the
  YAML schema.
- [x] Add CLI flags for config path, audio device overrides, mode/rate, debug
  categories, log file, version, and deterministic WAV input/output operation.
- [x] Configure structured logging with `log/slog`, defaulting to stdout; when
  a log file is configured, open it safely, report setup failures clearly, and
  flush/close it during graceful shutdown.
- [x] Add configuration golden tests for valid YAML, defaults, malformed YAML,
  duplicate/unknown fields, conflicts, invalid ranges, and excluded features.
- [x] Write a documented minimal YAML configuration and examples for every
  supported mode and interface.
- [x] Document how relevant modem-only Dire Wolf directives map to Vulpes YAML;
  do not implement a second Dire Wolf configuration parser.

Acceptance: all supported behavior can be expressed in YAML, modem-only Dire
Wolf configurations have a clear migration mapping, and configuration errors
identify their source file and field where possible.

### 11. System verification, documentation, and release

Verification note (2026-10-11): all automated unit and Dire Wolf waveform
tests, TCP client interoperability, race tests, vet, and builds pass with
`GOCACHE=/tmp`. This host has no configured ALSA loopback or radio-control
hardware. Real-device and release-tag verification therefore remain unchecked
rather than being inferred from mocks or clean skips.

- [x] Build a table-driven end-to-end suite spanning every modem, sample rate,
  channel mapping, audio direction, KISS transport, and AGW transport.
- [x] Add deterministic signal impairments and store per-mode thresholds so DSP
  regressions are measurable rather than subjective.
- [x] Add fuzzing, race testing, goroutine/leak checks, parser allocation
  limits, and sustained multi-client soak tests.
- [ ] Run real ALSA capture/playback tests with loopback cable/device for each
  feasible mode; record hardware, mixer settings, sample format, and results.
- [ ] Test serial KISS with a PTY and real USB serial adapter; test PTT keying,
  polarity, and fail-safe unkey with serial RTS/DTR, a GPIO loopback, and a
  CM108-compatible sound card where hardware is available.
- [x] Verify interoperability with the pinned Dire Wolf reference and at least
  one independent KISS and AGW client; save exact commands and results.
- [x] Document installation, permissions, ALSA selection/mixer setup, config,
  every mode, KISS TCP/serial, AGW TCP, PTT, diagnostics, and limitations.
- [x] Add graceful error messages and operational counters for audio overruns,
  bad FCS, decoder activity, DCD, queue drops, and client failures.
- [ ] Run final `goimports`, `GOCACHE=/tmp go test ./...`, race tests, vet,
  builds, and all interoperability/regression suites.
- [ ] Tag the first release only after every included feature has an automated
  acceptance test or an explicitly documented hardware-only test result.

Acceptance: a fresh Linux machine can build/configure Vulpes from the docs; all
automated gates pass; supported modes and interfaces have recorded Dire Wolf
interoperability evidence; excluded features are accurately documented.

## Test strategy

Testing proceeds upward through four complementary levels:

1. Pure unit/property/fuzz tests for framing, CRC, scramblers, symbol mappings,
   DSP primitives, parsers, and state machines.
2. Deterministic in-process waveform round trips with controlled noise,
   frequency error, sample-clock error, amplitude, clipping, and chunk sizes.
3. Cross-implementation WAV and wire-protocol tests against the pinned Dire
   Wolf checkout and independent KISS/AGW clients.
4. Hardware smoke/soak tests for ALSA, serial KISS, and PTT, kept optional in CI
   but documented with reproducible commands.

No mode is complete based only on a clean self-generated round trip; each must
also decode an external/reference waveform and be decoded by the reference when
the reference supports transmission.

## Deferred decisions to resolve before their milestone

- Exact module path and public API commitment (initial packages should remain
  `internal` until their boundaries settle).
- GPL-compatible licensing/attribution approach for a source-guided
  reimplementation.
- ALSA and serial dependency versions after proof-of-concept tests; pin
  `github.com/warthog618/go-gpiocdev` with the other selected dependencies.
- Connected-mode AGW support is required for Dire Wolf modem compatibility;
  Vulpes targets the standard modulo-8 command/event behavior with a
  conservative one-frame transmit window.
- Exact number of simultaneous audio devices/channels and supported PCM sample
  formats for the first release.
- Schema-version migration policy and whether a schema document should be
  generated from or maintained alongside the Go types.

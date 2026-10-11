# Test fixture provenance

Vulpes does not currently check copied Dire Wolf signal or packet fixtures into
this directory. Unit tests synthesize their own payloads, bit streams, PCM, and
impairments deterministically.

The AFSK interoperability tests generate temporary WAV files at test time:

- Dire Wolf's installed `gen_packets` generates 300 and 1200 bit/s input for
  the Vulpes receiver.
- Vulpes generates 300 and 1200 bit/s WAV files which Dire Wolf's installed
  `atest` validates.

Those tests skip when the corresponding external Dire Wolf utility is not
installed. Any future persisted fixture must record its generator, command,
upstream version or commit, license, and expected decoded content here.

## AFSK regression floor

The deterministic AFSK tests establish the following minimum regression floor
at 48 kHz. These are pass/fail safeguards, not claims about ultimate decoder
sensitivity:

| Condition | 300 bit/s | 1200 bit/s |
| --- | ---: | ---: |
| Common tone-frequency offset with uniform sample noise of +/-3000 | +10 Hz | +25 Hz |
| Transmit sample-clock error | +5 samples/s | +5 samples/s |
| Mark/space amplitude imbalance | 0.55 / 2.0 gain | 0.55 / 2.0 gain |
| Clipping in the imbalance test | signed 16-bit hard limit | signed 16-bit hard limit |

Clean round trips additionally run at 44.1 and 48 kHz. Profile A, profile B,
and A+ multi-slicer reception are checked with generated signals; both A and B
decode fresh Dire Wolf `gen_packets` WAV output.

The bidirectional PSK interoperability tests cover V.26A, V.26B/MFJ, and V.27.
They require Dire Wolf `atest` to decode Vulpes WAVs and require Vulpes to
decode fresh Dire Wolf `gen_packets` WAVs. Each direction skips when its
corresponding Dire Wolf utility is unavailable.

PSK receiver profile names and their Dire Wolf equivalents are:

| Mode | Vulpes profile | Dire Wolf behavior |
| --- | --- | --- |
| 2400 V.26 | P | Self correlation, no prefilter |
| 2400 V.26 | Q | Self correlation, band-pass prefilter |
| 2400 V.26 | R | Local oscillator, no prefilter (default) |
| 2400 V.26 | S | Local oscillator, band-pass prefilter |
| 4800 V.27 | T | Self correlation, no prefilter |
| 4800 V.27 | U | Self correlation, band-pass prefilter |
| 4800 V.27 | V | Local oscillator, no prefilter (default) |
| 4800 V.27 | W | Local oscillator, band-pass prefilter |

## PSK and G3RUH regression floors

Clean in-process round trips for every PSK mode run at 22.05, 44.1, and 48
kHz with varying input chunk boundaries. The deterministic PSK impairment
suite at 48 kHz combines a +20 Hz carrier offset, +100 ppm sample-clock error,
and uniform sample noise of +/-1500 (approximately 24 dB peak SNR).

G3RUH clean round trips run at 44.1 and 48 kHz with both signal polarities and
varying chunks. Its 48 kHz impairment floor combines +100 ppm sample-clock
error, uniform noise of +/-1000 (approximately 28 dB peak SNR), and two passes
of a three-tap 1/4, 1/2, 1/4 low-pass filter to represent restricted audio-path
bandwidth. These are conservative pass/fail regression floors, not sensitivity
claims.

## AIS and EAS vectors

AIS payload armor and receive tests use the type-3 example from Dire Wolf's
AIS source/documentation (`35NO=dPOiAJriVDH@94E84AJ0000`) and verify its exact
NMEA checksum after HDLC/FCS audio recovery. The waveform is generated locally
from that external payload vector; no third-party audio is copied into the
repository. Corrupt-FCS and truncated versions must produce no AIS message.

The EAS integration test asks the pinned/system Dire Wolf `gen_packets -B EAS`
utility to generate fresh 48 kHz audio, expects all three SAME headers and end
markers, and verifies that truncated and fully corrupted variants cannot
reproduce the complete message set.

## Wire-protocol interoperability

When Dire Wolf's `kissutil` is installed, the KISS integration suite launches
it as an external TCP client, verifies the `TNC:` hardware-query response, and
queues a TNC2-formatted AX.25 frame through Vulpes. The transport tests also
exercise a minimal raw-socket client independently of Vulpes's KISS parser.

The AGW suite checks headers against the pinned Dire Wolf 36-byte layout and
runs a standalone Python socket client which independently packs and parses
version, port-list, registration, and queue-depth exchanges. Scripted AX.25
loopback tests cover outgoing and incoming connect, connected data, per-link
outstanding counts, acknowledgement, connect-via validation, and disconnect.

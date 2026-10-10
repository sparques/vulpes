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

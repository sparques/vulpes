# Build and operation

Vulpes requires Linux, Go 1.25 or newer, ALSA development headers, and cgo for
live audio. Build and verify it with:

```sh
make format
make check
make build
```

Run `vulpes -config examples/vulpes.yaml`. ALSA device names can be inspected
with `aplay -L` and `arecord -L`; set capture/playback names and mixer levels
before transmitting. The process must have read/write access to the selected
PCM, serial, gpiochip, and hidraw devices. Prefer narrowly scoped udev rules or
the distribution's audio/dialout/GPIO groups over running as root.

KISS/TCP and AGWPE listeners bind to the configured addresses. KISS serial is
raw 8N1 and polls until an absent or removed device is available again.
Received AX.25 frames are broadcast through bounded client queues, so
a slow client cannot stall a decoder. AGW supports enumeration, registration,
queue depth, raw frames, monitor frames, and unproto transmit. Connected mode
and heard-list commands are deliberately unsupported because Vulpes has no
connected-mode link state or heard-station database.

For hardware acceptance, record the kernel, ALSA device and mixer settings,
adapter identities, configuration, and exact command. Verify capture and
playback at 44.1 and 48 kHz, serial reconnect/removal, both PTT polarities, and
fail-safe unkey after cancellation and injected I/O errors. Automated tests
skip enumeration when ALSA hardware is unavailable; destructive radio keying
is never performed automatically.

Operational counters cover ALSA overrun/underrun recoveries, audio chunks and
samples, decoder/HDLC outcomes (including bad FCS), receive activity, DCD,
transmit queue depth/drops, dropped subscriber/client events, and rejected
KISS/AGW input. Package APIs expose snapshots through `Stats`, `Metrics`, and
`Counters` methods for diagnostics and integrations.

Hardware PTT smoke tests are opt-in because they toggle physical lines. Run
the applicable command only with the named output safely connected to a meter
or loopback fixture:

```sh
VULPES_TEST_SERIAL_PTT=/dev/ttyUSB0 GOCACHE=/tmp go test ./internal/ptt -run SerialHardware -v
VULPES_TEST_GPIO_CHIP=/dev/gpiochip0 VULPES_TEST_GPIO_LINE=17 GOCACHE=/tmp go test ./internal/ptt -run GPIOHardware -v
VULPES_TEST_CM108=/dev/hidraw2 GOCACHE=/tmp go test ./internal/ptt -run CM108Hardware -v
```

Without these variables the tests report a clean skip. The serial KISS suite
uses a pseudo-terminal pair when `socat` is installed and likewise skips only
that hardware-shaped test when it is unavailable.
Use `logging.level: debug` or `-debug` during interoperability diagnostics.

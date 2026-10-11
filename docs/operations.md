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
raw 8N1. Received AX.25 frames are broadcast through bounded client queues, so
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

Operational counters currently cover decoder/HDLC statistics, DCD, transmit
queue depth, dropped subscriber/client events, and rejected KISS/AGW input.
Use `logging.level: debug` or `-debug` during interoperability diagnostics.

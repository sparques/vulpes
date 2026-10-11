# Vulpes

Vulpes is a Go implementation of the modem/TNC portions of
[Dire Wolf](https://github.com/wb2osz/direwolf). It is under active development;
see [plan.md](plan.md) for the supported scope and progress.

The behavioral reference vendored in `direwolf/` is commit
`eda1383f5fa9d8ba3cb27f99db1d2c79494404c9` (`1.8.1-4-geda1383`, source version
1.8.2 development). Dire Wolf is Copyright John Langner, WB2OSZ, and other
contributors. Vulpes is a source-guided reimplementation and is distributed
under GPL-2.0-or-later; see [LICENSE](LICENSE).

## Development

The standard checks use a writable cache under `/tmp`:

```sh
make format
make test
make race
make vet
make build
```

Go files are formatted with `goimports`.

See [configuration and Dire Wolf migration](docs/configuration.md) and the
[build and operations guide](docs/operations.md) for live audio, transports,
permissions, hardware acceptance, and deterministic WAV operation.

## Current limitations

Vulpes is not yet release-ready. The CLI connects configured channels to live
ALSA or deterministic WAV audio, KISS TCP/serial, AGW services, and
PTT backends. Deterministic and Dire Wolf cross-tests cover every modem; real
audio and GPIO acceptance still requires the documented hardware test host.

The AGWPE service implements raw and monitor traffic, unproto transmission,
queries, callsign registration, and modulo-8 connected mode with a one-frame
window, acknowledgement, retransmission, and disconnect handling. It
deliberately does not implement the heard-station `H` command because Vulpes
does not maintain a heard-station database.

Hardware checks require an explicit test host. A release test record should
identify the ALSA device and mixer settings, serial adapter, GPIO chip/line,
and CM108-family hidraw device used. Tests must confirm both active polarity
and fail-safe unkeying on cancellation and device errors.

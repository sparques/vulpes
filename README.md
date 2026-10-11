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

Vulpes is not yet release-ready. The deterministic modem, framing, YAML,
KISS, AGW raw/query, and PTT backend components are under test, but the CLI
does not yet connect them to live ALSA capture/playback. AIS and EAS receive
paths have protocol-level tests but still need reference audio fixtures and
repeat/error-recovery validation.

The AGWPE service deliberately does not implement the heard-station `H`
command because Vulpes does not maintain a heard-station database. Connected
mode commands are also deferred until a target client requires them; raw AX.25
transmit/monitor and the `R`, `G`, `g`, `X`, `x`, and `y` query/control subset
are implemented.

Hardware checks require an explicit test host. A release test record should
identify the ALSA device and mixer settings, serial adapter, GPIO chip/line,
and CM108-family hidraw device used. Tests must confirm both active polarity
and fail-safe unkeying on cancellation and device errors.

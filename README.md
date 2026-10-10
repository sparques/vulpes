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

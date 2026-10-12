// SPDX-License-Identifier: GPL-2.0-or-later

// Package modem is the external streaming audio/packet API for Vulpes.
//
// Firmware can import github.com/sparques/vulpes/pkg/modem, construct one
// Modem, pass each mono int16 audio chunk from its I2S input to ProcessAudio,
// and pass frame payloads to Encode before writing the returned samples to
// its I2S output. The package performs no hardware I/O and has no dependency
// on ALSA, the Vulpes YAML configuration, or the command-line application.
package modem

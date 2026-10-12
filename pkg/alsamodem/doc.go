// SPDX-License-Identifier: GPL-2.0-or-later

// Package alsamodem provides an embeddable, ALSA-backed Vulpes modem runtime.
// It opens the configured capture/playback PCMs and PTT controls, then exposes
// frame send, receive-event, timing, and metrics APIs without starting the
// command-line application's KISS/AGW servers or reading YAML.
//
// A typical host application builds a Config, calls Open, sends frames using
// Channel.Send, and drains Channel.Events until Modem.Done closes. Always call
// Modem.Close (or cancel the context passed to Open) to release hardware.
// See docs/alsamodem.md for a complete example and configuration details.
package alsamodem

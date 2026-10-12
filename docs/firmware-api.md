# Streaming modem API for firmware

Firmware can import `github.com/sparques/vulpes/pkg/modem` without using the
Vulpes CLI, YAML configuration, ALSA, or any sound-device code. The package
works on signed 16-bit mono PCM samples; an I2S driver remains responsible for
moving samples to and from the codec/radio.

```go
import radiomodem "github.com/sparques/vulpes/pkg/modem"

codec, err := radiomodem.New(radiomodem.Settings{
	Mode:       radiomodem.ModeAFSK1200,
	SampleRate: 48000,
})
if err != nil {
	// Handle invalid mode/settings.
}

var rx [256]int16
n := readI2S(rx[:]) // firmware-specific input
for _, event := range codec.ProcessAudio(rx[:n]) {
	if event.Kind == radiomodem.EventFrame {
		frameBytes := event.Payload
		_ = frameBytes // route the decoded frame to the application
	}
}

pcm, err := codec.Encode(frameBytesToTransmit)
if err == nil {
	writeI2S(pcm) // firmware-specific output
}
```

`ProcessAudio` is streaming: pass every input chunk in order and retain the
same `Modem` so timing and HDLC state survive chunk boundaries. `Encode`
accepts the frame payload bytes and returns mono PCM containing HDLC flags,
bit stuffing, FCS, and the selected waveform. It retains transmitter state
between calls. Call `Reset` after changing radio/sample-clock state or when
starting a new stream. A `Modem` is stateful and not safe for concurrent use;
use separate instances for simultaneous independent streams.

Supported modes are AFSK 300/1200, PSK 2400/4800, G3RUH 9600, AIS, and EAS
SAME. AIS events include both `Payload` and `NMEA`; EAS events include
`Text`. AIS and EAS are receive-only, so `Encode` returns
`modem.ErrReceiveOnly` for those modes. Zero-valued settings use Vulpes modem
defaults; transmit delay/tail default to 300/100 ms. Set `Amplitude`,
`TXDelayMillis`, or `TXTailMillis` explicitly to override them.

TinyGo compile-check example:

```sh
tinygo test -target=waveshare-rp2040-zero -c ./pkg/modem
```

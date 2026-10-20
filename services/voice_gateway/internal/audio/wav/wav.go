// Package wav strips the RIFF container from speech synthesis output so the
// Opus encoder receives raw signed 16-bit little-endian samples.
//
// The audio profile is fixed at 16 kHz mono 16-bit, so any other shape is
// refused rather than resampled: playing a mismatched stream would pitch-shift
// a child's reply, and playing container header bytes would emit a click.
//
// The decoder is stateful because provider chunk boundaries are arbitrary. A
// stream that never starts with a RIFF signature is passed through untouched,
// since some providers legitimately return a bare PCM payload.
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// SampleRateHz is the only supported sample rate.
	SampleRateHz = 16000
	// ChannelCount is the only supported channel count.
	ChannelCount = 1
	// BitsPerSample is the only supported sample width.
	BitsPerSample = 16

	// formatPCM is the classic linear PCM format tag.
	formatPCM = 1
	// formatExtensible is WAVE_FORMAT_EXTENSIBLE, which providers use when they
	// emit a channel mask or sub-format GUID over the same PCM payload.
	formatExtensible = 0xFFFE
)

// Errors returned by the decoder.
var (
	// ErrMalformed reports a byte stream that starts like RIFF but is not a
	// valid WAVE container.
	ErrMalformed = errors.New("WAV stream is malformed")
	// ErrUnsupportedFormat reports a valid container whose audio profile the
	// gateway cannot play without resampling.
	ErrUnsupportedFormat = errors.New("WAV stream is not 16 kHz mono 16-bit PCM")
)

// phase tracks how much of the container the decoder has consumed.
type phase int

const (
	// phaseProbe has not yet decided whether the stream is a container.
	phaseProbe phase = iota
	// phaseHeader is reading RIFF chunks while looking for the data chunk.
	phaseHeader
	// phasePayload is emitting audio bytes.
	phasePayload
	// phasePassthrough is emitting a stream that was never a container.
	phasePassthrough
)

// Decoder strips the header from one WAV stream.
//
// Push and Finish are not safe for concurrent use; one decoder belongs to one
// synthesis stream.
type Decoder struct {
	phase phase
	buf   []byte
	// payloadDeclared is the data chunk size, or -1 when it was omitted.
	payloadDeclared int64
	// payloadEmitted counts bytes already returned for the data chunk.
	payloadEmitted int64
}

// NewDecoder returns a decoder ready for the first chunk.
func NewDecoder() *Decoder {
	return &Decoder{phase: phaseProbe, payloadDeclared: -1}
}

// Push consumes one provider chunk and returns the audio bytes that became
// available. It returns nil bytes with a nil error while the header is still
// incomplete, and a non-nil error once the stream is definitively unusable.
func (d *Decoder) Push(chunk []byte) ([]byte, error) {
	if len(chunk) == 0 {
		return nil, nil
	}
	if d.phase == phasePassthrough || d.phase == phasePayload {
		return d.emit(chunk)
	}

	d.buf = append(d.buf, chunk...)
	if d.phase == phaseProbe {
		if len(d.buf) < 4 {
			return nil, nil
		}
		if string(d.buf[0:4]) != "RIFF" {
			// Not a container: release the buffered bytes as PCM.
			d.phase = phasePassthrough
			out := d.buf
			d.buf = nil
			return out, nil
		}
		if len(d.buf) < 12 {
			return nil, nil
		}
		if string(d.buf[8:12]) != "WAVE" {
			return nil, fmt.Errorf("%w: missing WAVE signature", ErrMalformed)
		}
		d.phase = phaseHeader
	}

	return d.parseHeader(false)
}

// Finish flushes any bytes still buffered after the provider stopped.
func (d *Decoder) Finish() ([]byte, error) {
	switch d.phase {
	case phasePassthrough:
		return nil, nil
	case phasePayload:
		// A declared size shorter than the stream was already truncated by
		// emit, so there is nothing left to release.
		out := d.buf
		d.buf = nil
		return out, nil
	case phaseProbe:
		// The provider sent fewer than 12 bytes and never declared a container.
		out := d.buf
		d.buf = nil
		return out, nil
	}

	// Still inside the header: the container is truncated.
	if len(d.buf) == 0 {
		return nil, ErrMalformed
	}
	out, err := d.parseHeader(true)
	if err != nil {
		return nil, err
	}
	d.buf = nil
	return out, nil
}

// emit returns payload bytes while honouring a declared data length.
func (d *Decoder) emit(chunk []byte) ([]byte, error) {
	if d.payloadDeclared < 0 {
		return chunk, nil
	}
	remaining := d.payloadDeclared - d.payloadEmitted
	if remaining <= 0 {
		return nil, nil
	}
	if int64(len(chunk)) > remaining {
		chunk = chunk[:remaining]
	}
	d.payloadEmitted += int64(len(chunk))
	return chunk, nil
}

// parseHeader walks the RIFF chunks in d.buf looking for fmt and data.
func (d *Decoder) parseHeader(final bool) ([]byte, error) {
	formatTag := uint16(0)
	channels := uint16(0)
	sampleRate := uint32(0)
	bitsPerSample := uint16(0)
	dataOffset := -1
	dataLength := int64(-1)

	offset := 12
	for {
		if offset+8 > len(d.buf) {
			if final {
				return nil, fmt.Errorf("%w: truncated chunk header", ErrMalformed)
			}
			return nil, nil
		}
		chunkID := string(d.buf[offset : offset+4])
		chunkSize := int64(binary.LittleEndian.Uint32(d.buf[offset+4 : offset+8]))
		body := offset + 8

		if chunkID == "fmt " {
			if body+16 > len(d.buf) {
				if final {
					return nil, fmt.Errorf("%w: truncated fmt chunk", ErrMalformed)
				}
				return nil, nil
			}
			formatTag = binary.LittleEndian.Uint16(d.buf[body : body+2])
			channels = binary.LittleEndian.Uint16(d.buf[body+2 : body+4])
			sampleRate = binary.LittleEndian.Uint32(d.buf[body+4 : body+8])
			bitsPerSample = binary.LittleEndian.Uint16(d.buf[body+14 : body+16])
		} else if chunkID == "data" {
			dataOffset = body
			dataLength = chunkSize
		}

		// Chunks are word aligned, so an odd size carries one pad byte.
		next := body + int(chunkSize)
		if chunkSize%2 == 1 {
			next++
		}
		if next <= offset {
			return nil, fmt.Errorf("%w: non-advancing chunk", ErrMalformed)
		}

		if dataOffset >= 0 && formatTag != 0 {
			break
		}
		if next > len(d.buf) {
			if final {
				return nil, fmt.Errorf("%w: truncated chunk body", ErrMalformed)
			}
			return nil, nil
		}
		offset = next
	}

	if formatTag != formatPCM && formatTag != formatExtensible {
		return nil, fmt.Errorf("%w: format tag %d", ErrUnsupportedFormat, formatTag)
	}
	if channels != ChannelCount {
		return nil, fmt.Errorf("%w: %d channels", ErrUnsupportedFormat, channels)
	}
	if sampleRate != SampleRateHz {
		return nil, fmt.Errorf("%w: %d Hz", ErrUnsupportedFormat, sampleRate)
	}
	if bitsPerSample != BitsPerSample {
		return nil, fmt.Errorf("%w: %d bits", ErrUnsupportedFormat, bitsPerSample)
	}

	available := d.buf[dataOffset:]
	d.buf = nil
	d.phase = phasePayload
	d.payloadDeclared = dataLength

	return d.emit(available)
}

// IsWAV reports whether prefix begins a RIFF/WAVE container.
func IsWAV(prefix []byte) bool {
	if len(prefix) < 12 {
		return false
	}
	return string(prefix[0:4]) == "RIFF" && string(prefix[8:12]) == "WAVE"
}

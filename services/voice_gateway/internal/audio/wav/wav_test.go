package wav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// buildWAV assembles a RIFF/WAVE document with a fmt and a data chunk.
func buildWAV(t *testing.T, formatTag, channels uint16, sampleRate uint32, bits uint16, payload []byte) []byte {
	t.Helper()

	blockAlign := channels * bits / 8
	byteRate := sampleRate * uint32(blockAlign)

	fmtChunk := new(bytes.Buffer)
	_ = binary.Write(fmtChunk, binary.LittleEndian, formatTag)
	_ = binary.Write(fmtChunk, binary.LittleEndian, channels)
	_ = binary.Write(fmtChunk, binary.LittleEndian, sampleRate)
	_ = binary.Write(fmtChunk, binary.LittleEndian, byteRate)
	_ = binary.Write(fmtChunk, binary.LittleEndian, blockAlign)
	_ = binary.Write(fmtChunk, binary.LittleEndian, bits)

	body := new(bytes.Buffer)
	body.WriteString("WAVE")
	body.WriteString("fmt ")
	_ = binary.Write(body, binary.LittleEndian, uint32(fmtChunk.Len()))
	body.Write(fmtChunk.Bytes())
	body.WriteString("data")
	_ = binary.Write(body, binary.LittleEndian, uint32(len(payload)))
	body.Write(payload)
	if len(payload)%2 == 1 {
		body.WriteByte(0)
	}

	document := new(bytes.Buffer)
	document.WriteString("RIFF")
	_ = binary.Write(document, binary.LittleEndian, uint32(body.Len()))
	document.Write(body.Bytes())
	return document.Bytes()
}

func TestDecoderStripsContainerHeader(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03, 0x04}
	document := buildWAV(t, formatPCM, 1, 16000, 16, payload)

	decoder := NewDecoder()
	out, err := decoder.Push(document)
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	tail, err := decoder.Finish()
	if err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}

	got := append(out, tail...)
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %v, want %v", got, payload)
	}
}

func TestDecoderHandlesArbitraryChunkBoundaries(t *testing.T) {
	payload := make([]byte, 1000)
	for index := range payload {
		payload[index] = byte(index)
	}
	document := buildWAV(t, formatPCM, 1, 16000, 16, payload)

	for _, chunkSize := range []int{1, 3, 7, 12, 13, 44, 500} {
		decoder := NewDecoder()
		var got []byte
		for offset := 0; offset < len(document); offset += chunkSize {
			end := offset + chunkSize
			if end > len(document) {
				end = len(document)
			}
			out, err := decoder.Push(document[offset:end])
			if err != nil {
				t.Fatalf("chunkSize %d: Push returned error: %v", chunkSize, err)
			}
			got = append(got, out...)
		}
		tail, err := decoder.Finish()
		if err != nil {
			t.Fatalf("chunkSize %d: Finish returned error: %v", chunkSize, err)
		}
		got = append(got, tail...)

		if !bytes.Equal(got, payload) {
			t.Fatalf("chunkSize %d: payload mismatch, got %d bytes want %d", chunkSize, len(got), len(payload))
		}
	}
}

func TestDecoderAcceptsExtensibleFormat(t *testing.T) {
	payload := []byte{0x10, 0x20, 0x30, 0x40}
	document := buildWAV(t, formatExtensible, 1, 16000, 16, payload)

	decoder := NewDecoder()
	out, err := decoder.Push(document)
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	tail, err := decoder.Finish()
	if err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}
	if !bytes.Equal(append(out, tail...), payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestDecoderPassesThroughRawPCM(t *testing.T) {
	raw := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	decoder := NewDecoder()
	out, err := decoder.Push(raw)
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	tail, err := decoder.Finish()
	if err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}
	if !bytes.Equal(append(out, tail...), raw) {
		t.Fatalf("raw payload was altered")
	}
}

func TestDecoderRejectsUnsupportedProfiles(t *testing.T) {
	cases := []struct {
		name     string
		document []byte
	}{
		{"stereo", buildWAV(t, formatPCM, 2, 16000, 16, []byte{1, 2, 3, 4})},
		{"wrong sample rate", buildWAV(t, formatPCM, 1, 44100, 16, []byte{1, 2, 3, 4})},
		{"8 bit", buildWAV(t, formatPCM, 1, 16000, 8, []byte{1, 2, 3, 4})},
		{"float", buildWAV(t, 3, 1, 16000, 32, []byte{1, 2, 3, 4})},
	}

	for _, testCase := range cases {
		decoder := NewDecoder()
		_, err := decoder.Push(testCase.document)
		if err == nil {
			_, err = decoder.Finish()
		}
		if !errors.Is(err, ErrUnsupportedFormat) {
			t.Fatalf("%s: error = %v, want ErrUnsupportedFormat", testCase.name, err)
		}
	}
}

func TestDecoderRejectsMalformedStreams(t *testing.T) {
	cases := []struct {
		name     string
		document []byte
	}{
		{"riff without wave", []byte("RIFF\x00\x00\x00\x00AVI ")},
		{"truncated header", buildWAV(t, formatPCM, 1, 16000, 16, nil)[:30]},
	}

	for _, testCase := range cases {
		decoder := NewDecoder()
		_, err := decoder.Push(testCase.document)
		if err == nil {
			_, err = decoder.Finish()
		}
		if !errors.Is(err, ErrMalformed) {
			t.Fatalf("%s: error = %v, want ErrMalformed", testCase.name, err)
		}
	}
}

func TestDecoderTruncatesTrailingChunks(t *testing.T) {
	payload := []byte{0x0A, 0x0B, 0x0C, 0x0D}
	document := buildWAV(t, formatPCM, 1, 16000, 16, payload)

	// Metadata after the declared data length must not reach the speaker.
	trailer := new(bytes.Buffer)
	trailer.Write(document)
	trailer.WriteString("LIST")
	_ = binary.Write(trailer, binary.LittleEndian, uint32(4))
	trailer.WriteString("INFO")

	decoder := NewDecoder()
	out, err := decoder.Push(trailer.Bytes())
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	tail, err := decoder.Finish()
	if err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}
	if !bytes.Equal(append(out, tail...), payload) {
		t.Fatalf("trailing metadata leaked into payload")
	}
}

func TestDecoderFinishOnShortNonRIFFStream(t *testing.T) {
	decoder := NewDecoder()
	if _, err := decoder.Push([]byte{0x01}); err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	tail, err := decoder.Finish()
	if err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}
	if !bytes.Equal(tail, []byte{0x01}) {
		t.Fatalf("short stream bytes were lost")
	}
}

func TestIsWAV(t *testing.T) {
	document := buildWAV(t, formatPCM, 1, 16000, 16, []byte{1, 2})
	if !IsWAV(document) {
		t.Fatal("IsWAV rejected a valid container")
	}
	if IsWAV([]byte{1, 2, 3}) {
		t.Fatal("IsWAV accepted a short prefix")
	}
	if IsWAV([]byte("RIFFxxxxAVI ")) {
		t.Fatal("IsWAV accepted a non-WAVE container")
	}
}

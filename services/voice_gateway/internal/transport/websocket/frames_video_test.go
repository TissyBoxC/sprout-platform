package websocket

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/session"
	"github.com/gorilla/websocket"
)

// buildBinaryEnvelope writes one envelope by hand so a test can produce the
// malformed headers a correct encoder can never emit, such as a mismatched
// header length or a reserved flag.
type binaryEnvelopeSpec struct {
	magic         string
	version       byte
	reserved      byte
	headerLength  uint16
	sequence      uint32
	capturedAtMS  int64
	sessionIDLen  uint16
	payloadLen    uint32
	sessionID     string
	payload       []byte
	overrideSizes bool
	// video selects the version 2 header with a 32-bit payload length. It is
	// deliberately a separate switch from the magic so a test can build the
	// "video magic on an audio-shaped header" mismatch too.
	video bool
}

func (spec binaryEnvelopeSpec) headerSize() int {
	if spec.video {
		return binaryEnvelopeVideoHeaderSize
	}
	return binaryEnvelopeHeaderSize
}

func buildBinaryEnvelope(spec binaryEnvelopeSpec) []byte {
	headerSize := spec.headerSize()
	if !spec.overrideSizes {
		spec.headerLength = uint16(headerSize + len(spec.sessionID))
		spec.sessionIDLen = uint16(len(spec.sessionID))
		spec.payloadLen = uint32(len(spec.payload))
	}
	header := make([]byte, headerSize)
	copy(header[0:4], spec.magic)
	header[4] = spec.version
	header[5] = spec.reserved
	binary.BigEndian.PutUint16(header[6:8], spec.headerLength)
	binary.BigEndian.PutUint32(header[8:12], spec.sequence)
	binary.BigEndian.PutUint64(header[12:20], uint64(spec.capturedAtMS))
	binary.BigEndian.PutUint16(header[20:22], spec.sessionIDLen)
	if spec.video {
		binary.BigEndian.PutUint32(header[22:26], spec.payloadLen)
	} else {
		binary.BigEndian.PutUint16(header[22:24], uint16(spec.payloadLen))
	}
	encoded := append(header, spec.sessionID...)
	return append(encoded, spec.payload...)
}

func validVideoSpec() binaryEnvelopeSpec {
	return binaryEnvelopeSpec{
		magic:        binaryEnvelopeMagicVideo,
		version:      binaryEnvelopeVideoVersion,
		sequence:     7,
		capturedAtMS: 1_760_000_000_000,
		sessionID:    "session_video",
		payload:      []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10},
		video:        true,
	}
}

func validAudioSpec() binaryEnvelopeSpec {
	return binaryEnvelopeSpec{
		magic:        binaryEnvelopeMagic,
		version:      binaryEnvelopeVersion,
		sequence:     1,
		capturedAtMS: 1_760_000_000_000,
		sessionID:    "session_audio",
		payload:      []byte{0x01, 0x02, 0x03},
	}
}

func TestDecodeVideoEnvelopeAcceptsWellFormedFrame(t *testing.T) {
	spec := validVideoSpec()
	decoded, err := DecodeVideoEnvelope(buildBinaryEnvelope(spec))
	if err != nil {
		t.Fatalf("decode video envelope: %v", err)
	}
	if decoded.SessionID != spec.sessionID {
		t.Fatalf("session id: got %q, want %q", decoded.SessionID, spec.sessionID)
	}
	if decoded.Sequence != spec.sequence {
		t.Fatalf("sequence: got %d, want %d", decoded.Sequence, spec.sequence)
	}
	if !decoded.CapturedAt.Equal(time.UnixMilli(spec.capturedAtMS).UTC()) {
		t.Fatalf("captured at: got %v", decoded.CapturedAt)
	}
	if string(decoded.Payload) != string(spec.payload) {
		t.Fatalf("payload: got %v, want %v", decoded.Payload, spec.payload)
	}
}

// documentedVideoCap is the video payload cap the task contract fixes at
// 256 KiB, independent of how the current header field happens to be sized.
const documentedVideoCap = 256 * 1024

func TestVideoCapExceedsAudioCap(t *testing.T) {
	if maxVideoFrameBytes != documentedVideoCap {
		t.Fatalf("video cap: got %d, want %d", maxVideoFrameBytes, documentedVideoCap)
	}
	if maxVideoFrameBytes <= frame.MaxPayloadBytes {
		t.Fatalf(
			"video cap %d must exceed the audio cap %d",
			maxVideoFrameBytes,
			frame.MaxPayloadBytes,
		)
	}
}

// maxEncodablePayload is the largest payload the envelope's 16-bit length
// field can describe, whichever kind is carried.
const maxEncodablePayload = 0xFFFF

func TestDecodeVideoEnvelopeAcceptsLargestEncodablePayload(t *testing.T) {
	spec := validVideoSpec()
	spec.payload = make([]byte, maxEncodablePayload)
	decoded, err := DecodeVideoEnvelope(buildBinaryEnvelope(spec))
	if err != nil {
		t.Fatalf("decode video envelope at the largest encodable payload: %v", err)
	}
	if len(decoded.Payload) != maxEncodablePayload {
		t.Fatalf("payload length: got %d, want %d", len(decoded.Payload), maxEncodablePayload)
	}
	// The same payload on the audio magic must still fail the audio cap.
	audioSpec := validAudioSpec()
	audioSpec.payload = make([]byte, maxEncodablePayload)
	if _, err := DecodeAudioEnvelope(buildBinaryEnvelope(audioSpec)); !errors.Is(err, ErrAudioEnvelopePayload) {
		t.Fatalf("expected audio cap rejection, got %v", err)
	}
}

func TestDecodeVideoEnvelopeRejectsOverVideoCapPayload(t *testing.T) {
	// No 16-bit header can describe a payload above 65535 bytes, so the
	// oversized branch is exercised through the parameterized decoder with a
	// lowered cap. Building a 300 KiB wire frame would only prove that the
	// length field cannot hold the value.
	capLimitedKind := videoEnvelopeKind
	capLimitedKind.maxPayload = 64
	spec := validVideoSpec()
	spec.payload = make([]byte, 65)
	_, err := decodeBinaryEnvelope(capLimitedKind, buildBinaryEnvelope(spec))
	if !errors.Is(err, ErrVideoEnvelopePayload) {
		t.Fatalf("expected %v, got %v", ErrVideoEnvelopePayload, err)
	}
	if errors.Is(err, ErrVideoEnvelopeInvalid) {
		t.Fatalf("oversized payload was reported as a header error: %v", err)
	}
	if !strings.Contains(err.Error(), "video payload of 65 bytes") {
		t.Fatalf("error should name the payload and the limit, got %q", err)
	}
}

func TestDecodeVideoEnvelopeRejectsInvalidFrames(t *testing.T) {
	zeroSessionID := validVideoSpec()
	zeroSessionID.overrideSizes = true
	zeroSessionID.sessionID = ""
	zeroSessionID.headerLength = binaryEnvelopeVideoHeaderSize
	zeroSessionID.payload = []byte{0x01}

	wrongHeaderLength := validVideoSpec()
	wrongHeaderLength.overrideSizes = true
	wrongHeaderLength.headerLength = binaryEnvelopeVideoHeaderSize + 1

	wrongPayloadLength := validVideoSpec()
	wrongPayloadLength.overrideSizes = true
	wrongPayloadLength.payloadLen = 1

	testCases := []struct {
		name     string
		frame    func() []byte
		sentinel error
	}{
		{
			name: "wrong version",
			frame: func() []byte {
				spec := validVideoSpec()
				// A still on the audio version must be rejected: that version's
				// 16-bit length field would truncate the payload silently.
				spec.version = binaryEnvelopeVersion
				return buildBinaryEnvelope(spec)
			},
			sentinel: ErrVideoEnvelopeInvalid,
		},
		{
			name: "non-zero reserved flag",
			frame: func() []byte {
				spec := validVideoSpec()
				spec.reserved = 0x01
				return buildBinaryEnvelope(spec)
			},
			sentinel: ErrVideoEnvelopeInvalid,
		},
		{
			name: "truncated header",
			frame: func() []byte {
				return buildBinaryEnvelope(validVideoSpec())[:binaryEnvelopeVideoHeaderSize-1]
			},
			sentinel: ErrVideoEnvelopeInvalid,
		},
		{
			name:     "zero-length session id",
			frame:    func() []byte { return buildBinaryEnvelope(zeroSessionID) },
			sentinel: ErrVideoEnvelopeInvalid,
		},
		{
			name: "invalid session identifier",
			frame: func() []byte {
				spec := validVideoSpec()
				spec.sessionID = "Session-Video"
				return buildBinaryEnvelope(spec)
			},
			sentinel: ErrVideoEnvelopeInvalid,
		},
		{
			name: "audio magic",
			frame: func() []byte {
				spec := validVideoSpec()
				spec.magic = binaryEnvelopeMagic
				return buildBinaryEnvelope(spec)
			},
			sentinel: ErrVideoEnvelopeInvalid,
		},
		{
			name: "unknown magic",
			frame: func() []byte {
				spec := validVideoSpec()
				spec.magic = "SRVX"
				return buildBinaryEnvelope(spec)
			},
			sentinel: ErrVideoEnvelopeInvalid,
		},
		{
			name:     "mismatched header length",
			frame:    func() []byte { return buildBinaryEnvelope(wrongHeaderLength) },
			sentinel: ErrVideoEnvelopeInvalid,
		},
		{
			name:     "mismatched payload length",
			frame:    func() []byte { return buildBinaryEnvelope(wrongPayloadLength) },
			sentinel: ErrVideoEnvelopeInvalid,
		},
		{
			name:     "truncated payload",
			frame:    func() []byte { return buildBinaryEnvelope(validVideoSpec())[:binaryEnvelopeVideoHeaderSize+8] },
			sentinel: ErrVideoEnvelopeInvalid,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := DecodeVideoEnvelope(testCase.frame())
			if err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
			if !errors.Is(err, testCase.sentinel) {
				t.Fatalf("expected %v, got %v", testCase.sentinel, err)
			}
		})
	}
}

func TestDecodeVideoEnvelopeAcceptsPayloadAboveAudioCap(t *testing.T) {
	// A still larger than any audio packet is exactly what the video kind
	// exists for, so the video kind must not inherit the audio bound.
	spec := validVideoSpec()
	spec.payload = make([]byte, frame.MaxPayloadBytes+1)
	decoded, err := DecodeVideoEnvelope(buildBinaryEnvelope(spec))
	if err != nil {
		t.Fatalf("decode video envelope above the audio cap: %v", err)
	}
	if len(decoded.Payload) != frame.MaxPayloadBytes+1 {
		t.Fatalf("payload length: got %d", len(decoded.Payload))
	}
}

func TestDecodeAudioEnvelopeRejectsVideoFrame(t *testing.T) {
	_, err := DecodeAudioEnvelope(buildBinaryEnvelope(validVideoSpec()))
	if !errors.Is(err, ErrAudioEnvelopeInvalid) {
		t.Fatalf("expected %v, got %v", ErrAudioEnvelopeInvalid, err)
	}
	if errors.Is(err, ErrAudioEnvelopePayload) {
		t.Fatalf("video magic was reported as an audio payload error: %v", err)
	}
}

func TestDecodeVideoEnvelopeRejectsAudioFrame(t *testing.T) {
	_, err := DecodeVideoEnvelope(buildBinaryEnvelope(validAudioSpec()))
	if !errors.Is(err, ErrVideoEnvelopeInvalid) {
		t.Fatalf("expected %v, got %v", ErrVideoEnvelopeInvalid, err)
	}
}

func TestAudioEnvelopeCapIsUnchanged(t *testing.T) {
	if maxVideoFrameBytes <= frame.MaxPayloadBytes {
		t.Fatalf(
			"video cap %d must exceed the audio cap %d",
			maxVideoFrameBytes,
			frame.MaxPayloadBytes,
		)
	}

	spec := validAudioSpec()
	spec.payload = make([]byte, frame.MaxPayloadBytes+1)
	if _, err := DecodeAudioEnvelope(buildBinaryEnvelope(spec)); !errors.Is(err, ErrAudioEnvelopePayload) {
		t.Fatalf("expected audio payload rejection, got %v", err)
	}

	// A still sized like a video frame must not become valid audio just
	// because the shared decoder learned about video.
	videoSized := validAudioSpec()
	videoSized.payload = make([]byte, maxVideoFrameBytes)
	if _, err := DecodeAudioEnvelope(buildBinaryEnvelope(videoSized)); !errors.Is(err, ErrAudioEnvelopePayload) {
		t.Fatalf("expected audio cap to reject video-sized payload, got %v", err)
	}

	decoded, err := DecodeVideoEnvelope(buildBinaryEnvelope(validVideoSpec()))
	if err != nil || len(decoded.Payload) == 0 {
		t.Fatalf("video decoder regressed: %v", err)
	}
	if _, err := EncodeAudioEnvelope(AudioEnvelope{
		SessionID:  "session_audio",
		CapturedAt: time.Now().UTC(),
		Payload:    make([]byte, maxVideoFrameBytes),
	}); !errors.Is(err, ErrAudioEnvelopePayload) {
		t.Fatalf("expected audio encoder to keep its cap, got %v", err)
	}
}

func TestBinaryEnvelopeClassification(t *testing.T) {
	audio := buildBinaryEnvelope(validAudioSpec())
	video := buildBinaryEnvelope(validVideoSpec())
	testCases := []struct {
		name      string
		data      []byte
		wantAudio bool
		wantVideo bool
	}{
		{name: "audio", data: audio, wantAudio: true},
		{name: "video", data: video, wantVideo: true},
		{name: "short", data: []byte("SRA")},
		{name: "empty", data: nil},
		{name: "unknown", data: []byte("SRVX0000")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isAudioEnvelope(testCase.data); got != testCase.wantAudio {
				t.Fatalf("isAudioEnvelope: got %v, want %v", got, testCase.wantAudio)
			}
			if got := isVideoEnvelope(testCase.data); got != testCase.wantVideo {
				t.Fatalf("isVideoEnvelope: got %v, want %v", got, testCase.wantVideo)
			}
		})
	}
}

func TestServerRoutesVideoFrameToFrameSource(t *testing.T) {
	manager := session.NewManager(session.ManagerConfig{MaxSessions: 8, MaxSessionsPerDevice: 2})
	defer manager.CloseAll()
	type capturedStill struct {
		frame DeviceVideoFrame
		sink  DeviceVideoSink
	}
	frames := make(chan capturedStill, 1)
	server, err := NewServer(ServerConfig{
		Manager:  manager,
		Verifier: testTokenVerifier{},
		FrameSource: func(_ context.Context, frame DeviceVideoFrame, sink DeviceVideoSink) {
			frames <- capturedStill{frame: frame, sink: sink}
		},
	}, nil)
	if err != nil {
		t.Fatalf("create websocket server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()

	spec := validVideoSpec()
	spec.sessionID = "session_camera"
	// Sent before any session_start: a camera still is valid outside a voice
	// session, so the transport must not answer with session_not_found.
	if err := connection.WriteMessage(websocket.BinaryMessage, buildBinaryEnvelope(spec)); err != nil {
		t.Fatalf("write video frame: %v", err)
	}

	select {
	case received := <-frames:
		if received.frame.DeviceID != "device_alpha" {
			t.Fatalf("unexpected device: %+v", received.frame)
		}
		if received.frame.SessionID != spec.sessionID {
			t.Fatalf("session id: got %q, want %q", received.frame.SessionID, spec.sessionID)
		}
		if received.frame.Sequence != spec.sequence {
			t.Fatalf("sequence: got %d, want %d", received.frame.Sequence, spec.sequence)
		}
		if received.sink == nil {
			t.Fatal("frame source received no reply sink")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("video frame never reached the frame source")
	}
}

func TestServerRejectsMalformedVideoFrame(t *testing.T) {
	server, manager := newTestServer(t)
	defer manager.CloseAll()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	connection := dialAuthenticated(t, httpServer.URL, "valid-token")
	defer connection.Close()

	spec := validVideoSpec()
	spec.version = 99
	if err := connection.WriteMessage(websocket.BinaryMessage, buildBinaryEnvelope(spec)); err != nil {
		t.Fatalf("write video frame: %v", err)
	}
	errorFrame := readControl(t, connection)
	if errorFrame.Type != controlTypeError || errorFrame.Code != errorCodeInvalidAudio {
		t.Fatalf("expected invalid_audio error frame, got %+v", errorFrame)
	}
	// A malformed video frame must be reported, not disconnect or wedge the
	// reader; a following audio-shaped frame is still answered.
	if err := connection.WriteMessage(websocket.BinaryMessage, buildBinaryEnvelope(validAudioSpec())); err != nil {
		t.Fatalf("write audio frame: %v", err)
	}
	audioError := readControl(t, connection)
	if audioError.Code != errorCodeSessionNotFound {
		t.Fatalf("expected audio path to require a session, got %+v", audioError)
	}
}

func TestMapVideoEnvelopeError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "oversized payload",
			err:  fmt.Errorf("%w: video payload of %d bytes", ErrVideoEnvelopePayload, maxVideoFrameBytes+1),
			want: errorCodeFrameTooLarge,
		},
		{
			name: "malformed header",
			err:  fmt.Errorf("%w: magic", ErrVideoEnvelopeInvalid),
			want: errorCodeInvalidAudio,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapVideoEnvelopeError(testCase.err); got != testCase.want {
				t.Fatalf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

package websocket

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
)

const (
	controlSchemaVersion = frame.SchemaVersion

	controlTypeSessionStart   = "session_start"
	controlTypeSessionStarted = "session_started"
	controlTypeSessionEnd     = "session_end"
	controlTypeSessionClosed  = "session_closed"
	controlTypeCancel         = "cancel"
	controlTypeError          = "error"
	controlTypePing           = "ping"
	controlTypePong           = "pong"

	errorCodeUnauthenticated = "unauthenticated"
	errorCodeSessionLimit    = "session_limit_reached"
	errorCodeSessionNotFound = "session_not_found"
	errorCodeInvalidControl  = "invalid_control_frame"
	errorCodeInvalidAudio    = "invalid_audio_frame"
	errorCodeFrameTooLarge   = "frame_too_large"
	errorCodeRateLimited     = "rate_limited"
	errorCodeSessionExpired  = "session_expired"
	errorCodeTransportClosed = "transport_closed"
	errorCodeProvider        = "provider_unavailable"
	errorCodeInternal        = "internal_error"
)

var (
	identifierPattern      = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$`)
	firmwareVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)
)

// Audio envelope errors are sentinel values so the transport can map them to
// stable device error codes without inspecting message text.
var (
	ErrAudioEnvelopeInvalid = errors.New("audio envelope is invalid")
	ErrAudioEnvelopePayload = errors.New("audio envelope payload is invalid")
)

var allowedCapabilities = map[string]struct{}{
	"audio_input":     {},
	"audio_output":    {},
	"wifi":            {},
	"camera":          {},
	"display":         {},
	"touch":           {},
	"led":             {},
	"battery":         {},
	"cellular_4g":     {},
	"motion":          {},
	"bluetooth_audio": {},
	"video_call":      {},
	"location":        {},
	"geofence":        {},
	"sos":             {},
	"multi_device":    {},
}

var allowedErrorCodes = map[string]struct{}{
	errorCodeUnauthenticated: {},
	errorCodeSessionLimit:    {},
	errorCodeSessionNotFound: {},
	errorCodeInvalidControl:  {},
	errorCodeInvalidAudio:    {},
	errorCodeFrameTooLarge:   {},
	errorCodeRateLimited:     {},
	errorCodeSessionExpired:  {},
	errorCodeTransportClosed: {},
	errorCodeProvider:        {},
	errorCodeInternal:        {},
}

// ControlFrame is one JSON control message in the realtime voice contract.
//
// Fields mirror packages/contracts/schemas/voice_control.schema.json exactly.
// Optional fields use pointers so a false boolean is still serialized for
// error frames without appearing on unrelated message types.
type ControlFrame struct {
	SchemaVersion   string   `json:"schema_version"`
	Type            string   `json:"type"`
	SessionID       string   `json:"session_id"`
	DeviceID        string   `json:"device_id"`
	SentAt          string   `json:"sent_at"`
	StreamID        string   `json:"stream_id,omitempty"`
	SampleRateHz    int      `json:"sample_rate_hz,omitempty"`
	ChannelCount    int      `json:"channel_count,omitempty"`
	DurationMS      int      `json:"duration_ms,omitempty"`
	Encoding        string   `json:"encoding,omitempty"`
	FirmwareVersion string   `json:"firmware_version,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	ExpiresAt       string   `json:"expires_at,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Code            string   `json:"code,omitempty"`
	Message         string   `json:"message,omitempty"`
	Retryable       *bool    `json:"retryable,omitempty"`
}

// AudioEnvelope is the fixed binary envelope used for one inbound Opus frame.
//
// The envelope is deliberately bounded and versioned so the transport can
// validate size, sequence, timestamp, and session identity before feeding the
// payload into the session codec. The payload itself is never copied into a
// control message or log.
type AudioEnvelope struct {
	SessionID  string
	Sequence   uint32
	CapturedAt time.Time
	Payload    []byte
}

const (
	binaryEnvelopeMagic           = "SRAW"
	serverEnvelopeMagic           = "SRSV"
	binaryEnvelopeVersion    byte = 1
	binaryEnvelopeHeaderSize      = 24
	maxIdentifierLength           = 128
)

// NewControlFrame returns a fully stamped control frame.
func NewControlFrame(frameType string, sessionID string, deviceID string) ControlFrame {
	return ControlFrame{
		SchemaVersion: controlSchemaVersion,
		Type:          frameType,
		SessionID:     sessionID,
		DeviceID:      deviceID,
		SentAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// NewErrorFrame returns an error frame with a stable code and retryability.
func NewErrorFrame(sessionID string, deviceID string, code string, message string, retryable bool) ControlFrame {
	// Even pre-session failures must satisfy the shared envelope, so use
	// canonical placeholders instead of emitting empty identifiers.
	if sessionID == "" {
		sessionID = "system_error"
	}
	if deviceID == "" {
		deviceID = "unknown_device"
	}
	control := NewControlFrame(controlTypeError, sessionID, deviceID)
	control.Code = code
	control.Message = message
	control.Retryable = boolPointer(retryable)
	return control
}

// DecodeControlFrame parses and validates one JSON control frame.
//
// Unknown fields and trailing JSON are rejected so a malformed or injected
// message cannot change the transport state. Validation is intentionally
// stricter than plain JSON decoding because this is the device boundary.
func DecodeControlFrame(data []byte) (ControlFrame, error) {
	if len(data) == 0 {
		return ControlFrame{}, errors.New("control frame is empty")
	}
	var control ControlFrame
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&control); err != nil {
		return ControlFrame{}, fmt.Errorf("decode control frame: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ControlFrame{}, errors.New("control frame contains trailing JSON")
	}
	if err := control.Validate(); err != nil {
		return ControlFrame{}, err
	}
	return control, nil
}

// Encode serializes one validated control frame into compact JSON.
func (control ControlFrame) Encode() ([]byte, error) {
	if err := control.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(control)
}

// Validate enforces the shared voice control schema and lifecycle fields.
func (control ControlFrame) Validate() error {
	if control.SchemaVersion != controlSchemaVersion {
		return fmt.Errorf("control frame schema_version is not supported: %q", control.SchemaVersion)
	}
	if !validIdentifier(control.SessionID) {
		return fmt.Errorf("control frame session_id is invalid: %q", control.SessionID)
	}
	if !validIdentifier(control.DeviceID) {
		return fmt.Errorf("control frame device_id is invalid: %q", control.DeviceID)
	}
	if _, err := time.Parse(time.RFC3339Nano, control.SentAt); err != nil {
		return fmt.Errorf("control frame sent_at is invalid: %w", err)
	}

	switch control.Type {
	case controlTypeSessionStart:
		return control.validateSessionStart()
	case controlTypeSessionStarted:
		return control.validateSessionStarted()
	case controlTypeSessionEnd, controlTypeSessionClosed, controlTypeCancel:
		if !validReason(control.Reason) {
			return errors.New("control frame reason is required")
		}
		return nil
	case controlTypeError:
		if _, ok := allowedErrorCodes[control.Code]; !ok {
			return fmt.Errorf("control frame error code is invalid: %q", control.Code)
		}
		if strings.TrimSpace(control.Message) == "" || len(control.Message) > 256 {
			return errors.New("control frame error message is invalid")
		}
		if control.Retryable == nil {
			return errors.New("control frame error retryable is required")
		}
		return nil
	case controlTypePing, controlTypePong:
		return nil
	default:
		return fmt.Errorf("control frame type is invalid: %q", control.Type)
	}
}

func (control ControlFrame) validateSessionStart() error {
	if !validIdentifier(control.StreamID) {
		return fmt.Errorf("session_start stream_id is invalid: %q", control.StreamID)
	}
	if control.SampleRateHz != frame.SampleRateHz ||
		control.ChannelCount != frame.ChannelCount ||
		control.DurationMS != frame.DurationMS ||
		control.Encoding != frame.EncodingName {
		return errors.New("session_start audio profile does not match 16 kHz mono opus 20 ms")
	}
	if !firmwareVersionPattern.MatchString(control.FirmwareVersion) {
		return fmt.Errorf("session_start firmware_version is invalid: %q", control.FirmwareVersion)
	}
	seen := make(map[string]struct{}, len(control.Capabilities))
	for _, capability := range control.Capabilities {
		if _, ok := allowedCapabilities[capability]; !ok {
			return fmt.Errorf("session_start capability is invalid: %q", capability)
		}
		if _, exists := seen[capability]; exists {
			return fmt.Errorf("session_start capability is duplicated: %q", capability)
		}
		seen[capability] = struct{}{}
	}
	return nil
}

func (control ControlFrame) validateSessionStarted() error {
	if _, err := time.Parse(time.RFC3339Nano, control.ExpiresAt); err != nil {
		return fmt.Errorf("session_started expires_at is invalid: %w", err)
	}
	return nil
}

// EncodeAudioEnvelope serializes one binary audio frame.
//
// The returned slice starts with the fixed SRAW header followed by the
// session identifier and Opus payload. The payload is copied so callers may
// release their source buffer immediately after this call.
func EncodeAudioEnvelope(envelope AudioEnvelope) ([]byte, error) {
	if !validIdentifier(envelope.SessionID) {
		return nil, fmt.Errorf("audio envelope session_id is invalid: %q", envelope.SessionID)
	}
	if envelope.CapturedAt.IsZero() {
		return nil, errors.New("audio envelope captured_at is required")
	}
	if len(envelope.Payload) == 0 || len(envelope.Payload) > frame.MaxPayloadBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrAudioEnvelopePayload, len(envelope.Payload))
	}

	sessionID := []byte(envelope.SessionID)
	headerLength := binaryEnvelopeHeaderSize + len(sessionID)
	encoded := make([]byte, headerLength+len(envelope.Payload))
	copy(encoded[0:4], binaryEnvelopeMagic)
	encoded[4] = binaryEnvelopeVersion
	encoded[5] = 0
	binary.BigEndian.PutUint16(encoded[6:8], uint16(headerLength))
	binary.BigEndian.PutUint32(encoded[8:12], envelope.Sequence)
	binary.BigEndian.PutUint64(encoded[12:20], uint64(envelope.CapturedAt.UTC().UnixMilli()))
	binary.BigEndian.PutUint16(encoded[20:22], uint16(len(sessionID)))
	binary.BigEndian.PutUint16(encoded[22:24], uint16(len(envelope.Payload)))
	copy(encoded[24:], sessionID)
	copy(encoded[headerLength:], envelope.Payload)
	return encoded, nil
}

// DecodeAudioEnvelope parses and validates one binary audio frame.
//
// Malformed headers, unknown versions, oversized payloads, and invalid session
// identifiers return descriptive errors without allocating a large payload.
func DecodeAudioEnvelope(data []byte) (AudioEnvelope, error) {
	if len(data) < binaryEnvelopeHeaderSize {
		return AudioEnvelope{}, fmt.Errorf("%w: truncated", ErrAudioEnvelopeInvalid)
	}
	if string(data[:4]) != binaryEnvelopeMagic {
		return AudioEnvelope{}, fmt.Errorf("%w: magic", ErrAudioEnvelopeInvalid)
	}
	if data[4] != binaryEnvelopeVersion {
		return AudioEnvelope{}, fmt.Errorf("%w: version %d", ErrAudioEnvelopeInvalid, data[4])
	}
	if data[5] != 0 {
		return AudioEnvelope{}, fmt.Errorf("%w: reserved flags", ErrAudioEnvelopeInvalid)
	}

	headerLength := int(binary.BigEndian.Uint16(data[6:8]))
	sequence := binary.BigEndian.Uint32(data[8:12])
	capturedAtMillis := int64(binary.BigEndian.Uint64(data[12:20]))
	sessionIDLength := int(binary.BigEndian.Uint16(data[20:22]))
	payloadLength := int(binary.BigEndian.Uint16(data[22:24]))
	if sessionIDLength == 0 || sessionIDLength > maxIdentifierLength {
		return AudioEnvelope{}, fmt.Errorf("%w: session_id length", ErrAudioEnvelopeInvalid)
	}
	if payloadLength == 0 || payloadLength > frame.MaxPayloadBytes {
		return AudioEnvelope{}, fmt.Errorf("%w: %d bytes", ErrAudioEnvelopePayload, payloadLength)
	}
	if headerLength != binaryEnvelopeHeaderSize+sessionIDLength {
		return AudioEnvelope{}, fmt.Errorf("%w: header length", ErrAudioEnvelopeInvalid)
	}
	if len(data) != headerLength+payloadLength {
		return AudioEnvelope{}, fmt.Errorf("%w: message length", ErrAudioEnvelopeInvalid)
	}

	sessionID := string(data[24:headerLength])
	if !validIdentifier(sessionID) {
		return AudioEnvelope{}, fmt.Errorf("%w: session_id", ErrAudioEnvelopeInvalid)
	}
	payload := append([]byte(nil), data[headerLength:]...)
	return AudioEnvelope{
		SessionID:  sessionID,
		Sequence:   sequence,
		CapturedAt: time.UnixMilli(capturedAtMillis).UTC(),
		Payload:    payload,
	}, nil
}

func validIdentifier(value string) bool {
	return identifierPattern.MatchString(value)
}

// EncodeServerAudioEnvelope serializes one outbound synthesized audio frame.
//
// The server frame mirrors the inbound SRAW envelope but uses the SRSV magic so
// the device never confuses its own loopback with a reply. Sequence is the
// gateway's playback index, letting the device drop duplicates after a resume.
func EncodeServerAudioEnvelope(sessionID string, sequence uint32, payload []byte) ([]byte, error) {
	if !validIdentifier(sessionID) {
		return nil, fmt.Errorf("server audio envelope session_id is invalid: %q", sessionID)
	}
	if len(payload) == 0 || len(payload) > frame.MaxPayloadBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrAudioEnvelopePayload, len(payload))
	}

	sessionIDBytes := []byte(sessionID)
	headerLength := binaryEnvelopeHeaderSize + len(sessionIDBytes)
	encoded := make([]byte, headerLength+len(payload))
	copy(encoded[0:4], serverEnvelopeMagic)
	encoded[4] = binaryEnvelopeVersion
	encoded[5] = 0
	binary.BigEndian.PutUint16(encoded[6:8], uint16(headerLength))
	binary.BigEndian.PutUint32(encoded[8:12], sequence)
	binary.BigEndian.PutUint64(encoded[12:20], uint64(time.Now().UTC().UnixMilli()))
	binary.BigEndian.PutUint16(encoded[20:22], uint16(len(sessionIDBytes)))
	binary.BigEndian.PutUint16(encoded[22:24], uint16(len(payload)))
	copy(encoded[24:], sessionIDBytes)
	copy(encoded[headerLength:], payload)
	return encoded, nil
}

func validReason(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && len(trimmed) <= 128
}

func boolPointer(value bool) *bool {
	return &value
}

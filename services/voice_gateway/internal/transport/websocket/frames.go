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
	controlTypeSessionState   = "session_state"
	controlTypeWakeDetected   = "wake_detected"
	controlTypeSessionEnd     = "session_end"
	controlTypeSessionClosed  = "session_closed"
	controlTypeCancel         = "cancel"
	controlTypeBargeIn        = "barge_in"
	controlTypeQualityMetrics = "quality_metrics"
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
	errorCodeNotSpeaking     = "not_speaking"
	errorCodeBargeInRejected = "barge_in_rejected"
	errorCodeTransportClosed = "transport_closed"
	errorCodeProvider        = "provider_unavailable"
	errorCodeInternal        = "internal_error"
)

var (
	identifierPattern      = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$`)
	firmwareVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)
	contentCategoryPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Audio envelope errors are sentinel values so the transport can map them to
// stable device error codes without inspecting message text.
var (
	ErrAudioEnvelopeInvalid = errors.New("audio envelope is invalid")
	ErrAudioEnvelopePayload = errors.New("audio envelope payload is invalid")
)

// Video envelope errors stay separate from the audio sentinels even though the
// decode rules are shared, because the device side must distinguish "this
// stream is not audio" from "this video frame is malformed".
var (
	ErrVideoEnvelopeInvalid = errors.New("video envelope is invalid")
	ErrVideoEnvelopePayload = errors.New("video envelope payload is invalid")
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
	errorCodeNotSpeaking:     {},
	errorCodeBargeInRejected: {},
	errorCodeTransportClosed: {},
	errorCodeProvider:        {},
	errorCodeInternal:        {},
}

var allowedSessionStates = map[string]struct{}{
	"idle":      {},
	"listening": {},
	"thinking":  {},
	"speaking":  {},
}

// ControlFrame is one JSON control message in the realtime voice contract.
//
// Fields mirror packages/contracts/schemas/voice_control.schema.json exactly.
// Optional fields use pointers so a false boolean is still serialized for
// error frames without appearing on unrelated message types.
type ControlFrame struct {
	SchemaVersion   string   `json:"schema_version"`
	Type            string   `json:"type"`
	State           string   `json:"state,omitempty"`
	SessionID       string   `json:"session_id"`
	DeviceID        string   `json:"device_id"`
	SentAt          string   `json:"sent_at"`
	StreamID        string   `json:"stream_id,omitempty"`
	SampleRateHz    int      `json:"sample_rate_hz,omitempty"`
	ChannelCount    int      `json:"channel_count,omitempty"`
	DurationMS      int      `json:"duration_ms,omitempty"`
	Encoding        string   `json:"encoding,omitempty"`
	FirmwareVersion string   `json:"firmware_version,omitempty"`
	WakeWord        string   `json:"wake_word,omitempty"`
	WakeConfidence  *int     `json:"wake_confidence_milli,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	ExpiresAt       string   `json:"expires_at,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Code            string   `json:"code,omitempty"`
	Message         string   `json:"message,omitempty"`
	Retryable       *bool    `json:"retryable,omitempty"`
	// ContentCategory declares the content category for a voice session. It is
	// optional so older firmware still connects; the gateway then applies only
	// the age-tier prompt and does not pretend a restricted category was
	// approved.
	ContentCategory string `json:"content_category,omitempty"`
	// Quality carries read-only preprocessing metrics. It is only populated on
	// quality_metrics frames and never contains PCM or provider payloads.
	Quality *QualityMetrics `json:"quality,omitempty"`
}

// QualityMetrics is the bounded, privacy-preserving preprocessing snapshot
// sent to the device and management plane.
type QualityMetrics struct {
	EchoConvergence int     `json:"echo_convergence"`
	NoiseFloor      float64 `json:"noise_floor"`
	AgcGain         float64 `json:"agc_gain"`
	VADConfidence   int     `json:"vad_confidence"`
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
	binaryEnvelopeMagic = "SRAW"
	// binaryEnvelopeMagicVideo marks a device video frame carrying one JPEG
	// still rather than Opus audio. The distinct magic keeps a camera frame
	// out of the session audio path and an audio packet out of the view path.
	binaryEnvelopeMagicVideo      = "SRVD"
	serverEnvelopeMagic           = "SRSV"
	binaryEnvelopeVersion    byte = 1
	binaryEnvelopeHeaderSize      = 24
	// A video still needs more than the 16-bit payload length an audio envelope
	// carries, so the video envelope is version 2 with a 32-bit length field and
	// a two-byte wider header. Version 2 exists only for the SRVD kind; every
	// audio envelope stays on version 1 and its original layout.
	binaryEnvelopeVideoVersion    byte = 2
	binaryEnvelopeVideoHeaderSize      = 26
	maxIdentifierLength                = 128
	// maxVideoFrameBytes caps the payload of a video still at 256 KiB.
	//
	// The envelope's 16-bit payload-length field addresses at most 65535
	// bytes, so this bound is a contract and a future format must widen that
	// field before the cap can actually admit a larger still. The value is
	// kept distinct from the audio cap so neither kind's limit can drift into
	// the other. The audio cap stays at frame.MaxPayloadBytes.
	maxVideoFrameBytes = 256 * 1024
)

// binaryEnvelopeKind describes one accepted inbound envelope magic and its
// payload bound, so decode and error mapping stay identical across kinds. Both
// sentinels of one kind must be distinct values: callers separate the two with
// errors.Is.
type binaryEnvelopeKind struct {
	magic       string
	maxPayload  int
	invalidErr  error
	payloadErr  error
	payloadUnit string
	// version is the envelope version this kind must carry.
	version byte
	// headerSize is the fixed header length before the session identifier.
	//
	// Video uses a wider header because a still does not fit the 16-bit payload
	// length an audio envelope spends bytes [22:24] on. Widening the field for
	// video only keeps the audio wire format byte-for-byte unchanged.
	headerSize int
	// payloadLengthBytes is 2 for audio and 4 for video.
	payloadLengthBytes int
}

var (
	audioEnvelopeKind = binaryEnvelopeKind{
		magic:              binaryEnvelopeMagic,
		maxPayload:         frame.MaxPayloadBytes,
		invalidErr:         ErrAudioEnvelopeInvalid,
		payloadErr:         ErrAudioEnvelopePayload,
		payloadUnit:        "audio",
		version:            binaryEnvelopeVersion,
		headerSize:         binaryEnvelopeHeaderSize,
		payloadLengthBytes: 2,
	}
	videoEnvelopeKind = binaryEnvelopeKind{
		magic:              binaryEnvelopeMagicVideo,
		maxPayload:         maxVideoFrameBytes,
		invalidErr:         ErrVideoEnvelopeInvalid,
		payloadErr:         ErrVideoEnvelopePayload,
		payloadUnit:        "video",
		version:            binaryEnvelopeVideoVersion,
		headerSize:         binaryEnvelopeVideoHeaderSize,
		payloadLengthBytes: 4,
	}
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
		if err := control.validateSessionStart(); err != nil {
			return err
		}
		return control.validateContentCategory()
	case controlTypeSessionStarted:
		return control.validateSessionStarted()
	case controlTypeSessionState:
		return control.validateSessionState()
	case controlTypeWakeDetected:
		if err := control.validateWakeDetected(); err != nil {
			return err
		}
		return control.validateContentCategory()
	case controlTypeSessionEnd, controlTypeSessionClosed, controlTypeCancel, controlTypeBargeIn:
		if !validReason(control.Reason) {
			return errors.New("control frame reason is required")
		}
		return nil
	case controlTypeQualityMetrics:
		if control.Quality == nil {
			return errors.New("control frame quality is required")
		}
		if control.Quality.EchoConvergence < 0 || control.Quality.EchoConvergence > 1024 {
			return errors.New("control frame echo_convergence is invalid")
		}
		if control.Quality.VADConfidence < 0 || control.Quality.VADConfidence > 1024 {
			return errors.New("control frame vad_confidence is invalid")
		}
		if control.Quality.NoiseFloor < 0 || control.Quality.NoiseFloor > 1 {
			return errors.New("control frame noise_floor is invalid")
		}
		if control.Quality.AgcGain < 0 || control.Quality.AgcGain > 64 {
			return errors.New("control frame agc_gain is invalid")
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

func (control ControlFrame) validateContentCategory() error {
	category := strings.TrimSpace(control.ContentCategory)
	if category == "" {
		return nil
	}
	if !contentCategoryPattern.MatchString(category) || len(category) > 32 {
		return fmt.Errorf("control frame content_category is invalid: %q", category)
	}
	return nil
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

func (control ControlFrame) validateSessionState() error {
	if _, ok := allowedSessionStates[control.State]; !ok {
		return fmt.Errorf("session_state state is invalid: %q", control.State)
	}
	return nil
}

func (control ControlFrame) validateWakeDetected() error {
	if !validIdentifier(control.StreamID) {
		return fmt.Errorf("wake_detected stream_id is invalid: %q", control.StreamID)
	}
	if strings.TrimSpace(control.WakeWord) == "" ||
		len(control.WakeWord) > 64 {
		return errors.New("wake_detected wake_word is invalid")
	}
	if control.WakeConfidence == nil ||
		*control.WakeConfidence < 0 ||
		*control.WakeConfidence > 1000 {
		return errors.New("wake_detected wake_confidence_milli is invalid")
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
// identifiers return descriptive errors without allocating a large payload. A
// video frame is rejected here: the two kinds never cross-accept.
func DecodeAudioEnvelope(data []byte) (AudioEnvelope, error) {
	return decodeBinaryEnvelope(audioEnvelopeKind, data)
}

// DecodeVideoEnvelope parses and validates one binary device video frame.
//
// The header is byte-identical to the audio envelope, so only the magic and
// the payload bound differ. The decoded payload is left for the transport's
// FrameSource handler; this package never parses JPEG itself.
func DecodeVideoEnvelope(data []byte) (AudioEnvelope, error) {
	return decodeBinaryEnvelope(videoEnvelopeKind, data)
}

// decodeBinaryEnvelope applies the shared envelope contract for one kind.
//
// Every bounds check runs before the payload is copied, so a lying header
// cannot make the transport retain a buffer sized by the device.
func decodeBinaryEnvelope(kind binaryEnvelopeKind, data []byte) (AudioEnvelope, error) {
	if len(data) < kind.headerSize {
		return AudioEnvelope{}, fmt.Errorf("%w: truncated", kind.invalidErr)
	}
	if string(data[:4]) != kind.magic {
		// A frame with a valid envelope but the other kind's magic is still
		// a decode failure for this caller; callers that accept both kinds
		// dispatch on the magic before choosing a decoder.
		return AudioEnvelope{}, fmt.Errorf("%w: magic", kind.invalidErr)
	}
	if data[4] != kind.version {
		return AudioEnvelope{}, fmt.Errorf("%w: version %d", kind.invalidErr, data[4])
	}
	if data[5] != 0 {
		return AudioEnvelope{}, fmt.Errorf("%w: reserved flags", kind.invalidErr)
	}

	headerLength := int(binary.BigEndian.Uint16(data[6:8]))
	sequence := binary.BigEndian.Uint32(data[8:12])
	capturedAtMillis := int64(binary.BigEndian.Uint64(data[12:20]))
	sessionIDLength := int(binary.BigEndian.Uint16(data[20:22]))
	var payloadLength int
	if kind.payloadLengthBytes == 4 {
		payloadLength = int(binary.BigEndian.Uint32(data[22:26]))
	} else {
		payloadLength = int(binary.BigEndian.Uint16(data[22:24]))
	}
	if sessionIDLength == 0 || sessionIDLength > maxIdentifierLength {
		return AudioEnvelope{}, fmt.Errorf("%w: session_id length", kind.invalidErr)
	}
	if payloadLength == 0 || payloadLength > kind.maxPayload {
		return AudioEnvelope{}, fmt.Errorf(
			"%w: %s payload of %d bytes exceeds the %d byte limit",
			kind.payloadErr,
			kind.payloadUnit,
			payloadLength,
			kind.maxPayload,
		)
	}
	if headerLength != kind.headerSize+sessionIDLength {
		return AudioEnvelope{}, fmt.Errorf("%w: header length", kind.invalidErr)
	}
	if len(data) != headerLength+payloadLength {
		return AudioEnvelope{}, fmt.Errorf("%w: message length", kind.invalidErr)
	}

	sessionID := string(data[kind.headerSize:headerLength])
	if !validIdentifier(sessionID) {
		return AudioEnvelope{}, fmt.Errorf("%w: session_id", kind.invalidErr)
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

// isVideoEnvelope reports whether a binary frame carries the video magic.
//
// The read loop must classify on the magic alone: routing happens before
// session lookup, because a camera still is accepted outside a voice session.
func isVideoEnvelope(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == binaryEnvelopeMagicVideo
}

// isAudioEnvelope reports whether a binary frame carries the audio magic.
func isAudioEnvelope(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == binaryEnvelopeMagic
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

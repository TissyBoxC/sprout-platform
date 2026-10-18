// Package vision implements the voice gateway's camera and vision pipeline.
//
// The pipeline is deliberately bounded: it accepts only symbolic task
// metadata, runs a deterministic content-safety gate, calls a vision model,
// and stores results in a short-lived in-memory window. Image bytes are never
// persisted, logged, or copied into results; only bounded labels, confidences,
// and a short summary leave this package.
package vision

import (
	"errors"
	"time"
)

var (
	ErrInvalidRequest   = errors.New("invalid vision request")
	ErrUnsupportedTask  = errors.New("unsupported vision task")
	ErrContentRejected  = errors.New("vision content rejected by policy")
	ErrModelUnavailable = errors.New("vision model unavailable")
	// ErrModelStreamInterrupted means the model response ended before the
	// provider's terminal SSE marker. A partial response must never be parsed
	// or stored as a successful result.
	ErrModelStreamInterrupted = errors.New("vision model stream interrupted")
	ErrResultNotFound         = errors.New("vision result not found")
	// ErrDeviceNotAuthorized means the guardian has withdrawn the consent that
	// permits camera/vision processing for the device's family.
	ErrDeviceNotAuthorized = errors.New("device is not authorized for vision")
	ErrStorageClosed       = errors.New("vision store is closed")
)

// TaskType mirrors the firmware and platform camera/vision contract.
type TaskType string

const (
	TaskTypeObjectRecognition TaskType = "object_recognition"
	TaskTypePictureBook       TaskType = "picture_book_recognition"
	TaskTypePhotoQA           TaskType = "photo_qa"
	TaskTypeCapture           TaskType = "capture"
)

// Failure codes are symbolic and safe to return to callers and metrics.
const (
	FailureConsentRevoked = "consent_revoked"
	FailureTimeout        = "timeout"
	FailureModelRejected  = "model_rejected"
	FailureCaptureFailed  = "capture_failed"
	FailureInvalidFrame   = "invalid_frame"
	FailureUnsupported    = "unsupported"
)

// ReasonContentRejected is the stable audit reason recorded when the safety
// gate refuses a vision prompt or model result.
const ReasonContentRejected = "vision_content_rejected"

const (
	// DefaultTaskTTL bounds how long a task may wait for a model response.
	DefaultTaskTTL = 2 * time.Minute
	// ResultRetention is the short-lived retention window for results.
	ResultRetention = 24 * time.Hour
	// MaxResultLabels caps how many recognized labels one result may carry.
	MaxResultLabels = 16
	// MaxLabelRunes bounds a single recognized label.
	MaxLabelRunes = 64
	// MaxSummaryRunes bounds the human-readable result summary.
	MaxSummaryRunes = 500
	// MaxPromptRunes bounds the guardian-supplied task prompt.
	MaxPromptRunes = 500
	// MaxConfidence is the integer confidence scale (0..10000 = 0..100%).
	MaxConfidence = 10000
	// MaxFrameDimension mirrors the firmware capture contract.
	MaxFrameDimension = 4096
	// MaxFrameBytes mirrors the firmware capture contract.
	MaxFrameBytes = 8 * 1024 * 1024
)

// Label is one recognized label with an integer confidence score.
type Label struct {
	Label      string `json:"label"`
	Confidence int    `json:"confidence"`
}

// Request describes one vision task handed to the gateway. It carries device
// provenance and bounded symbolic metadata only; the raw frame never reaches
// the model-call boundary without passing the content-safety gate.
type Request struct {
	TaskID   string
	DeviceID string
	TaskType TaskType
	// PolicyVersion is the effective child-safety policy revision supplied by
	// the trusted gateway caller. It is symbolic routing metadata, never child
	// data, and is forwarded to the platform execution proxy as a validated
	// policy hint.
	PolicyVersion string
	Prompt        string
	FrameWidth    int
	FrameHeight   int
	FrameBytes    int
}

// Result is one bounded recognition result for a completed task.
type Result struct {
	TaskID    string    `json:"task_id"`
	DeviceID  string    `json:"device_id"`
	TaskType  TaskType  `json:"task_type"`
	Model     string    `json:"model"`
	Labels    []Label   `json:"labels"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// TaskTypeIsValid reports whether a task type is part of the contract.
func TaskTypeIsValid(taskType TaskType) bool {
	switch taskType {
	case TaskTypeObjectRecognition,
		TaskTypePictureBook,
		TaskTypePhotoQA,
		TaskTypeCapture:
		return true
	default:
		return false
	}
}

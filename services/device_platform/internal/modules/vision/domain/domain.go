// Package domain defines camera/vision task, result, and privacy contracts.
//
// Only bounded, symbolic vision metadata is stored. Image bytes live in
// short-lived object storage outside this package and are never persisted in
// the platform database; results carry recognized labels and confidence, not
// captured pixels or child-identifying content.
package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidTask        = errors.New("invalid vision task")
	ErrTaskNotFound       = errors.New("vision task not found")
	ErrTaskNotPending     = errors.New("vision task is not pending")
	ErrPrivacyNotGranted  = errors.New("guardian image consent is required")
	ErrUnsupportedTask    = errors.New("unsupported vision task type")
	ErrResultNotFound     = errors.New("vision result not found")
	ErrInvalidResultQuery = errors.New("invalid vision result query")
)

// TaskType enumerates the supported camera and vision use cases.
type TaskType string

const (
	TaskTypeObjectRecognition TaskType = "object_recognition"
	TaskTypePictureBook       TaskType = "picture_book_recognition"
	TaskTypePhotoQA           TaskType = "photo_qa"
	TaskTypeCapture           TaskType = "capture"
)

// TaskStatus is the bounded task lifecycle.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusDispatched TaskStatus = "dispatched"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
	TaskStatusExpired    TaskStatus = "expired"
)

// Failure codes are symbolic so operators can alert without storing raw errors.
const (
	FailureConsentRevoked = "consent_revoked"
	FailureTimeout        = "timeout"
	FailureModelRejected  = "model_rejected"
	FailureCaptureFailed  = "capture_failed"
	FailureInvalidFrame   = "invalid_frame"
	FailureUnsupported    = "unsupported"
)

const (
	// DefaultTaskTTL bounds how long a task may wait for the device.
	DefaultTaskTTL = 2 * time.Minute
	// ResultRetention is the short-lived retention window for results.
	ResultRetention = 24 * time.Hour
	// MaxRequestIDRunes and MaxDeviceNameRunes keep persisted strings bounded.
	MaxRequestIDRunes = 128
	// MaxResultLabels caps how many recognized labels one result may carry.
	MaxResultLabels = 16
	// MaxLabelRunes bounds a single recognized label.
	MaxLabelRunes = 64
	// MaxConfidence is the integer confidence scale (0..10000 = 0..100%).
	MaxConfidence = 10000
	// MaxFrameDimension mirrors the firmware capture contract.
	MaxFrameDimension = 4096
	// MaxFrameBytes mirrors the firmware capture contract.
	MaxFrameBytes = 8 * 1024 * 1024
)

// ResultLabel is one recognized label with an integer confidence score.
type ResultLabel struct {
	Label      string `json:"label"`
	Confidence int    `json:"confidence"`
}

// Task is one bounded vision request for a device.
type Task struct {
	ID           string     `json:"task_id"`
	DeviceID     string     `json:"device_id"`
	TaskType     TaskType   `json:"task_type"`
	Status       TaskStatus `json:"status"`
	RequestedBy  string     `json:"-"`
	Prompt       string     `json:"prompt"`
	FrameWidth   int        `json:"frame_width"`
	FrameHeight  int        `json:"frame_height"`
	FrameBytes   int        `json:"frame_bytes"`
	FailureCode  string     `json:"failure_code"`
	CreatedAt    time.Time  `json:"created_at"`
	DispatchedAt *time.Time `json:"dispatched_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
}

// Result is one bounded recognition result for a completed task.
type Result struct {
	TaskID    string        `json:"task_id"`
	DeviceID  string        `json:"device_id"`
	TaskType  TaskType      `json:"task_type"`
	Labels    []ResultLabel `json:"labels"`
	Summary   string        `json:"summary"`
	CreatedAt time.Time     `json:"created_at"`
	ExpiresAt time.Time     `json:"expires_at"`
}

// CapabilityState summarizes whether a device may use the vision pipeline.
type CapabilityState struct {
	Supported      bool   `json:"supported"`
	ConsentGranted bool   `json:"consent_granted"`
	ReconsentPath  string `json:"reconsent_path"`
	RetentionHours int    `json:"retention_hours"`
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

// Package domain contains on-demand playback request types shared by the
// platform modules.
//
// A guardian asks one bound device to play a published content package; the
// device lists its pending requests and reports a terminal outcome. Everything
// is family-scoped: a guardian only ever reads or writes rows owned by the
// authenticated family, and a device only sees requests addressed to it.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrRequestNotFound      = errors.New("playback request not found")
	ErrInvalidRequest       = errors.New("invalid playback request")
	ErrInvalidPackageID     = errors.New("invalid playback package id")
	ErrInvalidDeviceID      = errors.New("invalid playback device id")
	ErrInvalidRequestState  = errors.New("invalid playback request state")
	ErrInvalidOutcome       = errors.New("invalid playback outcome")
	ErrInvalidNote          = errors.New("invalid playback note")
	ErrDuplicateRequest     = errors.New("playback request already pending")
	ErrReadinessUnavailable = errors.New("playback readiness is unavailable")
)

// Request source: a guardian tapped a package, or the platform proposed one.
const (
	SourceGuardian       = "guardian"
	SourceRecommendation = "recommendation"
)

// Request lifecycle. A request is created pending, the device acknowledges it,
// then reports a terminal completed/failed outcome; a guardian may cancel it
// while it is still pending.
const (
	StatusPending      = "pending"
	StatusAcknowledged = "acknowledged"
	StatusCompleted    = "completed"
	StatusFailed       = "failed"
	StatusCancelled    = "cancelled"
)

// Terminal outcomes a device reports for one request.
const (
	OutcomePlayed  = "played"
	OutcomeSkipped = "skipped"
	OutcomeError   = "error"
)

// NoteMaxLength bounds the optional guardian note.
const NoteMaxLength = 200

// packageIDPattern matches a stable, human-readable content package id.
var packageIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$`)

// Request is one on-demand playback request for a bound device.
type Request struct {
	ID             string
	FamilyID       string
	DeviceID       string
	PackageID      string
	Source         string
	Status         string
	Note           string
	RequestedAt    time.Time
	AcknowledgedAt *time.Time
	CompletedAt    *time.Time
	Outcome        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// RequestInput contains the editable fields accepted from a guardian.
type RequestInput struct {
	DeviceID  string
	PackageID string
	Note      string
}

// RequestQuery bounds one request read.
type RequestQuery struct {
	DeviceID string
	Status   string
	Limit    int
}

// IsSource reports whether value is a supported request source.
func IsSource(value string) bool {
	return value == SourceGuardian || value == SourceRecommendation
}

// IsStatus reports whether value is a supported request status.
func IsStatus(value string) bool {
	switch value {
	case StatusPending, StatusAcknowledged, StatusCompleted, StatusFailed,
		StatusCancelled:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether a status no longer changes.
func IsTerminal(status string) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// IsOutcome reports whether value is a supported terminal outcome.
func IsOutcome(value string) bool {
	return value == OutcomePlayed || value == OutcomeSkipped ||
		value == OutcomeError
}

// PackageIDValid reports whether value is a well-formed package identifier.
func PackageIDValid(value string) bool {
	return packageIDPattern.MatchString(value)
}

// ValidateRequestInput normalizes and validates one guardian request.
func ValidateRequestInput(input RequestInput) (RequestInput, error) {
	normalized := RequestInput{
		DeviceID:  strings.TrimSpace(input.DeviceID),
		PackageID: strings.TrimSpace(input.PackageID),
		Note:      strings.TrimSpace(input.Note),
	}
	if normalized.DeviceID == "" {
		return RequestInput{}, ErrInvalidDeviceID
	}
	if !PackageIDValid(normalized.PackageID) {
		return RequestInput{}, ErrInvalidPackageID
	}
	if lengthInRunes(normalized.Note) > NoteMaxLength {
		return RequestInput{}, ErrInvalidNote
	}
	return normalized, nil
}

// lengthInRunes returns the number of runes, so a limit is measured in
// characters rather than bytes.
func lengthInRunes(value string) int {
	return utf8.RuneCountInString(value)
}

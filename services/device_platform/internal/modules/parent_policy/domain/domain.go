// Package domain contains parent policy types shared by the platform modules.
package domain

import (
	"errors"
	"time"
)

var (
	ErrPolicyNotFound       = errors.New("parent policy not found")
	ErrInvalidPolicy        = errors.New("invalid parent policy")
	ErrInvalidDailyLimit    = errors.New("invalid daily limit")
	ErrInvalidCategories    = errors.New("invalid allowed categories")
	ErrInvalidDisabledHours = errors.New("invalid disabled periods")
	ErrInvalidVolume        = errors.New("invalid maximum volume")
)

// DisabledPeriod is one local-time window when the device must stay quiet.
// A period may cross midnight; start equals end is rejected as ambiguous.
type DisabledPeriod struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// Policy applies time, content, and volume limits to one child.
type Policy struct {
	ID                string
	FamilyID          string
	ChildID           string
	PolicyVersion     int
	DailyLimitMinutes int
	AllowedCategories []string
	DisabledPeriods   []DisabledPeriod
	MaxVolumePercent  int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// PolicyInput contains the guardian-editable policy fields.
//
// The zero value of DailyLimitMinutes means unlimited use; callers must set
// Unlimited explicitly so a missing field is not silently treated as zero.
type PolicyInput struct {
	DailyLimitMinutes int
	AllowedCategories []string
	DisabledPeriods   []DisabledPeriod
	MaxVolumePercent  int
}

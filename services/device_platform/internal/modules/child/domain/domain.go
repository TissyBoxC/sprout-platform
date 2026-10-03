// Package domain contains child profile types shared by the platform modules.
package domain

import (
	"errors"
	"time"
)

var (
	ErrChildNotFound        = errors.New("child profile not found")
	ErrInvalidChildProfile  = errors.New("invalid child profile")
	ErrInvalidChildNickname = errors.New("invalid child nickname")
	ErrInvalidAgeTier       = errors.New("invalid age tier")
	ErrInvalidInterests     = errors.New("invalid child interests")
	ErrInvalidCategories    = errors.New("invalid content categories")
	ErrGuardianConsent      = errors.New("guardian consent is required")
)

const (
	AgeTier3To4 = "age_3_4"
	AgeTier5To6 = "age_5_6"
	AgeTier7To8 = "age_7_8"
)

// AgeTiers is the canonical ordered list shared by clients and validation.
var AgeTiers = []string{AgeTier3To4, AgeTier5To6, AgeTier7To8}

// ContentCategories is the platform-approved content category pool.
var ContentCategories = []string{
	"story",
	"nursery_rhyme",
	"poetry",
	"english",
	"encyclopedia",
	"bedtime",
}

// Child is the guardian-owned profile used for age and content decisions.
type Child struct {
	ID                     string
	FamilyID               string
	Nickname               string
	AgeTier                string
	Interests              []string
	ContentCategories      []string
	GuardianConsentVersion string
	GuardianConsentedAt    time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// ProfileInput contains editable fields accepted from a guardian.
type ProfileInput struct {
	Nickname          string
	AgeTier           string
	Interests         []string
	ContentCategories []string
}

// Package domain contains content recommendation types shared by the platform
// modules.
//
// Recommendations are family-scoped and read-only: the platform suggests a
// bounded, ordered list of published content packages a guardian may want to
// play next, derived from the family's favourites, recent playback, and the
// selected child's age tier. A recommendation never invents a package: every
// item is a currently published catalog entry.
package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidQuery reports a malformed recommendation request.
	ErrInvalidQuery = errors.New("invalid recommendation query")
	// ErrInvalidAgeTier reports an age tier outside the approved set.
	ErrInvalidAgeTier = errors.New("invalid recommendation age tier")
)

// Approved age tiers mirrored from the content taxonomy.
const (
	AgeTier3To4 = "age_3_4"
	AgeTier5To6 = "age_5_6"
	AgeTier7To8 = "age_7_8"
)

// AgeTiers is the canonical ordered list shared by validation.
var AgeTiers = []string{AgeTier3To4, AgeTier5To6, AgeTier7To8}

// Reason names why a package was recommended, so the console can explain the
// suggestion without inventing its own taxonomy.
const (
	ReasonSameCategory = "same_category"
	ReasonSameAgeTier  = "same_age_tier"
	ReasonFresh        = "fresh"
)

// Query bounds one recommendation read.
type Query struct {
	// AgeTier narrows candidates to one child's age band. Empty means "any".
	AgeTier string
	// Limit bounds the result. Zero uses the service default.
	Limit int
	// ExcludePackageIDs are never recommended, e.g. items already shown.
	ExcludePackageIDs []string
}

// Candidate is one published catalog package the recommender may rank.
type Candidate struct {
	PackageID string
	Title     string
	Category  string
	AgeTiers  []string
	// PublishedAt is used only as a small freshness tiebreaker.
	PublishedAt time.Time
}

// Recommendation couples one candidate with its score and reason.
type Recommendation struct {
	PackageID string
	Title     string
	Category  string
	AgeTiers  []string
	Score     int
	Reason    string
}

// IsAgeTier reports whether value is an approved age band.
func IsAgeTier(value string) bool {
	for _, tier := range AgeTiers {
		if tier == value {
			return true
		}
	}
	return false
}

// ValidateQuery normalizes and validates one recommendation request.
func ValidateQuery(query Query) (Query, error) {
	normalized := Query{
		AgeTier:           strings.TrimSpace(query.AgeTier),
		Limit:             query.Limit,
		ExcludePackageIDs: make([]string, 0, len(query.ExcludePackageIDs)),
	}
	if normalized.AgeTier != "" && !IsAgeTier(normalized.AgeTier) {
		return Query{}, ErrInvalidAgeTier
	}
	for _, id := range query.ExcludePackageIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			normalized.ExcludePackageIDs = append(
				normalized.ExcludePackageIDs,
				trimmed,
			)
		}
	}
	return normalized, nil
}

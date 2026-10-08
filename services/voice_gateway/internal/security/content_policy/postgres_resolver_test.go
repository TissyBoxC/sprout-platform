package content_policy

import (
	"testing"
	"time"
)

func TestAggregateProfilesUsesMostRestrictiveValues(t *testing.T) {
	updatedAt := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	profiles := []resolvedPolicy{
		{
			allowedCategories:   []string{"story", "poetry", "bedtime"},
			disabledPeriodsJSON: []byte(`[{"start_time":"20:00","end_time":"07:00"}]`),
			maxVolumePercent:    70,
			updatedAt:           updatedAt,
			ageTier:             "age_5_6",
		},
		{
			allowedCategories:   []string{"story", "english"},
			disabledPeriodsJSON: []byte(`[{"start_time":"21:00","end_time":"06:00"}]`),
			maxVolumePercent:    50,
			updatedAt:           updatedAt.Add(time.Minute),
			ageTier:             "age_3_4",
		},
	}

	profile := aggregateProfiles(profiles)
	if len(profile.AllowedCategories) != 1 || profile.AllowedCategories[0] != "story" {
		t.Fatalf("allowed categories = %v, want [story]", profile.AllowedCategories)
	}
	if profile.MaxVolumePercent != 50 {
		t.Fatalf("max volume = %d, want 50", profile.MaxVolumePercent)
	}
	if profile.AgeTier != "age_3_4" {
		t.Fatalf("age tier = %q, want age_3_4", profile.AgeTier)
	}
	if len(profile.DisabledPeriods) != 2 {
		t.Fatalf("disabled periods = %v, want two periods", profile.DisabledPeriods)
	}
	if profile.SourceChildCount != 2 {
		t.Fatalf("source child count = %d, want 2", profile.SourceChildCount)
	}
}

func TestAggregateProfilesTreatsUnknownAgeTierAsYoungest(t *testing.T) {
	profiles := []resolvedPolicy{
		{
			allowedCategories: []string{"story"},
			maxVolumePercent:  80,
			ageTier:           "unknown",
		},
		{
			allowedCategories: []string{"story"},
			maxVolumePercent:  60,
			ageTier:           "age_7_8",
		},
	}

	profile := aggregateProfiles(profiles)
	if profile.AgeTier != "unknown" {
		t.Fatalf("age tier = %q, want the restrictive unknown tier", profile.AgeTier)
	}
	if profile.MaxVolumePercent != 60 {
		t.Fatalf("max volume = %d, want 60", profile.MaxVolumePercent)
	}
}

func TestDecodeDisabledPeriodsIgnoresMalformedData(t *testing.T) {
	if periods := decodeDisabledPeriods([]byte(`not-json`)); len(periods) != 0 {
		t.Fatalf("periods = %v, want none for malformed JSON", periods)
	}
}

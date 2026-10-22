package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/domain"
)

var referenceNow = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

// validInput is the shortest report a device may send. Every negative test
// changes exactly one field of it, so a failure names the rule that broke.
func validInput() domain.RecordInput {
	return domain.RecordInput{
		DeviceID:   "device-1",
		ChildID:    "child-1",
		Label:      "calm",
		Confidence: 72,
		ObservedAt: referenceNow.Add(-2 * time.Minute),
	}
}

func TestDefaultSettingsAreDisabled(t *testing.T) {
	// The non-negotiable rule: mood sensing is off until a guardian says
	// otherwise. A family with no row must read as disabled, which is what the
	// zero Settings value expresses.
	if (domain.Settings{}).Enabled {
		t.Fatal("zero settings must be disabled")
	}
	settings := domain.DefaultSettings("family-1", referenceNow)
	if settings.Enabled {
		t.Fatal("default settings must be disabled")
	}
	if settings.RetentionDays != domain.DefaultRetentionDays {
		t.Fatalf("retention = %d, want %d",
			settings.RetentionDays, domain.DefaultRetentionDays)
	}
}

func TestApplyRejectsRetentionOutsideRange(t *testing.T) {
	cases := []int{-1, domain.MaxRetentionDays + 1, 1000}
	for _, days := range cases {
		settings := domain.DefaultSettings("family-1", referenceNow)
		err := settings.Apply(true, days, referenceNow)
		if !errors.Is(err, domain.ErrInvalidRetention) {
			t.Errorf("apply(%d) err = %v, want ErrInvalidRetention", days, err)
		}
		// A refused change must leave the stored configuration untouched, so
		// the family never ends up enabled because a bad request arrived.
		if settings.Enabled {
			t.Errorf("apply(%d) enabled the family despite failing", days)
		}
	}
}

func TestApplyAcceptsRangeBoundaries(t *testing.T) {
	for _, days := range []int{domain.MinRetentionDays, domain.MaxRetentionDays} {
		settings := domain.DefaultSettings("family-1", referenceNow)
		if err := settings.Apply(true, days, referenceNow); err != nil {
			t.Fatalf("apply(%d): %v", days, err)
		}
		if settings.RetentionDays != days || !settings.Enabled {
			t.Fatalf("apply(%d) = %+v", days, settings)
		}
	}
}

func TestNormaliseRetentionUsesDefaultOnlyForZero(t *testing.T) {
	days, err := domain.NormaliseRetention(0)
	if err != nil || days != domain.DefaultRetentionDays {
		t.Fatalf("normalise(0) = %d/%v, want default", days, err)
	}
	if _, err := domain.NormaliseRetention(-3); !errors.Is(err, domain.ErrInvalidRetention) {
		t.Fatalf("normalise(-3) err = %v, want ErrInvalidRetention", err)
	}
}

func TestValidateRejectsUnknownLabel(t *testing.T) {
	// An open vocabulary would let a device store a free-text description of a
	// child, which is exactly what this module refuses to keep.
	for _, label := range []string{"", "angry", "CALM", "excited", "生气"} {
		input := validInput()
		input.Label = label
		if _, err := input.Validate(referenceNow); !errors.Is(err, domain.ErrInvalidLabel) {
			t.Errorf("label %q err = %v, want ErrInvalidLabel", label, err)
		}
	}
}

func TestValidateAcceptsEverySupportedLabel(t *testing.T) {
	for _, label := range domain.AllLabels() {
		if !domain.IsLabel(string(label)) {
			t.Fatalf("vocabulary label %q is not accepted", label)
		}
		input := validInput()
		input.Label = string(label)
		if _, err := input.Validate(referenceNow); err != nil {
			t.Fatalf("label %q: %v", label, err)
		}
	}
}

func TestValidateRejectsConfidenceOutsideRange(t *testing.T) {
	for _, confidence := range []int{-1, 101, 1000} {
		input := validInput()
		input.Confidence = confidence
		if _, err := input.Validate(referenceNow); !errors.Is(err, domain.ErrInvalidConfidence) {
			t.Errorf("confidence %d err = %v, want ErrInvalidConfidence",
				confidence, err)
		}
	}
}

func TestValidateAcceptsConfidenceBoundaries(t *testing.T) {
	for _, confidence := range []int{0, 55, 100} {
		input := validInput()
		input.Confidence = confidence
		if _, err := input.Validate(referenceNow); err != nil {
			t.Fatalf("confidence %d: %v", confidence, err)
		}
	}
}

func TestValidateRejectsFutureObservation(t *testing.T) {
	input := validInput()
	input.ObservedAt = referenceNow.Add(domain.ObservationFutureTolerance + time.Second)
	if _, err := input.Validate(referenceNow); !errors.Is(err, domain.ErrObservationFuture) {
		t.Fatalf("future err = %v, want ErrObservationFuture", err)
	}
}

func TestValidateToleratesSmallClockSkew(t *testing.T) {
	// A device a couple of minutes ahead is a clock problem, not a lie; refusing
	// it would drop a real sample over a few seconds of drift.
	input := validInput()
	input.ObservedAt = referenceNow.Add(2 * time.Minute)
	if _, err := input.Validate(referenceNow); err != nil {
		t.Fatalf("skewed observation: %v", err)
	}
}

func TestValidateRejectsStaleObservation(t *testing.T) {
	input := validInput()
	input.ObservedAt = referenceNow.Add(-domain.ObservationMaxAge - time.Hour)
	if _, err := input.Validate(referenceNow); !errors.Is(err, domain.ErrObservationTooOld) {
		t.Fatalf("stale err = %v, want ErrObservationTooOld", err)
	}
}

func TestValidateRejectsMissingDeviceOrInstant(t *testing.T) {
	missingDevice := validInput()
	missingDevice.DeviceID = "  "
	if _, err := missingDevice.Validate(referenceNow); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Errorf("missing device err = %v, want ErrInvalidRequest", err)
	}
	missingInstant := validInput()
	missingInstant.ObservedAt = time.Time{}
	if _, err := missingInstant.Validate(referenceNow); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Errorf("missing instant err = %v, want ErrInvalidRequest", err)
	}
}

func TestRecordInputHasNoTextField(t *testing.T) {
	// This test is a compile-time contract rather than an assertion: adding a
	// free-text or media field to RecordInput breaks the build here, forcing the
	// author to justify storing something a child said or looked like.
	input := validInput()
	normalised, err := input.Validate(referenceNow)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if normalised.ChildID != "child-1" || normalised.DeviceID != "device-1" {
		t.Fatalf("unexpected normalised input %+v", normalised)
	}
}

func TestCompareLabelsRefusesThinData(t *testing.T) {
	// Fewer than MinSamplesForTrend samples cannot support a claim about a
	// child's mood, so the answer is unknown rather than a confident trend.
	few := domain.MinSamplesForTrend - 1
	current := domain.LabelCount{Label: domain.LabelFrustrated, Count: few}
	previous := domain.LabelCount{Label: domain.LabelFrustrated, Count: 0}
	if got := domain.CompareLabels(current, previous, few, few); got != domain.TrendUnknown {
		t.Fatalf("thin trend = %s, want unknown", got)
	}
}

func TestCompareLabelsDetectsMovement(t *testing.T) {
	total := domain.MinSamplesForTrend * 10
	cases := []struct {
		name                        string
		currentCount, previousCount int
		want                        domain.Trend
	}{
		{"half to three quarters", total / 2, total / 4, domain.TrendUp},
		{"three quarters to half", total / 4, total / 2, domain.TrendDown},
		{"unchanged", total / 2, total / 2, domain.TrendFlat},
		{"marginally higher", total / 2, total/2 - total/50, domain.TrendFlat},
	}
	for _, testCase := range cases {
		current := domain.LabelCount{Label: domain.LabelCalm, Count: testCase.currentCount}
		previous := domain.LabelCount{Label: domain.LabelCalm, Count: testCase.previousCount}
		got := domain.CompareLabels(current, previous, total, total)
		if got != testCase.want {
			t.Errorf("%s: trend = %s, want %s", testCase.name, got, testCase.want)
		}
	}
}

func TestPercentRoundsOnce(t *testing.T) {
	cases := []struct {
		count, total, want int
	}{
		{0, 0, 0},
		{0, 10, 0},
		{1, 3, 33},
		{2, 3, 67},
		{10, 10, 100},
		{5, 0, 0},
	}
	for _, testCase := range cases {
		if got := domain.Percent(testCase.count, testCase.total); got != testCase.want {
			t.Errorf("percent(%d,%d) = %d, want %d",
				testCase.count, testCase.total, got, testCase.want)
		}
	}
}

func TestBuildSummaryAlwaysCarriesEveryLabel(t *testing.T) {
	settings := domain.Settings{Enabled: true, RetentionDays: 30}
	current := map[domain.Label]domain.LabelCount{
		domain.LabelCalm: {Label: domain.LabelCalm, Count: 30, ConfidenceSum: 2400},
	}
	summary := domain.BuildSummary(
		"family-1", "child-1", 7, referenceNow.AddDate(0, 0, -7), referenceNow,
		current, nil, settings,
	)
	if len(summary.Labels) != len(domain.AllLabels()) {
		t.Fatalf("labels = %d, want %d", len(summary.Labels), len(domain.AllLabels()))
	}
	for index, label := range domain.AllLabels() {
		if summary.Labels[index].Label != label {
			t.Fatalf("label %d = %s, want %s", index, summary.Labels[index].Label, label)
		}
	}
	if summary.TotalSamples != 30 {
		t.Fatalf("total = %d, want 30", summary.TotalSamples)
	}
	// 30 samples carrying a total confidence of 2400 average to 80, so the
	// field is the weighted mean rather than one label's figure.
	if summary.AverageConfidence != 80 {
		t.Fatalf("average confidence = %d, want 80", summary.AverageConfidence)
	}
}

func TestBuildSummaryTrendsAreUnknownWithoutHistory(t *testing.T) {
	settings := domain.Settings{Enabled: true, RetentionDays: 30}
	current := map[domain.Label]domain.LabelCount{
		domain.LabelCalm: {Label: domain.LabelCalm, Count: 40},
	}
	summary := domain.BuildSummary(
		"family-1", "", 7, referenceNow.AddDate(0, 0, -7), referenceNow,
		current, nil, settings,
	)
	for label, trend := range summary.Trends {
		if trend != domain.TrendUnknown {
			t.Fatalf("trend for %s = %s, want unknown on a first window", label, trend)
		}
	}
	if summary.PreviousTotalSamples != 0 {
		t.Fatalf("previous total = %d, want 0", summary.PreviousTotalSamples)
	}
}

func TestBuildSummaryEmptyWindowIsStillValid(t *testing.T) {
	summary := domain.BuildSummary(
		"family-1", "child-1", 7,
		referenceNow.AddDate(0, 0, -7), referenceNow,
		nil, nil, domain.DefaultSettings("family-1", referenceNow),
	)
	if summary.HasData() {
		t.Fatal("an empty window must report no data")
	}
	if summary.AverageConfidence != 0 {
		t.Fatalf("average confidence = %d, want 0", summary.AverageConfidence)
	}
	if summary.Enabled {
		t.Fatal("summary must report the family's disabled setting")
	}
	if len(summary.Labels) != len(domain.AllLabels()) {
		t.Fatalf("labels = %d, want every label", len(summary.Labels))
	}
}

func TestSummaryCarriesNoObservationInstant(t *testing.T) {
	// The guardian payload may only describe the window. If this fails to
	// compile, a field was added that could locate a child's mood in time.
	summary := domain.Summary{
		FamilyID:      "family-1",
		Days:          7,
		PeriodStart:   referenceNow.AddDate(0, 0, -7),
		PeriodEnd:     referenceNow,
		Labels:        []domain.LabelCount{},
		Trends:        map[domain.Label]domain.Trend{},
		RetentionDays: 30,
	}
	if summary.PeriodStart.After(summary.PeriodEnd) {
		t.Fatal("period bounds must be ordered")
	}
}

func TestNormaliseDaysUsesDefaultOnlyForZero(t *testing.T) {
	days, err := domain.NormaliseDays(0)
	if err != nil || days != domain.DefaultSummaryDays {
		t.Fatalf("normalise(0) = %d/%v, want default", days, err)
	}
	for _, invalid := range []int{-1, domain.MaxSummaryDays + 1} {
		if _, err := domain.NormaliseDays(invalid); !errors.Is(err, domain.ErrInvalidDays) {
			t.Errorf("normalise(%d) err = %v, want ErrInvalidDays", invalid, err)
		}
	}
}

func TestSettingsCutoffFollowsRetention(t *testing.T) {
	settings := domain.Settings{RetentionDays: 7}
	cutoff := settings.Cutoff(referenceNow)
	if want := referenceNow.AddDate(0, 0, -7); !cutoff.Equal(want) {
		t.Fatalf("cutoff = %s, want %s", cutoff, want)
	}
	// A row that somehow escaped validation still gets a bounded window rather
	// than a zero retention that would delete everything on the next sweep.
	broken := domain.Settings{RetentionDays: 0}
	if got := broken.Cutoff(referenceNow); !got.Equal(referenceNow.AddDate(0, 0, -domain.DefaultRetentionDays)) {
		t.Fatalf("broken cutoff = %s, want the default window", got)
	}
}

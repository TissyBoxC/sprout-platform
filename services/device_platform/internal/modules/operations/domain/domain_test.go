package domain

import (
	"encoding/json"
	"testing"
)

// Equal plain versions previously panicked because the comparator indexed a
// pre-release segment that SplitN never produced. The OTA and service-version
// modules both call this function, so the regression is release-blocking.
func TestCompareVersionsEqualPlainVersions(t *testing.T) {
	t.Parallel()

	if got := CompareVersions("0.12.6", "0.12.6"); got != 0 {
		t.Fatalf("CompareVersions(0.12.6, 0.12.6) = %d, want 0", got)
	}
	if got := CompareVersions("v0.12.6", "0.12.6"); got != 0 {
		t.Fatalf("CompareVersions(v0.12.6, 0.12.6) = %d, want 0", got)
	}
}

func TestCompareVersionsOrdering(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "older patch", left: "0.12.5", right: "0.12.6", want: -1},
		{name: "newer minor", left: "0.13.0", right: "0.12.9", want: 1},
		{name: "missing patch segment", left: "0.12", right: "0.12.0", want: 0},
		{name: "release sorts after prerelease", left: "1.0.0", right: "1.0.0-rc.1", want: 1},
		{name: "prerelease sorts before release", left: "1.0.0-beta", right: "1.0.0", want: -1},
		{name: "prerelease lexicographic", left: "1.0.0-alpha", right: "1.0.0-beta", want: -1},
		{name: "leading v prefix ignored", left: "v1.2.3", right: "1.2.3", want: 0},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := CompareVersions(testCase.left, testCase.right); got != testCase.want {
				t.Fatalf(
					"CompareVersions(%q, %q) = %d, want %d",
					testCase.left,
					testCase.right,
					got,
					testCase.want,
				)
			}
		})
	}
}

// A settings document written before usage-report retention existed must still
// receive the conservative default so the scheduled cleanup bounds the ledger
// instead of the field decoding to zero and disabling the purge.
func TestRetentionApplyUsageReportDefaultsBackfillsLegacyDocument(t *testing.T) {
	t.Parallel()

	var settings Settings
	if err := json.Unmarshal([]byte(`{"retention":{"conversation_days":30}}`), &settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if settings.Retention.UsageReportConfigured() {
		t.Fatal("legacy document must not report the field as explicitly configured")
	}
	settings.Retention.ApplyUsageReportDefaults()
	if settings.Retention.UsageReportDays != DefaultUsageReportRetentionDays {
		t.Fatalf(
			"usage report days = %d, want %d",
			settings.Retention.UsageReportDays,
			DefaultUsageReportRetentionDays,
		)
	}
}

// An explicit zero (or any explicit value) must survive decoding so an update
// is not silently rewritten by the defaulting helper.
func TestRetentionApplyUsageReportDefaultsPreservesExplicitValue(t *testing.T) {
	t.Parallel()

	var settings Settings
	if err := json.Unmarshal([]byte(`{"retention":{"usage_report_days":0}}`), &settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if !settings.Retention.UsageReportConfigured() {
		t.Fatal("explicit usage_report_days must be reported as configured")
	}
	settings.Retention.ApplyUsageReportDefaults()
	if settings.Retention.UsageReportDays != 0 {
		t.Fatalf("explicit zero was overwritten with %d", settings.Retention.UsageReportDays)
	}
}

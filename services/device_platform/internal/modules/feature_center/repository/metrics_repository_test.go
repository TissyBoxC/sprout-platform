package repository

import (
	"context"
	"strings"
	"testing"
)

func TestOfflinePolicyDeviceMetricRequiresCurrentAppliedRevision(t *testing.T) {
	for _, required := range []string{
		"device_bindings",
		"content_offline_policy_acks",
		"policy.offline_enabled = TRUE",
		"acks.applied_revision = policy.revision",
	} {
		if !strings.Contains(offlinePolicyDeviceMetricQuery, required) {
			t.Fatalf("metric query is missing %q", required)
		}
	}

	repository := &MetricsRepository{}
	// A nil pool must fail before returning a fabricated zero. This also keeps
	// the test independent of a live PostgreSQL instance.
	if _, err := repository.count(
		context.Background(),
		offlinePolicyDeviceMetricQuery,
	); err == nil {
		t.Fatal("offline policy metric without a database returned a value")
	}
}

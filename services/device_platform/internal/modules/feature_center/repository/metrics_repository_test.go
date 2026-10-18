package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
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

func TestOTAPublishedMetricUsesOTATable(t *testing.T) {
	if !strings.Contains(otaPublishedMetricQuery, "FROM ota_releases") {
		t.Fatalf("OTA metric must read ota_releases: %s", otaPublishedMetricQuery)
	}
	if strings.Contains(otaPublishedMetricQuery, "platform_releases") {
		t.Fatalf(
			"OTA metric must not read the unrelated operations table: %s",
			otaPublishedMetricQuery,
		)
	}
	if !strings.Contains(otaPublishedMetricQuery, "status = 'published'") {
		t.Fatalf("OTA metric must count only published releases: %s", otaPublishedMetricQuery)
	}
}

func TestTelemetryMetricUsesConfiguredOfflineThreshold(t *testing.T) {
	repository := NewMetricsRepository(nil, 137*time.Second)
	if repository.onlineThreshold != 137*time.Second {
		t.Fatalf(
			"online threshold = %s, want 137s",
			repository.onlineThreshold,
		)
	}
	if strings.Contains(telemetryMetricQuery, "90 seconds") {
		t.Fatalf("telemetry metric still hard-codes the offline threshold")
	}
	if !strings.Contains(telemetryMetricQuery, "$1::interval") {
		t.Fatalf("telemetry metric must bind the configured threshold")
	}
	if _, err := repository.triadWithArgs(
		context.Background(),
		telemetryMetricQuery,
		[]any{fmt.Sprintf(
			"%d milliseconds",
			repository.onlineThreshold.Milliseconds(),
		)},
		"telemetry_device_count",
		"telemetry_online_count",
		"telemetry_pending_total",
	); err == nil {
		t.Fatal("telemetry metric without a database returned a value")
	}
}

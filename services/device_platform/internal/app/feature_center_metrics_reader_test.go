package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	releaseStoreDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/domain"
	serviceVersionRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/repository"
	serviceVersionService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/service"
)

type stubMetricsRepository struct {
	values map[string]map[string]any
}

func (stub stubMetricsRepository) FeatureMetrics(
	_ context.Context,
	featureID string,
) (map[string]any, error) {
	return stub.values[featureID], nil
}

type stubReleaseStore struct {
	status releaseStoreDomain.IndexStatus
	err    error
}

func (stub stubReleaseStore) IndexStatus(
	context.Context,
) (releaseStoreDomain.IndexStatus, error) {
	return stub.status, stub.err
}

type stubNotificationChannels struct {
	channels []string
}

func (stub stubNotificationChannels) DeliveryChannels() []string {
	return append([]string(nil), stub.channels...)
}

func TestFeatureCenterMetricsReaderMergesOutdatedServices(t *testing.T) {
	stateDir := t.TempDir()
	snapshot := `{
		"generated_at": "2026-10-12T17:18:32Z",
		"services": [
			{"id": "device_platform", "status": "outdated"},
			{"id": "voice_gateway", "status": "outdated"},
			{"id": "admin_web", "status": "current"}
		]
	}`
	if err := os.WriteFile(
		filepath.Join(stateDir, "status.json"),
		[]byte(snapshot),
		0o600,
	); err != nil {
		t.Fatalf("write status snapshot: %v", err)
	}
	serviceVersions, err := serviceVersionService.New(serviceVersionService.Options{
		Repository: serviceVersionRepository.NewFileRepository(stateDir),
	})
	if err != nil {
		t.Fatalf("create service version service: %v", err)
	}
	reader := featureCenterMetricsReader{
		repository:      stubMetricsRepository{},
		serviceVersions: serviceVersions,
	}
	values, err := reader.FeatureMetrics(context.Background(), "service_version")
	if err != nil {
		t.Fatalf("read service_version metrics: %v", err)
	}
	if values["outdated_service_count"] != 2 {
		t.Fatalf(
			"outdated_service_count = %#v, want 2",
			values["outdated_service_count"],
		)
	}
}

func TestFeatureCenterMetricsReaderLeavesOutdatedUnreportedWithoutWorkerState(
	t *testing.T,
) {
	// An empty state directory means the worker has not written status.json yet.
	base := map[string]map[string]any{"service_version": {"other_metric": 1}}
	serviceVersions, err := serviceVersionService.New(serviceVersionService.Options{
		Repository: serviceVersionRepository.NewFileRepository(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("create service version service: %v", err)
	}
	reader := featureCenterMetricsReader{
		repository:      stubMetricsRepository{values: base},
		serviceVersions: serviceVersions,
	}
	values, err := reader.FeatureMetrics(context.Background(), "service_version")
	if err != nil {
		t.Fatalf("read service_version metrics: %v", err)
	}
	if _, ok := values["outdated_service_count"]; ok {
		t.Fatalf(
			"outdated_service_count must stay unreported, got %#v",
			values["outdated_service_count"],
		)
	}
	if values["other_metric"] != 1 {
		t.Fatalf("base metric was dropped: %#v", values)
	}
}

func TestFeatureCenterMetricsReaderMergesDownloadFileCount(t *testing.T) {
	reader := featureCenterMetricsReader{
		repository: stubMetricsRepository{},
		releaseStore: stubReleaseStore{status: releaseStoreDomain.IndexStatus{
			IsAvailable:      true,
			IndexedFileCount: 12,
		}},
	}
	values, err := reader.FeatureMetrics(context.Background(), "download_server")
	if err != nil {
		t.Fatalf("read download_server metrics: %v", err)
	}
	if values["download_file_count"] != 12 {
		t.Fatalf(
			"download_file_count = %#v, want 12",
			values["download_file_count"],
		)
	}
}

func TestFeatureCenterMetricsReaderLeavesDownloadCountUnreportedWithoutIndex(
	t *testing.T,
) {
	reader := featureCenterMetricsReader{
		repository:   stubMetricsRepository{},
		releaseStore: stubReleaseStore{status: releaseStoreDomain.IndexStatus{IsAvailable: false}},
	}
	values, err := reader.FeatureMetrics(context.Background(), "download_server")
	if err != nil {
		t.Fatalf("read download_server metrics: %v", err)
	}
	if _, ok := values["download_file_count"]; ok {
		t.Fatalf(
			"download_file_count must stay unreported, got %#v",
			values["download_file_count"],
		)
	}
}

func TestFeatureCenterMetricsReaderReportsConfiguredNotificationChannels(
	t *testing.T,
) {
	reader := featureCenterMetricsReader{
		repository: stubMetricsRepository{},
		notifications: stubNotificationChannels{
			channels: []string{"push", "email"},
		},
	}
	values, err := reader.FeatureMetrics(context.Background(), "notification")
	if err != nil {
		t.Fatalf("read notification metrics: %v", err)
	}
	if values["notification_configured_channel_count"] != 2 {
		t.Fatalf(
			"notification_configured_channel_count = %#v, want 2",
			values["notification_configured_channel_count"],
		)
	}
}

func TestFeatureCenterMetricsReaderNotificationChannelsUnwired(t *testing.T) {
	reader := featureCenterMetricsReader{
		repository:    stubMetricsRepository{},
		notifications: stubNotificationChannels{},
	}
	values, err := reader.FeatureMetrics(context.Background(), "notification")
	if err != nil {
		t.Fatalf("read notification metrics: %v", err)
	}
	if values["notification_configured_channel_count"] != 0 {
		t.Fatalf(
			"notification_configured_channel_count = %#v, want 0",
			values["notification_configured_channel_count"],
		)
	}
}

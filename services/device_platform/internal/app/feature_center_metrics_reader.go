package app

import (
	"context"

	releaseStoreDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/domain"
	serviceVersionDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/domain"
	serviceVersionService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/service"
)

// featureCenterMetricsReader is the feature center's read-only metric source.
//
// Most metrics are database counts owned by their modules and are answered by
// the repository. A few live on a worker-produced file rather than in
// PostgreSQL, so this adapter merges those in without moving the SQL out of
// the repository layer.
type featureCenterMetricsReader struct {
	repository interface {
		FeatureMetrics(ctx context.Context, featureID string) (map[string]any, error)
	}
	serviceVersions *serviceVersionService.Service
	releaseStore    interface {
		IndexStatus(ctx context.Context) (releaseStoreDomain.IndexStatus, error)
	}
	notifications interface {
		DeliveryChannels() []string
	}
}

// FeatureMetrics returns the metric values for one feature. Keys with no
// verifiable source are omitted so the console renders "not reported".
func (reader featureCenterMetricsReader) FeatureMetrics(
	ctx context.Context,
	featureID string,
) (map[string]any, error) {
	values, err := reader.repository.FeatureMetrics(ctx, featureID)
	if err != nil {
		return nil, err
	}
	merged := values
	switch featureID {
	case "service_version":
		merged = reader.withOutdatedServiceCount(ctx, merged)
	case "deployment_infrastructure":
		merged = reader.withRunningServiceCount(ctx, merged)
	case "download_server":
		merged = reader.withDownloadFileCount(ctx, merged)
	case "notification":
		merged = reader.withConfiguredNotificationChannelCount(merged)
	}
	return merged, nil
}

// withConfiguredNotificationChannelCount reports how many external senders are
// actually wired. In-app delivery is built in and intentionally excluded.
func (reader featureCenterMetricsReader) withConfiguredNotificationChannelCount(
	values map[string]any,
) map[string]any {
	if reader.notifications == nil {
		return values
	}
	return mergeMetric(
		values,
		"notification_configured_channel_count",
		len(reader.notifications.DeliveryChannels()),
	)
}

// withOutdatedServiceCount merges the upgrade worker's live view. The worker
// state is optional infrastructure, so a read failure keeps the repository
// values and leaves the outdated count unreported rather than guessing.
func (reader featureCenterMetricsReader) withOutdatedServiceCount(
	ctx context.Context,
	values map[string]any,
) map[string]any {
	if reader.serviceVersions == nil {
		return values
	}
	result, err := reader.serviceVersions.SnapshotResult(ctx)
	if err != nil {
		return values
	}
	// Without a worker-written snapshot every service is projected as unknown.
	// Counting that fallback would report "0 outdated" and falsely claim the
	// fleet is current, so the count stays unreported until the worker reports.
	if result.StateSource != "worker" {
		return values
	}
	outdated := 0
	for _, service := range result.Services {
		if service.Status == serviceVersionDomain.ServiceStatusOutdated {
			outdated++
		}
	}
	return mergeMetric(values, "outdated_service_count", outdated)
}

// withRunningServiceCount reports how many managed services the worker has
// confirmed are running a known image. Only current and outdated services
// count: unknown services have not been observed, updating services are
// mid-swap, and a failed upgrade leaves the running image ambiguous. Without a
// worker-written snapshot the count stays unreported so the console never
// claims services are running when the state has never been read.
func (reader featureCenterMetricsReader) withRunningServiceCount(
	ctx context.Context,
	values map[string]any,
) map[string]any {
	if reader.serviceVersions == nil {
		return values
	}
	result, err := reader.serviceVersions.SnapshotResult(ctx)
	if err != nil {
		return values
	}
	if result.StateSource != "worker" {
		return values
	}
	running := 0
	for _, service := range result.Services {
		switch service.Status {
		case serviceVersionDomain.ServiceStatusCurrent,
			serviceVersionDomain.ServiceStatusOutdated:
			running++
		}
	}
	return mergeMetric(values, "running_service_count", running)
}

// withDownloadFileCount reports how many canonical release files are published
// in the download store. A missing index is a valid first-deployment state, so
// the count stays unreported until the store has a readable index.
func (reader featureCenterMetricsReader) withDownloadFileCount(
	ctx context.Context,
	values map[string]any,
) map[string]any {
	if reader.releaseStore == nil {
		return values
	}
	status, err := reader.releaseStore.IndexStatus(ctx)
	if err != nil || !status.IsAvailable {
		return values
	}
	return mergeMetric(values, "download_file_count", status.IndexedFileCount)
}

func mergeMetric(
	values map[string]any,
	key string,
	value any,
) map[string]any {
	merged := make(map[string]any, len(values)+1)
	for existingKey, existingValue := range values {
		merged[existingKey] = existingValue
	}
	merged[key] = value
	return merged
}

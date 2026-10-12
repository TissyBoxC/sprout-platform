package app

import (
	"context"

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
	if featureID != "service_version" || reader.serviceVersions == nil {
		return values, nil
	}
	snapshot, _, err := reader.serviceVersions.Snapshot(ctx)
	if err != nil {
		// The upgrade worker state is optional infrastructure. A read failure
		// must not blank the feature's other metrics, so keep the repository
		// values and leave the outdated count unreported.
		return values, nil
	}
	outdated := 0
	for _, service := range snapshot {
		if service.Status == serviceVersionDomain.ServiceStatusOutdated {
			outdated++
		}
	}
	merged := make(map[string]any, len(values)+1)
	for key, value := range values {
		merged[key] = value
	}
	merged["outdated_service_count"] = outdated
	return merged, nil
}

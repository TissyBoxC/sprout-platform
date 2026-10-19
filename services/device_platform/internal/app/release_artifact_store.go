package app

import (
	"context"

	operationsDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	releaseStoreService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/service"
)

// releaseArtifactStoreAdapter lets release lookup fall back to files already
// published by CI. Those files are real download artifacts but have no row in
// platform_releases until an operator registers a release.
type releaseArtifactStoreAdapter struct {
	store *releaseStoreService.Service
}

func (adapter releaseArtifactStoreAdapter) FindArtifact(
	ctx context.Context,
	version string,
	kind string,
	platform string,
	channel string,
) (*operationsDomain.ReleaseArtifact, error) {
	if adapter.store == nil {
		return nil, operationsDomain.ErrReleaseNotFound
	}
	artifact, err := adapter.store.FindArtifact(ctx, version, kind, platform, channel)
	if err != nil {
		return nil, operationsDomain.ErrReleaseNotFound
	}
	return &operationsDomain.ReleaseArtifact{
		Version:     artifact.Version,
		Channel:     channel,
		Kind:        artifact.Kind,
		Platform:    artifact.Platform,
		DownloadURL: artifact.DownloadURL,
		SHA256:      artifact.SHA256,
	}, nil
}

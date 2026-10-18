package app

import (
	"context"
	"errors"

	otaDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
	releaseStoreDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/domain"
	releaseStoreService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/service"
)

// otaArtifactVerifier verifies the exact file that a signed OTA manifest
// advertises before the release can be stored or published.
type otaArtifactVerifier struct {
	store *releaseStoreService.Service
}

func (verifier otaArtifactVerifier) VerifyArtifact(
	ctx context.Context,
	artifactKey string,
) (int64, string, error) {
	if verifier.store == nil {
		return 0, "", otaDomain.ErrArtifactUnavailable
	}
	file, err := verifier.store.ReadArtifact(ctx, artifactKey)
	if errors.Is(err, releaseStoreDomain.ErrFileNotFound) ||
		errors.Is(err, releaseStoreDomain.ErrPathInvalid) {
		return 0, "", otaDomain.ErrArtifactMissing
	}
	if err != nil {
		return 0, "", otaDomain.ErrArtifactUnavailable
	}
	return file.SizeBytes, file.SHA256, nil
}

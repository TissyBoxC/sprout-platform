package app

import (
	"context"
	"strings"

	contentdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	releaseStoreService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/service"
)

// contentAssetReader adapts the release-store inventory to the content
// module's narrow asset contract.
//
// The release store remains the only component that reads the shared volume.
// This adapter performs no writes and only exposes the computed digest and
// public URL needed by the publishing workflow.
type contentAssetReader struct {
	store *releaseStoreService.Service
}

func (reader contentAssetReader) FindContentAsset(
	ctx context.Context,
	assetKey string,
) (*contentdomain.Asset, error) {
	if reader.store == nil {
		return nil, contentdomain.ErrAssetNotFound
	}
	normalized := strings.TrimPrefix(strings.TrimSpace(assetKey), "/")
	if normalized == "" {
		return nil, contentdomain.ErrAssetNotFound
	}
	inventory, err := reader.store.Inventory(ctx)
	if err != nil {
		return nil, contentdomain.ErrAssetNotFound
	}
	for _, file := range inventory.Files {
		if file.RelativePath != normalized {
			continue
		}
		return &contentdomain.Asset{
			Key:         file.RelativePath,
			SHA256:      file.SHA256,
			SizeBytes:   file.SizeBytes,
			DownloadURL: file.DownloadURL,
		}, nil
	}
	return nil, contentdomain.ErrAssetNotFound
}

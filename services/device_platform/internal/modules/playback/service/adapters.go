package service

import (
	"context"
	"errors"

	contentdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	contentrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/repository"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
)

// BindingDeviceReader adapts the device-binding service to the narrow
// DeviceOwnerReader the playback service depends on.
type BindingDeviceReader struct {
	Service *bindingservice.Service
}

// FamilyDeviceIDs returns the device ids bound to one family.
func (reader BindingDeviceReader) FamilyDeviceIDs(
	ctx context.Context,
	familyID string,
) ([]string, error) {
	if reader.Service == nil {
		return nil, errors.New("device binding service is unavailable")
	}
	bindings, err := reader.Service.List(ctx, familyID)
	if err != nil {
		return nil, err
	}
	deviceIDs := make([]string, 0, len(bindings))
	for index := range bindings {
		deviceIDs = append(deviceIDs, bindings[index].DeviceID)
	}
	return deviceIDs, nil
}

// ContentPackageReader adapts the content repository so the playback service
// can confirm a package is deliverable without depending on the whole content
// service.
type ContentPackageReader struct {
	Repository contentrepository.Repository
}

// IsPublished reports whether the package has a published version.
func (reader ContentPackageReader) IsPublished(
	ctx context.Context,
	packageID string,
) (bool, error) {
	if reader.Repository == nil {
		return false, errors.New("content repository is unavailable")
	}
	_, err := reader.Repository.PublishedDownload(ctx, packageID)
	if errors.Is(err, contentdomain.ErrPackageNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

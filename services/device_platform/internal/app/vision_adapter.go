package app

import (
	"context"
	"errors"
	"strings"

	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	privacyservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/service"
)

// visionBindingReader resolves a device's durable binding so the vision module
// can enforce guardian ownership and consent.
type visionBindingReader interface {
	GetByDeviceID(
		ctx context.Context,
		deviceID string,
	) (*bindingdomain.Binding, error)
}

// visionConsentReader adapts the privacy service to the narrow vision
// consent and ownership surface. Ownership is resolved from the durable
// device binding, and consent from the owning guardian's privacy record.
type visionConsentReader struct {
	bindings visionBindingReader
	privacy  *privacyservice.Service
}

// ImageUploadConsentForDevice reports whether the guardian who owns the device
// currently allows image upload. Unknown devices and errors deny consent.
func (reader visionConsentReader) ImageUploadConsentForDevice(
	ctx context.Context,
	deviceID string,
) (bool, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return false, nil
	}
	binding, err := reader.bindings.GetByDeviceID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, bindingdomain.ErrDeviceNotFound) {
			return false, nil
		}
		return false, err
	}
	return reader.privacy.ImageConsentForParent(ctx, binding.ParentAccountID)
}

// OwnsDevice reports whether the device is bound to the given guardian.
func (reader visionConsentReader) OwnsDevice(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
) (bool, error) {
	binding, err := reader.bindings.GetByDeviceID(ctx, strings.TrimSpace(deviceID))
	if err != nil {
		if errors.Is(err, bindingdomain.ErrDeviceNotFound) {
			return false, nil
		}
		return false, err
	}
	return binding.ParentAccountID == strings.TrimSpace(parentAccountID), nil
}

// Package service contains the guardian Bluetooth audio policy and device
// status use cases.
//
// The policy is per family: the guardian reads and edits one version and every
// bound device reads the same revision over its device session. The service
// validates every payload before it touches the store, and the device status
// path only ever exposes bounded scalars, never audio or peer addresses.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/bluetooth_audio/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	RecordStatus(ctx context.Context, status domain.Status) error
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// Service owns Bluetooth audio policy reads, publishes and device status.
type Service struct {
	repository Repository
}

const capabilityFreshness = 24 * time.Hour

// Options contains Bluetooth audio service dependencies.
type Options struct {
	Repository Repository
}

// New creates the Bluetooth audio service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("bluetooth audio repository is required")
	}
	return &Service{repository: options.Repository}, nil
}

// Catalog returns the preferred codec, active codec, adapter state and
// disconnect reason catalogs.
func (s *Service) Catalog() ([]string, []string, []string, []string) {
	return domain.PreferredCodecs,
		domain.ActiveCodecs,
		domain.AdapterStates,
		domain.DisconnectReasons
}

// Current returns the family policy or the default when none is set.
func (s *Service) Current(
	ctx context.Context,
	familyID string,
) (domain.Policy, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.Policy{}, domain.ErrInvalidPolicy
	}
	return s.repository.Current(ctx, familyID)
}

// Publish validates and stores a new family policy revision.
func (s *Service) Publish(
	ctx context.Context,
	familyID string,
	input domain.Input,
) (domain.Policy, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.Policy{}, domain.ErrInvalidPolicy
	}
	normalized, err := domain.ValidateInput(input)
	if err != nil {
		return domain.Policy{}, err
	}
	return s.repository.Save(ctx, familyID, normalized)
}

// Capability returns the Bluetooth audio capability for one device in a family.
func (s *Service) Capability(
	ctx context.Context,
	familyID string,
	deviceID string,
) (domain.Capability, error) {
	familyID = strings.TrimSpace(familyID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || deviceID == "" {
		return domain.Capability{}, domain.ErrInvalidInput
	}
	policy, err := s.repository.Current(ctx, familyID)
	if err != nil {
		return domain.Capability{}, err
	}
	statuses, err := s.repository.Statuses(ctx, familyID)
	if err != nil {
		return domain.Capability{}, err
	}
	for _, status := range statuses {
		if status.DeviceID != deviceID {
			continue
		}
		return domain.Capability{
			Supported:       status.BluetoothSupported,
			SpeakerMode:     status.SpeakerMode,
			AdapterState:    status.AdapterState,
			ActiveCodec:     status.ActiveCodec,
			VolumePercent:   status.VolumePercent,
			SchemaVersion:   domain.SchemaVersion,
			PolicyRevision:  policy.Revision,
			AppliedRevision: status.PolicyRevision,
			UpToDate:        status.PolicyRevision >= policy.Revision,
		}, nil
	}
	return domain.Capability{
		Supported:      false,
		AdapterState:   domain.AdapterOff,
		ActiveCodec:    domain.ActiveCodecUnknown,
		SchemaVersion:  domain.SchemaVersion,
		PolicyRevision: policy.Revision,
	}, nil
}

// ReportStatus stores one bounded device adapter status.
func (s *Service) ReportStatus(
	ctx context.Context,
	familyID string,
	deviceID string,
	input domain.StatusInput,
) (domain.Status, error) {
	familyID = strings.TrimSpace(familyID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || deviceID == "" {
		return domain.Status{}, domain.ErrInvalidInput
	}
	normalized, err := domain.ValidateStatus(input)
	if err != nil {
		return domain.Status{}, err
	}
	status := domain.Status{
		DeviceID:             deviceID,
		FamilyID:             familyID,
		BluetoothSupported:   normalized.BluetoothSupported,
		SpeakerMode:          normalized.SpeakerMode,
		AdapterState:         normalized.AdapterState,
		PairedDeviceCount:    normalized.PairedDeviceCount,
		ConnectedDeviceCount: normalized.ConnectedDeviceCount,
		ActiveCodec:          normalized.ActiveCodec,
		VolumePercent:        normalized.VolumePercent,
		LastDisconnectReason: normalized.LastDisconnectReason,
		PolicyRevision:       normalized.PolicyRevision,
	}
	if err := s.repository.RecordStatus(ctx, status); err != nil {
		return domain.Status{}, err
	}
	return status, nil
}

// DeviceStatuses returns each staged device status with an up-to-date flag.
func (s *Service) DeviceStatuses(
	ctx context.Context,
	familyID string,
) ([]domain.DeviceStatus, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidInput
	}
	policy, err := s.repository.Current(ctx, familyID)
	if err != nil {
		return nil, err
	}
	statuses, err := s.repository.Statuses(ctx, familyID)
	if err != nil {
		return nil, err
	}
	devices := make([]domain.DeviceStatus, 0, len(statuses))
	for _, status := range statuses {
		devices = append(devices, domain.DeviceStatus{
			DeviceID:             status.DeviceID,
			FamilyID:             status.FamilyID,
			BluetoothSupported:   status.BluetoothSupported,
			SpeakerMode:          status.SpeakerMode,
			AdapterState:         status.AdapterState,
			PairedDeviceCount:    status.PairedDeviceCount,
			ConnectedDeviceCount: status.ConnectedDeviceCount,
			ActiveCodec:          status.ActiveCodec,
			VolumePercent:        status.VolumePercent,
			LastDisconnectReason: status.LastDisconnectReason,
			UpToDate:             status.PolicyRevision >= policy.Revision,
			ReportedAt:           status.ReportedAt,
		})
	}
	return devices, nil
}

// AdminStats returns a bounded, windowed fleet summary.
func (s *Service) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	if windowDays <= 0 || windowDays > 90 {
		windowDays = 7
	}
	return s.repository.AdminStats(ctx, windowDays)
}

// CheckReadiness returns aggregate Bluetooth audio health for active devices
// that advertise the capability.
func (s *Service) CheckReadiness(
	ctx context.Context,
) (featurehealth.DeviceCapabilityReadiness, error) {
	reader, ok := s.repository.(interface {
		Readiness(
			context.Context,
			time.Duration,
		) (featurehealth.DeviceCapabilityReadiness, error)
	})
	if !ok {
		return featurehealth.DeviceCapabilityReadiness{
			Status: featurehealth.StatusUnavailable,
		}, nil
	}
	return reader.Readiness(ctx, capabilityFreshness)
}

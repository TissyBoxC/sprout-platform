// Package service contains the guardian power policy and device status use
// cases.
//
// The policy is per family: the guardian reads and edits one version and every
// bound device reads the same revision. The service validates the payload before
// it touches the store, and the device status path only ever exposes bounded
// scalars, never raw gauge telemetry.
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/power/domain"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	RecordStatus(ctx context.Context, status domain.Status) error
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// Service owns power policy reads, publishes, and status reports.
type Service struct {
	repository Repository
}

// Options contains power service dependencies.
type Options struct {
	Repository Repository
}

// New creates the power service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("power repository is required")
	}
	return &Service{repository: options.Repository}, nil
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

// Capability returns the battery capability for one device in a family.
//
// A device that never reported a power status is reported as unsupported, so
// the guardian UI can hide battery settings instead of showing controls that do
// nothing.
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
			Supported:       status.ChargeState != domain.ChargeUnknown,
			ChargeState:     string(status.ChargeState),
			PowerMode:       string(status.PowerMode),
			BatteryPercent:  status.BatteryPercent,
			Charging:        status.Charging,
			LowWarning:      status.BatteryPercent <= policy.LowBatteryWarnPercent,
			SchemaVersion:   domain.SchemaVersion,
			PolicyRevision:  policy.Revision,
			AppliedRevision: status.PolicyRevision,
			UpToDate:        status.PolicyRevision >= policy.Revision,
		}, nil
	}
	return domain.Capability{
		Supported:      false,
		SchemaVersion:  domain.SchemaVersion,
		PolicyRevision: policy.Revision,
	}, nil
}

// ReportStatus stores one bounded device power status.
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
		DeviceID:       deviceID,
		FamilyID:       familyID,
		ChargeState:    normalized.ChargeState,
		PowerMode:      normalized.PowerMode,
		BatteryPercent: normalized.BatteryPercent,
		Millivolts:     normalized.Millivolts,
		Charging:       normalized.Charging,
		PolicyRevision: normalized.PolicyRevision,
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
			DeviceID:       status.DeviceID,
			FamilyID:       status.FamilyID,
			ChargeState:    string(status.ChargeState),
			PowerMode:      string(status.PowerMode),
			BatteryPercent: status.BatteryPercent,
			Charging:       status.Charging,
			UpToDate:       status.PolicyRevision >= policy.Revision,
			ReportedAt:     status.ReportedAt,
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

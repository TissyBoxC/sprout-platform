// Package service contains the device form-factor use cases.
//
// A device reports its capability mask; the service composes the action mask
// from it and stores the profile. The guardian and admin surfaces only ever
// read composed, bounded profiles, never raw hardware detail.
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/form_factor/domain"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	Record(ctx context.Context, profile domain.Profile) error
	Profile(ctx context.Context, familyID string, deviceID string) (domain.Profile, bool, error)
	Statuses(ctx context.Context, familyID string) ([]domain.Profile, error)
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// Service owns form-factor reports and reads.
type Service struct {
	repository Repository
}

// Options contains form-factor service dependencies.
type Options struct {
	Repository Repository
}

// New creates the form-factor service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("form factor repository is required")
	}
	return &Service{repository: options.Repository}, nil
}

// Report validates one device report, composes the action mask and stores it.
func (s *Service) Report(
	ctx context.Context,
	familyID string,
	deviceID string,
	input domain.Input,
) (domain.Profile, error) {
	familyID = strings.TrimSpace(familyID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || deviceID == "" {
		return domain.Profile{}, domain.ErrInvalidProfile
	}
	normalized, err := domain.ValidateInput(input)
	if err != nil {
		return domain.Profile{}, err
	}
	profile := domain.Profile{
		DeviceID:       deviceID,
		FamilyID:       familyID,
		TechnicalName:  strings.TrimSpace(normalized.TechnicalName),
		CapabilityMask: normalized.CapabilityMask,
		ActionMask:     domain.ComposeActions(normalized.CapabilityMask),
		SchemaVersion:  normalized.SchemaVersion,
	}
	if err := s.repository.Record(ctx, profile); err != nil {
		return domain.Profile{}, err
	}
	return profile, nil
}

// Capability returns the composed form-factor capability for one device.
func (s *Service) Capability(
	ctx context.Context,
	familyID string,
	deviceID string,
) (domain.CapabilitySummary, error) {
	familyID = strings.TrimSpace(familyID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || deviceID == "" {
		return domain.CapabilitySummary{}, domain.ErrInvalidProfile
	}
	profile, ok, err := s.repository.Profile(ctx, familyID, deviceID)
	if err != nil {
		return domain.CapabilitySummary{}, err
	}
	if !ok {
		return domain.CapabilitySummary{SchemaVersion: domain.SchemaVersion}, nil
	}
	return domain.CapabilitySummary{
		Supported:      true,
		TechnicalName:  profile.TechnicalName,
		CapabilityMask: profile.CapabilityMask,
		ActionMask:     profile.ActionMask,
		Capabilities:   domain.Capabilities(profile.CapabilityMask),
		Actions:        domain.Actions(profile.ActionMask),
		SchemaVersion:  profile.SchemaVersion,
	}, nil
}

// DeviceProfiles returns each staged device profile in a family.
func (s *Service) DeviceProfiles(
	ctx context.Context,
	familyID string,
) ([]domain.DeviceProfile, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidProfile
	}
	profiles, err := s.repository.Statuses(ctx, familyID)
	if err != nil {
		return nil, err
	}
	devices := make([]domain.DeviceProfile, 0, len(profiles))
	for _, profile := range profiles {
		devices = append(devices, domain.DeviceProfile{
			DeviceID:       profile.DeviceID,
			FamilyID:       profile.FamilyID,
			TechnicalName:  profile.TechnicalName,
			CapabilityMask: profile.CapabilityMask,
			ActionMask:     profile.ActionMask,
			Capabilities:   domain.Capabilities(profile.CapabilityMask),
			Actions:        domain.Actions(profile.ActionMask),
			SchemaVersion:  profile.SchemaVersion,
			ReportedAt:     profile.ReportedAt,
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

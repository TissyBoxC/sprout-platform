// Package service contains the guardian learning-visualization policy and
// device status use cases.
//
// The policy is per family: the guardian reads and edits one version and every
// bound device reads the same revision over its device session. The service
// validates every payload before it touches the store, and the device status
// path only ever exposes bounded scalars, never frame buffers or touch traces.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/learning_visualization/domain"
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

// Service owns learning-visualization policy reads, publishes and device
// status.
type Service struct {
	repository Repository
}

const capabilityFreshness = 24 * time.Hour

// Options contains learning-visualization service dependencies.
type Options struct {
	Repository Repository
}

// New creates the learning-visualization service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("learning visualization repository is required")
	}
	return &Service{repository: options.Repository}, nil
}

// Catalog returns the fixed feedback-style, animation and state catalogs.
func (s *Service) Catalog() ([]string, []string, []string) {
	return domain.FeedbackStyles, domain.AnimationLevels, domain.VisualStates
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

// Capability returns the visualization capability for one device in a family.
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
			Supported:       status.VisualizationSupported,
			TouchSupported:  status.TouchSupported,
			LinkedLessonID:  status.LinkedLessonID,
			VisualState:     status.VisualState,
			FeedbackStyle:   status.FeedbackStyle,
			AnimationLevel:  status.AnimationLevel,
			SchemaVersion:   domain.SchemaVersion,
			PolicyRevision:  policy.Revision,
			AppliedRevision: status.PolicyRevision,
			UpToDate:        status.PolicyRevision >= policy.Revision,
		}, nil
	}
	return domain.Capability{
		Supported:      false,
		VisualState:    domain.VisualIdle,
		FeedbackStyle:  domain.StyleNeutral,
		AnimationLevel: domain.AnimationOff,
		SchemaVersion:  domain.SchemaVersion,
		PolicyRevision: policy.Revision,
	}, nil
}

// ReportStatus stores one bounded device visualization status.
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
		DeviceID:               deviceID,
		FamilyID:               familyID,
		VisualizationSupported: normalized.VisualizationSupported,
		TouchSupported:         normalized.TouchSupported,
		LinkedLessonID:         normalized.LinkedLessonID,
		VisualState:            normalized.VisualState,
		FeedbackStyle:          normalized.FeedbackStyle,
		AnimationLevel:         normalized.AnimationLevel,
		FeedbackEvents:         normalized.FeedbackEvents,
		SessionMinutes:         normalized.SessionMinutes,
		PolicyRevision:         normalized.PolicyRevision,
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
			DeviceID:               status.DeviceID,
			FamilyID:               status.FamilyID,
			VisualizationSupported: status.VisualizationSupported,
			TouchSupported:         status.TouchSupported,
			LinkedLessonID:         status.LinkedLessonID,
			VisualState:            status.VisualState,
			FeedbackStyle:          status.FeedbackStyle,
			AnimationLevel:         status.AnimationLevel,
			FeedbackEvents:         status.FeedbackEvents,
			SessionMinutes:         status.SessionMinutes,
			UpToDate:               status.PolicyRevision >= policy.Revision,
			ReportedAt:             status.ReportedAt,
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

// CheckReadiness returns aggregate learning-visualization health for active
// devices that advertise a display.
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

// Package service contains the guardian screen policy, point-to-read
// dictionary, device capability and video playback use cases.
//
// The policy is per family: the guardian reads and edits one version and every
// bound device reads the same revision. The service validates every payload
// before it touches the store, and the device status path only ever exposes
// bounded scalars, never pixels or screenshots.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/screen/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/google/uuid"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	Current(ctx context.Context, familyID string) (domain.Policy, error)
	Save(ctx context.Context, familyID string, input domain.Input) (domain.Policy, error)
	RecordStatus(ctx context.Context, status domain.Status) error
	Statuses(ctx context.Context, familyID string) ([]domain.Status, error)

	Entries(ctx context.Context, familyID string) ([]domain.DictionaryEntry, error)
	EntryByTerm(ctx context.Context, familyID string, term string) (domain.DictionaryEntry, bool, error)
	SaveEntry(ctx context.Context, entry domain.DictionaryEntry) error
	DeleteEntry(ctx context.Context, familyID string, entryID string) error
	EntryCount(ctx context.Context, familyID string) (int, error)

	Playback(ctx context.Context, familyID string, deviceID string) (domain.PlaybackPlan, bool, error)
	SavePlayback(ctx context.Context, familyID string, deviceID string, plan domain.PlaybackPlan) error

	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// Service owns screen policy reads, publishes, dictionary and playback.
type Service struct {
	repository Repository
}

const capabilityFreshness = 24 * time.Hour

// Options contains screen service dependencies.
type Options struct {
	Repository Repository
}

// New creates the screen service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("screen repository is required")
	}
	return &Service{repository: options.Repository}, nil
}

// Catalog returns the fixed expression and animation catalogs.
func (s *Service) Catalog() ([]string, []string) {
	return domain.Expressions, domain.Animations
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

// Capability returns the screen capability for one device in a family.
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
			Supported:            status.ScreenSupported,
			ExpressionsSupported: status.ExpressionsSupported,
			AnimationSupported:   status.AnimationSupported,
			PointReadSupported:   status.PointReadSupported,
			VideoSupported:       status.VideoSupported,
			SchemaVersion:        domain.SchemaVersion,
			PolicyRevision:       policy.Revision,
			AppliedRevision:      status.PolicyRevision,
			UpToDate:             status.PolicyRevision >= policy.Revision,
		}, nil
	}
	return domain.Capability{
		Supported:      false,
		SchemaVersion:  domain.SchemaVersion,
		PolicyRevision: policy.Revision,
	}, nil
}

// ReportStatus stores one bounded device screen status.
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
		ScreenSupported:      normalized.ScreenSupported,
		ExpressionsSupported: normalized.ExpressionsSupported,
		AnimationSupported:   normalized.AnimationSupported,
		PointReadSupported:   normalized.PointReadSupported,
		VideoSupported:       normalized.VideoSupported,
		Expression:           normalized.Expression,
		Animation:            normalized.Animation,
		VideoState:           normalized.VideoState,
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
			DeviceID:         status.DeviceID,
			FamilyID:         status.FamilyID,
			ScreenSupported:  status.ScreenSupported,
			PointReadSupport: status.PointReadSupported,
			VideoSupported:   status.VideoSupported,
			Expression:       status.Expression,
			VideoState:       status.VideoState,
			UpToDate:         status.PolicyRevision >= policy.Revision,
			ReportedAt:       status.ReportedAt,
		})
	}
	return devices, nil
}

// Dictionary returns the family point-to-read dictionary.
func (s *Service) Dictionary(
	ctx context.Context,
	familyID string,
) ([]domain.DictionaryEntry, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidDictionaryEntry
	}
	return s.repository.Entries(ctx, familyID)
}

// PutEntry validates and stores one point-to-read entry, replacing a duplicate
// term and refusing a dictionary already at its entry bound.
func (s *Service) PutEntry(
	ctx context.Context,
	familyID string,
	input domain.DictionaryInput,
) (domain.DictionaryEntry, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.DictionaryEntry{}, domain.ErrInvalidDictionaryEntry
	}
	normalized, err := domain.ValidateDictionaryInput(input)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}
	existing, found, err := s.repository.EntryByTerm(ctx, familyID, normalized.Term)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}
	if !found {
		count, err := s.repository.EntryCount(ctx, familyID)
		if err != nil {
			return domain.DictionaryEntry{}, err
		}
		if count >= domain.PointReadCapacity {
			return domain.DictionaryEntry{}, domain.ErrDictionaryFull
		}
	}
	entry := domain.DictionaryEntry{
		ID:       uuid.NewString(),
		FamilyID: familyID,
		Term:     normalized.Term,
		Text:     normalized.Text,
	}
	if found {
		// A duplicate term replaces its text but keeps the stable id.
		entry.ID = existing.ID
		entry.CreatedAt = existing.CreatedAt
	}
	if err := s.repository.SaveEntry(ctx, entry); err != nil {
		return domain.DictionaryEntry{}, err
	}
	stored, found, err := s.repository.EntryByTerm(ctx, familyID, normalized.Term)
	if err != nil {
		return domain.DictionaryEntry{}, err
	}
	if found {
		return stored, nil
	}
	return entry, nil
}

// DeleteEntry removes one point-to-read entry.
func (s *Service) DeleteEntry(
	ctx context.Context,
	familyID string,
	entryID string,
) error {
	familyID = strings.TrimSpace(familyID)
	entryID = strings.TrimSpace(entryID)
	if familyID == "" || entryID == "" {
		return domain.ErrInvalidDictionaryEntry
	}
	return s.repository.DeleteEntry(ctx, familyID, entryID)
}

// ResolvePlayback clamps a requested video profile to the policy ceiling and
// stores the plan for the device.
func (s *Service) ResolvePlayback(
	ctx context.Context,
	familyID string,
	deviceID string,
	requestedBitrateKBS int,
	requestedFPS int,
) (domain.PlaybackPlan, error) {
	familyID = strings.TrimSpace(familyID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || deviceID == "" {
		return domain.PlaybackPlan{}, domain.ErrInvalidInput
	}
	policy, err := s.repository.Current(ctx, familyID)
	if err != nil {
		return domain.PlaybackPlan{}, err
	}
	if !policy.VideoEnabled {
		return domain.PlaybackPlan{}, domain.ErrInvalidPlayback
	}
	plan := domain.PlaybackPlan{
		BitrateKBS: clampInt(
			requestedBitrateKBS,
			domain.MinVideoMaxBitrateKBS,
			policy.VideoMaxBitrateKBS,
		),
		FPS: clampInt(requestedFPS, domain.MinVideoMaxFPS, policy.VideoMaxFPS),
	}
	if err := s.repository.SavePlayback(ctx, familyID, deviceID, plan); err != nil {
		return domain.PlaybackPlan{}, err
	}
	return plan, nil
}

// Playback returns the stored video plan for a device, or the policy default.
func (s *Service) Playback(
	ctx context.Context,
	familyID string,
	deviceID string,
) (domain.PlaybackPlan, error) {
	familyID = strings.TrimSpace(familyID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || deviceID == "" {
		return domain.PlaybackPlan{}, domain.ErrInvalidInput
	}
	plan, found, err := s.repository.Playback(ctx, familyID, deviceID)
	if err != nil {
		return domain.PlaybackPlan{}, err
	}
	if found {
		return plan, nil
	}
	policy, err := s.repository.Current(ctx, familyID)
	if err != nil {
		return domain.PlaybackPlan{}, err
	}
	return domain.PlaybackPlan{
		BitrateKBS: policy.VideoMaxBitrateKBS,
		FPS:        policy.VideoMaxFPS,
	}, nil
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

// CheckReadiness returns aggregate screen health for active devices that
// advertise a display.
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

func clampInt(value int, minimum int, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

// Package service contains parent policy use cases.
package service

import (
	"context"
	"errors"
	"regexp"
	"strings"

	childdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

var timePattern = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)

// DefaultDailyLimitMinutes is the conservative safety default for a new child.
const DefaultDailyLimitMinutes = 60

// DefaultMaxVolumePercent keeps first use quiet until a guardian changes it.
const DefaultMaxVolumePercent = 70

// Service owns time, content, and volume restrictions.
type Service struct {
	repository repository.Repository
	timeSource clock.Clock
}

// Options contains parent policy service dependencies.
type Options struct {
	Repository repository.Repository
	Clock      clock.Clock
}

// New creates the parent policy service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("parent policy repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{repository: options.Repository, timeSource: timeSource}, nil
}

// CreateDefaultForChild creates the conservative default policy a new child
// receives. A nil categories list falls back to every approved category.
func (s *Service) CreateDefaultForChild(
	ctx context.Context,
	familyID string,
	childID string,
	contentCategories []string,
) error {
	categories := normalizeCategories(contentCategories)
	if len(categories) == 0 {
		categories = append([]string(nil), childdomain.ContentCategories...)
	}
	now := s.timeSource.Now().UTC()
	policy := &domain.Policy{
		ID:                uuid.NewString(),
		FamilyID:          familyID,
		ChildID:           childID,
		PolicyVersion:     1,
		DailyLimitMinutes: DefaultDailyLimitMinutes,
		AllowedCategories: categories,
		DisabledPeriods:   []domain.DisabledPeriod{},
		MaxVolumePercent:  DefaultMaxVolumePercent,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	return s.repository.Create(ctx, policy)
}

// DeleteForChild removes one dependent policy.
func (s *Service) DeleteForChild(ctx context.Context, childID string) error {
	return s.repository.DeleteByChildID(ctx, childID)
}

// Get returns one policy owned by the authenticated family.
func (s *Service) Get(
	ctx context.Context,
	familyID string,
	childID string,
) (*domain.Policy, error) {
	return s.repository.GetByFamilyID(ctx, familyID, childID)
}

// List returns every policy owned by one guardian.
func (s *Service) List(
	ctx context.Context,
	familyID string,
) ([]domain.Policy, error) {
	return s.repository.ListByFamilyID(ctx, familyID)
}

// Update validates and replaces one policy, bumping its version so devices
// can detect that their cached limits are stale.
func (s *Service) Update(
	ctx context.Context,
	familyID string,
	childID string,
	input domain.PolicyInput,
) (*domain.Policy, error) {
	policy, err := s.repository.GetByFamilyID(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	categories, err := validateCategories(input.AllowedCategories)
	if err != nil {
		return nil, err
	}
	if input.DailyLimitMinutes < 0 || input.DailyLimitMinutes > 720 {
		return nil, domain.ErrInvalidDailyLimit
	}
	if input.MaxVolumePercent < 0 || input.MaxVolumePercent > 100 {
		return nil, domain.ErrInvalidVolume
	}
	periods, err := validatePeriods(input.DisabledPeriods)
	if err != nil {
		return nil, err
	}
	policy.PolicyVersion++
	policy.DailyLimitMinutes = input.DailyLimitMinutes
	policy.AllowedCategories = categories
	policy.DisabledPeriods = periods
	policy.MaxVolumePercent = input.MaxVolumePercent
	policy.UpdatedAt = s.timeSource.Now().UTC()
	if err := s.repository.Update(ctx, policy); err != nil {
		return nil, err
	}
	return policy, nil
}

func normalizeCategories(values []string) []string {
	categories, err := validateCategories(values)
	if err != nil {
		return []string{}
	}
	return categories
}

func validateCategories(values []string) ([]string, error) {
	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		category := strings.TrimSpace(value)
		if !isApprovedCategory(category) {
			return nil, domain.ErrInvalidCategories
		}
		if _, exists := seen[category]; exists {
			continue
		}
		seen[category] = struct{}{}
		unique = append(unique, category)
	}
	if len(unique) == 0 {
		return nil, domain.ErrInvalidCategories
	}
	return unique, nil
}

func validatePeriods(values []domain.DisabledPeriod) ([]domain.DisabledPeriod, error) {
	periods := make([]domain.DisabledPeriod, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		start := strings.TrimSpace(value.StartTime)
		end := strings.TrimSpace(value.EndTime)
		if !timePattern.MatchString(start) || !timePattern.MatchString(end) ||
			start == end {
			return nil, domain.ErrInvalidDisabledHours
		}
		key := start + "-" + end
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		periods = append(periods, domain.DisabledPeriod{
			StartTime: start,
			EndTime:   end,
		})
	}
	return periods, nil
}

func isApprovedCategory(category string) bool {
	for _, approved := range childdomain.ContentCategories {
		if category == approved {
			return true
		}
	}
	return false
}

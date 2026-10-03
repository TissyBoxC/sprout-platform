// Package service contains child profile use cases.
package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

var interestPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// PolicyProvisioner creates the default policy for a newly created child.
// Keeping this dependency narrow lets the child module stay removable.
type PolicyProvisioner interface {
	CreateDefaultForChild(
		ctx context.Context,
		familyID string,
		childID string,
		contentCategories []string,
	) error
	DeleteForChild(ctx context.Context, childID string) error
	SyncDefaultCategories(
		ctx context.Context,
		familyID string,
		childID string,
		contentCategories []string,
	) error
}

// Service owns child profile validation and persistence.
type Service struct {
	repository        repository.Repository
	policyProvisioner PolicyProvisioner
	timeSource        clock.Clock
}

// Options contains child service dependencies.
type Options struct {
	Repository        repository.Repository
	PolicyProvisioner PolicyProvisioner
	Clock             clock.Clock
}

// New creates the child profile service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("child profile repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		repository:        options.Repository,
		policyProvisioner: options.PolicyProvisioner,
		timeSource:        timeSource,
	}, nil
}

// Create stores one guardian-owned child profile.
//
// The caller must supply the authenticated family id; the profile is never
// created from a client-provided family id.
func (s *Service) Create(
	ctx context.Context,
	familyID string,
	input domain.ProfileInput,
	guardianConsentVersion string,
) (*domain.Child, error) {
	return s.createWithSource(
		ctx,
		familyID,
		input,
		guardianConsentVersion,
		domain.SourceGuardian,
	)
}

// SyncRegistrationProfile creates or updates the one child profile captured
// from guardian registration. A missing birthday falls back to the oldest tier
// because the account registration form does not require a child birthday;
// guardians can correct the profile immediately from the child page.
func (s *Service) SyncRegistrationProfile(
	ctx context.Context,
	familyID string,
	nickname string,
	birthday string,
	guardianConsentVersion string,
) (*domain.Child, error) {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" {
		return nil, nil
	}
	ageTier, err := ageTierForBirthday(strings.TrimSpace(birthday), s.timeSource.Now().UTC())
	if err != nil {
		return nil, err
	}
	input := domain.ProfileInput{
		Nickname:          nickname,
		AgeTier:           ageTier,
		Interests:         []string{},
		ContentCategories: append([]string(nil), domain.ContentCategories...),
	}
	existing, err := s.repository.ListByFamilyID(ctx, familyID)
	if err != nil {
		return nil, err
	}
	for index := range existing {
		if existing[index].Source != domain.SourceRegistration {
			continue
		}
		updated, updateErr := s.Update(ctx, familyID, existing[index].ID, input)
		if updateErr != nil {
			return nil, updateErr
		}
		if s.policyProvisioner != nil {
			if policyErr := s.policyProvisioner.SyncDefaultCategories(
				ctx,
				familyID,
				updated.ID,
				updated.ContentCategories,
			); policyErr != nil {
				return nil, policyErr
			}
		}
		return updated, nil
	}
	return s.createWithSource(
		ctx,
		familyID,
		input,
		guardianConsentVersion,
		domain.SourceRegistration,
	)
}

func ageTierForBirthday(birthday string, now time.Time) (string, error) {
	if birthday == "" {
		return domain.AgeTier7To8, nil
	}
	birthdayTime, err := time.Parse("2006-01-02", birthday)
	if err != nil || birthdayTime.After(now) {
		return "", domain.ErrInvalidAgeTier
	}
	age := now.Year() - birthdayTime.Year()
	if now.Month() < birthdayTime.Month() ||
		(now.Month() == birthdayTime.Month() && now.Day() < birthdayTime.Day()) {
		age--
	}
	switch {
	case age <= 4:
		return domain.AgeTier3To4, nil
	case age <= 6:
		return domain.AgeTier5To6, nil
	default:
		return domain.AgeTier7To8, nil
	}
}

func (s *Service) createWithSource(
	ctx context.Context,
	familyID string,
	input domain.ProfileInput,
	guardianConsentVersion string,
	source string,
) (*domain.Child, error) {
	nickname, ageTier, interests, categories, err := validateProfile(input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(guardianConsentVersion) == "" {
		return nil, domain.ErrGuardianConsent
	}
	now := s.timeSource.Now().UTC()
	child := &domain.Child{
		ID:                     uuid.NewString(),
		FamilyID:               familyID,
		Nickname:               nickname,
		AgeTier:                ageTier,
		Interests:              interests,
		ContentCategories:      categories,
		GuardianConsentVersion: strings.TrimSpace(guardianConsentVersion),
		GuardianConsentedAt:    now,
		Source:                 source,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := s.repository.Create(ctx, child); err != nil {
		return nil, err
	}
	if s.policyProvisioner != nil {
		if err := s.policyProvisioner.CreateDefaultForChild(
			ctx,
			familyID,
			child.ID,
			child.ContentCategories,
		); err != nil {
			// The profile must not remain without its safety limits.
			_ = s.repository.Delete(ctx, familyID, child.ID)
			return nil, err
		}
	}
	return child, nil
}

// Update replaces the editable fields of one child owned by the family.
func (s *Service) Update(
	ctx context.Context,
	familyID string,
	childID string,
	input domain.ProfileInput,
) (*domain.Child, error) {
	child, err := s.repository.GetByID(ctx, childID)
	if err != nil {
		return nil, err
	}
	if child.FamilyID != familyID {
		return nil, domain.ErrChildNotFound
	}
	nickname, ageTier, interests, categories, err := validateProfile(input)
	if err != nil {
		return nil, err
	}
	child.Nickname = nickname
	child.AgeTier = ageTier
	child.Interests = interests
	child.ContentCategories = categories
	child.UpdatedAt = s.timeSource.Now().UTC()
	if err := s.repository.Update(ctx, child); err != nil {
		return nil, err
	}
	return child, nil
}

// List returns every child profile owned by one guardian.
func (s *Service) List(
	ctx context.Context,
	familyID string,
) ([]domain.Child, error) {
	return s.repository.ListByFamilyID(ctx, familyID)
}

// Get returns one child profile, enforcing family ownership.
func (s *Service) Get(
	ctx context.Context,
	familyID string,
	childID string,
) (*domain.Child, error) {
	child, err := s.repository.GetByID(ctx, childID)
	if err != nil {
		return nil, err
	}
	if child.FamilyID != familyID {
		return nil, domain.ErrChildNotFound
	}
	return child, nil
}

// Delete removes one child profile and its dependent policy.
func (s *Service) Delete(
	ctx context.Context,
	familyID string,
	childID string,
) error {
	if _, err := s.Get(ctx, familyID, childID); err != nil {
		return err
	}
	if s.policyProvisioner != nil {
		if err := s.policyProvisioner.DeleteForChild(ctx, childID); err != nil {
			return err
		}
	}
	return s.repository.Delete(ctx, familyID, childID)
}

func validateProfile(
	input domain.ProfileInput,
) (string, string, []string, []string, error) {
	nickname := strings.TrimSpace(input.Nickname)
	if len([]rune(nickname)) < 1 || len([]rune(nickname)) > 32 {
		return "", "", nil, nil, domain.ErrInvalidChildNickname
	}
	ageTier := strings.TrimSpace(input.AgeTier)
	if !contains(domain.AgeTiers, ageTier) {
		return "", "", nil, nil, domain.ErrInvalidAgeTier
	}
	interests, err := normalizeIdentifiers(input.Interests, false)
	if err != nil {
		return "", "", nil, nil, domain.ErrInvalidInterests
	}
	categories, err := normalizeIdentifiers(
		input.ContentCategories,
		true,
	)
	if err != nil {
		return "", "", nil, nil, domain.ErrInvalidCategories
	}
	for _, category := range categories {
		if !contains(domain.ContentCategories, category) {
			return "", "", nil, nil, domain.ErrInvalidCategories
		}
	}
	return nickname, ageTier, interests, categories, nil
}

func normalizeIdentifiers(values []string, requireOne bool) ([]string, error) {
	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if !interestPattern.MatchString(normalized) {
			return nil, errors.New("identifier is invalid")
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		unique = append(unique, normalized)
	}
	if requireOne && len(unique) == 0 {
		return nil, errors.New("at least one value is required")
	}
	if len(unique) > 12 {
		return nil, errors.New("too many values")
	}
	return unique, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

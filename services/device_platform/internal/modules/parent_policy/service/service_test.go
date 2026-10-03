package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
)

type memoryPolicyRepository struct {
	policies map[string]*domain.Policy
}

func newMemoryPolicyRepository() *memoryPolicyRepository {
	return &memoryPolicyRepository{policies: map[string]*domain.Policy{}}
}

func (r *memoryPolicyRepository) Create(
	_ context.Context,
	policy *domain.Policy,
) error {
	copyPolicy := *policy
	r.policies[policy.ChildID] = &copyPolicy
	return nil
}

func (r *memoryPolicyRepository) GetByChildID(
	_ context.Context,
	childID string,
) (*domain.Policy, error) {
	policy, ok := r.policies[childID]
	if !ok {
		return nil, domain.ErrPolicyNotFound
	}
	copyPolicy := *policy
	return &copyPolicy, nil
}

func (r *memoryPolicyRepository) GetByFamilyID(
	_ context.Context,
	familyID string,
	childID string,
) (*domain.Policy, error) {
	policy, ok := r.policies[childID]
	if !ok || policy.FamilyID != familyID {
		return nil, domain.ErrPolicyNotFound
	}
	copyPolicy := *policy
	return &copyPolicy, nil
}

func (r *memoryPolicyRepository) ListByFamilyID(
	_ context.Context,
	familyID string,
) ([]domain.Policy, error) {
	result := make([]domain.Policy, 0)
	for _, policy := range r.policies {
		if policy.FamilyID == familyID {
			result = append(result, *policy)
		}
	}
	return result, nil
}

func (r *memoryPolicyRepository) Update(
	_ context.Context,
	policy *domain.Policy,
) error {
	existing, ok := r.policies[policy.ChildID]
	if !ok || existing.FamilyID != policy.FamilyID {
		return domain.ErrPolicyNotFound
	}
	copyPolicy := *policy
	r.policies[policy.ChildID] = &copyPolicy
	return nil
}

func (r *memoryPolicyRepository) DeleteByChildID(
	_ context.Context,
	childID string,
) error {
	delete(r.policies, childID)
	return nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
}

func TestCreateDefaultForChildUsesAllApprovedCategoriesWhenUnset(t *testing.T) {
	repository := newMemoryPolicyRepository()
	service, err := New(Options{Repository: repository, Clock: fixedClock{}})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if err := service.CreateDefaultForChild(
		context.Background(),
		"family_1",
		"child_1",
		nil,
	); err != nil {
		t.Fatalf("create default policy: %v", err)
	}
	policy, err := service.Get(context.Background(), "family_1", "child_1")
	if err != nil {
		t.Fatalf("get policy: %v", err)
	}
	if len(policy.AllowedCategories) != 6 {
		t.Fatalf("expected all approved categories, got %v", policy.AllowedCategories)
	}
	if policy.DailyLimitMinutes != DefaultDailyLimitMinutes {
		t.Fatalf("expected default daily limit, got %d", policy.DailyLimitMinutes)
	}
}

func TestUpdateBumpsVersionAndRejectsCrossFamilyAccess(t *testing.T) {
	repository := newMemoryPolicyRepository()
	service, err := New(Options{Repository: repository, Clock: fixedClock{}})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if err := service.CreateDefaultForChild(
		context.Background(),
		"family_1",
		"child_1",
		nil,
	); err != nil {
		t.Fatalf("create default policy: %v", err)
	}
	updated, err := service.Update(
		context.Background(),
		"family_1",
		"child_1",
		domain.PolicyInput{
			DailyLimitMinutes: 30,
			AllowedCategories: []string{"story"},
			DisabledPeriods: []domain.DisabledPeriod{
				{StartTime: "21:00", EndTime: "07:00"},
			},
			MaxVolumePercent: 50,
		},
	)
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if updated.PolicyVersion != 2 {
		t.Fatalf("expected policy version 2, got %d", updated.PolicyVersion)
	}
	if _, err := service.Get(
		context.Background(),
		"family_2",
		"child_1",
	); !errors.Is(err, domain.ErrPolicyNotFound) {
		t.Fatalf("expected family isolation error, got %v", err)
	}
}

func TestUpdateRejectsInvalidLimits(t *testing.T) {
	repository := newMemoryPolicyRepository()
	service, err := New(Options{Repository: repository, Clock: fixedClock{}})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if err := service.CreateDefaultForChild(
		context.Background(),
		"family_1",
		"child_1",
		nil,
	); err != nil {
		t.Fatalf("create default policy: %v", err)
	}
	_, err = service.Update(
		context.Background(),
		"family_1",
		"child_1",
		domain.PolicyInput{
			DailyLimitMinutes: 721,
			AllowedCategories: []string{"story"},
			MaxVolumePercent:  50,
		},
	)
	if !errors.Is(err, domain.ErrInvalidDailyLimit) {
		t.Fatalf("expected invalid daily limit, got %v", err)
	}
	_, err = service.Update(
		context.Background(),
		"family_1",
		"child_1",
		domain.PolicyInput{
			DailyLimitMinutes: 60,
			AllowedCategories: []string{"unknown_category"},
			MaxVolumePercent:  50,
		},
	)
	if !errors.Is(err, domain.ErrInvalidCategories) {
		t.Fatalf("expected invalid categories, got %v", err)
	}
}

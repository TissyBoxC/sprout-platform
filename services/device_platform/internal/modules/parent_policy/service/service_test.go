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
	revision int64
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
	r.revision++
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
	return r.update(policy, 0)
}

func (r *memoryPolicyRepository) UpdateWithVersion(
	_ context.Context,
	policy *domain.Policy,
	expectedVersion int,
) error {
	return r.update(policy, expectedVersion)
}

func (r *memoryPolicyRepository) update(
	policy *domain.Policy,
	expectedVersion int,
) error {
	existing, ok := r.policies[policy.ChildID]
	if !ok || existing.FamilyID != policy.FamilyID {
		return domain.ErrPolicyNotFound
	}
	if expectedVersion > 0 && existing.PolicyVersion != expectedVersion {
		return domain.ErrPolicyVersionConflict
	}
	copyPolicy := *policy
	r.policies[policy.ChildID] = &copyPolicy
	r.revision++
	return nil
}

func (r *memoryPolicyRepository) DeleteByChildID(
	_ context.Context,
	childID string,
) error {
	delete(r.policies, childID)
	r.revision++
	return nil
}

func (r *memoryPolicyRepository) ListByFamilyIDWithRevision(
	_ context.Context,
	familyID string,
) ([]domain.Policy, int64, error) {
	policies, err := r.ListByFamilyID(context.Background(), familyID)
	if err != nil {
		return nil, 0, err
	}
	if len(policies) == 0 {
		return nil, 0, domain.ErrPolicyNotFound
	}
	return policies, r.revision, nil
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

func TestGetEffectiveUsesMostRestrictiveFamilyPolicy(t *testing.T) {
	repository := newMemoryPolicyRepository()
	service, err := New(Options{Repository: repository, Clock: fixedClock{}})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	for childID, dailyLimit := range map[string]int{
		"child_1": 120,
		"child_2": 30,
	} {
		if err := service.CreateDefaultForChild(
			context.Background(),
			"family_1",
			childID,
			nil,
		); err != nil {
			t.Fatalf("create policy %s: %v", childID, err)
		}
		if _, err := service.Update(
			context.Background(),
			"family_1",
			childID,
			domain.PolicyInput{
				DailyLimitMinutes: dailyLimit,
				AllowedCategories: []string{
					"story",
					"nursery_rhyme",
				},
				DisabledPeriods: []domain.DisabledPeriod{
					{StartTime: "21:00", EndTime: "07:00"},
				},
				MaxVolumePercent: 60,
			},
		); err != nil {
			t.Fatalf("update policy %s: %v", childID, err)
		}
	}

	effective, err := service.GetEffective(context.Background(), "family_1")
	if err != nil {
		t.Fatalf("get effective policy: %v", err)
	}
	if effective.DailyLimitMinutes != 30 {
		t.Fatalf("expected most restrictive daily limit, got %d", effective.DailyLimitMinutes)
	}
	if effective.SourceChildCount != 2 {
		t.Fatalf("expected two source children, got %d", effective.SourceChildCount)
	}
	if effective.PolicyVersion != 4 {
		t.Fatalf("expected revision 4, got %d", effective.PolicyVersion)
	}
	if len(effective.AllowedCategories) != 2 {
		t.Fatalf("expected category intersection, got %v", effective.AllowedCategories)
	}
}

func TestGetEffectiveRejectsDisjointCategoryPolicy(t *testing.T) {
	repository := newMemoryPolicyRepository()
	service, err := New(Options{Repository: repository, Clock: fixedClock{}})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	for childID, category := range map[string]string{
		"child_1": "story",
		"child_2": "poetry",
	} {
		if err := service.CreateDefaultForChild(
			context.Background(),
			"family_1",
			childID,
			[]string{category},
		); err != nil {
			t.Fatalf("create policy %s: %v", childID, err)
		}
	}
	if _, err := service.GetEffective(
		context.Background(),
		"family_1",
	); !errors.Is(err, domain.ErrInvalidCategories) {
		t.Fatalf("expected invalid category intersection, got %v", err)
	}
}

func TestAggregateMostRestrictiveRejectsEmptyFamily(t *testing.T) {
	_, err := aggregateMostRestrictive(nil)
	if !errors.Is(err, domain.ErrNoFamilyPolicy) {
		t.Fatalf("expected no-family-policy error, got %v", err)
	}
}

func TestCreateDefaultForChildEnablesVoiceConversationDefaults(t *testing.T) {
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
	voice := policy.VoiceConversation
	if !voice.ContinuousConversationEnabled || !voice.BargeInEnabled || !voice.FarFieldEnabled {
		t.Fatalf("expected continuous/barge-in/far-field enabled, got %+v", voice)
	}
	if voice.IdleWindowSeconds != DefaultVoiceIdleWindowSeconds {
		t.Fatalf("idle window = %d, want %d", voice.IdleWindowSeconds, DefaultVoiceIdleWindowSeconds)
	}
}

func TestUpdateVoiceConversationClampsIdleWindowAndRejectsNegative(t *testing.T) {
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
	// A window far above the accepted maximum clamps down to the ceiling.
	updated, err := service.Update(
		context.Background(),
		"family_1",
		"child_1",
		domain.PolicyInput{
			DailyLimitMinutes: 60,
			AllowedCategories: []string{"story"},
			MaxVolumePercent:  70,
			VoiceConversation: domain.VoiceConversationPolicy{
				ContinuousConversationEnabled: true,
				IdleWindowSeconds:             9999,
				BargeInEnabled:                false,
				FarFieldEnabled:               true,
			},
		},
	)
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if updated.VoiceConversation.IdleWindowSeconds != MaxVoiceIdleWindowSeconds {
		t.Fatalf(
			"clamped window = %d, want %d",
			updated.VoiceConversation.IdleWindowSeconds,
			MaxVoiceIdleWindowSeconds,
		)
	}
	if updated.VoiceConversation.BargeInEnabled {
		t.Fatal("expected barge-in to stay disabled after guardian update")
	}

	// A negative window is a malformed payload and must be rejected outright.
	if _, err := service.Update(
		context.Background(),
		"family_1",
		"child_1",
		domain.PolicyInput{
			DailyLimitMinutes: 60,
			AllowedCategories: []string{"story"},
			MaxVolumePercent:  70,
			VoiceConversation: domain.VoiceConversationPolicy{
				IdleWindowSeconds: -1,
			},
		},
	); !errors.Is(err, domain.ErrInvalidVoiceConversation) {
		t.Fatalf("expected invalid voice conversation error, got %v", err)
	}
}

func TestGetEffectiveVoiceConversationUsesMostRestrictive(t *testing.T) {
	repository := newMemoryPolicyRepository()
	service, err := New(Options{Repository: repository, Clock: fixedClock{}})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	inputs := map[string]domain.VoiceConversationPolicy{
		"child_1": {
			ContinuousConversationEnabled: true,
			IdleWindowSeconds:             20,
			BargeInEnabled:                true,
			FarFieldEnabled:               true,
		},
		"child_2": {
			ContinuousConversationEnabled: true,
			IdleWindowSeconds:             5,
			BargeInEnabled:                false,
			FarFieldEnabled:               true,
		},
	}
	for childID, voice := range inputs {
		if err := service.CreateDefaultForChild(
			context.Background(),
			"family_1",
			childID,
			nil,
		); err != nil {
			t.Fatalf("create policy %s: %v", childID, err)
		}
		if _, err := service.Update(
			context.Background(),
			"family_1",
			childID,
			domain.PolicyInput{
				DailyLimitMinutes: 60,
				AllowedCategories: []string{"story"},
				MaxVolumePercent:  70,
				VoiceConversation: voice,
			},
		); err != nil {
			t.Fatalf("update policy %s: %v", childID, err)
		}
	}
	effective, err := service.GetEffective(context.Background(), "family_1")
	if err != nil {
		t.Fatalf("get effective policy: %v", err)
	}
	voice := effective.VoiceConversation
	if voice.IdleWindowSeconds != 5 {
		t.Fatalf("expected shortest idle window, got %d", voice.IdleWindowSeconds)
	}
	if voice.BargeInEnabled {
		t.Fatal("expected barge-in disabled because one child forbids it")
	}
	if !voice.ContinuousConversationEnabled || !voice.FarFieldEnabled {
		t.Fatalf("expected shared capabilities to stay enabled, got %+v", voice)
	}
}

func TestUpdateWithVersionRejectsStaleGuardianWrite(t *testing.T) {
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
	if _, err := service.UpdateWithVersion(
		context.Background(),
		"family_1",
		"child_1",
		domain.PolicyInput{
			DailyLimitMinutes: 45,
			AllowedCategories: []string{"story"},
			MaxVolumePercent:  50,
		},
		1,
	); err != nil {
		t.Fatalf("update with current version: %v", err)
	}
	_, err = service.UpdateWithVersion(
		context.Background(),
		"family_1",
		"child_1",
		domain.PolicyInput{
			DailyLimitMinutes: 30,
			AllowedCategories: []string{"story"},
			MaxVolumePercent:  50,
		},
		1,
	)
	if !errors.Is(err, domain.ErrPolicyVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

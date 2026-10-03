package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
)

type memoryChildRepository struct {
	children map[string]*domain.Child
}

func newMemoryChildRepository() *memoryChildRepository {
	return &memoryChildRepository{children: map[string]*domain.Child{}}
}

func (r *memoryChildRepository) Create(
	_ context.Context,
	child *domain.Child,
) error {
	copyChild := *child
	r.children[child.ID] = &copyChild
	return nil
}

func (r *memoryChildRepository) Update(
	_ context.Context,
	child *domain.Child,
) error {
	existing, ok := r.children[child.ID]
	if !ok || existing.FamilyID != child.FamilyID {
		return domain.ErrChildNotFound
	}
	copyChild := *child
	r.children[child.ID] = &copyChild
	return nil
}

func (r *memoryChildRepository) GetByID(
	_ context.Context,
	childID string,
) (*domain.Child, error) {
	child, ok := r.children[childID]
	if !ok {
		return nil, domain.ErrChildNotFound
	}
	copyChild := *child
	return &copyChild, nil
}

func (r *memoryChildRepository) ListByFamilyID(
	_ context.Context,
	familyID string,
) ([]domain.Child, error) {
	result := make([]domain.Child, 0)
	for _, child := range r.children {
		if child.FamilyID == familyID {
			result = append(result, *child)
		}
	}
	return result, nil
}

func (r *memoryChildRepository) Delete(
	_ context.Context,
	familyID string,
	childID string,
) error {
	child, ok := r.children[childID]
	if !ok || child.FamilyID != familyID {
		return domain.ErrChildNotFound
	}
	delete(r.children, childID)
	return nil
}

type memoryPolicyProvisioner struct {
	createdChildIDs []string
	deletedChildIDs []string
}

func (p *memoryPolicyProvisioner) CreateDefaultForChild(
	_ context.Context,
	_ string,
	childID string,
	_ []string,
) error {
	p.createdChildIDs = append(p.createdChildIDs, childID)
	return nil
}

func (p *memoryPolicyProvisioner) DeleteForChild(
	_ context.Context,
	childID string,
) error {
	p.deletedChildIDs = append(p.deletedChildIDs, childID)
	return nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
}

func TestCreateValidatesProfileAndProvisionsPolicy(t *testing.T) {
	repository := newMemoryChildRepository()
	provisioner := &memoryPolicyProvisioner{}
	service, err := New(Options{
		Repository:        repository,
		PolicyProvisioner: provisioner,
		Clock:             fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	child, err := service.Create(
		context.Background(),
		"family_1",
		domain.ProfileInput{
			Nickname:          "小芽",
			AgeTier:           domain.AgeTier3To4,
			Interests:         []string{"animals", "music"},
			ContentCategories: []string{"story", "nursery_rhyme"},
		},
		"2026-01",
	)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if len(provisioner.createdChildIDs) != 1 ||
		provisioner.createdChildIDs[0] != child.ID {
		t.Fatalf("expected default policy provisioning, got %v", provisioner.createdChildIDs)
	}
	if child.FamilyID != "family_1" {
		t.Fatalf("expected family ownership to be set, got %s", child.FamilyID)
	}
}

func TestCreateRejectsUnknownAgeTierAndMissingConsent(t *testing.T) {
	service, err := New(Options{
		Repository: newMemoryChildRepository(),
		Clock:      fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	_, err = service.Create(
		context.Background(),
		"family_1",
		domain.ProfileInput{
			Nickname:          "小芽",
			AgeTier:           "age_9_10",
			ContentCategories: []string{"story"},
		},
		"2026-01",
	)
	if !errors.Is(err, domain.ErrInvalidAgeTier) {
		t.Fatalf("expected invalid age tier, got %v", err)
	}
	_, err = service.Create(
		context.Background(),
		"family_1",
		domain.ProfileInput{
			Nickname:          "小芽",
			AgeTier:           domain.AgeTier5To6,
			ContentCategories: []string{"story"},
		},
		"",
	)
	if !errors.Is(err, domain.ErrGuardianConsent) {
		t.Fatalf("expected guardian consent error, got %v", err)
	}
}

func TestDeleteRemovesDependentPolicy(t *testing.T) {
	repository := newMemoryChildRepository()
	provisioner := &memoryPolicyProvisioner{}
	service, err := New(Options{
		Repository:        repository,
		PolicyProvisioner: provisioner,
		Clock:             fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	child, err := service.Create(
		context.Background(),
		"family_1",
		domain.ProfileInput{
			Nickname:          "小芽",
			AgeTier:           domain.AgeTier3To4,
			ContentCategories: []string{"story"},
		},
		"2026-01",
	)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := service.Delete(context.Background(), "family_1", child.ID); err != nil {
		t.Fatalf("delete child: %v", err)
	}
	if len(provisioner.deletedChildIDs) != 1 ||
		provisioner.deletedChildIDs[0] != child.ID {
		t.Fatalf("expected dependent policy removal, got %v", provisioner.deletedChildIDs)
	}
}

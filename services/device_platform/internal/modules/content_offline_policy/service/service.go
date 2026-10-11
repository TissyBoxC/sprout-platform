// Package service contains the content offline policy use cases.
//
// The policy is platform-wide and singleton: an operator reads and publishes
// one version, and every bound device reads the same policy so its cache
// budget is consistent across the fleet. The service validates the payload
// before it touches the store, so a bad revision never replaces a good one.
package service

import (
	"context"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/domain"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	Current(ctx context.Context) (domain.Policy, error)
	Save(ctx context.Context, input domain.Input) (domain.Policy, error)
}

// Service owns offline policy reads and publishes.
type Service struct {
	repository Repository
}

// Options contains offline policy service dependencies.
type Options struct {
	Repository Repository
}

// New creates the offline policy service.
func New(options Options) *Service {
	return &Service{repository: options.Repository}
}

// Current returns the published policy or the default when none is set.
func (s *Service) Current(ctx context.Context) (domain.Policy, error) {
	if s.repository == nil {
		return domain.Default(), nil
	}
	return s.repository.Current(ctx)
}

// Publish validates and stores a new policy revision.
func (s *Service) Publish(
	ctx context.Context,
	input domain.Input,
) (domain.Policy, error) {
	if s.repository == nil {
		return domain.Default(), nil
	}
	normalized, err := domain.ValidateInput(input)
	if err != nil {
		return domain.Policy{}, err
	}
	return s.repository.Save(ctx, normalized)
}

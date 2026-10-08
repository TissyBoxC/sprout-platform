// Package handler exposes parent policy HTTP handlers.
package handler

import (
	"context"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
)

// Service is the narrow policy surface used by transport adapters.
type Service interface {
	Get(ctx context.Context, familyID string, childID string) (*domain.Policy, error)
	List(ctx context.Context, familyID string) ([]domain.Policy, error)
	Update(
		ctx context.Context,
		familyID string,
		childID string,
		input domain.PolicyInput,
	) (*domain.Policy, error)
	UpdateWithVersion(
		ctx context.Context,
		familyID string,
		childID string,
		input domain.PolicyInput,
		expectedVersion int,
	) (*domain.Policy, error)
	GetEffective(ctx context.Context, familyID string) (*domain.EffectivePolicy, error)
}

// Handler translates HTTP requests into parent policy service calls.
type Handler struct {
	Service Service
}

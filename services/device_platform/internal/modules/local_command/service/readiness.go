package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/domain"
)

// ReadinessReader provides aggregate local-command counters for capability
// health. It is narrower than Repository so tests use a small fake.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that command sets and their revision clock are queryable.
// No commands yet is healthy; enabled commands exceeding the total or
// commands without any revision clock is an inconsistent degraded snapshot.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read local command readiness: %w", err)
	}
	if readiness.CommandCount < 0 {
		readiness.CommandCount = 0
	}
	if readiness.EnabledCommandCount < 0 {
		readiness.EnabledCommandCount = 0
	}
	if readiness.FamilyCount < 0 ||
		readiness.CommandFamilyCount < 0 ||
		readiness.EnabledCommandCount > readiness.CommandCount ||
		(readiness.CommandCount > 0 && readiness.FamilyCount == 0) ||
		readiness.CommandFamilyCount != readiness.FamilyCount {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}

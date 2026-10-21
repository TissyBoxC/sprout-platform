package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/domain"
)

// ReadinessReader provides aggregate memory counters for capability health. It
// is narrower than Repository so tests use a small fake.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

type readinessReaderFunc func(ctx context.Context) (domain.Readiness, error)

func (reader readinessReaderFunc) Readiness(
	ctx context.Context,
) (domain.Readiness, error) {
	return reader(ctx)
}

// Readiness reports whether the memory table is readable. An empty store is
// healthy; a child at the per-child cap degrades the snapshot because new facts
// for that child will be refused until a guardian prunes them.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read memory readiness: %w", err)
	}
	if readiness.MemoryCount < 0 {
		readiness.MemoryCount = 0
	}
	if readiness.ChildCount < 0 {
		readiness.ChildCount = 0
	}
	if readiness.OverLimitChildCount < 0 {
		readiness.OverLimitChildCount = 0
	}
	if readiness.Limit <= 0 {
		readiness.Limit = s.childLimit
	}
	if readiness.OverLimitChildCount > 0 {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}

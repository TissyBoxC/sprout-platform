package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/domain"
)

// ReadinessReader provides aggregate alarm reminder counters for capability
// health. It is narrower than Repository so tests use a small fake.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that alarm schedules and firing history are queryable. An
// empty alarm set is healthy for a new family; enabled alarms exceeding the
// total or events without alarms is an inconsistent degraded snapshot.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read alarm reminder readiness: %w", err)
	}
	if readiness.AlarmCount < 0 {
		readiness.AlarmCount = 0
	}
	if readiness.EnabledCount < 0 {
		readiness.EnabledCount = 0
	}
	if readiness.EnabledCount > readiness.AlarmCount {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}

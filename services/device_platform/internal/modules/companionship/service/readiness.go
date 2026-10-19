package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/domain"
)

// ReadinessReader provides aggregate companionship counters for capability
// health. It is narrower than Repository so tests use a small fake.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that growth records and habit reminders are queryable. An
// empty timeline is healthy; enabled reminders exceeding the total is an
// inconsistent degraded snapshot.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read companionship readiness: %w", err)
	}
	if readiness.GrowthRecordCount < 0 {
		readiness.GrowthRecordCount = 0
	}
	if readiness.HabitReminderCount < 0 {
		readiness.HabitReminderCount = 0
	}
	if readiness.EnabledHabitReminderCount > readiness.HabitReminderCount {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}

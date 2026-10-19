package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
)

// ReadinessReader provides non-sensitive identity and session counters for
// capability health. It is deliberately narrower than the authentication
// repository so module tests can use a small fake.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies the authoritative account store and reports whether the
// login surface has the accounts and MFA dependencies required for use.
//
// Empty account tables and zero active sessions are normal first-deployment
// states. Authentication is unhealthy only when its required administration
// dependencies are missing; a missing MFA enrolment is security degradation,
// not a login outage.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read authentication readiness: %w", err)
	}
	readiness.MFACipherReady = s.mfaCipher != nil
	readiness.MFAAuditorReady = s.mfaAuditor != nil
	readiness.ActiveSessionCount = maxInt64(readiness.ActiveSessionCount, 0)
	readiness.State = authenticationReadinessState(readiness)
	return readiness, nil
}

func authenticationReadinessState(readiness domain.Readiness) domain.ReadinessState {
	if !readiness.MFACipherReady || !readiness.MFAAuditorReady {
		return domain.ReadinessDegraded
	}
	if readiness.ActiveAdminCount > 0 &&
		readiness.MFAEnabledAdminCount == 0 {
		return domain.ReadinessDegraded
	}
	return domain.ReadinessHealthy
}

func maxInt64(value int64, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}

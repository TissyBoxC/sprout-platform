package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/domain"
)

// ReadinessReader provides aggregate playback request counters for capability
// health. It is narrower than Repository so tests use a small fake.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that playback requests are readable and both ownership and
// publication checks are wired. No requests yet is healthy; missing validation
// readers degrade the guardian path even if stored requests remain readable.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read playback readiness: %w", err)
	}
	readiness.DeviceReaderReady = s.deviceReader != nil
	readiness.PackageReaderReady = s.packageReader != nil
	if !readiness.DeviceReaderReady || !readiness.PackageReaderReady {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}

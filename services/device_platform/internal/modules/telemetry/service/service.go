// Package service owns the read-only fleet telemetry projection.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/telemetry/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/telemetry/repository"
)

// defaultOnlineThreshold matches the device runtime default so the operations
// dashboard and the device page agree on what "online" means.
const defaultOnlineThreshold = 90 * time.Second

// Service exposes bounded fleet telemetry reads.
type Service struct {
	repository      repository.Repository
	onlineThreshold time.Duration
}

// Options contains telemetry service dependencies.
type Options struct {
	Repository      repository.Repository
	OnlineThreshold time.Duration
}

// New creates the telemetry service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("telemetry repository is required")
	}
	onlineThreshold := options.OnlineThreshold
	if onlineThreshold <= 0 {
		onlineThreshold = defaultOnlineThreshold
	}
	return &Service{
		repository:      options.Repository,
		onlineThreshold: onlineThreshold,
	}, nil
}

// Summary returns the fleet telemetry projection for the requested window.
func (s *Service) Summary(
	ctx context.Context,
	windowDays int,
) (domain.Summary, error) {
	if windowDays != 0 && (windowDays < 1 || windowDays > domain.MaxWindowDays) {
		return domain.Summary{}, domain.ErrInvalidWindow
	}
	windowDays = domain.NormalizeWindow(windowDays)
	return s.repository.Summary(ctx, windowDays, s.onlineThreshold)
}

package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
)

// ReadinessReader provides aggregate content lifecycle counters for capability
// health. It is narrower than Repository so tests use a small fake.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// EngagementReadinessReader provides aggregate favourite and playback-history
// counters for the content engagement feature.
type EngagementReadinessReader interface {
	EngagementReadiness(ctx context.Context) (domain.EngagementReadiness, error)
}

// ThemePackReadinessReader provides aggregate theme-pack lifecycle counters for
// the content theme-pack feature.
type ThemePackReadinessReader interface {
	ThemePackReadiness(ctx context.Context) (domain.ThemePackReadiness, error)
}

// Readiness verifies that content persistence, asset verification and family
// policy filtering have the dependencies required for publication and
// delivery. An empty catalog is healthy; missing asset or policy readers are
// degraded because a reachable database alone cannot serve content safely.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read content library readiness: %w", err)
	}
	if readiness.CatalogRevision < 0 {
		readiness.CatalogRevision = 0
	}
	readiness.AssetReaderReady = s.assetReader != nil
	readiness.PolicyReaderReady = s.policyReader != nil
	if !readiness.AssetReaderReady || !readiness.PolicyReaderReady {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}

// EngagementReadiness verifies that favourites and playback history are
// readable. If the repository predates the aggregate surface, the service
// degrades rather than inventing a healthy empty history.
func (s *Service) EngagementReadiness(ctx context.Context) (domain.EngagementReadiness, error) {
	if s == nil || s.engagementReadinessReader == nil {
		return domain.EngagementReadiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.engagementReadinessReader.EngagementReadiness(ctx)
	if err != nil {
		return domain.EngagementReadiness{}, fmt.Errorf("read content engagement readiness: %w", err)
	}
	if readiness.FavoriteCount < 0 {
		readiness.FavoriteCount = 0
	}
	if readiness.PlayHistoryCount < 0 {
		readiness.PlayHistoryCount = 0
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}

// ThemePackReadiness verifies that editorial theme packs are readable. An empty
// pack catalogue is healthy for a new deployment; a count mismatch between
// lifecycle total and published/draft/withdrawn classes is degraded.
func (s *Service) ThemePackReadiness(ctx context.Context) (domain.ThemePackReadiness, error) {
	if s == nil || s.themePackReadinessReader == nil {
		return domain.ThemePackReadiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.themePackReadinessReader.ThemePackReadiness(ctx)
	if err != nil {
		return domain.ThemePackReadiness{}, fmt.Errorf("read content theme readiness: %w", err)
	}
	classified := readiness.PublishedCount +
		readiness.DraftCount +
		readiness.WithdrawnCount
	if readiness.ThemePackCount < 0 ||
		readiness.PublishedCount < 0 ||
		readiness.DraftCount < 0 ||
		readiness.WithdrawnCount < 0 ||
		classified != readiness.ThemePackCount {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}

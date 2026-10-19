package service

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/domain"
)

// ReadinessReader provides aggregate privacy lifecycle state.
type ReadinessReader interface {
	Readiness(ctx context.Context) (domain.Readiness, error)
}

// Readiness verifies that consent, export, and deletion persistence is
// readable and that the retention disclosure is wired. An empty consent history
// is healthy before the first guardian decision. Stale or failed deletion work
// degrades lifecycle health because it requires operator attention.
func (s *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if s == nil || s.readinessReader == nil {
		return domain.Readiness{}, domain.ErrReadinessUnavailable
	}
	readiness, err := s.readinessReader.Readiness(ctx)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read privacy readiness: %w", err)
	}
	readiness.RetentionReaderReady = s.retention != nil
	readiness.DeletionWorkerReady = s.children != nil &&
		s.devices != nil &&
		s.aiAccounts != nil
	// Audit coverage is proven by a durable recent write. An expiry worker or
	// feature scheduler can run without an operator login, so this is the only
	// authoritative signal that append-only audit persistence is actually
	// writable across all management mutations.
	readiness.AuditCoverageProof = readiness.RecentAuditEventCount > 0
	if readiness.StaleDeletionCount > 0 ||
		readiness.FailedDeletionCount > 0 ||
		!readiness.RetentionReaderReady ||
		!readiness.DeletionWorkerReady ||
		!readiness.AuditCoverageProof {
		readiness.State = domain.ReadinessDegraded
		return readiness, nil
	}
	readiness.State = domain.ReadinessHealthy
	return readiness, nil
}

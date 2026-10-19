package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/domain"
)

// Readiness reads aggregate lifecycle state. A stale processing request is a
// deletion that was claimed but not completed within its worker lease; it is
// reported separately so the management console can distinguish queue depth
// from a stuck deletion.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("privacy database is unavailable")
	}
	var readiness domain.Readiness
	if err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM parent_consent_events),
			(SELECT COUNT(*) FROM parent_deletion_requests WHERE status = 'pending'),
			(SELECT COUNT(*) FROM parent_deletion_requests
			 WHERE status = 'processing'
			   AND updated_at <= NOW() - INTERVAL '30 minutes'),
			(SELECT COUNT(*) FROM parent_deletion_requests WHERE status = 'failed'),
			(SELECT COUNT(*) FROM parent_data_export_receipts),
			(SELECT COUNT(*) FROM platform_parent_audit
			 WHERE created_at >= NOW() - INTERVAL '7 days')
	`).Scan(
		&readiness.ConsentEventCount,
		&readiness.PendingDeletionCount,
		&readiness.StaleDeletionCount,
		&readiness.FailedDeletionCount,
		&readiness.ExportReceiptCount,
		&readiness.RecentAuditEventCount,
	); err != nil {
		return domain.Readiness{}, fmt.Errorf("read privacy lifecycle readiness: %w", err)
	}
	return readiness, nil
}

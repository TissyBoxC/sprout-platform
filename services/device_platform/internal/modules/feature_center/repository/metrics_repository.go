// Package repository persists feature-center state and projects live metrics.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultOnlineThreshold mirrors the operations dashboard default so the
// feature center and the operations overview never disagree about what counts
// as an online device.
const defaultOnlineThreshold = 90 * time.Second

// MetricsRepository answers the read-only metric keys declared by the feature
// registry. Every query is a plain count against a table owned by another
// module, so the feature center never invents an operational value it cannot
// verify. A key with no reliable source is intentionally absent from the
// result: the console then renders "not reported" instead of a fake number.
type MetricsRepository struct {
	pool            *pgxpool.Pool
	onlineThreshold time.Duration
}

// NewMetricsRepository creates a live feature metric source. A non-positive
// threshold falls back to the threshold the operations dashboard uses.
func NewMetricsRepository(
	pool *pgxpool.Pool,
	onlineThreshold time.Duration,
) *MetricsRepository {
	if onlineThreshold <= 0 {
		onlineThreshold = defaultOnlineThreshold
	}
	return &MetricsRepository{pool: pool, onlineThreshold: onlineThreshold}
}

// FeatureMetrics returns the live values for one feature id. Features without
// a verifiable source return a nil map and no error.
func (r *MetricsRepository) FeatureMetrics(
	ctx context.Context,
	featureID string,
) (map[string]any, error) {
	switch featureID {
	case "parent_account":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM parent_accounts WHERE role = 'parent' AND status = 'active'",
			"active_parent_count",
		)
	case "ai_account":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM ai_accounts WHERE status = 'active'",
			"active_ai_account_count",
		)
	case "child_profile":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM child_profiles",
			"child_profile_count",
		)
	case "parent_policy":
		return r.scalar(ctx,
			"SELECT COALESCE(SUM(revision), 0) FROM parent_policy_revisions",
			"policy_update_count",
		)
	case "device_binding":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM device_bindings",
			"bound_device_count",
		)
	case "device_runtime":
		return r.scalar(ctx,
			`SELECT COUNT(*)
			FROM device_runtime_status
			WHERE connection_state = 'online'
			  AND received_at >= NOW() - $1::interval`,
			"online_device_count",
			fmt.Sprintf("%d milliseconds", r.onlineThreshold.Milliseconds()),
		)
	case "device_diagnostics":
		return r.scalar(ctx,
			`SELECT
				(SELECT COUNT(*) FROM device_boot_events)
				+ (SELECT COUNT(*) FROM device_module_failures)
				+ (SELECT COUNT(*) FROM device_recovery_events)`,
			"diagnostic_event_count",
		)
	case "content_library":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM content_package_versions WHERE status = 'published'",
			"published_content_count",
		)
	case "ota_release":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM platform_releases WHERE kind = 'firmware' AND status = 'published'",
			"published_firmware_count",
		)
	case "app_update":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM platform_releases WHERE kind IN ('client', 'resource') AND status = 'published'",
			"app_release_count",
		)
	case "ui_text":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM ui_text_package_versions WHERE status = 'published'",
			"published_ui_text_count",
		)
	case "usage_report":
		return r.scalar(ctx,
			"SELECT COUNT(DISTINCT report_date) FROM device_usage_daily",
			"reported_usage_days",
		)
	case "notification":
		return r.triad(ctx,
			`SELECT
				(SELECT COUNT(*) FROM notifications),
				(SELECT COUNT(*) FROM notification_deliveries WHERE status = 'pending'),
				(SELECT COUNT(*) FROM notification_deliveries WHERE status = 'failed')`,
			"notification_total",
			"notification_pending_delivery",
			"notification_failed_delivery",
		)
	default:
		return nil, nil
	}
}

func (r *MetricsRepository) scalar(
	ctx context.Context,
	query string,
	key string,
	args ...any,
) (map[string]any, error) {
	value, err := r.count(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return map[string]any{key: value}, nil
}

func (r *MetricsRepository) triad(
	ctx context.Context,
	query string,
	firstKey string,
	secondKey string,
	thirdKey string,
) (map[string]any, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("feature metrics database is unavailable")
	}
	var first, second, third int64
	if err := r.pool.QueryRow(ctx, query).Scan(&first, &second, &third); err != nil {
		return nil, fmt.Errorf("load feature metrics %s: %w", firstKey, err)
	}
	return map[string]any{
		firstKey:  first,
		secondKey: second,
		thirdKey:  third,
	}, nil
}

func (r *MetricsRepository) count(
	ctx context.Context,
	query string,
	args ...any,
) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, fmt.Errorf("feature metrics database is unavailable")
	}
	var value int64
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		return 0, fmt.Errorf("load feature metric: %w", err)
	}
	return value, nil
}

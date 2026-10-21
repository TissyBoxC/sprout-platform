// Package repository persists feature-center state and projects live metrics.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/channel"
	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultOnlineThreshold mirrors the operations dashboard default so the
// feature center and the operations overview never disagree about what counts
// as an online device.
const defaultOnlineThreshold = 90 * time.Second

const offlinePolicyDeviceMetricQuery = `SELECT COUNT(*)
	FROM device_bindings AS bindings
	JOIN content_offline_policy_acks AS acks
	  ON acks.device_id = bindings.device_id
	 AND acks.family_id = bindings.parent_account_id
	JOIN content_offline_policy AS policy
	  ON policy.singleton = TRUE
	 AND policy.offline_enabled = TRUE
	 AND acks.applied_revision = policy.revision`

const otaPublishedMetricQuery = `SELECT COUNT(*)
	FROM ota_releases
	WHERE status = 'published'`

const telemetryMetricQuery = `SELECT
	(SELECT COUNT(*) FROM device_runtime_status),
	(SELECT COUNT(*) FROM device_runtime_status
	 WHERE connection_state = 'online'
	   AND received_at >= NOW() - $1::interval),
	(SELECT COALESCE(SUM(pending_telemetry), 0)
	 FROM device_runtime_status)`

const voiceConversationMetricQuery = `SELECT COALESCE(SUM(conversation_count), 0)
	FROM platform_usage_daily
	WHERE usage_day >= CURRENT_DATE - INTERVAL '6 days'`

// familySpendingLimitMetricQuery counts the families that actually hold a
// monthly cap and the checkouts refused for exceeding one this month. Both
// tables are owned by the billing module; the feature center only reads them so
// the console figure is the same one enforcement uses.
const familySpendingLimitMetricQuery = `SELECT
	(SELECT COUNT(*) FROM family_spending_limits WHERE monthly_limit_cents > 0),
	(SELECT COUNT(*) FROM family_spending_limit_rejections
	 WHERE created_at >= date_trunc('month', NOW()))`

// ChannelRegistryProbe reports which payment channels are registered. The
// billing metrics use it so a channel count is process state rather than a
// constant, and Feature Center never claims a provider is configured while the
// placeholder adapter is still the only one wired in.
type ChannelRegistryProbe interface {
	Health(ctx context.Context) map[string]channel.HealthStatus
}

// MetricsOptions contains the non-database inputs some metrics need.
type MetricsOptions struct {
	// OnlineThreshold defines what counts as an online device. A non-positive
	// value falls back to the operations dashboard threshold.
	OnlineThreshold time.Duration
	// ChannelRegistry backs the payment channel metrics. Nil reports zero
	// registered channels.
	ChannelRegistry ChannelRegistryProbe
	// MemoryRetentionDays is the child memory retention window the memory module
	// enforces. It is configuration, not stored data, and 0 means the memory
	// module is not wired in this deployment.
	MemoryRetentionDays int
}

// MetricsRepository answers the read-only metric keys declared by the feature
// registry. Every query is a plain count against a table owned by another
// module, so the feature center never invents an operational value it cannot
// verify. A key with no reliable source is intentionally absent from the
// result: the console then renders "not reported" instead of a fake number.
type MetricsRepository struct {
	pool                *pgxpool.Pool
	onlineThreshold     time.Duration
	channels            ChannelRegistryProbe
	memoryRetentionDays int
}

// NewMetricsRepository creates a live feature metric source. A non-positive
// threshold falls back to the threshold the operations dashboard uses.
func NewMetricsRepository(
	pool *pgxpool.Pool,
	onlineThreshold time.Duration,
) *MetricsRepository {
	return NewMetricsRepositoryWithOptions(pool, MetricsOptions{
		OnlineThreshold: onlineThreshold,
	})
}

// NewMetricsRepositoryWithOptions creates a live feature metric source that
// also reads process-level signals the database cannot answer.
func NewMetricsRepositoryWithOptions(
	pool *pgxpool.Pool,
	options MetricsOptions,
) *MetricsRepository {
	onlineThreshold := options.OnlineThreshold
	if onlineThreshold <= 0 {
		onlineThreshold = defaultOnlineThreshold
	}
	return &MetricsRepository{
		pool:                pool,
		onlineThreshold:     onlineThreshold,
		channels:            options.ChannelRegistry,
		memoryRetentionDays: options.MemoryRetentionDays,
	}
}

// FeatureMetrics returns the live values for one feature id. Features without
// a verifiable source return a nil map and no error.
func (r *MetricsRepository) FeatureMetrics(
	ctx context.Context,
	featureID string,
) (map[string]any, error) {
	switch featureID {
	case "auth":
		return r.scalar(ctx,
			`SELECT COUNT(*)
			FROM auth_sessions
			WHERE revoked_at IS NULL
			  AND expires_at > NOW()`,
			"active_auth_session_count",
		)
	case "parent_account":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM parent_accounts WHERE role = 'parent' AND status = 'active'",
			"active_parent_count",
		)
	case "family_membership":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM family_members),
				(SELECT COUNT(*) FROM family_invitations WHERE status = 'pending')`,
			"family_member_count",
			"family_pending_invitation_count",
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
			otaPublishedMetricQuery,
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
	case "telemetry":
		return r.triadWithArgs(ctx,
			telemetryMetricQuery,
			[]any{fmt.Sprintf("%d milliseconds", r.onlineThreshold.Milliseconds())},
			"telemetry_device_count",
			"telemetry_online_count",
			"telemetry_pending_total",
		)
	case "voice_conversation":
		return r.scalar(ctx,
			voiceConversationMetricQuery,
			"conversation_count_7d",
		)
	case "ai_model_gateway":
		return r.scalar(ctx,
			`SELECT COUNT(DISTINCT model)
			FROM ai_accounts
			CROSS JOIN LATERAL unnest(available_models) AS model
			WHERE status = 'active'`,
			"available_model_count",
		)
	case "platform_security":
		return r.scalar(ctx,
			`SELECT COUNT(*)
			FROM admin_totp_credentials AS credentials
			JOIN parent_accounts AS accounts
			  ON accounts.id = credentials.parent_account_id
			WHERE accounts.role = 'admin'
			  AND accounts.status = 'active'
			  AND credentials.enabled_at IS NOT NULL`,
			"mfa_enabled_admin_count",
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
	case "vision":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM vision_tasks),
				(SELECT COUNT(*) FROM vision_tasks WHERE status = 'failed')`,
			"vision_task_count",
			"vision_failed_task_count",
		)
	case "display":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM display_device_status),
				(SELECT COUNT(*) FROM display_device_status WHERE touch_supported = true)`,
			"display_device_count",
			"display_touch_device_count",
		)
	case "touch":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM touch_device_status),
				(SELECT COUNT(*) FROM touch_device_status WHERE haptic_supported = true)`,
			"touch_device_count",
			"touch_haptic_device_count",
		)
	case "screen":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM screen_device_status),
				(SELECT COUNT(*) FROM screen_device_status WHERE point_read_supported = true)`,
			"screen_device_count",
			"screen_point_read_device_count",
		)
	case "connectivity":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM device_connectivity_status WHERE bearer = 'cellular'),
				(SELECT COUNT(*) FROM device_connectivity_status WHERE roaming = true)`,
			"cellular_device_count",
			"cellular_roaming_count",
		)
	case "power":
		return r.triad(ctx,
			`SELECT
				(SELECT COUNT(*) FROM device_power_status),
				(SELECT COUNT(*) FROM device_power_status WHERE power_mode = 'low_power'),
				(SELECT COUNT(*) FROM device_power_status WHERE power_mode = 'deep_sleep')`,
			"power_device_count",
			"power_low_power_count",
			"power_deep_sleep_count",
		)
	case "motion":
		return r.triad(ctx,
			`SELECT
				(SELECT COUNT(*) FROM device_motion_status),
				(SELECT COUNT(*) FROM device_motion_status WHERE state = 'running'),
				(SELECT COUNT(*) FROM device_motion_status WHERE faulted = true)`,
			"motion_device_count",
			"motion_running_count",
			"motion_faulted_count",
		)
	case "form_factor":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM device_form_factor),
				(SELECT COUNT(*) FROM device_form_factor WHERE action_mask > 0)`,
			"form_factor_device_count",
			"form_factor_action_count",
		)
	case "video_companion":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM video_calls",
			"video_call_count",
		)
	case "video_session":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM device_video_session_status",
			"video_session_device_count",
		)
	case "wearable":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM wearable_device_status),
				(SELECT COUNT(*) FROM wearable_sos_events)`,
			"wearable_device_count",
			"wearable_sos_event_count",
		)
	case "device_mesh":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM device_groups),
				(SELECT COUNT(*) FROM content_sync_states WHERE sync_state <> 'applied')`,
			"device_group_count",
			"device_mesh_pending_sync_count",
		)
	case "bluetooth_audio":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM device_bluetooth_audio_status),
				(SELECT COUNT(*) FROM device_bluetooth_audio_status WHERE adapter_state = 'connected')`,
			"bluetooth_device_count",
			"bluetooth_connected_count",
		)
	case "learning_visualization":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM device_learning_visualization_status",
			"learning_visualization_device_count",
		)
	case "image_privacy":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM device_image_privacy_status),
				(SELECT COUNT(*) FROM device_image_privacy_status WHERE upload_allowed = true)`,
			"image_privacy_device_count",
			"image_privacy_upload_allowed_count",
		)
	case "english_learning":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM english_lessons),
				(SELECT COUNT(*) FROM english_practice_attempts)`,
			"english_course_count",
			"english_practice_count",
		)
	case "alarm_reminder":
		return r.triad(ctx,
			`SELECT
				(SELECT COUNT(*) FROM alarm_reminders),
				(SELECT COUNT(*) FROM alarm_reminders WHERE enabled = true),
				(SELECT COUNT(*) FROM alarm_reminder_events
				 WHERE reported_at >= NOW() - INTERVAL '7 days')`,
			"alarm_reminder_count",
			"alarm_reminder_enabled_count",
			"alarm_reminder_event_count",
		)
	case "companionship":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM companionship_growth_records),
				(SELECT COUNT(*) FROM companionship_habit_reminders)`,
			"companionship_record_count",
			"habit_reminder_count",
		)
	case "child_memory":
		// Only aggregate counters reach the console: a memory value is child
		// personal information and must never appear in an operator metric. The
		// retention column reports the window this repository is configured with
		// (0 when unwired), so an operator never reads an invented policy.
		return r.triadWithArgs(ctx,
			`SELECT
				(SELECT COUNT(*) FROM child_memories),
				(SELECT COUNT(DISTINCT child_id) FROM child_memories),
				$1::bigint`,
			[]any{r.memoryRetentionDays},
			"memory_count",
			"memory_child_count",
			"memory_retention_days",
		)
	case "local_command":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM local_commands",
			"local_command_count",
		)
	case "offline_content":
		return r.scalar(ctx,
			offlinePolicyDeviceMetricQuery,
			"offline_policy_device_count",
		)
	case "content_engagement":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM content_favorites),
				(SELECT COUNT(*) FROM content_play_history)`,
			"content_favorite_count",
			"content_play_history_count",
		)
	case "content_theme_pack":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM content_theme_packs WHERE status = 'published'",
			"content_theme_pack_count",
		)
	case "playback":
		return r.triad(ctx,
			`SELECT
				(SELECT COUNT(*) FROM playback_requests),
				(SELECT COUNT(*) FROM playback_requests WHERE status IN ('pending', 'acknowledged')),
				(SELECT COUNT(*) FROM playback_requests WHERE status = 'failed')`,
			"playback_request_count",
			"playback_pending_count",
			"playback_failed_count",
		)
	case "recommendation":
		return r.scalar(ctx,
			"SELECT COUNT(*) FROM content_package_versions WHERE status = 'published'",
			"recommendation_candidate_count",
		)
	case "privacy_lifecycle":
		return r.pair(ctx,
			`SELECT
				(SELECT COUNT(*) FROM parent_consent_events),
				(SELECT COUNT(*) FROM parent_deletion_requests WHERE status = 'pending')`,
			"consent_event_count",
			"pending_deletion_count",
		)
	case "billing_orders":
		return r.triad(ctx,
			`SELECT
				(SELECT COUNT(*) FROM billing_orders WHERE status = 'pending'),
				(SELECT COUNT(*) FROM billing_orders
				 WHERE status IN ('paid', 'partially_refunded', 'refunded')
				   AND paid_at >= NOW() - INTERVAL '30 days'),
				(SELECT COUNT(*) FROM billing_orders WHERE status = 'expired')`,
			"pending_order_count",
			"paid_order_count_30d",
			"expired_order_count",
		)
	case "billing_payments":
		// No registered payment channel can collect money yet, because every
		// provider adapter is still the unconfigured placeholder. Both probes
		// are therefore real process state, not a stored preference.
		active := 0
		if r.channels != nil {
			active = len(r.channels.Health(ctx))
		}
		return map[string]any{
			"configured_channel_count": int64(0),
			"registered_channel_count": int64(active),
		}, nil
	case "billing_reconciliation":
		return r.triad(ctx,
			`SELECT
				(SELECT COUNT(*) FROM billing_reconciliation_runs),
				(SELECT COALESCE((
					SELECT difference_cny_cents
					FROM billing_reconciliation_runs
					ORDER BY started_at DESC, id DESC
					LIMIT 1
				), 0)),
				(SELECT COUNT(*) FROM billing_reconciliation_items
				 WHERE resolved = FALSE AND discrepancy <> 'matched')`,
			"reconciliation_run_count",
			"reconciliation_difference_cny_cents",
			"unresolved_discrepancy_count",
		)
	case "family_spending_limit":
		return r.pair(ctx,
			familySpendingLimitMetricQuery,
			"spending_limit_family_count",
			"spending_limit_rejection_count",
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
	return r.triadWithArgs(
		ctx,
		query,
		nil,
		firstKey,
		secondKey,
		thirdKey,
	)
}

func (r *MetricsRepository) triadWithArgs(
	ctx context.Context,
	query string,
	args []any,
	firstKey string,
	secondKey string,
	thirdKey string,
) (map[string]any, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("feature metrics database is unavailable")
	}
	var first, second, third int64
	if err := r.pool.QueryRow(ctx, query, args...).Scan(
		&first,
		&second,
		&third,
	); err != nil {
		return nil, fmt.Errorf("load feature metrics %s: %w", firstKey, err)
	}
	return map[string]any{
		firstKey:  first,
		secondKey: second,
		thirdKey:  third,
	}, nil
}

func (r *MetricsRepository) pair(
	ctx context.Context,
	query string,
	firstKey string,
	secondKey string,
) (map[string]any, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("feature metrics database is unavailable")
	}
	var first, second int64
	if err := r.pool.QueryRow(ctx, query).Scan(&first, &second); err != nil {
		return nil, fmt.Errorf("load feature metrics %s: %w", firstKey, err)
	}
	return map[string]any{
		firstKey:  first,
		secondKey: second,
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

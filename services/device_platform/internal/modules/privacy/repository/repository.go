// Package repository persists guardian privacy lifecycle records and the
// administrator audit projection.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines the privacy lifecycle and audit persistence surface.
type Repository interface {
	AppendConsentEvent(ctx context.Context, event *domain.ConsentEvent) error
	ListConsentEvents(
		ctx context.Context,
		parentAccountID string,
	) ([]domain.ConsentEvent, error)
	CreateExportReceipt(ctx context.Context, receipt *domain.ExportReceipt) error
	LatestExportReceipt(
		ctx context.Context,
		parentAccountID string,
	) (*domain.ExportReceipt, error)
	RequestDeletion(ctx context.Context, request *domain.DeletionRequest) error
	CancelDeletion(
		ctx context.Context,
		parentAccountID string,
	) (*domain.DeletionRequest, error)
	CurrentDeletion(
		ctx context.Context,
		parentAccountID string,
	) (*domain.DeletionRequest, error)
	ClaimDueDeletions(
		ctx context.Context,
		limit int,
	) ([]domain.DeletionCandidate, error)
	CompleteDeletion(
		ctx context.Context,
		requestID string,
	) error
	FailDeletion(
		ctx context.Context,
		requestID string,
		reason string,
	) error
	PurgeParentData(ctx context.Context, parentAccountID string) error
	PurgeExpiredRecords(
		ctx context.Context,
		now time.Time,
	) (ExpiredRecordPurge, error)
	PurgeRetainedRecords(
		ctx context.Context,
		imageBefore time.Time,
		conversationBefore time.Time,
	) (RetainedRecordPurge, error)
	PurgeUsageReports(
		ctx context.Context,
		before time.Time,
	) (UsageReportPurge, error)
	AppendAudit(ctx context.Context, entry *domain.AuditEntry) error
	QueryAudit(
		ctx context.Context,
		filter domain.AuditFilter,
	) (*domain.AuditPage, error)
}

// ExpiredRecordPurge reports how many already-expired rows were removed per
// table so the retention worker can be observed without reading row contents.
type ExpiredRecordPurge struct {
	AuthSessions             int64 `json:"auth_sessions"`
	AdminMFAChallenges       int64 `json:"admin_mfa_challenges"`
	DeviceSessions           int64 `json:"device_sessions"`
	DeviceBindingTokens      int64 `json:"device_binding_tokens"`
	DeviceRegistrationTokens int64 `json:"device_registration_tokens"`
	DeviceChallenges         int64 `json:"device_challenges"`
	PhoneVerificationCodes   int64 `json:"phone_verification_codes"`
	ExportReceipts           int64 `json:"export_receipts"`
	FamilyInvitations        int64 `json:"family_invitations"`
}

// Total returns the number of rows removed across every table.
func (purge ExpiredRecordPurge) Total() int64 {
	return purge.AuthSessions +
		purge.AdminMFAChallenges +
		purge.DeviceSessions +
		purge.DeviceBindingTokens +
		purge.DeviceRegistrationTokens +
		purge.DeviceChallenges +
		purge.PhoneVerificationCodes +
		purge.ExportReceipts +
		purge.FamilyInvitations
}

// RetainedRecordPurge reports bounded history removed by the operator's
// disclosed media and conversation windows. Raw audio is never persisted by
// the platform, so it intentionally has no deletion counter.
type RetainedRecordPurge struct {
	VisionResults    int64 `json:"vision_results"`
	VisionTasks      int64 `json:"vision_tasks"`
	VideoCalls       int64 `json:"video_calls"`
	DeviceVideoCalls int64 `json:"device_video_calls"`
}

// Total returns the number of retained history rows removed.
func (purge RetainedRecordPurge) Total() int64 {
	return purge.VisionResults +
		purge.VisionTasks +
		purge.VideoCalls +
		purge.DeviceVideoCalls
}

// UsageReportPurge reports how many aggregated usage-ledger rows were removed
// by the usage-report retention window.
type UsageReportPurge struct {
	DeviceUsageDays   int64 `json:"device_usage_days"`
	PlatformUsageDays int64 `json:"platform_usage_days"`
}

// Total returns the number of rows removed across both usage ledgers.
func (purge UsageReportPurge) Total() int64 {
	return purge.DeviceUsageDays + purge.PlatformUsageDays
}

// PostgresRepository is the PostgreSQL-backed privacy repository.
type PostgresRepository struct {
	pool privacyDatabase
}

// privacyDatabase is the narrow SQL surface used by the repository. pgxpool
// satisfies it in production while tests can use a deterministic transaction
// double without constructing a live PostgreSQL server.
type privacyDatabase interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// NewPostgresRepository creates a privacy repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// AppendConsentEvent appends one immutable guardian decision.
func (r *PostgresRepository) AppendConsentEvent(
	ctx context.Context,
	event *domain.ConsentEvent,
) error {
	detail, err := encodeJSONObject(event.Detail)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO parent_consent_events (
			id,
			parent_account_id,
			consent_type,
			consent_version,
			granted,
			source,
			detail,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
	`,
		event.ID,
		event.ParentAccountID,
		event.ConsentType,
		event.ConsentVersion,
		event.Granted,
		event.Source,
		detail,
		event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert guardian consent event: %w", err)
	}
	return nil
}

// ListConsentEvents returns the append-only history for one guardian.
func (r *PostgresRepository) ListConsentEvents(
	ctx context.Context,
	parentAccountID string,
) ([]domain.ConsentEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			parent_account_id,
			consent_type,
			consent_version,
			granted,
			source,
			detail,
			created_at
		FROM parent_consent_events
		WHERE parent_account_id = $1
		ORDER BY created_at DESC, id DESC
	`, parentAccountID)
	if err != nil {
		return nil, fmt.Errorf("list guardian consent events: %w", err)
	}
	defer rows.Close()

	events := make([]domain.ConsentEvent, 0)
	for rows.Next() {
		event, scanErr := scanConsentEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		events = append(events, *event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate guardian consent events: %w", err)
	}
	return events, nil
}

// CreateExportReceipt records one completed data export.
func (r *PostgresRepository) CreateExportReceipt(
	ctx context.Context,
	receipt *domain.ExportReceipt,
) error {
	counts, err := encodeJSONObjectFromInts(receipt.ItemCounts)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO parent_data_export_receipts (
			id,
			parent_account_id,
			requested_by,
			format,
			status,
			item_counts,
			created_at,
			expires_at
		)
		VALUES ($1, $2, $2, 'json', $3, $4::jsonb, $5, $6)
	`,
		receipt.ID,
		receipt.ParentAccountID,
		receipt.Status,
		counts,
		receipt.CreatedAt,
		receipt.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert guardian export receipt: %w", err)
	}
	return nil
}

// LatestExportReceipt returns the newest export receipt for the guardian.
func (r *PostgresRepository) LatestExportReceipt(
	ctx context.Context,
	parentAccountID string,
) (*domain.ExportReceipt, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			format,
			status,
			item_counts,
			created_at,
			expires_at
		FROM parent_data_export_receipts
		WHERE parent_account_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, parentAccountID)
	return scanExportReceipt(row)
}

// RequestDeletion creates the single open deletion request for a guardian.
func (r *PostgresRepository) RequestDeletion(
	ctx context.Context,
	request *domain.DeletionRequest,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO parent_deletion_requests (
			id,
			parent_account_id,
			status,
			reason,
			requested_at,
			execute_after,
			updated_at
		)
		VALUES ($1, $2, 'pending', $3, $4, $5, $6)
	`,
		request.ID,
		request.ParentAccountID,
		request.Reason,
		request.RequestedAt,
		request.ExecuteAfter,
		request.UpdatedAt,
	)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "23505" {
			return domain.ErrDeletionPending
		}
		return fmt.Errorf("insert guardian deletion request: %w", err)
	}
	return nil
}

// CancelDeletion cancels the current open request.
func (r *PostgresRepository) CancelDeletion(
	ctx context.Context,
	parentAccountID string,
) (*domain.DeletionRequest, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE parent_deletion_requests
		SET status = 'cancelled',
		    cancelled_at = NOW(),
		    updated_at = NOW()
		WHERE id = (
			SELECT id
			FROM parent_deletion_requests
			WHERE parent_account_id = $1
			  AND status = 'pending'
			ORDER BY requested_at DESC
			LIMIT 1
		)
		RETURNING
			id,
			status,
			reason,
			requested_at,
			execute_after,
			cancelled_at,
			completed_at,
			failure_reason,
			updated_at
	`, parentAccountID)
	request, err := scanDeletionRequest(row)
	if errors.Is(err, domain.ErrDeletionNotFound) {
		return nil, domain.ErrDeletionNotPending
	}
	return request, err
}

// CurrentDeletion returns the newest deletion request in any state.
func (r *PostgresRepository) CurrentDeletion(
	ctx context.Context,
	parentAccountID string,
) (*domain.DeletionRequest, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id,
			status,
			reason,
			requested_at,
			execute_after,
			cancelled_at,
			completed_at,
			failure_reason,
			updated_at
		FROM parent_deletion_requests
		WHERE parent_account_id = $1
		ORDER BY requested_at DESC, id DESC
		LIMIT 1
	`, parentAccountID)
	request, err := scanDeletionRequest(row)
	if errors.Is(err, domain.ErrDeletionNotFound) {
		return nil, nil
	}
	return request, err
}

// ClaimDueDeletions marks due requests as being processed and returns them.
// The update is a no-op when another worker already claimed the row.
func (r *PostgresRepository) ClaimDueDeletions(
	ctx context.Context,
	limit int,
) ([]domain.DeletionCandidate, error) {
	if limit < 1 {
		limit = domain.DefaultDeletionBatchSize
	}
	rows, err := r.pool.Query(ctx, `
		WITH due AS (
			SELECT id
			FROM parent_deletion_requests
			WHERE (
				status = 'pending'
				AND execute_after <= NOW()
			) OR (
				status = 'processing'
				AND updated_at <= NOW() - INTERVAL '30 minutes'
			)
			ORDER BY
				CASE WHEN status = 'pending' THEN 0 ELSE 1 END,
				execute_after ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE parent_deletion_requests AS requests
		SET status = 'processing',
		    updated_at = NOW()
		FROM due
		WHERE requests.id = due.id
		RETURNING
			requests.id,
			requests.parent_account_id,
			requests.status,
			requests.reason,
			requests.requested_at,
			requests.execute_after,
			requests.cancelled_at,
			requests.completed_at,
			requests.failure_reason,
			requests.updated_at
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim due guardian deletions: %w", err)
	}
	defer rows.Close()
	candidates := make([]domain.DeletionCandidate, 0)
	for rows.Next() {
		var candidate domain.DeletionCandidate
		var request domain.DeletionRequest
		if err := rows.Scan(
			&request.ID,
			&candidate.ParentAccountID,
			&request.Status,
			&request.Reason,
			&request.RequestedAt,
			&request.ExecuteAfter,
			&request.CancelledAt,
			&request.CompletedAt,
			&request.FailureReason,
			&request.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan due guardian deletion: %w", err)
		}
		request.Cancellable = false
		request.ScheduledFor = &request.ExecuteAfter
		candidate.Request = request
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due guardian deletions: %w", err)
	}
	return candidates, nil
}

// CompleteDeletion records a successful clean-up.
func (r *PostgresRepository) CompleteDeletion(
	ctx context.Context,
	requestID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE parent_deletion_requests
		SET status = 'completed',
		    completed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
		  AND status = 'processing'
	`, requestID)
	if err != nil {
		return fmt.Errorf("complete guardian deletion: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrDeletionNotPending
	}
	return nil
}

// FailDeletion records a failure so an operator can retry safely.
func (r *PostgresRepository) FailDeletion(
	ctx context.Context,
	requestID string,
	reason string,
) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE parent_deletion_requests
		SET status = 'failed',
		    failure_reason = $2,
		    updated_at = NOW()
		WHERE id = $1
		  AND status = 'processing'
	`, requestID, reason)
	if err != nil {
		return fmt.Errorf("mark guardian deletion failed: %w", err)
	}
	return nil
}

// PurgeParentData anonymizes the guardian and deletes all data that exists
// only because that guardian and their devices exist. The account row itself
// stays as a disabled tombstone so audit records retain a stable identifier.
func (r *PostgresRepository) PurgeParentData(
	ctx context.Context,
	parentAccountID string,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin guardian data purge: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()

	rows, err := transaction.Query(ctx, `
		SELECT device_id
		FROM device_bindings
		WHERE parent_account_id = $1
	`, parentAccountID)
	if err != nil {
		return fmt.Errorf("list guardian devices for purge: %w", err)
	}
	deviceIDs := make([]string, 0)
	for rows.Next() {
		var deviceID string
		if err := rows.Scan(&deviceID); err != nil {
			rows.Close()
			return fmt.Errorf("scan guardian device for purge: %w", err)
		}
		deviceIDs = append(deviceIDs, deviceID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate guardian devices for purge: %w", err)
	}
	rows.Close()

	if len(deviceIDs) > 0 {
		deviceStatements := []struct {
			query string
			label string
		}{
			{
				query: `DELETE FROM ota_events WHERE deployment_id IN (
					SELECT id FROM ota_deployments WHERE device_id = ANY($1)
				)`,
				label: "OTA events",
			},
			{
				query: `DELETE FROM ota_deployments WHERE device_id = ANY($1)`,
				label: "OTA deployments",
			},
			{
				query: `DELETE FROM ota_group_members WHERE device_id = ANY($1)`,
				label: "OTA group members",
			},
			{
				query: `DELETE FROM playback_requests WHERE device_id = ANY($1)`,
				label: "playback requests",
			},
			{
				query: `DELETE FROM content_play_history WHERE device_id = ANY($1)`,
				label: "content play history",
			},
			{
				query: `DELETE FROM vision_results WHERE device_id = ANY($1)`,
				label: "vision results",
			},
			{
				query: `DELETE FROM vision_tasks WHERE device_id = ANY($1)`,
				label: "vision tasks",
			},
			{
				query: `DELETE FROM video_calls WHERE device_id = ANY($1)`,
				label: "video calls",
			},
			{
				query: `DELETE FROM wearable_sos_events WHERE device_id = ANY($1)`,
				label: "wearable SOS events",
			},
			{
				query: `DELETE FROM wearable_device_status WHERE device_id = ANY($1)`,
				label: "wearable device status",
			},
			{
				query: `DELETE FROM device_group_members WHERE device_id = ANY($1)`,
				label: "device group members",
			},
			{
				query: `DELETE FROM screen_video_playback WHERE device_id = ANY($1)`,
				label: "screen video playback",
			},
			{
				query: `DELETE FROM screen_device_status WHERE device_id = ANY($1)`,
				label: "screen device status",
			},
			{
				query: `DELETE FROM device_connectivity_status WHERE device_id = ANY($1)`,
				label: "device connectivity status",
			},
			{
				query: `DELETE FROM device_form_factor WHERE device_id = ANY($1)`,
				label: "device form factor status",
			},
			{
				query: `DELETE FROM device_motion_status WHERE device_id = ANY($1)`,
				label: "device motion status",
			},
			{
				query: `DELETE FROM device_power_status WHERE device_id = ANY($1)`,
				label: "device power status",
			},
			{
				query: `DELETE FROM device_video_status WHERE device_id = ANY($1)`,
				label: "device video status",
			},
			{
				query: `DELETE FROM device_video_session_status WHERE device_id = ANY($1)`,
				label: "device video session status",
			},
			{
				query: `DELETE FROM device_bluetooth_audio_status WHERE device_id = ANY($1)`,
				label: "device Bluetooth audio status",
			},
			{
				query: `DELETE FROM device_learning_visualization_status WHERE device_id = ANY($1)`,
				label: "device learning visualization status",
			},
			{
				query: `DELETE FROM device_image_privacy_status WHERE device_id = ANY($1)`,
				label: "device image privacy status",
			},
			{
				query: `DELETE FROM display_device_status WHERE device_id = ANY($1)`,
				label: "display device status",
			},
			{
				query: `DELETE FROM touch_device_status WHERE device_id = ANY($1)`,
				label: "touch device status",
			},
			{
				query: `DELETE FROM device_provisioning_events WHERE device_id = ANY($1)`,
				label: "provisioning events",
			},
			{
				query: `DELETE FROM device_crash_events WHERE device_id = ANY($1)`,
				label: "crash events",
			},
			{
				query: `DELETE FROM notifications WHERE device_id = ANY($1)`,
				label: "device notifications",
			},
			{
				query: `DELETE FROM device_runtime_commands WHERE device_id = ANY($1)`,
				label: "runtime commands",
			},
			{
				query: `DELETE FROM device_runtime_heartbeats WHERE device_id = ANY($1)`,
				label: "runtime heartbeats",
			},
			{
				query: `DELETE FROM device_runtime_status WHERE device_id = ANY($1)`,
				label: "runtime status",
			},
			{
				query: `DELETE FROM device_boot_events WHERE device_id = ANY($1)`,
				label: "boot events",
			},
			{
				query: `DELETE FROM device_module_failures WHERE device_id = ANY($1)`,
				label: "module failures",
			},
			{
				query: `DELETE FROM device_recovery_events WHERE device_id = ANY($1)`,
				label: "recovery events",
			},
			{
				query: `DELETE FROM device_interaction_events WHERE device_id = ANY($1)`,
				label: "interaction events",
			},
			{
				query: `DELETE FROM device_usage_daily WHERE device_id = ANY($1)`,
				label: "device usage",
			},
			{
				query: `DELETE FROM device_binding_tokens WHERE device_id = ANY($1)`,
				label: "binding tokens",
			},
			{
				query: `DELETE FROM device_registration_tokens WHERE device_id = ANY($1)`,
				label: "registration tokens",
			},
			{
				query: `DELETE FROM device_challenges WHERE device_id = ANY($1)`,
				label: "device challenges",
			},
			{
				query: `DELETE FROM device_sessions WHERE device_id = ANY($1)`,
				label: "device sessions",
			},
			{
				query: `DELETE FROM device_bindings WHERE device_id = ANY($1)`,
				label: "device bindings",
			},
			{
				query: `DELETE FROM device_credentials WHERE device_id = ANY($1)`,
				label: "device credentials",
			},
		}
		for _, statement := range deviceStatements {
			if _, err := transaction.Exec(ctx, statement.query, deviceIDs); err != nil {
				return fmt.Errorf("delete guardian %s: %w", statement.label, err)
			}
		}
	}

	parentStatements := []struct {
		query string
		label string
	}{
		{`DELETE FROM notification_deliveries WHERE parent_account_id = $1`, "notification deliveries"},
		{`DELETE FROM notifications WHERE parent_account_id = $1`, "notifications"},
		// Billing rows are deleted child-first so no foreign key is left
		// dangling: notifications and payments reference an order, refunds
		// reference both an order and a payment, and the quota ledger
		// references orders and refunds. Purchase history is a guardian's
		// personal data like any other, so it leaves with the account.
		{`DELETE FROM billing_payment_notifications WHERE payment_id IN (
			SELECT id FROM billing_payments WHERE parent_account_id = $1
		)`, "billing payment notifications"},
		{`DELETE FROM billing_refunds WHERE parent_account_id = $1`, "billing refunds"},
		{`DELETE FROM billing_payments WHERE parent_account_id = $1`, "billing payments"},
		{`DELETE FROM billing_subscriptions WHERE parent_account_id = $1`, "billing subscriptions"},
		{`DELETE FROM billing_spending_consents WHERE parent_account_id = $1`, "billing spending consents"},
		// The spending cap is the guardian's own money policy and the rejection
		// log is derived from their purchases, so both leave with the account
		// rather than keeping a deleted family's spending profile.
		{`DELETE FROM family_spending_limit_rejections WHERE family_id = $1`, "family spending limit rejections"},
		{`DELETE FROM family_spending_limits WHERE family_id = $1`, "family spending limits"},
		{`DELETE FROM ai_quota_ledger WHERE parent_account_id = $1`, "AI quota ledger"},
		{`DELETE FROM billing_orders WHERE parent_account_id = $1`, "billing orders"},
		{`DELETE FROM companionship_growth_records WHERE family_id = $1`, "companionship growth records"},
		{`DELETE FROM companionship_habit_reminders WHERE family_id = $1`, "companionship habit reminders"},
		// Child long-term memories are the child's own statements, so they are
		// deleted before child_profiles. The tombstone table is emptied in the
		// same transaction: a leftover tombstone still ties a deletion fact to the
		// family and would keep the child's memory keys discoverable after the
		// account itself is anonymized.
		{`DELETE FROM child_memory_tombstones WHERE family_id = $1`, "child memory tombstones"},
		{`DELETE FROM child_memories WHERE family_id = $1`, "child memories"},
		// A growth report describes one child's week; it is derived data but
		// still identifiable, so a deleted account must not leave the summaries
		// behind.
		{`DELETE FROM growth_reports WHERE family_id = $1`, "growth reports"},
		{`DELETE FROM alarm_reminder_events WHERE family_id = $1`, "alarm reminder events"},
		{`DELETE FROM alarm_reminders WHERE family_id = $1`, "alarm reminders"},
		{`DELETE FROM english_practice_attempts WHERE family_id = $1`, "english practice attempts"},
		{`DELETE FROM local_commands WHERE family_id = $1`, "local commands"},
		{`DELETE FROM local_command_revisions WHERE family_id = $1`, "local command revisions"},
		{`DELETE FROM content_favorites WHERE parent_account_id = $1`, "content favorites"},
		{`DELETE FROM content_sync_states WHERE family_id = $1`, "content sync states"},
		{`DELETE FROM video_calls WHERE family_id = $1`, "video calls"},
		{`DELETE FROM video_contacts WHERE family_id = $1`, "video contacts"},
		{`DELETE FROM family_invitations WHERE family_id = $1`, "family invitations"},
		{`DELETE FROM family_members WHERE family_id = $1`, "family members"},
		{`DELETE FROM family_membership_revisions WHERE family_id = $1`, "family membership revisions"},
		{`DELETE FROM video_session_policies WHERE family_id = $1`, "video session policies"},
		{`DELETE FROM bluetooth_audio_policies WHERE family_id = $1`, "Bluetooth audio policies"},
		{`DELETE FROM learning_visualization_policies WHERE family_id = $1`, "learning visualization policies"},
		{`DELETE FROM image_privacy_policies WHERE family_id = $1`, "image privacy policies"},
		{`DELETE FROM wearable_sos_events WHERE family_id = $1`, "wearable SOS events"},
		{`DELETE FROM wearable_device_status WHERE family_id = $1`, "wearable device status"},
		{`DELETE FROM wearable_geofences WHERE family_id = $1`, "wearable geofences"},
		{`DELETE FROM wearable_location_policy WHERE family_id = $1`, "wearable location policy"},
		{`DELETE FROM wearable_contacts WHERE family_id = $1`, "wearable contacts"},
		{`DELETE FROM screen_video_playback WHERE family_id = $1`, "screen video playback"},
		{`DELETE FROM screen_point_read_entries WHERE family_id = $1`, "screen point-read entries"},
		{`DELETE FROM screen_policies WHERE family_id = $1`, "screen policies"},
		{`DELETE FROM device_messages WHERE family_id = $1`, "device messages"},
		{`DELETE FROM device_group_members WHERE family_id = $1`, "device group members"},
		{`DELETE FROM device_groups WHERE family_id = $1`, "device groups"},
		{`DELETE FROM cellular_policies WHERE family_id = $1`, "cellular policies"},
		{`DELETE FROM display_policies WHERE family_id = $1`, "display policies"},
		{`DELETE FROM motion_policies WHERE family_id = $1`, "motion policies"},
		{`DELETE FROM power_policies WHERE family_id = $1`, "power policies"},
		{`DELETE FROM touch_configs WHERE family_id = $1`, "touch configs"},
		{`DELETE FROM parent_policy_revisions WHERE family_id = $1`, "parent policy revisions"},
		{`DELETE FROM parent_data_export_receipts WHERE parent_account_id = $1`, "export receipts"},
		{`DELETE FROM parent_consent_events WHERE parent_account_id = $1`, "consent history"},
		{`DELETE FROM platform_usage_daily WHERE parent_account_id = $1`, "platform usage"},
		{`DELETE FROM phone_verification_codes WHERE phone = (
			SELECT phone FROM parent_accounts WHERE id = $1
		)`, "phone verification codes"},
		{`DELETE FROM ai_accounts WHERE parent_account_id = $1`, "AI account projection"},
		{`DELETE FROM parent_policies WHERE family_id = $1`, "parent policies"},
		{`DELETE FROM child_profiles WHERE family_id = $1`, "child profiles"},
		{`DELETE FROM admin_mfa_challenges WHERE parent_account_id = $1`, "MFA challenges"},
		{`DELETE FROM admin_totp_credentials WHERE parent_account_id = $1`, "TOTP credentials"},
		{`DELETE FROM auth_sessions WHERE parent_account_id = $1`, "parent sessions"},
	}
	for _, statement := range parentStatements {
		if _, err := transaction.Exec(ctx, statement.query, parentAccountID); err != nil {
			return fmt.Errorf("delete guardian %s: %w", statement.label, err)
		}
	}

	tag, err := transaction.Exec(ctx, `
		UPDATE parent_accounts
		SET email = CONCAT('deleted+', id::text, '@deleted.sprout.local'),
		    phone = NULL,
		    password_hash = md5(random()::text) || md5(random()::text),
		    display_name = '已注销用户',
		    guardian_family_name = '',
		    child_nickname = NULL,
		    child_birthday = NULL,
		    status = 'disabled',
		    phone_verified_at = NULL,
		    email_verified_at = NULL,
		    last_login_at = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`, parentAccountID)
	if err != nil {
		return fmt.Errorf("anonymize guardian account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAccountUnavailable
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit guardian data purge: %w", err)
	}
	return nil
}

// PurgeExpiredRecords removes authentication artefacts and export receipts
// whose lifetime has elapsed. It never removes live rows: every statement is
// bounded by `expires_at <= $1` so the caller's clock decides eligibility.
// Consumed-but-unexpired rows are deliberately retained until their own expiry
// so a late replay is still rejected by the existing validity checks. Each
// delete is independent, so a transient failure on one table does not hide
// progress on the others.
func (r *PostgresRepository) PurgeExpiredRecords(
	ctx context.Context,
	now time.Time,
) (ExpiredRecordPurge, error) {
	var purge ExpiredRecordPurge
	statements := []struct {
		query string
		dest  *int64
		label string
	}{
		{
			query: `DELETE FROM auth_sessions
				WHERE expires_at <= $1`,
			dest:  &purge.AuthSessions,
			label: "parent sessions",
		},
		{
			query: `DELETE FROM admin_mfa_challenges
				WHERE expires_at <= $1`,
			dest:  &purge.AdminMFAChallenges,
			label: "MFA challenges",
		},
		{
			query: `DELETE FROM device_sessions
				WHERE expires_at <= $1`,
			dest:  &purge.DeviceSessions,
			label: "device sessions",
		},
		{
			query: `DELETE FROM device_binding_tokens
				WHERE expires_at <= $1`,
			dest:  &purge.DeviceBindingTokens,
			label: "binding tokens",
		},
		{
			query: `DELETE FROM device_registration_tokens
				WHERE expires_at <= $1`,
			dest:  &purge.DeviceRegistrationTokens,
			label: "registration tokens",
		},
		{
			query: `DELETE FROM device_challenges
				WHERE expires_at <= $1`,
			dest:  &purge.DeviceChallenges,
			label: "device challenges",
		},
		{
			query: `DELETE FROM phone_verification_codes
				WHERE expires_at <= $1`,
			dest:  &purge.PhoneVerificationCodes,
			label: "phone verification codes",
		},
		{
			query: `DELETE FROM parent_data_export_receipts
				WHERE expires_at <= $1`,
			dest:  &purge.ExportReceipts,
			label: "export receipts",
		},
		{
			query: `DELETE FROM family_invitations
				WHERE expires_at <= $1`,
			dest:  &purge.FamilyInvitations,
			label: "family invitations",
		},
	}
	for _, statement := range statements {
		tag, err := r.pool.Exec(ctx, statement.query, now)
		if err != nil {
			return purge, fmt.Errorf(
				"purge expired guardian %s: %w",
				statement.label,
				err,
			)
		}
		*statement.dest = tag.RowsAffected()
	}
	return purge, nil
}

// PurgeRetainedRecords applies the operator-configured history windows. Vision
// results and tasks are image-derived history; video calls are conversation
// metadata. The statements run independently so one failure remains visible
// and already completed deletions are not repeated.
func (r *PostgresRepository) PurgeRetainedRecords(
	ctx context.Context,
	imageBefore time.Time,
	conversationBefore time.Time,
) (RetainedRecordPurge, error) {
	var purge RetainedRecordPurge
	imageStatements := []struct {
		query string
		dest  *int64
		label string
	}{
		{
			query: `DELETE FROM vision_results
				WHERE created_at < $1`,
			dest:  &purge.VisionResults,
			label: "vision results",
		},
		{
			query: `DELETE FROM vision_tasks
				WHERE created_at < $1`,
			dest:  &purge.VisionTasks,
			label: "vision tasks",
		},
	}
	if !imageBefore.IsZero() {
		for _, statement := range imageStatements {
			tag, err := r.pool.Exec(ctx, statement.query, imageBefore)
			if err != nil {
				return purge, fmt.Errorf(
					"purge retained %s: %w",
					statement.label,
					err,
				)
			}
			*statement.dest = tag.RowsAffected()
		}
	}
	conversationStatements := []struct {
		query string
		dest  *int64
		label string
	}{
		{
			query: `DELETE FROM video_calls
				WHERE started_at < $1`,
			dest:  &purge.VideoCalls,
			label: "video calls",
		},
		{
			query: `DELETE FROM device_video_status
				WHERE last_call_id IS NOT NULL
				  AND last_call_id NOT IN (SELECT id FROM video_calls)`,
			dest:  &purge.DeviceVideoCalls,
			label: "device video call references",
		},
	}
	if !conversationBefore.IsZero() {
		for index, statement := range conversationStatements {
			var args []any
			if index == 0 {
				args = []any{conversationBefore}
			}
			tag, err := r.pool.Exec(ctx, statement.query, args...)
			if err != nil {
				return purge, fmt.Errorf(
					"purge retained %s: %w",
					statement.label,
					err,
				)
			}
			*statement.dest = tag.RowsAffected()
		}
	}
	return purge, nil
}

// PurgeUsageReports removes aggregated usage-ledger days older than the
// configured window. Both the device-reported and the gateway-recorded ledgers
// are pruned by report day so the guardian usage report stays bounded. The
// caller passes a cutoff date; every row strictly before it is removed.
func (r *PostgresRepository) PurgeUsageReports(
	ctx context.Context,
	before time.Time,
) (UsageReportPurge, error) {
	var purge UsageReportPurge
	statements := []struct {
		query string
		dest  *int64
		label string
	}{
		{
			query: `DELETE FROM device_usage_daily
				WHERE report_date < $1`,
			dest:  &purge.DeviceUsageDays,
			label: "device usage report days",
		},
		{
			query: `DELETE FROM platform_usage_daily
				WHERE usage_day < $1`,
			dest:  &purge.PlatformUsageDays,
			label: "platform usage report days",
		},
	}
	for _, statement := range statements {
		tag, err := r.pool.Exec(ctx, statement.query, before)
		if err != nil {
			return purge, fmt.Errorf(
				"purge %s: %w",
				statement.label,
				err,
			)
		}
		*statement.dest = tag.RowsAffected()
	}
	return purge, nil
}

// AppendAudit inserts one safe operation event.
func (r *PostgresRepository) AppendAudit(
	ctx context.Context,
	entry *domain.AuditEntry,
) error {
	detail, err := encodeJSONObject(entry.Detail)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO platform_parent_audit (
			id,
			actor_account_id,
			target_account_id,
			action,
			detail,
			created_at
		)
		VALUES (
			$1,
			NULLIF($2, '')::uuid,
			NULLIF($3, '')::uuid,
			$4,
			$5::jsonb,
			$6
		)
	`,
		entry.ID,
		entry.ActorAccountID,
		entry.TargetAccountID,
		entry.Action,
		detail,
		entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert platform audit event: %w", err)
	}
	return nil
}

// QueryAudit returns a paginated, filtered administrator audit projection.
func (r *PostgresRepository) QueryAudit(
	ctx context.Context,
	filter domain.AuditFilter,
) (*domain.AuditPage, error) {
	filter = normalizeAuditFilter(filter)
	conditions := make([]string, 0, 6)
	arguments := make([]any, 0, 8)
	addCondition := func(sql string, value any) {
		arguments = append(arguments, value)
		conditions = append(
			conditions,
			strings.Replace(sql, "?", fmt.Sprintf("$%d", len(arguments)), 1),
		)
	}
	if filter.Action != "" {
		addCondition("audit.action = ?", filter.Action)
	}
	if filter.ActorAccountID != "" {
		addCondition("audit.actor_account_id::text = ?", filter.ActorAccountID)
	}
	if filter.TargetAccountID != "" {
		addCondition("audit.target_account_id::text = ?", filter.TargetAccountID)
	}
	if filter.From != nil {
		addCondition("audit.created_at >= ?", *filter.From)
	}
	if filter.To != nil {
		addCondition("audit.created_at <= ?", *filter.To)
	}
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	countQuery := `
		SELECT COUNT(*)
		FROM platform_parent_audit AS audit
	` + " " + whereClause
	if err := r.pool.QueryRow(ctx, countQuery, arguments...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count platform audit events: %w", err)
	}

	pageArguments := append([]any(nil), arguments...)
	pageArguments = append(
		pageArguments,
		filter.PageSize,
		(filter.Page-1)*filter.PageSize,
	)
	rows, err := r.pool.Query(ctx, `
		SELECT
			audit.id,
			COALESCE(audit.actor_account_id::text, ''),
			COALESCE(actor.display_name, ''),
			COALESCE(audit.target_account_id::text, ''),
			COALESCE(target.display_name, ''),
			audit.action,
			audit.detail,
			audit.created_at
		FROM platform_parent_audit AS audit
		LEFT JOIN parent_accounts AS actor
		  ON actor.id = audit.actor_account_id
		LEFT JOIN parent_accounts AS target
		  ON target.id = audit.target_account_id
		`+whereClause+`
		ORDER BY audit.created_at DESC, audit.id DESC
		LIMIT $`+fmt.Sprintf("%d", len(pageArguments)-1)+
		` OFFSET $`+fmt.Sprintf("%d", len(pageArguments)),
		pageArguments...,
	)
	if err != nil {
		return nil, fmt.Errorf("list platform audit events: %w", err)
	}
	defer rows.Close()

	items := make([]domain.AuditEntry, 0)
	for rows.Next() {
		entry, scanErr := scanAuditEntry(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate platform audit events: %w", err)
	}
	actions, err := r.listAuditActions(ctx)
	if err != nil {
		return nil, err
	}
	return &domain.AuditPage{
		Items:    items,
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
		Actions:  actions,
	}, nil
}

func (r *PostgresRepository) listAuditActions(
	ctx context.Context,
) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT action
		FROM platform_parent_audit
		ORDER BY action ASC
		LIMIT 200
	`)
	if err != nil {
		return nil, fmt.Errorf("list platform audit actions: %w", err)
	}
	defer rows.Close()
	actions := make([]string, 0)
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			return nil, fmt.Errorf("scan platform audit action: %w", err)
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate platform audit actions: %w", err)
	}
	return actions, nil
}

type scanner interface {
	Scan(destinations ...any) error
}

func scanConsentEvent(row scanner) (*domain.ConsentEvent, error) {
	var event domain.ConsentEvent
	var detail []byte
	err := row.Scan(
		&event.ID,
		&event.ParentAccountID,
		&event.ConsentType,
		&event.ConsentVersion,
		&event.Granted,
		&event.Source,
		&detail,
		&event.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan guardian consent event: %w", err)
	}
	if err := json.Unmarshal(detail, &event.Detail); err != nil {
		return nil, fmt.Errorf("decode guardian consent detail: %w", err)
	}
	return &event, nil
}

func scanExportReceipt(row scanner) (*domain.ExportReceipt, error) {
	var receipt domain.ExportReceipt
	var counts []byte
	err := row.Scan(
		&receipt.ID,
		&receipt.Format,
		&receipt.Status,
		&counts,
		&receipt.CreatedAt,
		&receipt.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAccountUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("scan guardian export receipt: %w", err)
	}
	if err := json.Unmarshal(counts, &receipt.ItemCounts); err != nil {
		return nil, fmt.Errorf("decode guardian export counts: %w", err)
	}
	if receipt.ItemCounts == nil {
		receipt.ItemCounts = map[string]int{}
	}
	return &receipt, nil
}

func scanDeletionRequest(row scanner) (*domain.DeletionRequest, error) {
	var request domain.DeletionRequest
	err := row.Scan(
		&request.ID,
		&request.Status,
		&request.Reason,
		&request.RequestedAt,
		&request.ExecuteAfter,
		&request.CancelledAt,
		&request.CompletedAt,
		&request.FailureReason,
		&request.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrDeletionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan guardian deletion request: %w", err)
	}
	request.Cancellable = request.Status == domain.DeletionStatusPending
	request.ScheduledFor = &request.ExecuteAfter
	return &request, nil
}

func scanAuditEntry(row scanner) (*domain.AuditEntry, error) {
	var entry domain.AuditEntry
	var detail []byte
	err := row.Scan(
		&entry.ID,
		&entry.ActorAccountID,
		&entry.ActorDisplayName,
		&entry.TargetAccountID,
		&entry.TargetDisplayName,
		&entry.Action,
		&detail,
		&entry.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan platform audit event: %w", err)
	}
	if err := json.Unmarshal(detail, &entry.Detail); err != nil {
		return nil, fmt.Errorf("decode platform audit detail: %w", err)
	}
	if entry.Detail == nil {
		entry.Detail = map[string]any{}
	}
	return &entry, nil
}

func normalizeAuditFilter(filter domain.AuditFilter) domain.AuditFilter {
	filter.Action = strings.TrimSpace(filter.Action)
	filter.ActorAccountID = strings.TrimSpace(filter.ActorAccountID)
	filter.TargetAccountID = strings.TrimSpace(filter.TargetAccountID)
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = domain.DefaultAuditPageSize
	}
	if filter.PageSize > domain.MaxAuditPageSize {
		filter.PageSize = domain.MaxAuditPageSize
	}
	return filter
}

func encodeJSONObject(value map[string]any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode JSON object: %w", err)
	}
	return encoded, nil
}

func encodeJSONObjectFromInts(value map[string]int) ([]byte, error) {
	if value == nil {
		value = map[string]int{}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode JSON counts: %w", err)
	}
	return encoded, nil
}

var _ Repository = (*PostgresRepository)(nil)

// The methods below are implemented in repository_lifecycle.go so the core
// consent/export/audit file stays readable.
//
// Time is injected through service-level clocks; this file only persists the
// resulting timestamps.
var _ = time.Time{}

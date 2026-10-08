// Package repository persists device diagnostic events and query projections.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultQueryLimit = 20
	maxQueryLimit     = 100
	// Keep the provisioning projection ordered by the same device-reported
	// heartbeat timestamp used by the runtime status upsert.
	provisioningFreshnessPredicate = "device_runtime_status.reported_at <= $7"
)

// Repository defines the diagnostic persistence and query contract.
type Repository interface {
	SaveDiagnostics(
		ctx context.Context,
		deviceID string,
		reportedAt time.Time,
		diagnostics *domain.Diagnostics,
	) error
	GetDiagnostics(
		ctx context.Context,
		deviceID string,
		limit int,
	) (*domain.Snapshot, error)
	SaveProvisioning(
		ctx context.Context,
		deviceID string,
		reportedAt time.Time,
		provisioning *domain.Provisioning,
	) error
	GetProvisioning(
		ctx context.Context,
		deviceID string,
		limit int,
	) (*domain.ProvisioningSnapshot, error)
}

type provisioningStatusScanner interface {
	Scan(dest ...any) error
}

// PostgresRepository stores diagnostic events in the platform database.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a PostgreSQL-backed diagnostic repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// SaveDiagnostics inserts every reported event idempotently, then applies the
// per-device retention limits. All writes share one transaction so a partial
// heartbeat extension can never leave only part of the diagnostic set stored.
func (r *PostgresRepository) SaveDiagnostics(
	ctx context.Context,
	deviceID string,
	reportedAt time.Time,
	diagnostics *domain.Diagnostics,
) error {
	if diagnostics == nil {
		return nil
	}

	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin diagnostic transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()

	for index := range diagnostics.BootEvents {
		event := diagnostics.BootEvents[index]
		if _, err := transaction.Exec(ctx, `
			INSERT INTO device_boot_events (
				id, device_id, event_id, sequence, uptime_ms, boot_count,
				reset_reason, firmware_version, reported_at, received_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
			ON CONFLICT (device_id, event_id) DO NOTHING
		`,
			uuid.NewString(),
			deviceID,
			event.EventID,
			event.Sequence,
			event.UptimeMS,
			event.BootCount,
			event.ResetReason,
			event.FirmwareVersion,
			reportedAt,
		); err != nil {
			return fmt.Errorf("insert device boot event: %w", err)
		}
	}

	for index := range diagnostics.RecoveryEvents {
		event := diagnostics.RecoveryEvents[index]
		if _, err := transaction.Exec(ctx, `
			INSERT INTO device_recovery_events (
				id, device_id, event_id, sequence, module_name,
				firmware_version, reported_at, received_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
			ON CONFLICT (device_id, event_id) DO NOTHING
		`,
			uuid.NewString(),
			deviceID,
			event.EventID,
			event.Sequence,
			event.ModuleName,
			event.FirmwareVersion,
			reportedAt,
		); err != nil {
			return fmt.Errorf("insert device recovery event: %w", err)
		}
	}

	for index := range diagnostics.InteractionEvents {
		event := diagnostics.InteractionEvents[index]
		if _, err := transaction.Exec(ctx, `
			INSERT INTO device_interaction_events (
				id, device_id, event_id, sequence, event_type, detail_code,
				duration_ms, firmware_version, reported_at, received_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
			ON CONFLICT (device_id, event_id) DO NOTHING
		`,
			uuid.NewString(),
			deviceID,
			event.EventID,
			event.Sequence,
			event.EventType,
			event.DetailCode,
			event.DurationMS,
			event.FirmwareVersion,
			reportedAt,
		); err != nil {
			return fmt.Errorf("insert device interaction event: %w", err)
		}
	}

	if diagnostics.LatestFailure != nil {
		failure := diagnostics.LatestFailure
		if _, err := transaction.Exec(ctx, `
			INSERT INTO device_module_failures (
				id, device_id, event_id, sequence, module_name, error_code,
				failure_count, firmware_version, reported_at, received_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
			ON CONFLICT (device_id, event_id) DO NOTHING
		`,
			uuid.NewString(),
			deviceID,
			failure.EventID,
			failure.Sequence,
			failure.ModuleName,
			failure.ErrorCode,
			failure.FailureCount,
			failure.FirmwareVersion,
			reportedAt,
		); err != nil {
			return fmt.Errorf("insert device module failure: %w", err)
		}
	}

	if err := pruneDiagnostics(ctx, transaction, deviceID); err != nil {
		return err
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit diagnostic transaction: %w", err)
	}
	return nil
}

func pruneDiagnostics(
	ctx context.Context,
	transaction pgx.Tx,
	deviceID string,
) error {
	statements := []struct {
		name  string
		table string
		limit int
	}{
		{name: "boot", table: "device_boot_events", limit: domain.RetentionBootEvents},
		{name: "failure", table: "device_module_failures", limit: domain.RetentionFailures},
		{name: "recovery", table: "device_recovery_events", limit: domain.RetentionRecovery},
		{name: "interaction", table: "device_interaction_events", limit: domain.RetentionInteraction},
		{name: "provisioning", table: "device_provisioning_events", limit: domain.RetentionProvisioning},
	}
	for _, statement := range statements {
		sql := fmt.Sprintf(`
			DELETE FROM %s
			WHERE device_id = $1
			  AND id NOT IN (
				SELECT id
				FROM %s
				WHERE device_id = $1
				ORDER BY received_at DESC, id DESC
				LIMIT $2
			  )
		`, statement.table, statement.table)
		if _, err := transaction.Exec(ctx, sql, deviceID, statement.limit); err != nil {
			return fmt.Errorf("prune device %s events: %w", statement.name, err)
		}
	}
	return nil
}

// GetDiagnostics returns the newest bounded diagnostic history for one device.
func (r *PostgresRepository) GetDiagnostics(
	ctx context.Context,
	deviceID string,
	limit int,
) (*domain.Snapshot, error) {
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}

	snapshot := &domain.Snapshot{
		DeviceID:             deviceID,
		BootEvents:           make([]domain.BootEvent, 0),
		Failures:             make([]domain.ModuleFailure, 0),
		RecoveryEvents:       make([]domain.RecoveryEvent, 0),
		InteractionEvents:    make([]domain.InteractionEvent, 0),
		HealthState:          domain.HealthUnknown,
		RetentionBoot:        domain.RetentionBootEvents,
		RetentionFailures:    domain.RetentionFailures,
		RetentionRecovery:    domain.RetentionRecovery,
		RetentionInteraction: domain.RetentionInteraction,
	}

	bootRows, err := r.pool.Query(ctx, `
		SELECT event_id, sequence, uptime_ms, boot_count,
		       reset_reason, firmware_version, reported_at
		FROM device_boot_events
		WHERE device_id = $1
		ORDER BY sequence DESC, received_at DESC, id DESC
		LIMIT $2
	`, deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("query device boot events: %w", err)
	}
	defer bootRows.Close()
	for bootRows.Next() {
		event := domain.BootEvent{EventType: domain.EventTypeBoot}
		if err := bootRows.Scan(
			&event.EventID,
			&event.Sequence,
			&event.UptimeMS,
			&event.BootCount,
			&event.ResetReason,
			&event.FirmwareVersion,
			&event.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan device boot event: %w", err)
		}
		snapshot.BootEvents = append(snapshot.BootEvents, event)
		snapshot.UpdatedAt = laterTime(snapshot.UpdatedAt, event.ReportedAt)
	}
	if err := bootRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device boot events: %w", err)
	}

	failureRows, err := r.pool.Query(ctx, `
		SELECT event_id, sequence, module_name, error_code,
		       failure_count, firmware_version, reported_at
		FROM device_module_failures
		WHERE device_id = $1
		ORDER BY sequence DESC, received_at DESC, id DESC
		LIMIT $2
	`, deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("query device module failures: %w", err)
	}
	defer failureRows.Close()
	for failureRows.Next() {
		failure := domain.ModuleFailure{EventType: domain.EventTypeFailure}
		if err := failureRows.Scan(
			&failure.EventID,
			&failure.Sequence,
			&failure.ModuleName,
			&failure.ErrorCode,
			&failure.FailureCount,
			&failure.FirmwareVersion,
			&failure.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan device module failure: %w", err)
		}
		snapshot.Failures = append(snapshot.Failures, failure)
		snapshot.UpdatedAt = laterTime(snapshot.UpdatedAt, failure.ReportedAt)
	}
	if err := failureRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device module failures: %w", err)
	}
	if len(snapshot.Failures) > 0 {
		latest := snapshot.Failures[0]
		snapshot.LatestFailure = &latest
	}

	recoveryRows, err := r.pool.Query(ctx, `
		SELECT event_id, sequence, module_name, firmware_version, reported_at
		FROM device_recovery_events
		WHERE device_id = $1
		ORDER BY sequence DESC, received_at DESC, id DESC
		LIMIT $2
	`, deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("query device recovery events: %w", err)
	}
	defer recoveryRows.Close()
	for recoveryRows.Next() {
		event := domain.RecoveryEvent{EventType: domain.EventTypeRecovered}
		if err := recoveryRows.Scan(
			&event.EventID,
			&event.Sequence,
			&event.ModuleName,
			&event.FirmwareVersion,
			&event.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan device recovery event: %w", err)
		}
		snapshot.RecoveryEvents = append(snapshot.RecoveryEvents, event)
		snapshot.UpdatedAt = laterTime(snapshot.UpdatedAt, event.ReportedAt)
	}
	if err := recoveryRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device recovery events: %w", err)
	}

	interactionRows, err := r.pool.Query(ctx, `
		SELECT event_id, sequence, event_type, detail_code,
		       duration_ms, firmware_version, reported_at
		FROM device_interaction_events
		WHERE device_id = $1
		ORDER BY sequence DESC, received_at DESC, id DESC
		LIMIT $2
	`, deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("query device interaction events: %w", err)
	}
	defer interactionRows.Close()
	for interactionRows.Next() {
		event := domain.InteractionEvent{}
		if err := interactionRows.Scan(
			&event.EventID,
			&event.Sequence,
			&event.EventType,
			&event.DetailCode,
			&event.DurationMS,
			&event.FirmwareVersion,
			&event.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan device interaction event: %w", err)
		}
		snapshot.InteractionEvents = append(snapshot.InteractionEvents, event)
		snapshot.UpdatedAt = laterTime(snapshot.UpdatedAt, event.ReportedAt)
	}
	if err := interactionRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device interaction events: %w", err)
	}

	if err := r.populateCounters(ctx, snapshot, deviceID); err != nil {
		return nil, err
	}
	snapshot.HealthState = deriveHealthState(snapshot)
	if snapshot.UpdatedAt.IsZero() {
		return nil, domain.ErrEventNotFound
	}
	return snapshot, nil
}

func (r *PostgresRepository) populateCounters(
	ctx context.Context,
	snapshot *domain.Snapshot,
	deviceID string,
) error {
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM device_module_failures
		WHERE device_id = $1
	`, deviceID).Scan(&snapshot.ErrorCount); err != nil {
		return fmt.Errorf("count device module failures: %w", err)
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM device_recovery_events
		WHERE device_id = $1
	`, deviceID).Scan(&snapshot.RecoveryCount); err != nil {
		return fmt.Errorf("count device recovery events: %w", err)
	}
	return nil
}

func deriveHealthState(snapshot *domain.Snapshot) string {
	if snapshot.LatestFailure == nil {
		return domain.HealthHealthy
	}
	// Sequence, not wall-clock time, is the cross-event ordering contract.
	// A failure and its recovery can be delivered in the same heartbeat.
	if len(snapshot.RecoveryEvents) > 0 &&
		snapshot.RecoveryEvents[0].Sequence >
			snapshot.LatestFailure.Sequence {
		return domain.HealthDegraded
	}
	return domain.HealthFaulted
}

// SaveProvisioning inserts every reported provisioning event idempotently and
// mirrors the current state into the runtime status row. Both writes share one
// transaction so the admin list can never show a state that disagrees with the
// event history it was derived from.
func (r *PostgresRepository) SaveProvisioning(
	ctx context.Context,
	deviceID string,
	reportedAt time.Time,
	provisioning *domain.Provisioning,
) error {
	if provisioning == nil {
		return nil
	}

	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin provisioning transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()

	for index := range provisioning.Events {
		event := provisioning.Events[index]
		if _, err := transaction.Exec(ctx, `
			INSERT INTO device_provisioning_events (
				id, device_id, event_id, sequence, event_type, detail_code,
				duration_ms, firmware_version, reported_at, received_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
			ON CONFLICT (device_id, event_id) DO NOTHING
		`,
			uuid.NewString(),
			deviceID,
			event.EventID,
			event.Sequence,
			event.EventType,
			event.DetailCode,
			event.DurationMS,
			event.FirmwareVersion,
			reportedAt,
		); err != nil {
			return fmt.Errorf("insert device provisioning event: %w", err)
		}
	}

	if _, err := transaction.Exec(ctx, `
		UPDATE device_runtime_status
		SET provisioning_state = $2,
		    wifi_configured = $3,
		    session_state = $4,
		    last_provisioned_at = $5,
		    provisioning_dropped_events = $6,
		    updated_at = NOW()
		WHERE device_id = $1
		  AND `+provisioningFreshnessPredicate+`
	`,
		deviceID,
		provisioning.State,
		provisioning.WiFiConfigured,
		provisioning.SessionState,
		provisioning.LastProvisionedAt,
		provisioning.DroppedEvents,
		reportedAt,
	); err != nil {
		return fmt.Errorf("update device provisioning status: %w", err)
	}

	if err := pruneDiagnostics(ctx, transaction, deviceID); err != nil {
		return err
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit provisioning transaction: %w", err)
	}
	return nil
}

// GetProvisioning returns the newest bounded provisioning history for one
// device together with the current state mirrored on the runtime status row.
func (r *PostgresRepository) GetProvisioning(
	ctx context.Context,
	deviceID string,
	limit int,
) (*domain.ProvisioningSnapshot, error) {
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}

	snapshot := &domain.ProvisioningSnapshot{
		DeviceID:        deviceID,
		State:           domain.ProvisioningStateUnprovisioned,
		SessionState:    domain.SessionStateReady,
		Events:          make([]domain.ProvisioningEvent, 0),
		RetentionEvents: domain.RetentionProvisioning,
	}

	var storedState, storedSessionState *string
	var storedWiFiConfigured *bool
	var storedDroppedEvents *uint64
	statusRow := r.pool.QueryRow(ctx, `
		SELECT provisioning_state, wifi_configured, session_state,
		       last_provisioned_at, provisioning_dropped_events, updated_at
		FROM device_runtime_status
		WHERE device_id = $1
	`, deviceID)
	if err := scanProvisioningStatus(
		statusRow,
		&storedState,
		&storedWiFiConfigured,
		&storedSessionState,
		&snapshot.LastProvisionedAt,
		&storedDroppedEvents,
		&snapshot.UpdatedAt,
	); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("query device provisioning status: %w", err)
	}
	if storedState != nil {
		snapshot.State = *storedState
	}
	if storedSessionState != nil {
		snapshot.SessionState = *storedSessionState
	}
	if storedWiFiConfigured != nil {
		snapshot.WiFiConfigured = *storedWiFiConfigured
	}
	if storedDroppedEvents != nil {
		snapshot.DroppedEvents = *storedDroppedEvents
	}

	rows, err := r.pool.Query(ctx, `
		SELECT event_id, sequence, event_type, detail_code,
		       duration_ms, firmware_version, reported_at
		FROM device_provisioning_events
		WHERE device_id = $1
		ORDER BY sequence DESC, received_at DESC, id DESC
		LIMIT $2
	`, deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("query device provisioning events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event domain.ProvisioningEvent
		if err := rows.Scan(
			&event.EventID,
			&event.Sequence,
			&event.EventType,
			&event.DetailCode,
			&event.DurationMS,
			&event.FirmwareVersion,
			&event.ReportedAt,
		); err != nil {
			return nil, fmt.Errorf("scan device provisioning event: %w", err)
		}
		if event.Sequence > snapshot.NewestSequence {
			snapshot.NewestSequence = event.Sequence
		}
		snapshot.Events = append(snapshot.Events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device provisioning events: %w", err)
	}

	if snapshot.UpdatedAt.IsZero() && len(snapshot.Events) == 0 {
		return nil, domain.ErrEventNotFound
	}
	return snapshot, nil
}

func scanProvisioningStatus(
	row provisioningStatusScanner,
	state **string,
	wifiConfigured **bool,
	sessionState **string,
	lastProvisionedAt **time.Time,
	droppedEvents **uint64,
	updatedAt *time.Time,
) error {
	return row.Scan(
		state,
		wifiConfigured,
		sessionState,
		lastProvisionedAt,
		droppedEvents,
		updatedAt,
	)
}

func laterTime(current time.Time, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate
	}
	return current
}

var _ Repository = (*PostgresRepository)(nil)

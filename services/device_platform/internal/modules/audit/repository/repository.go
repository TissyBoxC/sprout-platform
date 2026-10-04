// Package repository persists device diagnostic events and query projections.
package repository

import (
	"context"
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

func laterTime(current time.Time, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate
	}
	return current
}

var _ Repository = (*PostgresRepository)(nil)

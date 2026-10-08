// Package repository persists device runtime status, heartbeats, and commands.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines device runtime persistence operations.
type Repository interface {
	SaveHeartbeat(ctx context.Context, heartbeat *domain.Heartbeat) (bool, error)
	GetStatus(ctx context.Context, deviceID string) (*domain.RuntimeStatus, error)
	ListStatuses(ctx context.Context, deviceIDs []string) ([]domain.RuntimeStatus, error)
	ListAllStatuses(ctx context.Context) ([]domain.RuntimeStatus, error)
	CreateCommand(ctx context.Context, command *domain.Command) error
	GetCommand(ctx context.Context, commandID string) (*domain.Command, error)
	ListCommands(ctx context.Context, deviceID string) ([]domain.Command, error)
	ListPendingCommands(ctx context.Context, deviceID string) ([]domain.Command, error)
	AcknowledgeCommand(
		ctx context.Context,
		commandID string,
		deviceID string,
		status domain.CommandStatus,
		resultCode string,
	) error
}

// PostgresRepository is the PostgreSQL-backed runtime repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a runtime repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// SaveHeartbeat stores one heartbeat snapshot idempotently.
//
// The insert into device_runtime_heartbeats is the idempotency gate. A
// retransmitted QoS 1 message returns inserted=false and leaves both the
// history and latest-status rows unchanged.
func (r *PostgresRepository) SaveHeartbeat(
	ctx context.Context,
	heartbeat *domain.Heartbeat,
) (bool, error) {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin heartbeat transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()

	tag, err := transaction.Exec(ctx, `
		INSERT INTO device_runtime_heartbeats (
			device_id,
			heartbeat_id,
			reported_at,
			received_at
		)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (device_id, heartbeat_id) DO NOTHING
	`,
		heartbeat.DeviceID,
		heartbeat.HeartbeatID,
		heartbeat.ReportedAt,
		heartbeat.ReceivedAt,
	)
	if err != nil {
		return false, fmt.Errorf("insert heartbeat snapshot: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	_, err = transaction.Exec(ctx, `
		INSERT INTO device_runtime_status (
			device_id,
			last_heartbeat_id,
			connection_state,
			transport,
			network_quality_level,
			rssi_dbm,
			latency_ms,
			packet_loss_percent,
			time_sync_state,
			time_sync_source,
			time_synced_at,
			time_offset_ms,
			offline_state,
			offline_reason,
			fallback_active,
			pending_telemetry,
			firmware_version,
			reported_at,
			received_at,
			updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19, $20
		)
		ON CONFLICT (device_id) DO UPDATE
		SET last_heartbeat_id = EXCLUDED.last_heartbeat_id,
		    connection_state = EXCLUDED.connection_state,
		    transport = EXCLUDED.transport,
		    network_quality_level = EXCLUDED.network_quality_level,
		    rssi_dbm = EXCLUDED.rssi_dbm,
		    latency_ms = EXCLUDED.latency_ms,
		    packet_loss_percent = EXCLUDED.packet_loss_percent,
		    time_sync_state = EXCLUDED.time_sync_state,
		    time_sync_source = EXCLUDED.time_sync_source,
		    time_synced_at = EXCLUDED.time_synced_at,
		    time_offset_ms = EXCLUDED.time_offset_ms,
		    offline_state = EXCLUDED.offline_state,
		    offline_reason = EXCLUDED.offline_reason,
		    fallback_active = EXCLUDED.fallback_active,
		    pending_telemetry = EXCLUDED.pending_telemetry,
		    firmware_version = EXCLUDED.firmware_version,
		    reported_at = EXCLUDED.reported_at,
		    received_at = EXCLUDED.received_at,
		    updated_at = EXCLUDED.updated_at
		WHERE device_runtime_status.received_at <= EXCLUDED.received_at
	`,
		heartbeat.DeviceID,
		heartbeat.HeartbeatID,
		heartbeat.ConnectionState,
		heartbeat.Transport,
		heartbeat.NetworkQuality,
		heartbeat.RSSIDBM,
		heartbeat.LatencyMS,
		heartbeat.PacketLossPercent,
		heartbeat.TimeSyncState,
		heartbeat.TimeSyncSource,
		heartbeat.TimeSyncedAt,
		heartbeat.TimeOffsetMS,
		heartbeat.OfflineState,
		heartbeat.OfflineReason,
		heartbeat.FallbackActive,
		heartbeat.PendingTelemetry,
		heartbeat.FirmwareVersion,
		heartbeat.ReportedAt,
		heartbeat.ReceivedAt,
		heartbeat.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("upsert device runtime status: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit heartbeat transaction: %w", err)
	}
	return true, nil
}

// GetStatus loads one latest runtime snapshot.
func (r *PostgresRepository) GetStatus(
	ctx context.Context,
	deviceID string,
) (*domain.RuntimeStatus, error) {
	row := r.pool.QueryRow(ctx, runtimeStatusSelect+`
		WHERE status.device_id = $1
	`, deviceID)
	status, err := scanRuntimeStatus(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrDeviceNotFound
	}
	if err != nil {
		return nil, err
	}
	return status, nil
}

// ListStatuses loads runtime snapshots for a bounded set of device ids.
func (r *PostgresRepository) ListStatuses(
	ctx context.Context,
	deviceIDs []string,
) ([]domain.RuntimeStatus, error) {
	if len(deviceIDs) == 0 {
		return []domain.RuntimeStatus{}, nil
	}
	rows, err := r.pool.Query(ctx, runtimeStatusSelect+`
		WHERE status.device_id = ANY($1)
		ORDER BY status.received_at DESC
	`, deviceIDs)
	if err != nil {
		return nil, fmt.Errorf("list device runtime statuses: %w", err)
	}
	defer rows.Close()
	return scanRuntimeStatuses(rows)
}

// ListAllStatuses loads status for the operations console.
func (r *PostgresRepository) ListAllStatuses(
	ctx context.Context,
) ([]domain.RuntimeStatus, error) {
	rows, err := r.pool.Query(ctx, runtimeStatusSelect+`
		ORDER BY status.received_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list all device runtime statuses: %w", err)
	}
	defer rows.Close()
	return scanRuntimeStatuses(rows)
}

// CreateCommand stores one idempotent maintenance command.
func (r *PostgresRepository) CreateCommand(
	ctx context.Context,
	command *domain.Command,
) error {
	payload, err := json.Marshal(command.Payload)
	if err != nil {
		return fmt.Errorf("encode runtime command payload: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO device_runtime_commands (
			id,
			device_id,
			command_type,
			payload,
			status,
			requested_by,
			request_id,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		command.ID,
		command.DeviceID,
		command.Type,
		payload,
		command.Status,
		command.RequestedBy,
		command.RequestID,
		command.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert device runtime command: %w", err)
	}
	return nil
}

// GetCommand loads one command by its opaque id.
func (r *PostgresRepository) GetCommand(
	ctx context.Context,
	commandID string,
) (*domain.Command, error) {
	row := r.pool.QueryRow(ctx, runtimeCommandSelect+`
		WHERE id = $1
	`, commandID)
	command, err := scanRuntimeCommand(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrCommandNotFound
	}
	if err != nil {
		return nil, err
	}
	return command, nil
}

// ListCommands returns recent commands for one device.
func (r *PostgresRepository) ListCommands(
	ctx context.Context,
	deviceID string,
) ([]domain.Command, error) {
	rows, err := r.pool.Query(ctx, runtimeCommandSelect+`
		WHERE device_id = $1
		ORDER BY created_at DESC
		LIMIT 100
	`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("list device runtime commands: %w", err)
	}
	defer rows.Close()

	commands := make([]domain.Command, 0)
	for rows.Next() {
		command, err := scanRuntimeCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, *command)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device runtime commands: %w", err)
	}
	return commands, nil
}

// ListPendingCommands returns commands a device has not acknowledged yet.
func (r *PostgresRepository) ListPendingCommands(
	ctx context.Context,
	deviceID string,
) ([]domain.Command, error) {
	rows, err := r.pool.Query(ctx, runtimeCommandSelect+`
		WHERE device_id = $1
		  AND status IN ('pending', 'delivered')
		ORDER BY created_at ASC
		LIMIT 20
	`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("list pending device runtime commands: %w", err)
	}
	defer rows.Close()

	commands := make([]domain.Command, 0)
	for rows.Next() {
		command, err := scanRuntimeCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, *command)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending device runtime commands: %w", err)
	}
	return commands, nil
}

// AcknowledgeCommand records the device result once.
func (r *PostgresRepository) AcknowledgeCommand(
	ctx context.Context,
	commandID string,
	deviceID string,
	status domain.CommandStatus,
	resultCode string,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE device_runtime_commands
		SET status = $3,
		    delivered_at = COALESCE(delivered_at, NOW()),
		    acknowledged_at = NOW(),
		    result_code = $4
		WHERE id = $1
		  AND device_id = $2
		  AND status IN ('pending', 'delivered')
	`, commandID, deviceID, status, resultCode)
	if err != nil {
		return fmt.Errorf("acknowledge runtime command: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCommandNotFound
	}
	return nil
}

const runtimeStatusSelect = `
	SELECT
		status.device_id,
		status.last_heartbeat_id,
		status.reported_at,
		status.firmware_version,
		status.connection_state,
		status.transport,
		status.network_quality_level,
		status.rssi_dbm,
		status.latency_ms,
		status.packet_loss_percent,
		status.time_sync_state,
		status.time_sync_source,
		status.time_synced_at,
		status.time_offset_ms,
		status.offline_state,
		status.offline_reason,
		status.fallback_active,
		status.pending_telemetry,
		status.provisioning_state,
		status.wifi_configured,
		status.session_state,
		status.last_provisioned_at,
		status.received_at,
		status.updated_at
	FROM device_runtime_status AS status
`

const runtimeCommandSelect = `
	SELECT
		id,
		device_id,
		command_type,
		payload,
		status,
		requested_by,
		request_id,
		created_at,
		delivered_at,
		acknowledged_at,
		result_code
	FROM device_runtime_commands
`

type runtimeScanner interface {
	Scan(dest ...any) error
}

func scanRuntimeStatus(row runtimeScanner) (*domain.RuntimeStatus, error) {
	var status domain.RuntimeStatus
	// Provisioning columns are nullable so a device that only ever reports the
	// base heartbeat keeps the unprovisioned defaults instead of failing to
	// scan. Defaults are applied after the scan when the columns are NULL.
	var provisioningState *string
	var wifiConfigured *bool
	var sessionState *string
	err := row.Scan(
		&status.DeviceID,
		&status.HeartbeatID,
		&status.ReportedAt,
		&status.FirmwareVersion,
		&status.ConnectionState,
		&status.Transport,
		&status.NetworkQuality,
		&status.RSSIDBM,
		&status.LatencyMS,
		&status.PacketLossPercent,
		&status.TimeSyncState,
		&status.TimeSyncSource,
		&status.TimeSyncedAt,
		&status.TimeOffsetMS,
		&status.OfflineState,
		&status.OfflineReason,
		&status.FallbackActive,
		&status.PendingTelemetry,
		&provisioningState,
		&wifiConfigured,
		&sessionState,
		&status.LastProvisionedAt,
		&status.ReceivedAt,
		&status.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	status.ProvisioningState = domain.ProvisioningStateUnprovisioned
	if provisioningState != nil {
		status.ProvisioningState = domain.ProvisioningState(*provisioningState)
	}
	if wifiConfigured != nil {
		status.WiFiConfigured = *wifiConfigured
	}
	status.SessionState = domain.SessionStateReady
	if sessionState != nil {
		status.SessionState = domain.SessionState(*sessionState)
	}
	return &status, nil
}

func scanRuntimeStatuses(rows pgx.Rows) ([]domain.RuntimeStatus, error) {
	statuses := make([]domain.RuntimeStatus, 0)
	for rows.Next() {
		status, err := scanRuntimeStatus(rows)
		if err != nil {
			return nil, fmt.Errorf("scan device runtime status: %w", err)
		}
		statuses = append(statuses, *status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device runtime statuses: %w", err)
	}
	return statuses, nil
}

func scanRuntimeCommand(row runtimeScanner) (*domain.Command, error) {
	var command domain.Command
	var payload []byte
	err := row.Scan(
		&command.ID,
		&command.DeviceID,
		&command.Type,
		&payload,
		&command.Status,
		&command.RequestedBy,
		&command.RequestID,
		&command.CreatedAt,
		&command.DeliveredAt,
		&command.AcknowledgedAt,
		&command.ResultCode,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &command.Payload); err != nil {
		return nil, fmt.Errorf("decode runtime command payload: %w", err)
	}
	return &command, nil
}

var _ Repository = (*PostgresRepository)(nil)

// Package repository provides the read-only telemetry aggregation queries.
//
// The underlying heartbeat, network and usage rows are owned by
// device_runtime and usage_report. This repository only reads them; it never
// writes another module's tables, which keeps the telemetry boundary
// removable and prevents a second source of truth.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/telemetry/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines the read-only telemetry aggregation surface.
type Repository interface {
	// Summary returns fleet telemetry counters and histograms over the window.
	Summary(
		ctx context.Context,
		windowDays int,
		onlineThreshold time.Duration,
	) (domain.Summary, error)
}

// PostgresRepository reads fleet telemetry from the owning module tables.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a telemetry repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a telemetry repository with an
// injected time source for deterministic tests.
func NewPostgresRepositoryWithClock(
	pool *pgxpool.Pool,
	timeSource clock.Clock,
) *PostgresRepository {
	repository := NewPostgresRepository(pool)
	if timeSource != nil {
		repository.clock = timeSource
	}
	return repository
}

// Summary returns the fleet telemetry projection for the requested window.
func (r *PostgresRepository) Summary(
	ctx context.Context,
	windowDays int,
	onlineThreshold time.Duration,
) (domain.Summary, error) {
	summary := domain.Summary{
		WindowDays:            windowDays,
		OnlineThresholdSecond: int(onlineThreshold.Seconds()),
		NetworkQualityBuckets: []domain.Count{},
		TemperatureBuckets:    []domain.Count{},
		TransportBuckets:      []domain.Count{},
		Devices:               []domain.DeviceTelemetry{},
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (
				WHERE connection_state = 'online'
				  AND received_at >= NOW() - ($2::int * INTERVAL '1 millisecond')
			),
			COUNT(*) FILTER (
				WHERE network_quality_level IN ('poor', 'fair')
			),
			COUNT(*) FILTER (
				WHERE temperature_state IN ('warm', 'hot')
			),
			COUNT(*) FILTER (
				WHERE temperature_state = 'critical'
			),
			COALESCE(SUM(pending_telemetry), 0),
			COUNT(*) FILTER (
				WHERE received_at >= date_trunc('day', NOW())
			)
		FROM device_runtime_status
		WHERE received_at >= NOW() - ($1::int * INTERVAL '1 day')
	`, windowDays, onlineThreshold.Milliseconds()).Scan(
		&summary.DeviceCount,
		&summary.OnlineCount,
		&summary.DegradedNetworkCount,
		&summary.WarmDeviceCount,
		&summary.CriticalDeviceCount,
		&summary.PendingTelemetryTotal,
		&summary.ReportedTodayCount,
	); err != nil {
		return domain.Summary{}, fmt.Errorf("read telemetry summary: %w", err)
	}
	if summary.DeviceCount >= summary.OnlineCount {
		summary.OfflineCount = summary.DeviceCount - summary.OnlineCount
	}

	networkBuckets, err := r.buckets(
		ctx,
		windowDays,
		"network_quality_level",
	)
	if err != nil {
		return domain.Summary{}, err
	}
	summary.NetworkQualityBuckets = networkBuckets
	temperatureBuckets, err := r.buckets(ctx, windowDays, "temperature_state")
	if err != nil {
		return domain.Summary{}, err
	}
	summary.TemperatureBuckets = temperatureBuckets
	transportBuckets, err := r.buckets(ctx, windowDays, "transport")
	if err != nil {
		return domain.Summary{}, err
	}
	summary.TransportBuckets = transportBuckets

	devices, err := r.devices(ctx, windowDays, onlineThreshold)
	if err != nil {
		return domain.Summary{}, err
	}
	summary.Devices = devices
	summary.GeneratedAt = r.clock.Now().UTC()
	return summary, nil
}

// buckets returns one column's histogram over the window. The column is chosen
// from a fixed allow-list so it can never be attacker-controlled.
func (r *PostgresRepository) buckets(
	ctx context.Context,
	windowDays int,
	column string,
) ([]domain.Count, error) {
	query := ""
	switch column {
	case "network_quality_level":
		query = `
			SELECT network_quality_level AS bucket, COUNT(*)
			FROM device_runtime_status
			WHERE received_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	case "temperature_state":
		query = `
			SELECT temperature_state AS bucket, COUNT(*)
			FROM device_runtime_status
			WHERE received_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	case "transport":
		query = `
			SELECT transport AS bucket, COUNT(*)
			FROM device_runtime_status
			WHERE received_at >= NOW() - ($1::int * INTERVAL '1 day')
			GROUP BY bucket
			ORDER BY bucket
		`
	default:
		return nil, fmt.Errorf("unsupported telemetry bucket column %q", column)
	}
	rows, err := r.pool.Query(ctx, query, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read telemetry %s buckets: %w", column, err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 6)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan telemetry %s bucket: %w", column, err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate telemetry %s buckets: %w", column, err)
	}
	return buckets, nil
}

// devices returns the bounded per-device projection. The list is capped so a
// fleet summary request can never stream an unbounded result set.
func (r *PostgresRepository) devices(
	ctx context.Context,
	windowDays int,
	onlineThreshold time.Duration,
) ([]domain.DeviceTelemetry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT device_id,
			connection_state,
			transport,
			network_quality_level,
			rssi_dbm,
			latency_ms,
			packet_loss_percent,
			temperature_state,
			temperature_celsius_tenths,
			pending_telemetry,
			firmware_version,
			reported_at,
			(
				connection_state = 'online'
				AND received_at >= NOW() - ($2::int * INTERVAL '1 millisecond')
			)
		FROM device_runtime_status
		WHERE received_at >= NOW() - ($1::int * INTERVAL '1 day')
		ORDER BY received_at DESC
		LIMIT 200
	`, windowDays, onlineThreshold.Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("list telemetry devices: %w", err)
	}
	defer rows.Close()
	devices := make([]domain.DeviceTelemetry, 0, 32)
	for rows.Next() {
		var device domain.DeviceTelemetry
		if err := rows.Scan(
			&device.DeviceID,
			&device.ConnectionState,
			&device.Transport,
			&device.NetworkQuality,
			&device.RSSIDBM,
			&device.LatencyMS,
			&device.PacketLossPercent,
			&device.TemperatureState,
			&device.TemperatureTenths,
			&device.PendingTelemetry,
			&device.FirmwareVersion,
			&device.ReportedAt,
			&device.IsOnline,
		); err != nil {
			return nil, fmt.Errorf("scan telemetry device: %w", err)
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate telemetry devices: %w", err)
	}
	return devices, nil
}

var _ Repository = (*PostgresRepository)(nil)

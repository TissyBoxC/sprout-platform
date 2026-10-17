package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/database"
)

func TestSaveHeartbeatRejectsExpiredSnapshotWithoutRollingBackCapabilities(t *testing.T) {
	dsn := os.Getenv("DEVICE_PLATFORM_DATABASE_DSN")
	if dsn == "" {
		t.Skip("DEVICE_PLATFORM_DATABASE_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open and migrate database: %v", err)
	}
	defer store.Close(context.Background())
	pool := store.Pool()

	const deviceID = "device_runtime_snapshot_test"
	if _, err := pool.Exec(ctx, `
		DELETE FROM device_runtime_heartbeats WHERE device_id = $1
	`, deviceID); err != nil {
		t.Fatalf("clear heartbeat history: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO parent_accounts (
			id, email, phone, password_hash, display_name,
			guardian_consent_version, guardian_consented_at
		)
		VALUES (
			'00000000-0000-0000-0000-000000000062',
			'capability-test@example.com',
			'13800000061',
			'test-hash',
			'capability test',
			'1.0.0',
			NOW()
		)
		ON CONFLICT (id) DO NOTHING
	`); err != nil {
		t.Fatalf("seed parent account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO device_credentials (
			device_id, hardware_model, firmware_version, capability_set,
			capability_revision, public_key, status, registered_at, updated_at
		)
		VALUES ($1, 'test-model', '1.1.0', ARRAY['wifi', 'camera'], 2, 'test-key', 'active', NOW(), NOW())
		ON CONFLICT (device_id) DO UPDATE
		SET firmware_version = EXCLUDED.firmware_version,
		    capability_set = EXCLUDED.capability_set,
		    capability_revision = EXCLUDED.capability_revision,
		    updated_at = EXCLUDED.updated_at
	`, deviceID); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO device_bindings (
			id, parent_account_id, device_id, device_name, hardware_model,
			firmware_version, capability_set, capability_revision, bound_at, updated_at
		)
		VALUES (
			'00000000-0000-0000-0000-000000000061',
			'00000000-0000-0000-0000-000000000062',
			$1, 'test-device', 'test-model', '1.1.0',
			ARRAY['wifi', 'camera'], 2, NOW(), NOW()
		)
		ON CONFLICT (device_id) DO UPDATE
		SET firmware_version = EXCLUDED.firmware_version,
		    capability_set = EXCLUDED.capability_set,
		    capability_revision = EXCLUDED.capability_revision,
		    updated_at = EXCLUDED.updated_at
	`, deviceID); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	repository := NewPostgresRepository(pool)
	newer := heartbeatFixture(
		deviceID,
		"heartbeat_newer_0001",
		now,
		"1.1.0",
		[]string{"wifi", "camera"},
		2,
	)
	if inserted, err := repository.SaveHeartbeat(ctx, newer); err != nil || !inserted {
		t.Fatalf("save newer heartbeat: inserted=%v err=%v", inserted, err)
	}
	older := heartbeatFixture(
		deviceID,
		"heartbeat_older_0001",
		now.Add(-5*time.Minute),
		"1.0.0",
		[]string{"wifi"},
		1,
	)
	if inserted, err := repository.SaveHeartbeat(ctx, older); err != nil || !inserted {
		t.Fatalf("save older heartbeat: inserted=%v err=%v", inserted, err)
	}

	var runtimeRevision, credentialRevision, bindingRevision uint64
	var runtimeCapabilities, credentialCapabilities, bindingCapabilities []string
	if err := pool.QueryRow(ctx, `
		SELECT status.capability_revision, status.capabilities,
		       credential.capability_revision, credential.capability_set,
		       binding.capability_revision, binding.capability_set
		FROM device_runtime_status AS status
		JOIN device_credentials AS credential USING (device_id)
		JOIN device_bindings AS binding USING (device_id)
		WHERE status.device_id = $1
	`, deviceID).Scan(
		&runtimeRevision,
		&runtimeCapabilities,
		&credentialRevision,
		&credentialCapabilities,
		&bindingRevision,
		&bindingCapabilities,
	); err != nil {
		t.Fatalf("load snapshots: %v", err)
	}
	if runtimeRevision != 2 || credentialRevision != 2 || bindingRevision != 2 {
		t.Fatalf(
			"expected revisions 2/2/2, got %d/%d/%d",
			runtimeRevision,
			credentialRevision,
			bindingRevision,
		)
	}
	for name, capabilities := range map[string][]string{
		"runtime":    runtimeCapabilities,
		"credential": credentialCapabilities,
		"binding":    bindingCapabilities,
	} {
		if len(capabilities) != 2 || capabilities[0] != "wifi" || capabilities[1] != "camera" {
			t.Fatalf("%s capabilities rolled back: %v", name, capabilities)
		}
	}
}

func heartbeatFixture(
	deviceID string,
	heartbeatID string,
	reportedAt time.Time,
	firmwareVersion string,
	capabilities []string,
	revision uint64,
) *domain.Heartbeat {
	return &domain.Heartbeat{
		DeviceID:           deviceID,
		HeartbeatID:        heartbeatID,
		ReportedAt:         reportedAt,
		FirmwareVersion:    firmwareVersion,
		Capabilities:       capabilities,
		CapabilityRevision: revision,
		ConnectionState:    domain.ConnectionStateOnline,
		Transport:          domain.TransportWiFi,
		NetworkQuality:     domain.NetworkQualityGood,
		RSSIDBM:            -50,
		LatencyMS:          20,
		PacketLossPercent:  0,
		Temperature:        domain.TemperatureStateUnknown,
		TimeSyncState:      domain.TimeSyncSynchronized,
		TimeSyncSource:     domain.TimeSyncSourcePlatform,
		OfflineState:       domain.OfflineStateOnline,
		OfflineReason:      domain.OfflineReasonNone,
		ReceivedAt:         reportedAt.Add(time.Second),
		UpdatedAt:          reportedAt.Add(time.Second),
	}
}

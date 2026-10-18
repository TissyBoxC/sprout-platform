package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/database"
)

func TestPostgresHealthSnapshotRejectsOlderObservation(t *testing.T) {
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

	repository := NewPostgresRepository(store.Pool())
	const featureID = "voice_gateway"
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := store.Pool().Exec(
		ctx,
		"DELETE FROM platform_feature_health_snapshots WHERE feature_id = $1",
		featureID,
	); err != nil {
		t.Fatalf("clear health snapshot: %v", err)
	}
	newer := &domain.HealthSnapshot{
		FeatureID: featureID,
		Status:    domain.HealthHealthy,
		CheckedAt: now,
		Message:   "功能运行正常",
		LatencyMS: 10,
		Trigger:   domain.HealthTriggerSchedule,
	}
	if err := repository.SaveHealth(ctx, newer); err != nil {
		t.Fatalf("save newer snapshot: %v", err)
	}
	older := &domain.HealthSnapshot{
		FeatureID: featureID,
		Status:    domain.HealthUnavailable,
		CheckedAt: now.Add(-time.Minute),
		Message:   "功能暂时不可用",
		LatencyMS: 99,
		Trigger:   domain.HealthTriggerManual,
	}
	if err := repository.SaveHealth(ctx, older); !errors.Is(err, domain.ErrHealthSnapshotStale) {
		t.Fatalf("older snapshot = %v, want stale snapshot error", err)
	}
	stored, err := repository.GetHealth(ctx, featureID)
	if err != nil {
		t.Fatalf("load stored snapshot: %v", err)
	}
	if stored.Status != domain.HealthHealthy || !stored.CheckedAt.Equal(newer.CheckedAt) {
		t.Fatalf("newer snapshot was overwritten: %+v", stored)
	}
}

func TestPostgresSchedulerLeaseIsExclusiveAcrossConnections(t *testing.T) {
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

	first := NewPostgresRepository(store.Pool())
	second := NewPostgresRepository(store.Pool())
	entered := make(chan struct{})
	release := make(chan struct{})
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- first.WithLeaderLease(ctx, func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatalf("first lease did not acquire: %v", ctx.Err())
	}

	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	err = second.WithLeaderLease(ctx, func(context.Context) error {
		return errors.New("non-leader work must not run")
	})
	if !errors.Is(err, domain.ErrSchedulerLeaseUnavailable) {
		t.Fatalf("second lease = %v, want unavailable", err)
	}
	close(release)
	select {
	case err := <-firstErr:
		if err != nil {
			t.Fatalf("first lease: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("first lease did not finish: %v", ctx.Err())
	}
}

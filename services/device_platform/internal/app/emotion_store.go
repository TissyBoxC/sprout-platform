package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/domain"
	emotionrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/repository"
	emotionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/service"
)

// emotionStore adapts the emotion repository to the service contract.
//
// The service must not import the repository, so the repository's sentinels are
// translated here into the ones the service knows about. Everything else passes
// through untouched, which keeps this adapter free of business decisions.
type emotionStore struct {
	repository *emotionrepository.Repository
}

func (store emotionStore) GetSettings(
	ctx context.Context,
	familyID string,
) (domain.Settings, error) {
	settings, err := store.repository.GetSettings(ctx, familyID)
	if errors.Is(err, emotionrepository.ErrSettingsNotFound) {
		return domain.Settings{}, emotionservice.StoreSentinelNotFound
	}
	return settings, err
}

func (store emotionStore) SaveSettings(ctx context.Context, settings domain.Settings) error {
	return store.repository.SaveSettings(ctx, settings)
}

func (store emotionStore) DeviceFamily(ctx context.Context, deviceID string) (string, error) {
	return store.repository.DeviceFamily(ctx, deviceID)
}

func (store emotionStore) DeviceChildBinding(
	ctx context.Context,
	familyID, deviceID, childID string,
) (bool, error) {
	return store.repository.DeviceChildBinding(ctx, familyID, deviceID, childID)
}

func (store emotionStore) InsertObservation(
	ctx context.Context,
	observation domain.Observation,
) error {
	err := store.repository.InsertObservation(ctx, observation)
	if errors.Is(err, emotionrepository.ErrDeviceNotBound) {
		return emotionservice.StoreSentinelNotBound
	}
	return err
}

func (store emotionStore) CountLabels(
	ctx context.Context,
	familyID, childID string,
	from, to time.Time,
) (map[domain.Label]domain.LabelCount, error) {
	return store.repository.CountLabels(ctx, familyID, childID, from, to)
}

func (store emotionStore) DeleteBefore(
	ctx context.Context,
	familyID string,
	cutoff time.Time,
) (int, error) {
	return store.repository.DeleteBefore(ctx, familyID, cutoff)
}

func (store emotionStore) ListEnabledFamilies(
	ctx context.Context,
	limit int,
) ([]domain.Settings, error) {
	return store.repository.ListEnabledFamilies(ctx, limit)
}

func (store emotionStore) CountEnabledFamilies(ctx context.Context) (int, error) {
	return store.repository.CountEnabledFamilies(ctx)
}

func (store emotionStore) CountObservationsSince(
	ctx context.Context,
	since time.Time,
) (int, error) {
	return store.repository.CountObservationsSince(ctx, since)
}

// Compile-time proof the adapter satisfies the service contract.
var _ emotionservice.Store = emotionStore{}

// emotionSwitches reads the family's emotion setting for the record path.
//
// It reads the table on every report rather than caching a decision, so a
// guardian turning sensing off takes effect on the very next sample. A family
// without a row is DISABLED: the feature is never enabled by default.
type emotionSwitches struct {
	pool *pgxpool.Pool
}

// SettingsEnabled reports whether the family turned sensing on.
func (switches emotionSwitches) SettingsEnabled(
	ctx context.Context,
	familyID string,
) (bool, error) {
	var enabled bool
	err := switches.pool.QueryRow(ctx,
		`SELECT is_enabled FROM emotion_settings WHERE family_id = $1::uuid`,
		familyID,
	).Scan(&enabled)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, err
	}
	return enabled, nil
}

// Compile-time proof the switches satisfy the service contract.
var _ emotionservice.SettingsReader = emotionSwitches{}

// emotionDevices resolves which family owns a device and which child it serves.
//
// It reads the binding tables directly rather than calling the device binding
// service, so a mood report still resolves while that service is busy with a
// provisioning write. Ownership is also re-checked by the store inside the
// insert, so a device unbound mid-report cannot slip a sample through.
type emotionDevices struct {
	pool *pgxpool.Pool
}

// DeviceFamily returns the owning family, or an empty string for an unknown
// device.
func (devices emotionDevices) DeviceFamily(ctx context.Context, deviceID string) (string, error) {
	var familyID string
	err := devices.pool.QueryRow(ctx,
		`SELECT COALESCE(parent_account_id::text, '') FROM device_bindings
		 WHERE device_id = $1::text`,
		deviceID,
	).Scan(&familyID)
	if err != nil {
		if isNoRows(err) {
			return "", nil
		}
		return "", err
	}
	return familyID, nil
}

// DeviceChild returns the device's active child, or an empty string when the
// device serves none.
func (devices emotionDevices) DeviceChild(ctx context.Context, deviceID string) (string, error) {
	var childID string
	err := devices.pool.QueryRow(ctx,
		`SELECT COALESCE(binding.child_id::text, '')
		 FROM device_child_bindings binding
		 JOIN child_profiles profile ON profile.id = binding.child_id
		 WHERE binding.device_id = $1::text AND binding.is_active`,
		deviceID,
	).Scan(&childID)
	if err != nil {
		if isNoRows(err) {
			return "", nil
		}
		return "", err
	}
	return childID, nil
}

// Compile-time proof the device lookup satisfies the service contract.
var _ emotionservice.DeviceLookup = emotionDevices{}

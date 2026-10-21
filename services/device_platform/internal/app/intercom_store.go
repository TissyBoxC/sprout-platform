package app

import (
	"context"
	"errors"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/domain"
	intercomrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/repository"
	intercomservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/service"
)

// intercomStore adapts the repository to the service's Store contract.
//
// The two packages deliberately do not import each other, so this thin adapter
// is the single place that translates storage sentinels into service sentinels.
type intercomStore struct {
	repository *intercomrepository.Repository
}

func (store intercomStore) CreateRinging(
	ctx context.Context,
	callID, familyID, deviceID, childID string,
	startedAt time.Time,
) (domain.Call, error) {
	call, err := store.repository.CreateRinging(ctx, callID, familyID, deviceID, childID, startedAt)
	return call, mapIntercomStoreError(err)
}

func (store intercomStore) Get(ctx context.Context, familyID, callID string) (domain.Call, error) {
	call, err := store.repository.Get(ctx, familyID, callID)
	return call, mapIntercomStoreError(err)
}

func (store intercomStore) GetForDevice(ctx context.Context, deviceID, callID string) (domain.Call, error) {
	call, err := store.repository.GetForDevice(ctx, deviceID, callID)
	return call, mapIntercomStoreError(err)
}

func (store intercomStore) Transition(
	ctx context.Context,
	call domain.Call,
	expected domain.CallState,
) (domain.Call, error) {
	updated, err := store.repository.Transition(ctx, call, expected)
	return updated, mapIntercomStoreError(err)
}

func (store intercomStore) CountStartedSince(ctx context.Context, familyID string, since time.Time) (int, error) {
	return store.repository.CountStartedSince(ctx, familyID, since)
}

func (store intercomStore) ListOpen(ctx context.Context) ([]domain.Call, error) {
	return store.repository.ListOpen(ctx)
}

func (store intercomStore) History(ctx context.Context, familyID string, limit int) ([]domain.Call, error) {
	return store.repository.History(ctx, familyID, limit)
}

func (store intercomStore) CountOpen(ctx context.Context) (int, error) {
	return store.repository.CountOpen(ctx)
}

func (store intercomStore) CountCallsSince(ctx context.Context, since time.Time) (int, error) {
	return store.repository.CountCallsSince(ctx, since)
}

func mapIntercomStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, intercomrepository.ErrCallNotFound):
		return intercomservice.StoreSentinelNotFound
	case errors.Is(err, intercomrepository.ErrDeviceBusy):
		return intercomservice.StoreSentinelBusy
	default:
		return err
	}
}

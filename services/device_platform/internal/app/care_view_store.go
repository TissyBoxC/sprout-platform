package app

import (
	"context"
	"errors"
	"time"

	careviewrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/domain"
	careviewservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/service"
)

// careViewStore adapts the repository to the service contract.
//
// The service must not import the repository, so the repository's own sentinels
// are translated here into the ones the service knows about.
type careViewStore struct {
	repository *careviewrepository.Repository
}

func (store careViewStore) Create(ctx context.Context, session domain.Session) error {
	err := store.repository.Create(ctx, session)
	if errors.Is(err, careviewrepository.ErrDeviceBusy) {
		return careviewservice.StoreSentinelBusy
	}
	return err
}

func (store careViewStore) Get(ctx context.Context, familyID, sessionID string) (domain.Session, error) {
	session, err := store.repository.Get(ctx, familyID, sessionID)
	if errors.Is(err, careviewrepository.ErrSessionNotFound) {
		return domain.Session{}, careviewservice.StoreSentinelNotFound
	}
	return session, err
}

func (store careViewStore) GetForDevice(ctx context.Context, deviceID, sessionID string) (domain.Session, error) {
	session, err := store.repository.GetForDevice(ctx, deviceID, sessionID)
	if errors.Is(err, careviewrepository.ErrSessionNotFound) {
		return domain.Session{}, careviewservice.StoreSentinelNotFound
	}
	return session, err
}

func (store careViewStore) Transition(
	ctx context.Context,
	session domain.Session,
	expectedState domain.ViewState,
) error {
	err := store.repository.Transition(ctx, session, expectedState)
	if errors.Is(err, careviewrepository.ErrDeviceBusy) {
		return careviewservice.StoreSentinelBusy
	}
	return err
}

func (store careViewStore) ListOpen(ctx context.Context) ([]domain.Session, error) {
	return store.repository.ListOpen(ctx)
}

func (store careViewStore) History(ctx context.Context, familyID string, limit int) ([]domain.Session, error) {
	return store.repository.History(ctx, familyID, limit)
}

func (store careViewStore) CountOpen(ctx context.Context) (int, error) {
	return store.repository.CountOpen(ctx)
}

func (store careViewStore) CountSessionsSince(ctx context.Context, familyID string, since time.Time) (int, error) {
	return store.repository.CountSessionsSince(ctx, familyID, since)
}

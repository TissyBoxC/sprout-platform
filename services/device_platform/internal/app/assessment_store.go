package app

import (
	"context"
	"errors"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/domain"
	assessmentrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/repository"
	assessmentService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/service"
)

// assessmentStore adapts the repository to the service contract.
//
// The service must not import the repository, so the repository's own sentinels
// are translated here into the ones the service knows about.
type assessmentStore struct {
	repository *assessmentrepository.Repository
}

func (store assessmentStore) Create(ctx context.Context, session domain.Session) error {
	err := store.repository.Create(ctx, session)
	if errors.Is(err, assessmentrepository.ErrAlreadyActive) {
		return assessmentService.StoreSentinelAlreadyActive
	}
	return err
}

func (store assessmentStore) Get(
	ctx context.Context,
	familyID, sessionID string,
) (domain.Session, error) {
	session, err := store.repository.Get(ctx, familyID, sessionID)
	if errors.Is(err, assessmentrepository.ErrSessionNotFound) {
		return domain.Session{}, assessmentService.StoreSentinelNotFound
	}
	return session, err
}

func (store assessmentStore) GetForDevice(
	ctx context.Context,
	deviceID, sessionID string,
) (domain.Session, error) {
	session, err := store.repository.GetForDevice(ctx, deviceID, sessionID)
	if errors.Is(err, assessmentrepository.ErrSessionNotFound) {
		return domain.Session{}, assessmentService.StoreSentinelNotFound
	}
	return session, err
}

func (store assessmentStore) ListExpired(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]domain.Session, error) {
	return store.repository.ListExpired(ctx, now, limit)
}

func (store assessmentStore) History(
	ctx context.Context,
	familyID, childID string,
	limit int,
) ([]domain.Session, error) {
	return store.repository.History(ctx, familyID, childID, limit)
}

func (store assessmentStore) ListAnswers(
	ctx context.Context,
	sessionID string,
) ([]domain.Answer, error) {
	return store.repository.ListAnswers(ctx, sessionID)
}

func (store assessmentStore) RecordAnswer(
	ctx context.Context,
	session domain.Session,
	answer domain.Answer,
	previousAnswered int,
) error {
	err := store.repository.RecordAnswer(ctx, session, answer, previousAnswered)
	if errors.Is(err, assessmentrepository.ErrSessionConflict) {
		return assessmentService.StoreSentinelConflict
	}
	return err
}

func (store assessmentStore) Transition(
	ctx context.Context,
	session domain.Session,
	previousAnswered int,
) error {
	err := store.repository.Transition(ctx, session, previousAnswered)
	if errors.Is(err, assessmentrepository.ErrSessionConflict) {
		return assessmentService.StoreSentinelConflict
	}
	return err
}

func (store assessmentStore) CountActive(ctx context.Context) (int, error) {
	return store.repository.CountActive(ctx)
}

func (store assessmentStore) CountSessionsSince(
	ctx context.Context,
	familyID string,
	since time.Time,
) (int, error) {
	return store.repository.CountSessionsSince(ctx, familyID, since)
}

func (store assessmentStore) CountStartedSince(ctx context.Context, since time.Time) (int, error) {
	return store.repository.CountStartedSince(ctx, since)
}

func (store assessmentStore) ChildInFamily(
	ctx context.Context,
	familyID, childID string,
) (bool, error) {
	return store.repository.ChildInFamily(ctx, familyID, childID)
}

// Compile-time proof the adapter satisfies the service contract.
var _ assessmentService.Store = assessmentStore{}

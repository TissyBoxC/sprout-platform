package service

import (
	"context"
	"errors"
	"testing"
	"time"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
)

type fakeSettingsRepository struct {
	savedSettings  *domain.Settings
	savedActorID   string
	savedVersion   int64
	currentVersion int64
}

func (repository *fakeSettingsRepository) GetSettings(
	context.Context,
) (*domain.Settings, int64, error) {
	return repository.savedSettings, repository.currentVersion, nil
}

func (repository *fakeSettingsRepository) RuntimePolicy(
	context.Context,
) (*domain.RuntimePolicy, error) {
	return nil, nil
}

func (repository *fakeSettingsRepository) SaveSettings(
	_ context.Context,
	settings *domain.Settings,
	actorAccountID string,
	expectedVersion int64,
) (int64, error) {
	if expectedVersion != repository.currentVersion {
		return 0, &domain.SettingsVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  repository.currentVersion,
		}
	}
	repository.savedSettings = settings
	repository.savedActorID = actorAccountID
	repository.savedVersion = expectedVersion
	repository.currentVersion++
	return repository.currentVersion, nil
}

func (*fakeSettingsRepository) Overview(context.Context, time.Duration) (*domain.Overview, error) {
	return nil, nil
}

func (*fakeSettingsRepository) ListFamilyAccounts(context.Context) ([]domain.FamilyAccount, error) {
	return nil, nil
}

func (*fakeSettingsRepository) ParentOverview(context.Context, string, time.Duration) (*authdomain.ParentOverview, error) {
	return nil, nil
}

func (*fakeSettingsRepository) ListReleases(context.Context) ([]domain.Release, error) {
	return nil, nil
}

func (*fakeSettingsRepository) CreateRelease(context.Context, *domain.Release) error {
	return nil
}

func (*fakeSettingsRepository) PublishRelease(context.Context, string, string) error {
	return nil
}

func (*fakeSettingsRepository) DeleteRelease(context.Context, string) error {
	return nil
}

func (*fakeSettingsRepository) FindReleaseArtifact(context.Context, string, string, string) (*domain.ReleaseArtifact, error) {
	return nil, nil
}

func (*fakeSettingsRepository) LatestPublishedUpdate(context.Context, string, string) (*domain.Release, error) {
	return nil, nil
}

func validSettings() *domain.Settings {
	return &domain.Settings{
		AI: domain.AISettings{
			DefaultBalanceUSD:  10,
			DefaultConcurrency: 2,
			DefaultModels:      []string{"model-a"},
		},
		Account: domain.AccountSettings{
			RegistrationEnabled: true,
		},
		Update: domain.UpdateSettings{
			Channel: domain.ReleaseChannelStable,
		},
		Retention: domain.RetentionSettings{
			ConversationDays: 30,
		},
	}
}

func TestUpdateSettingsSavesWithMatchingVersion(t *testing.T) {
	repository := &fakeSettingsRepository{currentVersion: 4}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	settings := validSettings()
	saved, version, err := service.UpdateSettings(context.Background(), settings, " actor-id ", 4)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if saved != settings {
		t.Fatal("expected the validated settings document to be returned")
	}
	if version != 5 {
		t.Fatalf("expected version 5, got %d", version)
	}
	if repository.savedVersion != 4 {
		t.Fatalf("expected repository to receive version 4, got %d", repository.savedVersion)
	}
	if repository.savedActorID != "actor-id" {
		t.Fatalf("expected trimmed actor id, got %q", repository.savedActorID)
	}
}

func TestUpdateSettingsReturnsVersionConflict(t *testing.T) {
	repository := &fakeSettingsRepository{currentVersion: 5}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	_, _, err = service.UpdateSettings(context.Background(), validSettings(), "actor-id", 3)
	if !errors.Is(err, domain.ErrSettingsVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestUpdateSettingsRejectsNegativeVersion(t *testing.T) {
	repository := &fakeSettingsRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	_, _, err = service.UpdateSettings(context.Background(), validSettings(), "actor-id", -1)
	if !errors.Is(err, domain.ErrInvalidSettings) {
		t.Fatalf("expected invalid settings, got %v", err)
	}
}

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
)

type fakeSettingsRepository struct {
	savedSettings    *domain.Settings
	savedActorID     string
	savedVersion     int64
	currentVersion   int64
	requestedChannel string
	latestUpdate     *domain.Release
	familyPage       domain.PageRequest
	releasePage      domain.PageRequest
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

func (repository *fakeSettingsRepository) ListFamilyAccounts(
	_ context.Context,
	page domain.PageRequest,
) ([]domain.FamilyAccount, int, error) {
	repository.familyPage = page
	return nil, 0, nil
}

func (*fakeSettingsRepository) ParentOverview(context.Context, string, time.Duration) (*authdomain.ParentOverview, error) {
	return nil, nil
}

func (repository *fakeSettingsRepository) ListReleases(
	_ context.Context,
	page domain.PageRequest,
) ([]domain.Release, int, error) {
	repository.releasePage = page
	return nil, 0, nil
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

func (repository *fakeSettingsRepository) LatestPublishedUpdate(
	_ context.Context,
	_ string,
	channel string,
) (*domain.Release, error) {
	repository.requestedChannel = channel
	if repository.latestUpdate == nil {
		return nil, domain.ErrReleaseNotFound
	}
	return repository.latestUpdate, nil
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
			UsageReportDays:  domain.DefaultUsageReportRetentionDays,
		},
		Voice: domain.DefaultVoiceSettings(),
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

func TestListFamilyAccountsNormalizesPaginationBeforeRepository(t *testing.T) {
	repository := &fakeSettingsRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	page, err := service.ListFamilyAccounts(context.Background(), 3, 500)
	if err != nil {
		t.Fatalf("list family accounts: %v", err)
	}
	if repository.familyPage.Page != 3 ||
		repository.familyPage.PageSize != domain.MaxListPageSize ||
		repository.familyPage.Offset != 2*domain.MaxListPageSize {
		t.Fatalf("unexpected repository page: %+v", repository.familyPage)
	}
	if page.Pagination.Page != 3 ||
		page.Pagination.PageSize != domain.MaxListPageSize ||
		page.Pagination.Total != 0 ||
		page.Pagination.TotalPages != 0 {
		t.Fatalf("unexpected response pagination: %+v", page.Pagination)
	}
}

func TestListReleasesNormalizesPaginationBeforeRepository(t *testing.T) {
	repository := &fakeSettingsRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	page, err := service.ListReleases(context.Background(), -2, 0)
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	if repository.releasePage.Page != 1 ||
		repository.releasePage.PageSize != domain.DefaultListPageSize ||
		repository.releasePage.Offset != 0 {
		t.Fatalf("unexpected repository page: %+v", repository.releasePage)
	}
	if page.Pagination.Page != 1 ||
		page.Pagination.PageSize != domain.DefaultListPageSize ||
		page.Pagination.Total != 0 ||
		page.Pagination.TotalPages != 0 {
		t.Fatalf("unexpected response pagination: %+v", page.Pagination)
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

func TestUpdateSettingsPreservesStoredVoiceDefaultsWhenOmitted(t *testing.T) {
	repository := &fakeSettingsRepository{
		currentVersion: 2,
		savedSettings: &domain.Settings{
			Voice: domain.VoiceSettings{
				ContinuousConversationEnabled: false,
				IdleWindowSeconds:             25,
				BargeInEnabled:                false,
				FarFieldEnabled:               true,
			},
		},
	}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	settings := validSettings()
	settings.Voice = domain.VoiceSettings{}
	saved, _, err := service.UpdateSettings(
		context.Background(),
		settings,
		"actor-id",
		2,
	)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if saved.Voice != repository.savedSettings.Voice {
		t.Fatalf("expected stored voice defaults preserved, got %+v", saved.Voice)
	}
}

func TestUpdateSettingsRejectsOutOfRangeVoiceIdleWindow(t *testing.T) {
	repository := &fakeSettingsRepository{currentVersion: 1}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	settings := validSettings()
	settings.Voice.IdleWindowSeconds = 1
	settings.SetVoiceConfigured()
	_, _, err = service.UpdateSettings(
		context.Background(),
		settings,
		"actor-id",
		1,
	)
	if !errors.Is(err, domain.ErrInvalidSettings) {
		t.Fatalf("expected invalid settings, got %v", err)
	}
}

// Minor mode is a platform safety invariant, not an operator preference. A
// request that tries to turn it off must be persisted as enabled so the
// console can never display a disabled protection that is actually mandatory.
func TestUpdateSettingsForcesMinorModeDefaultOn(t *testing.T) {
	repository := &fakeSettingsRepository{currentVersion: 3}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	settings := validSettings()
	settings.Safety.MinorModeDefault = false

	saved, _, err := service.UpdateSettings(
		context.Background(),
		settings,
		"actor-id",
		3,
	)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if !saved.Safety.MinorModeDefault {
		t.Fatal("minor mode default was persisted as disabled")
	}
}

// A client that does not pin a channel must be served the operator-configured
// default channel, not an unconditional "stable" fallback that ignores the
// console setting.
func TestAppUpdateUsesConfiguredDefaultChannelWhenClientOmitsChannel(t *testing.T) {
	repository := &fakeSettingsRepository{
		savedSettings: &domain.Settings{
			Update: domain.UpdateSettings{Channel: domain.ReleaseChannelBeta},
		},
		latestUpdate: &domain.Release{
			Kind:        domain.ReleaseKindClient,
			Version:     "1.2.0",
			Channel:     domain.ReleaseChannelBeta,
			DownloadURL: "https://downloads.example.com/app-1.2.0.apk",
			SHA256:      strings.Repeat("a", 64),
		},
	}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	update, err := service.AppUpdate(context.Background(), "android", "", "1.1.0")
	if err != nil {
		t.Fatalf("app update: %v", err)
	}
	if repository.requestedChannel != domain.ReleaseChannelBeta {
		t.Fatalf(
			"requested channel = %q, want %q",
			repository.requestedChannel,
			domain.ReleaseChannelBeta,
		)
	}
	if update == nil || update.Version != "1.2.0" {
		t.Fatalf("unexpected update: %+v", update)
	}
}

// An explicit client channel still overrides the configured default so a
// tester can opt into beta/canary without changing platform settings.
func TestAppUpdateExplicitChannelOverridesDefault(t *testing.T) {
	repository := &fakeSettingsRepository{
		savedSettings: &domain.Settings{
			Update: domain.UpdateSettings{Channel: domain.ReleaseChannelStable},
		},
		latestUpdate: &domain.Release{
			Kind:        domain.ReleaseKindClient,
			Version:     "1.3.0",
			Channel:     domain.ReleaseChannelCanary,
			DownloadURL: "https://downloads.example.com/app-1.3.0.apk",
			SHA256:      strings.Repeat("b", 64),
		},
	}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	if _, err := service.AppUpdate(
		context.Background(),
		"android",
		domain.ReleaseChannelCanary,
		"1.1.0",
	); err != nil {
		t.Fatalf("app update: %v", err)
	}
	if repository.requestedChannel != domain.ReleaseChannelCanary {
		t.Fatalf(
			"requested channel = %q, want %q",
			repository.requestedChannel,
			domain.ReleaseChannelCanary,
		)
	}
}

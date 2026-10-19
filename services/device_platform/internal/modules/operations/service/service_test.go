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
	latestUpdate     *domain.PublishedUpdates
	familyPage       domain.PageRequest
	releasePage      domain.PageRequest
	createdRelease   *domain.Release
	publishedID      string
	deletedID        string
	artifactLookup   domain.ReleaseIdentity
	artifactResult   *domain.ReleaseArtifact
	artifactErr      error
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

func (repository *fakeSettingsRepository) CreateRelease(
	_ context.Context,
	release *domain.Release,
) error {
	repository.createdRelease = release
	return nil
}

func (repository *fakeSettingsRepository) PublishRelease(
	_ context.Context,
	releaseID string,
	_ string,
) error {
	repository.publishedID = releaseID
	return nil
}

func (repository *fakeSettingsRepository) DeleteRelease(
	_ context.Context,
	releaseID string,
) error {
	repository.deletedID = releaseID
	return nil
}

func (repository *fakeSettingsRepository) FindReleaseArtifact(
	_ context.Context,
	version string,
	kind string,
	platform string,
	channel string,
) (*domain.ReleaseArtifact, error) {
	repository.artifactLookup = domain.ReleaseIdentity{
		Version:  version,
		Channel:  channel,
		Kind:     kind,
		Platform: platform,
	}
	return repository.artifactResult, repository.artifactErr
}

func (repository *fakeSettingsRepository) LatestPublishedUpdate(
	_ context.Context,
	_ string,
	channel string,
) (*domain.PublishedUpdates, error) {
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
			RegistrationEnabled:    true,
			GuardianConsentVersion: domain.DefaultGuardianConsentVersion,
		},
		Update: domain.UpdateSettings{
			Channel: domain.ReleaseChannelStable,
		},
		Safety: domain.SafetySettings{
			MinorModeDefault: true,
		},
		Retention: domain.RetentionSettings{
			ConversationDays: 30,
			AudioDays:        0,
			UsageReportDays:  domain.DefaultUsageReportRetentionDays,
		},
		Voice: domain.DefaultVoiceSettings(),
	}
}

func TestUpdateSettingsRejectsPersistedRawAudioRetention(t *testing.T) {
	repository := &fakeSettingsRepository{currentVersion: 1}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	settings := validSettings()
	settings.Retention.AudioDays = 7
	if _, _, err := service.UpdateSettings(
		context.Background(),
		settings,
		"actor-id",
		1,
	); !errors.Is(err, domain.ErrInvalidSettings) {
		t.Fatalf("expected invalid settings for raw audio retention, got %v", err)
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

// Minor mode is a platform safety invariant, not an operator preference. An
// API caller cannot persist a settings document that disables it.
func TestUpdateSettingsRejectsMinorModeDisabled(t *testing.T) {
	repository := &fakeSettingsRepository{currentVersion: 3}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	settings := validSettings()
	settings.Safety.MinorModeDefault = false

	if _, _, err := service.UpdateSettings(
		context.Background(),
		settings,
		"actor-id",
		3,
	); !errors.Is(err, domain.ErrInvalidSettings) {
		t.Fatalf("expected invalid settings for disabled minor mode, got %v", err)
	}
}

func TestUpdateSettingsPreservesPublishedGuardianConsentVersion(t *testing.T) {
	repository := &fakeSettingsRepository{currentVersion: 3}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	settings := validSettings()
	settings.Account.GuardianConsentVersion = " 2026-06 "

	saved, _, err := service.UpdateSettings(
		context.Background(),
		settings,
		"actor-id",
		3,
	)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if saved.Account.GuardianConsentVersion != "2026-06" {
		t.Fatalf(
			"guardian consent version = %q, want %q",
			saved.Account.GuardianConsentVersion,
			"2026-06",
		)
	}
}

func TestUpdateSettingsKeepsStoredConsentVersionForLegacyClients(t *testing.T) {
	repository := &fakeSettingsRepository{
		currentVersion: 3,
		savedSettings: &domain.Settings{
			Account: domain.AccountSettings{
				GuardianConsentVersion: "2026-05",
			},
		},
	}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	settings := validSettings()
	settings.Account.GuardianConsentVersion = ""

	saved, _, err := service.UpdateSettings(
		context.Background(),
		settings,
		"actor-id",
		3,
	)
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if saved.Account.GuardianConsentVersion != "2026-05" {
		t.Fatalf(
			"legacy update overwrote consent version with %q",
			saved.Account.GuardianConsentVersion,
		)
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
		latestUpdate: &domain.PublishedUpdates{
			Client: &domain.Release{
				Kind:        domain.ReleaseKindClient,
				Version:     "1.2.0",
				Channel:     domain.ReleaseChannelBeta,
				DownloadURL: "https://downloads.example.com/app-1.2.0.apk",
				SHA256:      strings.Repeat("a", 64),
			},
		},
	}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	update, err := service.AppUpdate(
		context.Background(),
		"android",
		"",
		"1.1.0",
		"",
	)
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
		latestUpdate: &domain.PublishedUpdates{
			Client: &domain.Release{
				Kind:        domain.ReleaseKindClient,
				Version:     "1.3.0",
				Channel:     domain.ReleaseChannelCanary,
				DownloadURL: "https://downloads.example.com/app-1.3.0.apk",
				SHA256:      strings.Repeat("b", 64),
			},
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
		"",
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

func TestAppUpdateTreatsClientAndResourceVersionsIndependently(t *testing.T) {
	t.Parallel()
	resource := &domain.Release{
		Kind:        domain.ReleaseKindResource,
		Version:     "2.0.0",
		Channel:     domain.ReleaseChannelStable,
		DownloadURL: "https://downloads.example.com/resources-2.0.0.zip",
		SHA256:      strings.Repeat("c", 64),
	}
	client := &domain.Release{
		Kind:        domain.ReleaseKindClient,
		Version:     "1.4.0",
		Channel:     domain.ReleaseChannelStable,
		DownloadURL: "https://downloads.example.com/app-1.4.0.apk",
		SHA256:      strings.Repeat("d", 64),
	}
	repository := &fakeSettingsRepository{
		savedSettings: &domain.Settings{
			Update: domain.UpdateSettings{Channel: domain.ReleaseChannelStable},
		},
		latestUpdate: &domain.PublishedUpdates{
			Client:   client,
			Resource: resource,
		},
	}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	update, err := service.AppUpdate(
		context.Background(),
		"android",
		"",
		"1.4.0",
		"2.0.0",
	)
	if !errors.Is(err, domain.ErrUpdateNotAvailable) || update != nil {
		t.Fatalf("already current update = %#v, error = %v", update, err)
	}

	update, err = service.AppUpdate(
		context.Background(),
		"android",
		"",
		"1.4.0",
		"1.9.0",
	)
	if err != nil {
		t.Fatalf("resource update: %v", err)
	}
	if update == nil || update.Kind != domain.ReleaseKindResource ||
		update.Version != "2.0.0" {
		t.Fatalf("expected resource update, got %#v", update)
	}

	update, err = service.AppUpdate(
		context.Background(),
		"android",
		"",
		"1.3.0",
		"2.0.0",
	)
	if err != nil {
		t.Fatalf("client update: %v", err)
	}
	if update == nil || update.Kind != domain.ReleaseKindClient ||
		update.Version != "1.4.0" {
		t.Fatalf("resource marker hid the client update: %#v", update)
	}
}

func TestCreateReleaseAllowsSameVersionForDifferentArtifactIdentity(t *testing.T) {
	t.Parallel()
	repository := &fakeSettingsRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	input := domain.ReleaseInput{
		Version:             "0.29.0",
		Channel:             domain.ReleaseChannelStable,
		Kind:                domain.ReleaseKindClient,
		Platform:            "android",
		DownloadURL:         "https://download.example.test/0.29.0.apk",
		SHA256:              strings.Repeat("a", 64),
		ReleaseNotes:        "修复设备连接。",
		MinSupportedVersion: "",
	}
	release, err := service.CreateRelease(context.Background(), input)
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	if release.ID == "" || repository.createdRelease == nil {
		t.Fatal("expected a persisted release with its generated identity")
	}
	if repository.createdRelease.Version != input.Version ||
		repository.createdRelease.Kind != input.Kind ||
		repository.createdRelease.Platform != input.Platform ||
		repository.createdRelease.Channel != input.Channel {
		t.Fatalf("unexpected persisted identity: %+v", repository.createdRelease)
	}
}

func TestPublishAndDeleteAddressOneReleaseByID(t *testing.T) {
	t.Parallel()
	repository := &fakeSettingsRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	releaseID := "72453d1e-dce3-401a-96db-e96db54c9fd5"

	if err := service.PublishRelease(context.Background(), releaseID, "admin-id"); err != nil {
		t.Fatalf("publish release: %v", err)
	}
	if err := service.DeleteRelease(context.Background(), releaseID); err != nil {
		t.Fatalf("delete release: %v", err)
	}
	if repository.publishedID != releaseID || repository.deletedID != releaseID {
		t.Fatalf(
			"delegated ids = publish %q delete %q, want %q",
			repository.publishedID,
			repository.deletedID,
			releaseID,
		)
	}
	if err := service.PublishRelease(context.Background(), "0.29.0", "admin-id"); !errors.Is(
		err,
		domain.ErrReleaseNotFound,
	) {
		t.Fatalf("version-only publish error = %v, want release not found", err)
	}
}

func TestFindReleaseArtifactPreservesChannelAndRegistrationIdentity(t *testing.T) {
	t.Parallel()
	expected := &domain.ReleaseArtifact{
		Version:     "0.29.0",
		Kind:        domain.ReleaseKindClient,
		Platform:    "android",
		DownloadURL: "https://download.example.test/0.29.0.apk",
		SHA256:      strings.Repeat("b", 64),
	}
	repository := &fakeSettingsRepository{artifactResult: expected}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	artifact, err := service.FindReleaseArtifact(
		context.Background(),
		"0.29.0",
		domain.ReleaseKindClient,
		"android",
		domain.ReleaseChannelBeta,
	)
	if err != nil {
		t.Fatalf("find artifact: %v", err)
	}
	if artifact != expected {
		t.Fatalf("artifact = %+v, want exact repository result", artifact)
	}
	if repository.artifactLookup != (domain.ReleaseIdentity{
		Version:  "0.29.0",
		Channel:  domain.ReleaseChannelBeta,
		Kind:     domain.ReleaseKindClient,
		Platform: "android",
	}) {
		t.Fatalf("lookup identity = %+v", repository.artifactLookup)
	}
}

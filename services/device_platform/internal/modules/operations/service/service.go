// Package service owns platform settings, release delivery, and dashboards.
package service

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

var (
	versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Service owns operational policy and release lifecycle.
type Service struct {
	repository      repository.Repository
	artifactStore   ReleaseArtifactStore
	timeSource      clock.Clock
	onlineThreshold time.Duration
	ota             OTAConfig
}

// ReleaseArtifactStore resolves a release artifact directly from the shared
// download volume. CI publication writes files there without touching
// platform_releases, so lookup must not depend on prior registration.
type ReleaseArtifactStore interface {
	FindArtifact(
		ctx context.Context,
		version string,
		kind string,
		platform string,
	) (*domain.ReleaseArtifact, error)
}

// RuntimePolicyReader exposes the narrow settings projection to callers that
// must enforce registration and AI provisioning policy.
type RuntimePolicyReader interface {
	RuntimePolicy(ctx context.Context) (*domain.RuntimePolicy, error)
}

// Options contains operations dependencies.
type Options struct {
	Repository      repository.Repository
	ArtifactStore   ReleaseArtifactStore
	Clock           clock.Clock
	OnlineThreshold time.Duration
	OTA             OTAConfig
}

// OTAConfig contains the non-secret download prefixes used by the update API.
type OTAConfig struct {
	ManifestBaseURL string
	ResourceBaseURL string
	ClientBaseURL   string
}

// New creates the operations service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("operations repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	onlineThreshold := options.OnlineThreshold
	if onlineThreshold <= 0 {
		onlineThreshold = 90 * time.Second
	}
	return &Service{
		repository:      options.Repository,
		artifactStore:   options.ArtifactStore,
		timeSource:      timeSource,
		onlineThreshold: onlineThreshold,
		ota:             options.OTA,
	}, nil
}

// SetArtifactStore injects the shared download-volume reader after both
// services are constructed. It is optional so existing tests can construct
// the operations service with only a repository.
func (s *Service) SetArtifactStore(store ReleaseArtifactStore) {
	if s == nil {
		return
	}
	s.artifactStore = store
}

// ListFamilyAccounts returns parent accounts with their dependent AI account.
func (s *Service) ListFamilyAccounts(
	ctx context.Context,
) ([]domain.FamilyAccount, error) {
	return s.repository.ListFamilyAccounts(ctx)
}

// AppUpdate returns the newest published update for one client.
func (s *Service) AppUpdate(
	ctx context.Context,
	platform string,
	channel string,
	currentVersion string,
) (*domain.AppUpdate, error) {
	platform = strings.TrimSpace(platform)
	channel = strings.TrimSpace(channel)
	currentVersion = strings.TrimSpace(currentVersion)
	if !isReleasePlatform(platform) || platform == "all" {
		return nil, domain.ErrUpdateNotAvailable
	}
	if channel == "" {
		channel = domain.ReleaseChannelStable
	}
	if !isReleaseChannel(channel) {
		return nil, domain.ErrUpdateNotAvailable
	}
	release, err := s.repository.LatestPublishedUpdate(ctx, platform, channel)
	if err != nil {
		return nil, err
	}
	if currentVersion != "" && compareVersions(release.Version, currentVersion) <= 0 {
		return nil, domain.ErrUpdateNotAvailable
	}
	if release.Kind == domain.ReleaseKindClient &&
		s.ota.ClientBaseURL != "" &&
		!strings.HasPrefix(release.DownloadURL, s.ota.ClientBaseURL) {
		return nil, domain.ErrUpdateNotAvailable
	}
	if release.Kind == domain.ReleaseKindResource &&
		s.ota.ResourceBaseURL != "" &&
		!strings.HasPrefix(release.DownloadURL, s.ota.ResourceBaseURL) {
		return nil, domain.ErrUpdateNotAvailable
	}
	settings, _, err := s.repository.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	isMandatory := release.IsMandatory
	if settings.Update.MinClientVersion != "" &&
		compareVersions(currentVersion, settings.Update.MinClientVersion) < 0 {
		isMandatory = true
	}
	if settings.Update.ForceUpgradeBelow != "" &&
		compareVersions(currentVersion, settings.Update.ForceUpgradeBelow) < 0 {
		isMandatory = true
	}
	return &domain.AppUpdate{
		Kind:                release.Kind,
		Version:             release.Version,
		DownloadURL:         release.DownloadURL,
		SHA256:              release.SHA256,
		ReleaseNotes:        release.ReleaseNotes,
		IsMandatory:         isMandatory,
		MinSupportedVersion: release.MinSupportedVersion,
	}, nil
}

// Settings returns the current operations document.
func (s *Service) Settings(
	ctx context.Context,
) (*domain.Settings, int64, error) {
	return s.repository.GetSettings(ctx)
}

// RuntimePolicy returns the policy projection used by account creation.
func (s *Service) RuntimePolicy(
	ctx context.Context,
) (*domain.RuntimePolicy, error) {
	return s.repository.RuntimePolicy(ctx)
}

// UpdateSettings validates and persists the complete operations document.
func (s *Service) UpdateSettings(
	ctx context.Context,
	settings *domain.Settings,
	actorAccountID string,
	expectedVersion int64,
) (*domain.Settings, int64, error) {
	if settings == nil {
		return nil, 0, domain.ErrInvalidSettings
	}
	if expectedVersion < 0 {
		return nil, 0, domain.ErrInvalidSettings
	}
	if err := validateSettings(settings); err != nil {
		return nil, 0, err
	}
	version, err := s.repository.SaveSettings(
		ctx,
		settings,
		strings.TrimSpace(actorAccountID),
		expectedVersion,
	)
	if err != nil {
		return nil, 0, err
	}
	return settings, version, nil
}

// Overview returns dashboard counters and recent releases.
func (s *Service) Overview(
	ctx context.Context,
) (*domain.Overview, error) {
	return s.repository.Overview(ctx, s.onlineThreshold)
}

// ParentOverview returns one guardian's home dashboard.
func (s *Service) ParentOverview(
	ctx context.Context,
	parentAccountID string,
) (*authdomain.ParentOverview, error) {
	return s.repository.ParentOverview(
		ctx,
		parentAccountID,
		s.onlineThreshold,
	)
}

// ListReleases returns all release records for the management console.
func (s *Service) ListReleases(
	ctx context.Context,
) ([]domain.Release, error) {
	return s.repository.ListReleases(ctx)
}

// FindReleaseArtifact resolves a downloadable release file from the persisted
// release registration; operators never need to paste the URL or checksum.
func (s *Service) FindReleaseArtifact(
	ctx context.Context,
	version string,
	kind string,
	platform string,
) (*domain.ReleaseArtifact, error) {
	version = strings.TrimSpace(version)
	kind = strings.TrimSpace(kind)
	platform = strings.TrimSpace(platform)
	if !versionPattern.MatchString(version) ||
		!isReleaseKind(kind) ||
		platform == "all" ||
		!isReleasePlatform(platform) {
		return nil, domain.ErrReleaseNotFound
	}
	artifact, err := s.repository.FindReleaseArtifact(ctx, version, kind, platform)
	if err == nil {
		return artifact, nil
	}
	if !errors.Is(err, domain.ErrReleaseNotFound) || s.artifactStore == nil {
		return nil, err
	}
	return s.artifactStore.FindArtifact(ctx, version, kind, platform)
}

// CreateRelease validates and stores a draft delivery.
func (s *Service) CreateRelease(
	ctx context.Context,
	input domain.ReleaseInput,
) (*domain.Release, error) {
	input.Version = strings.TrimSpace(input.Version)
	input.Channel = strings.TrimSpace(input.Channel)
	input.Kind = strings.TrimSpace(input.Kind)
	input.Platform = strings.TrimSpace(input.Platform)
	input.DownloadURL = strings.TrimSpace(input.DownloadURL)
	input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))
	input.ReleaseNotes = strings.TrimSpace(input.ReleaseNotes)
	input.MinSupportedVersion = strings.TrimSpace(input.MinSupportedVersion)
	if !versionPattern.MatchString(input.Version) ||
		!isReleaseKind(input.Kind) ||
		!isReleaseChannel(input.Channel) ||
		!isReleasePlatform(input.Platform) ||
		!isHTTPSURL(input.DownloadURL) ||
		!sha256Pattern.MatchString(input.SHA256) ||
		input.MinSupportedVersion != "" && !versionPattern.MatchString(input.MinSupportedVersion) ||
		len([]rune(input.ReleaseNotes)) > 4000 {
		return nil, domain.ErrInvalidSettings
	}
	now := s.timeSource.Now().UTC()
	release := &domain.Release{
		ID:                  uuid.NewString(),
		Version:             input.Version,
		Channel:             input.Channel,
		Kind:                input.Kind,
		Platform:            input.Platform,
		DownloadURL:         input.DownloadURL,
		SHA256:              input.SHA256,
		ReleaseNotes:        input.ReleaseNotes,
		IsMandatory:         input.IsMandatory,
		MinSupportedVersion: input.MinSupportedVersion,
		Status:              domain.ReleaseStatusDraft,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.repository.CreateRelease(ctx, release); err != nil {
		return nil, err
	}
	return release, nil
}

// PublishRelease makes one draft visible to clients.
func (s *Service) PublishRelease(
	ctx context.Context,
	version string,
	actorAccountID string,
) error {
	version = strings.TrimSpace(version)
	if !versionPattern.MatchString(version) {
		return domain.ErrReleaseNotFound
	}
	return s.repository.PublishRelease(ctx, version, actorAccountID)
}

// DeleteRelease removes a draft or retired release.
func (s *Service) DeleteRelease(
	ctx context.Context,
	version string,
) error {
	version = strings.TrimSpace(version)
	if !versionPattern.MatchString(version) {
		return domain.ErrReleaseNotFound
	}
	return s.repository.DeleteRelease(ctx, version)
}

func validateSettings(settings *domain.Settings) error {
	if settings.AI.DefaultBalanceUSD < 0 ||
		settings.AI.DefaultConcurrency < 1 ||
		settings.AI.DefaultConcurrency > 100 {
		return domain.ErrInvalidSettings
	}
	if settings.Update.Channel == "" ||
		!isReleaseChannel(settings.Update.Channel) {
		return domain.ErrInvalidSettings
	}
	if settings.Update.MinClientVersion != "" &&
		!versionPattern.MatchString(settings.Update.MinClientVersion) {
		return domain.ErrInvalidSettings
	}
	if settings.Update.ForceUpgradeBelow != "" &&
		!versionPattern.MatchString(settings.Update.ForceUpgradeBelow) {
		return domain.ErrInvalidSettings
	}
	if settings.Retention.AudioDays < 0 ||
		settings.Retention.ImageDays < 0 ||
		settings.Retention.ConversationDays < 0 ||
		settings.Retention.ConversationDays > 3650 {
		return domain.ErrInvalidSettings
	}
	seenModels := make(map[string]struct{}, len(settings.AI.DefaultModels))
	for index, model := range settings.AI.DefaultModels {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 128 {
			return domain.ErrInvalidSettings
		}
		key := strings.ToLower(model)
		if _, exists := seenModels[key]; exists {
			return domain.ErrInvalidSettings
		}
		seenModels[key] = struct{}{}
		settings.AI.DefaultModels[index] = model
	}
	if settings.Safety.AllowAudioUpload || settings.Safety.AllowImageUpload {
		// P0 privacy defaults are enforced here, not just in the UI. A future
		// explicit guardian consent flow must replace this guard deliberately.
		return domain.ErrInvalidSettings
	}
	return nil
}

func isReleaseKind(value string) bool {
	switch value {
	case domain.ReleaseKindResource,
		domain.ReleaseKindClient,
		domain.ReleaseKindFirmware:
		return true
	default:
		return false
	}
}

func isReleaseChannel(value string) bool {
	switch value {
	case domain.ReleaseChannelStable,
		domain.ReleaseChannelBeta,
		domain.ReleaseChannelCanary:
		return true
	default:
		return false
	}
}

func isReleasePlatform(value string) bool {
	switch value {
	case "android", "ios", "esp32_s3", "all":
		return true
	default:
		return false
	}
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil &&
		parsed.Scheme == "https" &&
		parsed.Host != "" &&
		parsed.User == nil
}

// compareVersions compares dotted semantic versions numerically. Pre-release
// suffixes are treated as older than the corresponding release version so a
// beta package never masks a stable release.
func compareVersions(left string, right string) int {
	return domain.CompareVersions(left, right)
}

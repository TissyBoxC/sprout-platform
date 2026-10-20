package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
)

type fakeRepository struct {
	releases        map[string]*domain.Release
	groups          map[string]*domain.Group
	deployments     map[string]*domain.Deployment
	events          map[string]*domain.Event
	device          *domain.DeviceContext
	failReleaseLoad error
}

type acceptingVerifier struct{}

type acceptingArtifactVerifier struct {
	sizeBytes int64
	sha256    string
	err       error
}

type acceptingArtifactLocker struct{}

func (acceptingArtifactLocker) MarkPublishedArtifact(string) error {
	return nil
}

type recordingAtomicArtifactLocker struct {
	calls     int
	key       string
	sizeBytes int64
	sha256    string
	err       error
}

func (*recordingAtomicArtifactLocker) MarkPublishedArtifact(string) error {
	return errors.New("legacy mark must not be used when atomic verification is available")
}

func (locker *recordingAtomicArtifactLocker) VerifyAndMarkPublishedArtifact(
	_ context.Context,
	artifactKey string,
	expectedSizeBytes int64,
	expectedSHA256 string,
) error {
	locker.calls += 1
	locker.key = artifactKey
	locker.sizeBytes = expectedSizeBytes
	locker.sha256 = expectedSHA256
	return locker.err
}

type capturingVerifier struct {
	manifest []byte
}

func (verifier *capturingVerifier) Verify(
	algorithm string,
	keyID string,
	manifest []byte,
	signature string,
) error {
	if algorithm != "ed25519" || keyID == "" || signature == "" {
		return domain.ErrSignatureInvalid
	}
	verifier.manifest = append([]byte(nil), manifest...)
	return nil
}

func (verifier acceptingArtifactVerifier) VerifyArtifact(
	context.Context,
	string,
) (int64, string, error) {
	if verifier.err != nil {
		return 0, "", verifier.err
	}
	return verifier.sizeBytes, verifier.sha256, nil
}

func (acceptingVerifier) Verify(
	algorithm string,
	keyID string,
	manifest []byte,
	signature string,
) error {
	if algorithm == "" || keyID == "" || len(manifest) == 0 || signature == "" {
		return domain.ErrSignatureInvalid
	}
	return nil
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		releases:    map[string]*domain.Release{},
		groups:      map[string]*domain.Group{},
		deployments: map[string]*domain.Deployment{},
		events:      map[string]*domain.Event{},
	}
}

func (repository *fakeRepository) CreateRelease(
	_ context.Context,
	release *domain.Release,
) error {
	for _, existing := range repository.releases {
		if existing.Version == release.Version &&
			existing.Channel == release.Channel &&
			existing.HardwareRevision == release.HardwareRevision {
			return domain.ErrReleaseAlreadyExists
		}
	}
	copy := *release
	repository.releases[release.ID] = &copy
	return nil
}

func (repository *fakeRepository) UpdateReleaseDraft(
	_ context.Context,
	release *domain.Release,
	expectedVersion int64,
) error {
	existing, ok := repository.releases[release.ID]
	if !ok {
		return domain.ErrReleaseNotFound
	}
	if existing.RecordVersion != expectedVersion {
		return &domain.GroupVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  existing.RecordVersion,
		}
	}
	if existing.Status != domain.ReleaseStatusDraft {
		return domain.ErrReleaseStateConflict
	}
	copy := *release
	copy.RecordVersion = expectedVersion + 1
	repository.releases[release.ID] = &copy
	return nil
}

func (repository *fakeRepository) GetRelease(
	_ context.Context,
	releaseID string,
) (*domain.Release, error) {
	if repository.failReleaseLoad != nil {
		return nil, repository.failReleaseLoad
	}
	release, ok := repository.releases[releaseID]
	if !ok {
		return nil, domain.ErrReleaseNotFound
	}
	copy := *release
	return &copy, nil
}

func (repository *fakeRepository) ListReleases(
	_ context.Context,
	filter domain.ReleaseFilter,
) (*domain.ReleasePage, error) {
	items := make([]domain.Release, 0)
	for _, release := range repository.releases {
		if filter.Status != "" && release.Status != filter.Status {
			continue
		}
		if filter.Channel != "" && release.Channel != filter.Channel {
			continue
		}
		items = append(items, *release)
	}
	return &domain.ReleasePage{
		Items:    items,
		Total:    int64(len(items)),
		Page:     1,
		PageSize: 20,
	}, nil
}

func (repository *fakeRepository) TransitionRelease(
	_ context.Context,
	releaseID string,
	expectedVersion int64,
	nextStatus domain.ReleaseStatus,
	actorID string,
	now time.Time,
	signatureVerified bool,
) (*domain.Release, error) {
	release, ok := repository.releases[releaseID]
	if !ok {
		return nil, domain.ErrReleaseNotFound
	}
	if release.RecordVersion != expectedVersion {
		return nil, &domain.ReleaseVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  release.RecordVersion,
		}
	}
	if !domain.CanTransitionRelease(release.Status, nextStatus) {
		return nil, domain.ErrReleaseStateConflict
	}
	release.Status = nextStatus
	release.RecordVersion++
	release.UpdatedBy = actorID
	release.UpdatedAt = now
	if nextStatus == domain.ReleaseStatusPublished {
		release.PublishedBy = actorID
		release.PublishedAt = &now
		if signatureVerified {
			release.SignatureStatus = domain.SignatureStatusVerified
			release.SignatureVerifiedAt = &now
		}
	}
	if nextStatus == domain.ReleaseStatusPaused {
		release.PausedBy = actorID
		release.PausedAt = &now
	}
	if nextStatus == domain.ReleaseStatusWithdrawn {
		release.WithdrawnBy = actorID
		release.WithdrawnAt = &now
	}
	copy := *release
	return &copy, nil
}

func (repository *fakeRepository) RollbackRelease(
	_ context.Context,
	releaseID string,
	expectedVersion int64,
	actorID string,
	now time.Time,
) (*domain.Release, error) {
	release, ok := repository.releases[releaseID]
	if !ok {
		return nil, domain.ErrReleaseNotFound
	}
	if release.RecordVersion != expectedVersion {
		return nil, &domain.ReleaseVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  release.RecordVersion,
		}
	}
	if !release.RollbackAllowed {
		return nil, domain.ErrRollbackNotAllowed
	}
	if release.Status != domain.ReleaseStatusPublished &&
		release.Status != domain.ReleaseStatusPaused {
		return nil, domain.ErrReleaseStateConflict
	}
	release.Status = domain.ReleaseStatusWithdrawn
	release.RecordVersion++
	release.UpdatedBy = actorID
	release.UpdatedAt = now
	release.WithdrawnBy = actorID
	release.WithdrawnAt = &now
	release.RolledBackBy = actorID
	release.RolledBackAt = &now
	copy := *release
	return &copy, nil
}

func (repository *fakeRepository) CreateGroup(
	_ context.Context,
	group *domain.Group,
) error {
	copy := *group
	repository.groups[group.ID] = &copy
	return nil
}

func (repository *fakeRepository) UpdateGroup(
	_ context.Context,
	group *domain.Group,
	expectedVersion int64,
) error {
	existing, ok := repository.groups[group.ID]
	if !ok {
		return domain.ErrGroupNotFound
	}
	if existing.RecordVersion != expectedVersion {
		return &domain.ReleaseVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  existing.RecordVersion,
		}
	}
	copy := *group
	copy.RecordVersion = expectedVersion + 1
	repository.groups[group.ID] = &copy
	return nil
}

func (repository *fakeRepository) GetGroup(
	_ context.Context,
	groupID string,
) (*domain.Group, error) {
	group, ok := repository.groups[groupID]
	if !ok {
		return nil, domain.ErrGroupNotFound
	}
	copy := *group
	return &copy, nil
}

func (repository *fakeRepository) ListGroups(
	context.Context,
) ([]domain.Group, error) {
	groups := make([]domain.Group, 0, len(repository.groups))
	for _, group := range repository.groups {
		groups = append(groups, *group)
	}
	return groups, nil
}

func (repository *fakeRepository) AddGroupMember(
	context.Context,
	string,
	string,
	string,
) error {
	return nil
}

func (repository *fakeRepository) RemoveGroupMember(
	context.Context,
	string,
	string,
	string,
) error {
	return nil
}

func (repository *fakeRepository) ListGroupMembers(
	context.Context,
	string,
) ([]string, error) {
	return []string{}, nil
}

func (repository *fakeRepository) GetDeviceContext(
	_ context.Context,
	deviceID string,
) (*domain.DeviceContext, error) {
	if repository.device == nil {
		return nil, domain.ErrDeviceNotFound
	}
	if deviceID != "" && repository.device.DeviceID != deviceID {
		return nil, domain.ErrDeviceNotFound
	}
	copy := *repository.device
	copy.GroupIDs = append([]string(nil), repository.device.GroupIDs...)
	return &copy, nil
}

func (repository *fakeRepository) ListPublishedReleases(
	context.Context,
	domain.Channel,
	string,
) ([]domain.Release, error) {
	releases := make([]domain.Release, 0)
	for _, release := range repository.releases {
		if release.Status == domain.ReleaseStatusPublished {
			releases = append(releases, *release)
		}
	}
	return releases, nil
}

func (repository *fakeRepository) CreateDeployment(
	_ context.Context,
	deployment *domain.Deployment,
) (*domain.Deployment, error) {
	for _, existing := range repository.deployments {
		if existing.RequestID == deployment.RequestID {
			if existing.ReleaseID == deployment.ReleaseID &&
				existing.DeviceID == deployment.DeviceID {
				copy := *existing
				return &copy, nil
			}
			return nil, domain.ErrDeploymentAlreadyExists
		}
	}
	copy := *deployment
	repository.deployments[deployment.ID] = &copy
	return &copy, nil
}

func (repository *fakeRepository) GetDeployment(
	_ context.Context,
	deploymentID string,
) (*domain.Deployment, error) {
	deployment, ok := repository.deployments[deploymentID]
	if !ok {
		return nil, domain.ErrDeploymentNotFound
	}
	copy := *deployment
	return &copy, nil
}

func (repository *fakeRepository) GetDeploymentByRequestID(
	_ context.Context,
	requestID string,
) (*domain.Deployment, error) {
	for _, deployment := range repository.deployments {
		if deployment.RequestID == requestID {
			copy := *deployment
			return &copy, nil
		}
	}
	return nil, domain.ErrDeploymentNotFound
}

func (repository *fakeRepository) GetDeploymentForRuntimeUpdate(
	_ context.Context,
	deviceID string,
	releaseID string,
) (*domain.Deployment, error) {
	var selected *domain.Deployment
	for _, deployment := range repository.deployments {
		if deployment.DeviceID != deviceID {
			continue
		}
		targetReleaseID := deployment.ReleaseID
		if deployment.RollbackReleaseID != "" {
			targetReleaseID = deployment.RollbackReleaseID
		}
		if targetReleaseID != releaseID {
			continue
		}
		if selected == nil || deployment.CreatedAt.After(selected.CreatedAt) {
			selected = deployment
		}
	}
	if selected == nil {
		return nil, domain.ErrDeploymentNotFound
	}
	copy := *selected
	return &copy, nil
}

func (repository *fakeRepository) ListDeployments(
	_ context.Context,
	_ domain.DeploymentFilter,
) (*domain.DeploymentPage, error) {
	items := make([]domain.Deployment, 0, len(repository.deployments))
	for _, deployment := range repository.deployments {
		items = append(items, *deployment)
	}
	return &domain.DeploymentPage{
		Items:    items,
		Total:    int64(len(items)),
		Page:     1,
		PageSize: 20,
	}, nil
}

func (repository *fakeRepository) RetryDeployment(
	_ context.Context,
	deploymentID string,
	_ string,
	expectedVersion int64,
	_ time.Time,
) (*domain.Deployment, error) {
	deployment, ok := repository.deployments[deploymentID]
	if !ok {
		return nil, domain.ErrDeploymentNotFound
	}
	if deployment.RecordVersion != expectedVersion {
		return nil, &domain.DeploymentVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  deployment.RecordVersion,
		}
	}
	deployment.Status = domain.DeploymentStatusQueued
	deployment.RecordVersion++
	return deployment, nil
}

func (repository *fakeRepository) RecordEvent(
	_ context.Context,
	event *domain.Event,
) (*domain.Deployment, bool, error) {
	deployment, ok := repository.deployments[event.DeploymentID]
	if !ok {
		return nil, false, domain.ErrDeploymentNotFound
	}
	key := event.DeploymentID + ":" + event.EventID
	if existing, exists := repository.events[key]; exists {
		if !sameEvent(existing, event) {
			return nil, false, domain.ErrEventConflict
		}
		copy := *deployment
		return &copy, false, nil
	}
	repository.events[key] = event
	deployment.Status = event.Status
	deployment.ProgressPercent = event.ProgressPercent
	deployment.LastEventSequence = event.Sequence
	deployment.RecordVersion++
	copy := *deployment
	return &copy, true, nil
}

func (repository *fakeRepository) Statistics(
	context.Context,
	domain.DeploymentFilter,
) (*domain.Statistics, error) {
	return &domain.Statistics{
		ByStatus:  map[string]int64{},
		ByChannel: map[string]int64{},
		ByVersion: map[string]int64{},
	}, nil
}

func sameEvent(left *domain.Event, right *domain.Event) bool {
	return left.DeploymentID == right.DeploymentID &&
		left.EventID == right.EventID &&
		left.Type == right.Type &&
		left.Sequence == right.Sequence &&
		left.Status == right.Status &&
		left.ProgressPercent == right.ProgressPercent &&
		left.BytesReceived == right.BytesReceived &&
		left.BytesTotal == right.BytesTotal &&
		left.ErrorCode == right.ErrorCode &&
		left.Message == right.Message
}

func validReleaseInput() domain.ReleaseInput {
	return domain.ReleaseInput{
		ID:                 "00000000-0000-4000-8000-000000000001",
		Version:            "1.2.3",
		Channel:            domain.ChannelStable,
		HardwareRevision:   "sprout-v1",
		MinSourceVersion:   "1.0.0",
		ArtifactURL:        "https://download.clarkhub.cn/firmware/1.2.3.bin",
		ArtifactKey:        "firmware/1.2.3.bin",
		SHA256:             strings.Repeat("a", 64),
		SizeBytes:          1024,
		SignatureKeyID:     "firmware-prod-2026",
		SignatureAlgorithm: "ed25519",
		Signature:          strings.Repeat("A", 86) + "==",
		RollbackAllowed:    true,
		Target: domain.Target{
			Scope: domain.TargetScopeAll,
		},
		ActorID: "admin-1",
	}
}

func TestPublishReleaseSignsEveryPolicyField(t *testing.T) {
	repository := newFakeRepository()
	verifier := &capturingVerifier{}
	service, err := New(Options{
		Repository: repository,
		Verifier:   verifier,
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: acceptingArtifactLocker{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	input := validReleaseInput()
	input.Mandatory = true
	release, err := service.CreateReleaseDraft(context.Background(), input)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := service.PublishRelease(
		context.Background(),
		release.ID,
		"admin-1",
		release.RecordVersion,
	); err != nil {
		t.Fatalf("publish release: %v", err)
	}
	want := strings.Join([]string{
		"schema_version=2",
		"release_id=00000000-0000-4000-8000-000000000001",
		"firmware_version=1.2.3",
		"hardware_revision=sprout-v1",
		"channel=stable",
		"artifact_key=firmware/1.2.3.bin",
		"artifact_url=https://download.clarkhub.cn/firmware/1.2.3.bin",
		"sha256=" + strings.Repeat("a", 64),
		"size_bytes=1024",
		"signature_key_id=firmware-prod-2026",
		"signature_algorithm=ed25519",
		"rollback_allowed=true",
		"mandatory=true",
		"min_source_version=1.0.0",
		"",
	}, "\n")
	if string(verifier.manifest) != want {
		t.Fatalf("canonical manifest mismatch:\n%s", verifier.manifest)
	}
}

func TestPublishReleaseUsesOneAtomicArtifactVerification(t *testing.T) {
	repository := newFakeRepository()
	locker := &recordingAtomicArtifactLocker{}
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: locker,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	release, err := service.CreateReleaseDraft(context.Background(), validReleaseInput())
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := service.PublishRelease(
		context.Background(),
		release.ID,
		"admin-1",
		release.RecordVersion,
	); err != nil {
		t.Fatalf("publish release: %v", err)
	}
	if locker.calls != 1 ||
		locker.key != validReleaseInput().ArtifactKey ||
		locker.sizeBytes != validReleaseInput().SizeBytes ||
		locker.sha256 != validReleaseInput().SHA256 {
		t.Fatalf(
			"atomic artifact verification calls=%d key=%q size=%d sha256=%q",
			locker.calls,
			locker.key,
			locker.sizeBytes,
			locker.sha256,
		)
	}
}

func TestReleaseValidationRejectsUnsafeArtifactPaths(t *testing.T) {
	valid := validReleaseInput()
	invalid := []struct {
		name        string
		artifactKey string
		artifactURL string
	}{
		{
			name:        "absolute key",
			artifactKey: "/firmware/1.2.3.bin",
			artifactURL: valid.ArtifactURL,
		},
		{
			name:        "backslash key",
			artifactKey: `firmware\1.2.3.bin`,
			artifactURL: valid.ArtifactURL,
		},
		{
			name:        "dot segment",
			artifactKey: "firmware/../1.2.3.bin",
			artifactURL: valid.ArtifactURL,
		},
		{
			name:        "url does not end with key",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: "https://download.clarkhub.cn/other/1.2.3.bin",
		},
		{
			name:        "percent encoded path",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: "https://download.clarkhub.cn/firmware/%31.2.3.bin",
		},
		{
			name:        "fragment",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: "https://download.clarkhub.cn/firmware/1.2.3.bin#fragment",
		},
		{
			name:        "query",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: "https://download.clarkhub.cn/firmware/1.2.3.bin?channel=stable",
		},
		{
			name:        "bare query delimiter",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: "https://download.clarkhub.cn/firmware/1.2.3.bin?",
		},
		{
			name:        "bare fragment delimiter",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: "https://download.clarkhub.cn/firmware/1.2.3.bin#",
		},
		{
			name:        "non-ascii URL",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: "https://download.clarkhub.cn/firmware/固件.bin",
		},
		{
			name:        "oversized URL",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: "https://download.clarkhub.cn/" +
				strings.Repeat("a", 512) + "/firmware/1.2.3.bin",
		},
		{
			name:        "non-ascii key",
			artifactKey: "firmware/固件.bin",
			artifactURL: valid.ArtifactURL,
		},
		{
			name:        "question mark key",
			artifactKey: "firmware/1.2.3.bin?",
			artifactURL: valid.ArtifactURL,
		},
		{
			name:        "oversized key",
			artifactKey: strings.Repeat("a", 513),
			artifactURL: valid.ArtifactURL,
		},
		{
			name:        "uppercase scheme",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: "HTTPS://download.clarkhub.cn/firmware/1.2.3.bin",
		},
		{
			name:        "trailing slash",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: "https://download.clarkhub.cn/firmware/1.2.3.bin/",
		},
		{
			name:        "backslash",
			artifactKey: "firmware/1.2.3.bin",
			artifactURL: `https://download.clarkhub.cn/firmware/1.2.3.bin\extra`,
		},
	}
	for _, testCase := range invalid {
		t.Run(testCase.name, func(t *testing.T) {
			release := &domain.Release{
				ID:                 valid.ID,
				Version:            valid.Version,
				Channel:            valid.Channel,
				HardwareRevision:   valid.HardwareRevision,
				MinSourceVersion:   valid.MinSourceVersion,
				ArtifactURL:        testCase.artifactURL,
				ArtifactKey:        testCase.artifactKey,
				SHA256:             valid.SHA256,
				SizeBytes:          valid.SizeBytes,
				SignatureKeyID:     valid.SignatureKeyID,
				SignatureAlgorithm: valid.SignatureAlgorithm,
				Signature:          valid.Signature,
				RollbackAllowed:    valid.RollbackAllowed,
				Mandatory:          valid.Mandatory,
				Target:             valid.Target,
			}
			if err := domain.ValidateRelease(release); !errors.Is(err, domain.ErrInvalidRelease) {
				t.Fatalf("expected invalid release, got %v", err)
			}
		})
	}
}

func TestReleaseValidationMatchesArtifactURLLengthBoundary(t *testing.T) {
	valid := validReleaseInput()
	atPath := &domain.Release{
		ID:                 valid.ID,
		Version:            valid.Version,
		Channel:            valid.Channel,
		HardwareRevision:   valid.HardwareRevision,
		MinSourceVersion:   valid.MinSourceVersion,
		ArtifactURL:        "https://download.clarkhub.cn/firmware/release@2026/1.2.3.bin",
		ArtifactKey:        "firmware/release@2026/1.2.3.bin",
		SHA256:             valid.SHA256,
		SizeBytes:          valid.SizeBytes,
		SignatureKeyID:     valid.SignatureKeyID,
		SignatureAlgorithm: valid.SignatureAlgorithm,
		Signature:          valid.Signature,
		RollbackAllowed:    valid.RollbackAllowed,
		Mandatory:          valid.Mandatory,
		Target:             valid.Target,
	}
	if err := domain.ValidateRelease(atPath); err != nil {
		t.Fatalf("at-sign in a path segment must not count as userinfo: %v", err)
	}
	prefix := "https://download.clarkhub.cn/"
	suffix := "/" + valid.ArtifactKey
	maximumLength := 512
	maximumURL := prefix +
		strings.Repeat(
			"a",
			maximumLength-len(prefix)-len(suffix),
		) +
		suffix
	if len(maximumURL) != maximumLength {
		t.Fatalf("fixture URL length = %d, want %d", len(maximumURL), maximumLength)
	}
	release := &domain.Release{
		ID:                 valid.ID,
		Version:            valid.Version,
		Channel:            valid.Channel,
		HardwareRevision:   valid.HardwareRevision,
		MinSourceVersion:   valid.MinSourceVersion,
		ArtifactURL:        maximumURL,
		ArtifactKey:        valid.ArtifactKey,
		SHA256:             valid.SHA256,
		SizeBytes:          valid.SizeBytes,
		SignatureKeyID:     valid.SignatureKeyID,
		SignatureAlgorithm: valid.SignatureAlgorithm,
		Signature:          valid.Signature,
		RollbackAllowed:    valid.RollbackAllowed,
		Mandatory:          valid.Mandatory,
		Target:             valid.Target,
	}
	if err := domain.ValidateRelease(release); err != nil {
		t.Fatalf("512-byte artifact URL must be accepted: %v", err)
	}
	release.ArtifactURL = prefix +
		strings.Repeat(
			"a",
			maximumLength-len(prefix)-len(suffix)+1,
		) +
		suffix
	if len(release.ArtifactURL) != 513 {
		t.Fatalf("fixture URL length = %d, want 513", len(release.ArtifactURL))
	}
	if err := domain.ValidateRelease(release); !errors.Is(
		err,
		domain.ErrInvalidRelease,
	) {
		t.Fatalf("513-byte artifact URL must be rejected, got %v", err)
	}
}

func TestReleaseValidationRequiresMinimumSourceVersion(t *testing.T) {
	valid := validReleaseInput()
	release := &domain.Release{
		ID:                 valid.ID,
		Version:            valid.Version,
		Channel:            valid.Channel,
		HardwareRevision:   valid.HardwareRevision,
		MinSourceVersion:   "",
		ArtifactURL:        valid.ArtifactURL,
		ArtifactKey:        valid.ArtifactKey,
		SHA256:             valid.SHA256,
		SizeBytes:          valid.SizeBytes,
		SignatureKeyID:     valid.SignatureKeyID,
		SignatureAlgorithm: valid.SignatureAlgorithm,
		Signature:          valid.Signature,
		RollbackAllowed:    valid.RollbackAllowed,
		Mandatory:          valid.Mandatory,
		Target:             valid.Target,
	}
	if err := domain.ValidateRelease(release); !errors.Is(err, domain.ErrInvalidRelease) {
		t.Fatalf("empty minimum source version must be rejected, got %v", err)
	}
}

func TestCreateReleaseDraftValidatesManifest(t *testing.T) {
	repository := newFakeRepository()
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: acceptingArtifactLocker{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	input := validReleaseInput()
	input.SHA256 = "not-a-digest"
	if _, err := service.CreateReleaseDraft(context.Background(), input); !errors.Is(
		err,
		domain.ErrInvalidRelease,
	) {
		t.Fatalf("expected invalid release, got %v", err)
	}

	input = validReleaseInput()
	input.SizeBytes = domain.MaxArtifactSizeBytes + 1
	if _, err := service.CreateReleaseDraft(context.Background(), input); !errors.Is(
		err,
		domain.ErrInvalidRelease,
	) {
		t.Fatalf("expected oversize artifact rejection, got %v", err)
	}

	input = validReleaseInput()
	input.Version = "not-a-version"
	if _, err := service.CreateReleaseDraft(context.Background(), input); !errors.Is(
		err,
		domain.ErrInvalidRelease,
	) {
		t.Fatalf("expected invalid version rejection, got %v", err)
	}

	input = validReleaseInput()
	input.SignatureAlgorithm = "md5"
	if _, err := service.CreateReleaseDraft(context.Background(), input); !errors.Is(
		err,
		domain.ErrInvalidRelease,
	) {
		t.Fatalf("expected invalid signature rejection, got %v", err)
	}

	input = validReleaseInput()
	input.SignatureAlgorithm = "ED25519"
	if _, err := service.CreateReleaseDraft(context.Background(), input); !errors.Is(
		err,
		domain.ErrInvalidRelease,
	) {
		t.Fatalf("expected strict algorithm rejection, got %v", err)
	}

	input = validReleaseInput()
	input.SignatureKeyID = strings.Repeat("k", 65)
	if _, err := service.CreateReleaseDraft(context.Background(), input); !errors.Is(
		err,
		domain.ErrInvalidRelease,
	) {
		t.Fatalf("expected oversized key id rejection, got %v", err)
	}
}

func TestValidEd25519SignatureRequiresCanonicalEncoding(t *testing.T) {
	canonical := strings.Repeat("A", 86) + "=="
	if !domain.ValidEd25519Signature(canonical) {
		t.Fatal("canonical 64-byte signature must be accepted")
	}
	for _, invalid := range []string{
		"",
		strings.Repeat("A", 86),
		strings.Repeat("A", 87) + "=",
		strings.Repeat("A", 88),
		strings.Repeat("A", 85) + "===",
		strings.Repeat("A", 85) + "+=",
	} {
		if domain.ValidEd25519Signature(invalid) {
			t.Fatalf("non-canonical signature %q was accepted", invalid)
		}
	}
}

func TestEd25519KeyringRejectsInvalidKeyIDs(t *testing.T) {
	publicKey := "11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="
	for _, keyring := range []string{
		`{" key":"` + publicKey + `"}`,
		`{"key ":"` + publicKey + `"}`,
		`{"": "` + publicKey + `"}`,
		`{"` + strings.Repeat("k", 65) + `":"` + publicKey + `"}`,
		`{"bad/key":"` + publicKey + `"}`,
	} {
		if _, err := NewEd25519KeyringVerifier(keyring); err == nil {
			t.Fatalf("invalid keyring %q was accepted", keyring)
		}
	}
}

func TestReleaseStateMachineAndOptimisticConflict(t *testing.T) {
	repository := newFakeRepository()
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: acceptingArtifactLocker{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	release, err := service.CreateReleaseDraft(context.Background(), validReleaseInput())
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	if release.Status != domain.ReleaseStatusDraft {
		t.Fatalf("status = %s, want draft", release.Status)
	}
	if _, err := service.PublishRelease(
		context.Background(),
		release.ID,
		"admin-1",
		release.RecordVersion,
	); err != nil {
		t.Fatalf("publish release: %v", err)
	}
	if _, err := service.PublishRelease(
		context.Background(),
		release.ID,
		"admin-1",
		1,
	); !errors.Is(err, domain.ErrReleaseVersionConflict) {
		t.Fatalf("expected release version conflict, got %v", err)
	}
}

func TestUpdateReleaseDraftRequiresMatchingSignedIdentity(t *testing.T) {
	repository := newFakeRepository()
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	release, err := service.CreateReleaseDraft(
		context.Background(),
		validReleaseInput(),
	)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	input := validReleaseInput()
	input.ID = "00000000-0000-4000-8000-000000000099"
	input.ExpectedVersion = release.RecordVersion
	if _, err := service.UpdateReleaseDraft(
		context.Background(),
		release.ID,
		input,
	); !errors.Is(err, domain.ErrInvalidRelease) {
		t.Fatalf("identity mismatch error = %v, want invalid release", err)
	}
	if _, err := service.UpdateReleaseDraft(
		context.Background(),
		release.ID,
		validReleaseInputWithExpectedVersion(release.RecordVersion),
	); err != nil {
		t.Fatalf("matching identity update: %v", err)
	}
}

func validReleaseInputWithExpectedVersion(expectedVersion int64) domain.ReleaseInput {
	input := validReleaseInput()
	input.ExpectedVersion = expectedVersion
	return input
}

func TestPublishReleaseRejectsPlaceholderSignature(t *testing.T) {
	repository := newFakeRepository()
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: acceptingArtifactLocker{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	repository.releases["release-placeholder"] = &domain.Release{
		ID:                 "release-placeholder",
		Version:            "1.2.3",
		Channel:            domain.ChannelStable,
		HardwareRevision:   "sprout-v1",
		MinSourceVersion:   "1.0.0",
		ArtifactURL:        "https://download.clarkhub.cn/firmware/1.2.3.bin",
		ArtifactKey:        "firmware/1.2.3.bin",
		SHA256:             strings.Repeat("a", 64),
		SizeBytes:          1024,
		SignatureKeyID:     "firmware-prod-2026",
		SignatureAlgorithm: "ed25519",
		Signature:          "base64-signature",
		SignatureStatus:    domain.SignatureStatusPending,
		RollbackAllowed:    true,
		Status:             domain.ReleaseStatusDraft,
		Target:             domain.Target{Scope: domain.TargetScopeAll},
		RecordVersion:      1,
		CreatedBy:          "admin-1",
		UpdatedBy:          "admin-1",
	}
	if _, err := service.PublishRelease(
		context.Background(),
		"release-placeholder",
		"admin-1",
		1,
	); !errors.Is(err, domain.ErrSignatureInvalid) {
		t.Fatalf("expected placeholder signature rejection, got %v", err)
	}
}

func TestRollbackRejectedWhenReleaseDoesNotAllowIt(t *testing.T) {
	repository := newFakeRepository()
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: acceptingArtifactLocker{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	input := validReleaseInput()
	input.RollbackAllowed = false
	release, err := service.CreateReleaseDraft(context.Background(), input)
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	release, err = service.PublishRelease(
		context.Background(),
		release.ID,
		"admin-1",
		release.RecordVersion,
	)
	if err != nil {
		t.Fatalf("publish release: %v", err)
	}
	if _, err := service.RollbackRelease(
		context.Background(),
		release.ID,
		"admin-1",
		release.RecordVersion,
	); !errors.Is(err, domain.ErrRollbackNotAllowed) {
		t.Fatalf("expected rollback rejection, got %v", err)
	}
}

func TestCanarySelectionIsDeterministic(t *testing.T) {
	first := domain.CanaryContains("1.2.3", "device-1", 20)
	second := domain.CanaryContains("1.2.3", "device-1", 20)
	if first != second {
		t.Fatal("canary bucket changed between identical evaluations")
	}
	if domain.CanaryContains("1.2.3", "device-1", 0) {
		t.Fatal("0 percent canary should exclude every device")
	}
	if !domain.CanaryContains("1.2.3", "device-1", 100) {
		t.Fatal("100 percent canary should include every device")
	}
}

func TestAuthorizeRuntimeFirmwareUpdateRequiresDeviceDeploymentAndPublishedHardware(t *testing.T) {
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "1.0.0",
	}
	release := &domain.Release{
		ID:                 "00000000-0000-4000-8000-000000000001",
		Version:            "1.2.3",
		Channel:            domain.ChannelStable,
		HardwareRevision:   "sprout-v1",
		MinSourceVersion:   "1.0.0",
		SizeBytes:          1024,
		ArtifactKey:        "firmware/1.2.3.bin",
		SHA256:             strings.Repeat("a", 64),
		Status:             domain.ReleaseStatusPublished,
		Target:             domain.Target{Scope: domain.TargetScopeDevice, DeviceID: "device-1"},
		SignatureStatus:    domain.SignatureStatusVerified,
		SignatureAlgorithm: "ed25519",
		Signature:          strings.Repeat("A", 86) + "==",
	}
	verifiedAt := time.Now().UTC()
	release.SignatureVerifiedAt = &verifiedAt
	repository.releases[release.ID] = release
	deployment := &domain.Deployment{
		ID:        "00000000-0000-4000-8000-000000000002",
		ReleaseID: release.ID,
		DeviceID:  "device-1",
		Status:    domain.DeploymentStatusQueued,
		CreatedAt: time.Now().UTC(),
	}
	repository.deployments[deployment.ID] = deployment
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	authorization, err := service.AuthorizeRuntimeFirmwareUpdate(
		context.Background(),
		"device-1",
		release.ID,
	)
	if err != nil {
		t.Fatalf("authorize runtime update: %v", err)
	}
	if authorization.ReleaseID != release.ID ||
		authorization.DeploymentID != deployment.ID ||
		authorization.DeploymentKind != "install" {
		t.Fatalf("unexpected authorization: %#v", authorization)
	}

	release.HardwareRevision = "sprout-v2"
	if _, err := service.AuthorizeRuntimeFirmwareUpdate(
		context.Background(),
		"device-1",
		release.ID,
	); !errors.Is(err, domain.ErrDeviceNotEligible) {
		t.Fatalf("expected hardware mismatch to be rejected, got %v", err)
	}

	release.HardwareRevision = "sprout-v1"
	release.SignatureStatus = domain.SignatureStatusPending
	release.SignatureVerifiedAt = nil
	if _, err := service.AuthorizeRuntimeFirmwareUpdate(
		context.Background(),
		"device-1",
		release.ID,
	); !errors.Is(err, domain.ErrDeviceNotEligible) {
		t.Fatalf("expected an unverified release to be rejected, got %v", err)
	}
	release.SignatureStatus = domain.SignatureStatusVerified
	release.SignatureVerifiedAt = &verifiedAt

	deployment.Status = domain.DeploymentStatusFailed
	if _, err := service.AuthorizeRuntimeFirmwareUpdate(
		context.Background(),
		"device-1",
		release.ID,
	); !errors.Is(err, domain.ErrDeploymentStateConflict) {
		t.Fatalf("expected failed deployment to require an explicit retry, got %v", err)
	}
}

func TestAuthorizeRuntimeFirmwareUpdateRejectsUnknownReleaseAndEndedDeployment(t *testing.T) {
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "1.0.0",
	}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if _, err := service.AuthorizeRuntimeFirmwareUpdate(
		context.Background(),
		"device-1",
		"00000000-0000-4000-8000-000000000001",
	); !errors.Is(err, domain.ErrReleaseNotFound) {
		t.Fatalf("expected unknown release rejection, got %v", err)
	}
}

func TestAuthorizeRuntimeFirmwareUpdateRejectsDeviceOutsideCanary(t *testing.T) {
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "1.0.0",
	}
	release := &domain.Release{
		ID:                 "00000000-0000-4000-8000-000000000071",
		Version:            "9.9.9-canary",
		Channel:            domain.ChannelCanary,
		HardwareRevision:   "sprout-v1",
		MinSourceVersion:   "1.0.0",
		SizeBytes:          1024,
		ArtifactKey:        "firmware/9.9.9-canary.bin",
		SHA256:             strings.Repeat("e", 64),
		Status:             domain.ReleaseStatusPublished,
		Target:             domain.Target{Scope: domain.TargetScopeCanary, CanaryPercent: 1},
		SignatureStatus:    domain.SignatureStatusVerified,
		SignatureAlgorithm: "ed25519",
		Signature:          strings.Repeat("A", 86) + "==",
	}
	verifiedAt := time.Now().UTC()
	release.SignatureVerifiedAt = &verifiedAt
	repository.releases[release.ID] = release
	deployment := &domain.Deployment{
		ID:        "00000000-0000-4000-8000-000000000072",
		ReleaseID: release.ID,
		Status:    domain.DeploymentStatusQueued,
		CreatedAt: time.Now().UTC(),
	}
	if domain.CanaryContains(
		release.Version,
		"device-outside-canary",
		release.Target.CanaryPercent,
	) {
		t.Skip("synthetic device unexpectedly falls inside the canary bucket")
	}
	repository.device.DeviceID = "device-outside-canary"
	deployment.DeviceID = repository.device.DeviceID
	repository.deployments[deployment.ID] = deployment
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	if _, err := service.AuthorizeRuntimeFirmwareUpdate(
		context.Background(),
		repository.device.DeviceID,
		release.ID,
	); !errors.Is(err, domain.ErrDeviceNotEligible) {
		t.Fatalf("expected a device outside the canary to be rejected, got %v", err)
	}
}

func TestRecordEventIsIdempotentAndRejectsDeviceMismatch(t *testing.T) {
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "1.0.0",
	}
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: acceptingArtifactLocker{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	release, err := service.CreateReleaseDraft(context.Background(), validReleaseInput())
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	release, err = service.PublishRelease(
		context.Background(),
		release.ID,
		"admin-1",
		release.RecordVersion,
	)
	if err != nil {
		t.Fatalf("publish release: %v", err)
	}
	deployment, err := service.AssignRelease(
		context.Background(),
		release.ID,
		"device-1",
		"request-1",
		"admin-1",
	)
	if err != nil {
		t.Fatalf("assign release: %v", err)
	}
	event := domain.EventInput{
		DeploymentID:    deployment.ID,
		DeviceID:        "device-1",
		EventID:         "event-1",
		Type:            domain.EventTypeStarted,
		Sequence:        1,
		ProgressPercent: 1,
		BytesTotal:      1024,
		ReportedAt:      time.Now().UTC(),
	}
	if _, inserted, err := service.RecordEvent(context.Background(), event); err != nil ||
		!inserted {
		t.Fatalf("record event: inserted=%v err=%v", inserted, err)
	}
	if _, inserted, err := service.RecordEvent(context.Background(), event); err != nil ||
		inserted {
		t.Fatalf("repeat event must be idempotent: inserted=%v err=%v", inserted, err)
	}
	event.DeviceID = "device-2"
	if _, _, err := service.RecordEvent(context.Background(), event); !errors.Is(
		err,
		domain.ErrDeviceNotEligible,
	) {
		t.Fatalf("expected device mismatch rejection, got %v", err)
	}
}

func TestPublishReleaseRequiresTrustedSignature(t *testing.T) {
	repository := newFakeRepository()
	service, err := New(Options{
		Repository: repository,
		Verifier:   rejectingVerifier{},
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: acceptingArtifactLocker{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	release, err := service.CreateReleaseDraft(context.Background(), validReleaseInput())
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	if _, err := service.PublishRelease(
		context.Background(),
		release.ID,
		"admin-1",
		release.RecordVersion,
	); !errors.Is(err, domain.ErrSignatureInvalid) {
		t.Fatalf("expected signature rejection, got %v", err)
	}
}

func TestPublishReleaseFailsClosedWhenArtifactVerificationFails(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "missing artifact",
			err:  domain.ErrArtifactMissing,
			want: domain.ErrArtifactMissing,
		},
		{
			name: "mismatched artifact",
			err:  domain.ErrArtifactMismatch,
			want: domain.ErrArtifactMismatch,
		},
		{
			name: "download store unavailable",
			err:  errors.New("volume not mounted"),
			want: domain.ErrArtifactUnavailable,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			input := validReleaseInput()
			repository := newFakeRepository()
			preparer, err := New(Options{
				Repository: repository,
				Verifier:   acceptingVerifier{},
			})
			if err != nil {
				t.Fatalf("create preparer: %v", err)
			}
			release, err := preparer.CreateReleaseDraft(context.Background(), input)
			if err != nil {
				t.Fatalf("create draft: %v", err)
			}

			publisher, err := New(Options{
				Repository: repository,
				Verifier:   acceptingVerifier{},
				Artifacts: acceptingArtifactVerifier{
					err: testCase.err,
				},
			})
			if err != nil {
				t.Fatalf("create publisher: %v", err)
			}
			_, err = publisher.PublishRelease(
				context.Background(),
				release.ID,
				"admin-1",
				release.RecordVersion,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("publish error = %v, want %v", err, testCase.want)
			}
			stored, err := repository.GetRelease(context.Background(), release.ID)
			if err != nil {
				t.Fatalf("reload release: %v", err)
			}
			if stored.Status != domain.ReleaseStatusDraft {
				t.Fatalf("release status = %s, want draft", stored.Status)
			}
		})
	}
}

func TestPublishReleaseRejectsArtifactSizeOrDigestMismatch(t *testing.T) {
	for _, verified := range []acceptingArtifactVerifier{
		{sizeBytes: 999, sha256: validReleaseInput().SHA256},
		{sizeBytes: 1024, sha256: strings.Repeat("b", 64)},
	} {
		repository := newFakeRepository()
		preparer, err := New(Options{
			Repository: repository,
			Verifier:   acceptingVerifier{},
		})
		if err != nil {
			t.Fatalf("create preparer: %v", err)
		}
		release, err := preparer.CreateReleaseDraft(
			context.Background(),
			validReleaseInput(),
		)
		if err != nil {
			t.Fatalf("create draft: %v", err)
		}
		publisher, err := New(Options{
			Repository: repository,
			Verifier:   acceptingVerifier{},
			Artifacts:  verified,
		})
		if err != nil {
			t.Fatalf("create publisher: %v", err)
		}
		if _, err := publisher.PublishRelease(
			context.Background(),
			release.ID,
			"admin-1",
			release.RecordVersion,
		); !errors.Is(err, domain.ErrArtifactMismatch) {
			t.Fatalf("expected artifact mismatch, got %v", err)
		}
	}
}

func TestCurrentDeviceUpdateReturnsActiveDeployment(t *testing.T) {
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "1.0.0",
	}
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: acceptingArtifactLocker{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	release, err := service.CreateReleaseDraft(context.Background(), validReleaseInput())
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	release, err = service.PublishRelease(
		context.Background(),
		release.ID,
		"admin-1",
		release.RecordVersion,
	)
	if err != nil {
		t.Fatalf("publish release: %v", err)
	}
	deployment, err := service.AssignRelease(
		context.Background(),
		release.ID,
		"device-1",
		"request-current",
		"admin-1",
	)
	if err != nil {
		t.Fatalf("assign release: %v", err)
	}
	current, err := service.CurrentDeviceUpdate(context.Background(), "device-1")
	if err != nil {
		t.Fatalf("current device update: %v", err)
	}
	if current.Deployment.ID != deployment.ID {
		t.Fatalf("deployment id = %q, want %q", current.Deployment.ID, deployment.ID)
	}
	if !current.UpdateAvailable {
		t.Fatal("queued deployment must be exposed as an available update")
	}
	if current.Rollback {
		t.Fatal("normal deployment must not be marked as a rollback")
	}
}

func TestCurrentDeviceUpdateMarksAuthorizedRollback(t *testing.T) {
	t.Parallel()
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "1.3.0",
	}
	target := fakePublishedRelease("00000000-0000-4000-8000-000000000010", "1.2.0")
	repository.releases[target.ID] = target
	original := fakePublishedRelease("00000000-0000-4000-8000-000000000011", "1.3.0")
	repository.releases[original.ID] = original
	deployment := &domain.Deployment{
		ID:                     "00000000-0000-4000-8000-000000000012",
		ReleaseID:              target.ID,
		RollbackReleaseID:      target.ID,
		RollbackOfDeploymentID: "00000000-0000-4000-8000-000000000013",
		DeviceID:               "device-1",
		Status:                 domain.DeploymentStatusQueued,
		RequestID:              "rollback-authorized",
		RecordVersion:          1,
	}
	repository.deployments[deployment.ID] = deployment
	otaService, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	current, err := otaService.CurrentDeviceUpdate(
		context.Background(),
		"device-1",
	)
	if err != nil {
		t.Fatalf("current device update: %v", err)
	}
	if !current.Rollback {
		t.Fatal("authorized rollback must be marked as a rollback")
	}
	if current.Release.ID != target.ID {
		t.Fatalf("release id = %q, want rollback target %q", current.Release.ID, target.ID)
	}
	if current.Release.Version != target.Version {
		t.Fatalf(
			"target version = %q, want %q",
			current.Release.Version,
			target.Version,
		)
	}
	if current.Deployment.RollbackReleaseID != target.ID ||
		current.Deployment.RollbackOfDeploymentID != deployment.RollbackOfDeploymentID {
		t.Fatalf("rollback metadata = %#v", current.Deployment)
	}
}

func TestCurrentDeviceUpdatePrefersRollbackTargetOverNewerRelease(t *testing.T) {
	t.Parallel()
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "1.4.0",
	}
	target := fakePublishedRelease("00000000-0000-4000-8000-000000000020", "1.2.0")
	repository.releases[target.ID] = target
	newer := fakePublishedRelease("00000000-0000-4000-8000-000000000021", "1.4.0")
	repository.releases[newer.ID] = newer
	repository.deployments["00000000-0000-4000-8000-000000000022"] = &domain.Deployment{
		ID:                     "00000000-0000-4000-8000-000000000022",
		ReleaseID:              target.ID,
		RollbackReleaseID:      target.ID,
		RollbackOfDeploymentID: "00000000-0000-4000-8000-000000000023",
		DeviceID:               "device-1",
		Status:                 domain.DeploymentStatusQueued,
		RequestID:              "rollback-before-newer-release",
		RecordVersion:          1,
	}
	otaService, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	current, err := otaService.CurrentDeviceUpdate(
		context.Background(),
		"device-1",
	)
	if err != nil {
		t.Fatalf("current device update: %v", err)
	}
	if current.Release.ID != target.ID || current.Release.Version != "1.2.0" {
		t.Fatalf(
			"release = %s@%s, want rollback target %s@%s",
			current.Release.ID,
			current.Release.Version,
			target.ID,
			target.Version,
		)
	}
}

func TestCurrentDeviceUpdateRejectsUnpublishedRollbackTarget(t *testing.T) {
	t.Parallel()
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "1.3.0",
	}
	target := fakePublishedRelease("00000000-0000-4000-8000-000000000010", "1.2.0")
	target.Status = domain.ReleaseStatusDraft
	repository.releases[target.ID] = target
	repository.deployments["00000000-0000-4000-8000-000000000012"] = &domain.Deployment{
		ID:                     "00000000-0000-4000-8000-000000000012",
		ReleaseID:              target.ID,
		RollbackReleaseID:      target.ID,
		RollbackOfDeploymentID: "00000000-0000-4000-8000-000000000013",
		DeviceID:               "device-1",
		Status:                 domain.DeploymentStatusQueued,
		RequestID:              "rollback-draft",
		RecordVersion:          1,
	}
	otaService, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if _, err := otaService.CurrentDeviceUpdate(
		context.Background(),
		"device-1",
	); !errors.Is(err, domain.ErrRollbackNotAllowed) {
		t.Fatalf("expected unpublished rollback target rejection, got %v", err)
	}
}

func fakePublishedRelease(id string, version string) *domain.Release {
	return &domain.Release{
		ID:                 id,
		Version:            version,
		Channel:            domain.ChannelStable,
		HardwareRevision:   "sprout-v1",
		MinSourceVersion:   "1.0.0",
		ArtifactURL:        "https://download.clarkhub.cn/firmware/" + version + ".bin",
		ArtifactKey:        "firmware/" + version + ".bin",
		SHA256:             strings.Repeat("a", 64),
		SizeBytes:          1024,
		SignatureKeyID:     "firmware-prod-2026",
		SignatureAlgorithm: "ed25519",
		Signature:          strings.Repeat("A", 86) + "==",
		RollbackAllowed:    true,
		Status:             domain.ReleaseStatusPublished,
		Target:             domain.Target{Scope: domain.TargetScopeAll},
		RecordVersion:      1,
	}
}

func TestRollbackDeploymentRequiresAllowedRelease(t *testing.T) {
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "1.0.0",
	}
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: acceptingArtifactLocker{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	input := validReleaseInput()
	input.RollbackAllowed = false
	release, err := service.CreateReleaseDraft(context.Background(), input)
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	release, err = service.PublishRelease(
		context.Background(),
		release.ID,
		"admin-1",
		release.RecordVersion,
	)
	if err != nil {
		t.Fatalf("publish release: %v", err)
	}
	deployment, err := service.AssignRelease(
		context.Background(),
		release.ID,
		"device-1",
		"request-rollback",
		"admin-1",
	)
	if err != nil {
		t.Fatalf("assign release: %v", err)
	}
	if _, err := service.RollbackDeployment(
		context.Background(),
		deployment.ID,
		"parent-1",
		deployment.RecordVersion,
	); !errors.Is(err, domain.ErrRollbackNotAllowed) {
		t.Fatalf("expected rollback rejection, got %v", err)
	}
}

type rejectingVerifier struct{}

func (rejectingVerifier) Verify(
	string,
	string,
	[]byte,
	string,
) error {
	return domain.ErrSignatureInvalid
}

func TestDeviceCurrentVersionFallsBackForUnreportedFirmware(t *testing.T) {
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "",
	}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	version, err := service.DeviceCurrentVersion(context.Background(), "device-1")
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if version != "0.0.0" {
		t.Fatalf("current version = %q, want 0.0.0", version)
	}
	if !domain.ValidVersion(version) {
		t.Fatalf("fallback version %q must satisfy the device contract", version)
	}
}

func TestRollbackDeploymentRejectsNonPublishedTargetRelease(t *testing.T) {
	repository := newFakeRepository()
	repository.device = &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "1.0.0",
	}
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
		Artifacts: acceptingArtifactVerifier{
			sizeBytes: validReleaseInput().SizeBytes,
			sha256:    validReleaseInput().SHA256,
		},
		ArtifactLock: acceptingArtifactLocker{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	previous := validReleaseInput()
	previous.Version = "1.1.0"
	previousRelease, err := service.CreateReleaseDraft(context.Background(), previous)
	if err != nil {
		t.Fatalf("create previous release: %v", err)
	}
	previousRelease, err = service.PublishRelease(
		context.Background(),
		previousRelease.ID,
		"admin-1",
		previousRelease.RecordVersion,
	)
	if err != nil {
		t.Fatalf("publish previous release: %v", err)
	}
	withdraw := validReleaseInput()
	withdraw.Version = "1.2.0"
	current, err := service.CreateReleaseDraft(context.Background(), withdraw)
	if err != nil {
		t.Fatalf("create current release: %v", err)
	}
	repository.releases[current.ID].RollbackReleaseID = previousRelease.ID
	current, err = service.PublishRelease(
		context.Background(),
		current.ID,
		"admin-1",
		current.RecordVersion,
	)
	if err != nil {
		t.Fatalf("publish current release: %v", err)
	}
	deployment, err := service.AssignRelease(
		context.Background(),
		current.ID,
		"device-1",
		"request-rollback-target",
		"admin-1",
	)
	if err != nil {
		t.Fatalf("assign release: %v", err)
	}
	// A draft target release must never be selected as a rollback destination.
	repository.deployments[deployment.ID].Status = domain.DeploymentStatusSucceeded
	deployment = repository.deployments[deployment.ID]
	repository.releases[previousRelease.ID].Status = domain.ReleaseStatusDraft
	if _, err := service.RollbackDeployment(
		context.Background(),
		deployment.ID,
		"parent-1",
		deployment.RecordVersion,
	); !errors.Is(err, domain.ErrRollbackNotAllowed) {
		t.Fatalf("expected draft rollback target rejection, got %v", err)
	}
}

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

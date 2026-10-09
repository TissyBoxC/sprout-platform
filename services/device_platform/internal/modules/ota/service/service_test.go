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

func (acceptingVerifier) Verify(
	algorithm string,
	keyID string,
	digest string,
	signature string,
) error {
	if algorithm == "" || keyID == "" || digest == "" || signature == "" {
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
		Signature:          "c2lnbmF0dXJl",
		RollbackAllowed:    true,
		Target: domain.Target{
			Scope: domain.TargetScopeAll,
		},
		ActorID: "admin-1",
	}
}

func TestCreateReleaseDraftValidatesManifest(t *testing.T) {
	repository := newFakeRepository()
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
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
}

func TestReleaseStateMachineAndOptimisticConflict(t *testing.T) {
	repository := newFakeRepository()
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
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

func TestRollbackRejectedWhenReleaseDoesNotAllowIt(t *testing.T) {
	repository := newFakeRepository()
	service, err := New(Options{
		Repository: repository,
		Verifier:   acceptingVerifier{},
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
	string,
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

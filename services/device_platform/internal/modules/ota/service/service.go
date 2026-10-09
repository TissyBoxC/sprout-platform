// Package service owns the device OTA release and deployment use cases.
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

// Service owns firmware releases, targeting, progress, and rollback.
type Service struct {
	repository repository.Repository
	timeSource clock.Clock
	verifier   SignatureVerifier
}

// Options contains OTA service dependencies.
type Options struct {
	Repository repository.Repository
	Clock      clock.Clock
	Verifier   SignatureVerifier
}

// SignatureVerifier validates a detached firmware signature against a trusted
// keyring. Production must configure one before OTA release publication.
type SignatureVerifier interface {
	Verify(
		algorithm string,
		keyID string,
		digest string,
		signature string,
	) error
}

// CurrentDeviceUpdate is the active deployment (if any) plus the release it
// targets. It is returned to devices before offering a new release so the
// resume/retry path observes the same deployment id.
type CurrentDeviceUpdate struct {
	Release         domain.Release
	Deployment      domain.Deployment
	UpdateAvailable bool
	CurrentVersion  string
}

// New creates the OTA service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("OTA repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		repository: options.Repository,
		timeSource: timeSource,
		verifier:   options.Verifier,
	}, nil
}

// CreateReleaseDraft validates a complete immutable manifest and stores it.
func (s *Service) CreateReleaseDraft(
	ctx context.Context,
	input domain.ReleaseInput,
) (*domain.Release, error) {
	actorID, err := validateActor(input.ActorID)
	if err != nil {
		return nil, err
	}
	if input.ExpectedVersion != 0 {
		return nil, domain.ErrInvalidRelease
	}
	now := s.timeSource.Now().UTC()
	if input.ID != "" && !validUUID(input.ID) {
		return nil, domain.ErrInvalidRelease
	}
	release := &domain.Release{
		ID:                 releaseIdentity(input.ID),
		Version:            input.Version,
		Channel:            input.Channel,
		HardwareRevision:   input.HardwareRevision,
		MinSourceVersion:   input.MinSourceVersion,
		ArtifactURL:        input.ArtifactURL,
		ArtifactKey:        input.ArtifactKey,
		SHA256:             input.SHA256,
		SizeBytes:          input.SizeBytes,
		SignatureKeyID:     input.SignatureKeyID,
		SignatureAlgorithm: input.SignatureAlgorithm,
		Signature:          input.Signature,
		SignatureStatus:    domain.SignatureStatusPending,
		RollbackAllowed:    input.RollbackAllowed,
		Mandatory:          input.Mandatory,
		ReleaseNotes:       input.ReleaseNotes,
		Status:             domain.ReleaseStatusDraft,
		Target:             input.Target,
		RecordVersion:      1,
		CreatedBy:          actorID,
		UpdatedBy:          actorID,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := validateReleaseTarget(ctx, s.repository, release); err != nil {
		return nil, err
	}
	if err := domain.ValidateRelease(release); err != nil {
		return nil, err
	}
	if err := validateRollbackReference(ctx, s.repository, release); err != nil {
		return nil, err
	}
	if err := s.repository.CreateRelease(ctx, release); err != nil {
		return nil, err
	}
	return release, nil
}

// UpdateReleaseDraft replaces a draft manifest with optimistic concurrency.
func (s *Service) UpdateReleaseDraft(
	ctx context.Context,
	releaseID string,
	input domain.ReleaseInput,
) (*domain.Release, error) {
	actorID, err := validateActor(input.ActorID)
	if err != nil {
		return nil, err
	}
	current, err := s.repository.GetRelease(ctx, strings.TrimSpace(releaseID))
	if err != nil {
		return nil, err
	}
	if current.Status != domain.ReleaseStatusDraft {
		return nil, domain.ErrReleaseStateConflict
	}
	release := *current
	release.Version = input.Version
	release.Channel = input.Channel
	release.HardwareRevision = input.HardwareRevision
	release.MinSourceVersion = input.MinSourceVersion
	release.ArtifactURL = input.ArtifactURL
	release.ArtifactKey = input.ArtifactKey
	release.SHA256 = input.SHA256
	release.SizeBytes = input.SizeBytes
	release.SignatureKeyID = input.SignatureKeyID
	release.SignatureAlgorithm = input.SignatureAlgorithm
	release.Signature = input.Signature
	release.RollbackAllowed = input.RollbackAllowed
	release.Mandatory = input.Mandatory
	release.ReleaseNotes = input.ReleaseNotes
	release.Target = input.Target
	release.UpdatedBy = actorID
	release.UpdatedAt = s.timeSource.Now().UTC()
	if err := validateReleaseTarget(ctx, s.repository, &release); err != nil {
		return nil, err
	}
	if err := domain.ValidateRelease(&release); err != nil {
		return nil, err
	}
	if err := validateRollbackReference(ctx, s.repository, &release); err != nil {
		return nil, err
	}
	if input.ExpectedVersion <= 0 {
		return nil, domain.ErrInvalidRelease
	}
	if err := s.repository.UpdateReleaseDraft(
		ctx,
		&release,
		input.ExpectedVersion,
	); err != nil {
		return nil, err
	}
	return &release, nil
}

// GetRelease returns one release.
func (s *Service) GetRelease(
	ctx context.Context,
	releaseID string,
) (*domain.Release, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, domain.ErrReleaseNotFound
	}
	return s.repository.GetRelease(ctx, releaseID)
}

// ListReleases returns a bounded release page.
func (s *Service) ListReleases(
	ctx context.Context,
	filter domain.ReleaseFilter,
) (*domain.ReleasePage, error) {
	return s.repository.ListReleases(ctx, normalizeReleaseFilter(filter))
}

// PublishRelease moves a draft into the published state.
func (s *Service) PublishRelease(
	ctx context.Context,
	releaseID string,
	actorID string,
	expectedVersion int64,
) (*domain.Release, error) {
	actorID, err := validateActor(actorID)
	if err != nil {
		return nil, err
	}
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" || expectedVersion <= 0 {
		return nil, domain.ErrInvalidRelease
	}
	release, err := s.repository.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	if release.RecordVersion != expectedVersion {
		return nil, &domain.ReleaseVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  release.RecordVersion,
		}
	}
	if release.Status != domain.ReleaseStatusDraft {
		return nil, domain.ErrReleaseStateConflict
	}
	if strings.TrimSpace(release.Signature) == "" {
		return nil, domain.ErrSignatureRequired
	}
	if s.verifier == nil {
		return nil, domain.ErrSignatureKeyUnknown
	}
	if err := s.verifier.Verify(
		release.SignatureAlgorithm,
		release.SignatureKeyID,
		release.SHA256,
		release.Signature,
	); err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	release.SignatureStatus = domain.SignatureStatusVerified
	release.SignatureVerifiedAt = &now
	return s.transition(
		ctx,
		releaseID,
		actorID,
		expectedVersion,
		domain.ReleaseStatusPublished,
		true,
	)
}

// PauseRelease stops new offers while retaining existing deployments.
func (s *Service) PauseRelease(
	ctx context.Context,
	releaseID string,
	actorID string,
	expectedVersion int64,
) (*domain.Release, error) {
	return s.transition(
		ctx,
		releaseID,
		actorID,
		expectedVersion,
		domain.ReleaseStatusPaused,
	)
}

// WithdrawRelease retires a release from all future assignment.
func (s *Service) WithdrawRelease(
	ctx context.Context,
	releaseID string,
	actorID string,
	expectedVersion int64,
) (*domain.Release, error) {
	return s.transition(
		ctx,
		releaseID,
		actorID,
		expectedVersion,
		domain.ReleaseStatusWithdrawn,
	)
}

// RollbackRelease records an operator rollback for a release that explicitly
// allowed it and withdraws the release from future assignment.
func (s *Service) RollbackRelease(
	ctx context.Context,
	releaseID string,
	actorID string,
	expectedVersion int64,
) (*domain.Release, error) {
	actorID, err := validateActor(actorID)
	if err != nil {
		return nil, err
	}
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" || expectedVersion <= 0 {
		return nil, domain.ErrInvalidRelease
	}
	return s.repository.RollbackRelease(
		ctx,
		releaseID,
		expectedVersion,
		actorID,
		s.timeSource.Now().UTC(),
	)
}

func (s *Service) transition(
	ctx context.Context,
	releaseID string,
	actorID string,
	expectedVersion int64,
	status domain.ReleaseStatus,
	signatureVerified ...bool,
) (*domain.Release, error) {
	actorID, err := validateActor(actorID)
	if err != nil {
		return nil, err
	}
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" || expectedVersion <= 0 {
		return nil, domain.ErrInvalidRelease
	}
	return s.repository.TransitionRelease(
		ctx,
		releaseID,
		expectedVersion,
		status,
		actorID,
		s.timeSource.Now().UTC(),
		len(signatureVerified) > 0 && signatureVerified[0],
	)
}

// CreateGroup validates and stores a firmware target group.
func (s *Service) CreateGroup(
	ctx context.Context,
	input domain.GroupInput,
) (*domain.Group, error) {
	actorID, err := validateActor(input.ActorID)
	if err != nil {
		return nil, err
	}
	if input.ExpectedVersion != 0 {
		return nil, domain.ErrInvalidGroup
	}
	now := s.timeSource.Now().UTC()
	if input.ID != "" && !validUUID(input.ID) {
		return nil, domain.ErrInvalidGroup
	}
	group := &domain.Group{
		ID:               releaseIdentity(input.ID),
		Name:             strings.TrimSpace(input.Name),
		Description:      strings.TrimSpace(input.Description),
		Channel:          input.Channel,
		HardwareRevision: strings.TrimSpace(input.HardwareRevision),
		RecordVersion:    1,
		CreatedBy:        actorID,
		UpdatedBy:        actorID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := validateGroup(group); err != nil {
		return nil, err
	}
	if err := s.repository.CreateGroup(ctx, group); err != nil {
		return nil, err
	}
	return group, nil
}

// UpdateGroup updates group metadata with optimistic concurrency.
func (s *Service) UpdateGroup(
	ctx context.Context,
	groupID string,
	input domain.GroupInput,
) (*domain.Group, error) {
	actorID, err := validateActor(input.ActorID)
	if err != nil {
		return nil, err
	}
	if input.ExpectedVersion <= 0 {
		return nil, domain.ErrInvalidGroup
	}
	group, err := s.repository.GetGroup(ctx, strings.TrimSpace(groupID))
	if err != nil {
		return nil, err
	}
	group.Name = strings.TrimSpace(input.Name)
	group.Description = strings.TrimSpace(input.Description)
	group.Channel = input.Channel
	group.HardwareRevision = strings.TrimSpace(input.HardwareRevision)
	group.UpdatedBy = actorID
	group.UpdatedAt = s.timeSource.Now().UTC()
	if err := validateGroup(group); err != nil {
		return nil, err
	}
	if err := s.repository.UpdateGroup(ctx, group, input.ExpectedVersion); err != nil {
		return nil, err
	}
	return group, nil
}

// ListGroups returns every target group.
func (s *Service) ListGroups(
	ctx context.Context,
) ([]domain.Group, error) {
	return s.repository.ListGroups(ctx)
}

// AddGroupMember adds one device to a target group.
func (s *Service) AddGroupMember(
	ctx context.Context,
	groupID string,
	deviceID string,
	actorID string,
) error {
	if _, err := validateActor(actorID); err != nil {
		return err
	}
	if strings.TrimSpace(groupID) == "" || strings.TrimSpace(deviceID) == "" {
		return domain.ErrInvalidGroup
	}
	return s.repository.AddGroupMember(
		ctx,
		strings.TrimSpace(groupID),
		strings.TrimSpace(deviceID),
		strings.TrimSpace(actorID),
	)
}

// RemoveGroupMember removes one device from a target group.
func (s *Service) RemoveGroupMember(
	ctx context.Context,
	groupID string,
	deviceID string,
	actorID string,
) error {
	if _, err := validateActor(actorID); err != nil {
		return err
	}
	if strings.TrimSpace(groupID) == "" || strings.TrimSpace(deviceID) == "" {
		return domain.ErrInvalidGroup
	}
	return s.repository.RemoveGroupMember(
		ctx,
		strings.TrimSpace(groupID),
		strings.TrimSpace(deviceID),
		strings.TrimSpace(actorID),
	)
}

// ListGroupMembers returns the stable device membership of one group.
func (s *Service) ListGroupMembers(
	ctx context.Context,
	groupID string,
) ([]string, error) {
	return s.repository.ListGroupMembers(ctx, strings.TrimSpace(groupID))
}

// DeviceUpdate resolves the release a device should receive now.
func (s *Service) DeviceUpdate(
	ctx context.Context,
	deviceID string,
	channel domain.Channel,
) (*domain.DeviceRelease, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, domain.ErrDeviceNotFound
	}
	if channel != "" && !channel.Valid() {
		return nil, domain.ErrInvalidTarget
	}
	device, err := s.repository.GetDeviceContext(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	releases, err := s.repository.ListPublishedReleases(
		ctx,
		channel,
		device.HardwareRevision,
	)
	if err != nil {
		return nil, err
	}
	release, err := domain.SelectRelease(releases, *device, channel)
	if err != nil {
		return nil, err
	}
	manifest := release.DeviceManifest()
	return &manifest, nil
}

// DeviceCurrentVersion returns the firmware version a device reported in its
// runtime state. Callers use it to build a correct current/target version pair
// instead of treating the offered release version as the installed one.
func (s *Service) DeviceCurrentVersion(
	ctx context.Context,
	deviceID string,
) (string, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return "", domain.ErrDeviceNotFound
	}
	device, err := s.repository.GetDeviceContext(ctx, deviceID)
	if err != nil {
		return "", err
	}
	currentVersion := strings.TrimSpace(device.CurrentVersion)
	if !domain.ValidVersion(currentVersion) {
		return "0.0.0", nil
	}
	return currentVersion, nil
}

// CurrentDeviceUpdate returns the newest in-progress or terminal deployment
// for a device. Callers fall back to DeviceUpdate when there is none.
func (s *Service) CurrentDeviceUpdate(
	ctx context.Context,
	deviceID string,
) (*CurrentDeviceUpdate, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return nil, domain.ErrDeviceNotFound
	}
	page, err := s.repository.ListDeployments(ctx, domain.DeploymentFilter{
		DeviceID: deviceID,
		Page:     1,
		PageSize: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(page.Items) == 0 {
		return nil, domain.ErrNoUpdateAvailable
	}
	deployment := page.Items[0]
	switch deployment.Status {
	case domain.DeploymentStatusSucceeded, domain.DeploymentStatusRolledBack:
		return nil, domain.ErrNoUpdateAvailable
	}
	release, err := s.repository.GetRelease(ctx, deployment.ReleaseID)
	if err != nil {
		return nil, err
	}
	device, err := s.repository.GetDeviceContext(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	return &CurrentDeviceUpdate{
		Release:         *release,
		Deployment:      deployment,
		UpdateAvailable: deployment.Status != domain.DeploymentStatusFailed,
		CurrentVersion:  device.CurrentVersion,
	}, nil
}

// AssignRelease creates one device deployment. A repeated request id returns
// the same deployment, which makes guardian retries safe.
func (s *Service) AssignRelease(
	ctx context.Context,
	releaseID string,
	deviceID string,
	requestID string,
	actorID string,
) (*domain.Deployment, error) {
	actorID, err := validateActor(actorID)
	if err != nil {
		return nil, err
	}
	releaseID = strings.TrimSpace(releaseID)
	deviceID = strings.TrimSpace(deviceID)
	requestID = strings.TrimSpace(requestID)
	if releaseID == "" ||
		deviceID == "" ||
		requestID == "" ||
		len(requestID) > 128 {
		return nil, domain.ErrInvalidDeployment
	}
	release, err := s.repository.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	if release.Status != domain.ReleaseStatusPublished {
		return nil, domain.ErrReleaseStateConflict
	}
	device, err := s.repository.GetDeviceContext(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if !domain.EligibleForRelease(release, *device) {
		return nil, domain.ErrDeviceNotEligible
	}
	now := s.timeSource.Now().UTC()
	deployment := &domain.Deployment{
		ID:            uuid.NewString(),
		ReleaseID:     release.ID,
		DeviceID:      deviceID,
		GroupID:       release.Target.GroupID,
		Status:        domain.DeploymentStatusQueued,
		RequestedBy:   actorID,
		RequestID:     requestID,
		BytesTotal:    release.SizeBytes,
		RecordVersion: 1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := domain.ValidateDeployment(deployment); err != nil {
		return nil, err
	}
	return s.repository.CreateDeployment(ctx, deployment)
}

// ListDeployments returns a bounded deployment page for operators.
func (s *Service) ListDeployments(
	ctx context.Context,
	filter domain.DeploymentFilter,
) (*domain.DeploymentPage, error) {
	return s.repository.ListDeployments(ctx, normalizeDeploymentFilter(filter))
}

// GetDeployment returns one deployment.
func (s *Service) GetDeployment(
	ctx context.Context,
	deploymentID string,
) (*domain.Deployment, error) {
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return nil, domain.ErrDeploymentNotFound
	}
	return s.repository.GetDeployment(ctx, deploymentID)
}

// Statistics returns rollout counts and failure rates.
func (s *Service) Statistics(
	ctx context.Context,
	filter domain.DeploymentFilter,
) (*domain.Statistics, error) {
	return s.repository.Statistics(ctx, normalizeDeploymentFilter(filter))
}

// RetryDeployment requeues a failed deployment.
func (s *Service) RetryDeployment(
	ctx context.Context,
	deploymentID string,
	actorID string,
	expectedVersion int64,
) (*domain.Deployment, error) {
	actorID, err := validateActor(actorID)
	if err != nil {
		return nil, err
	}
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" || expectedVersion <= 0 {
		return nil, domain.ErrInvalidDeployment
	}
	return s.repository.RetryDeployment(
		ctx,
		deploymentID,
		actorID,
		expectedVersion,
		s.timeSource.Now().UTC(),
	)
}

// RollbackDeployment requeues a device deployment as a rollback. The physical
// partition switch remains the device's responsibility; the platform records
// the authorized target and the device reports rollback_started/rolled_back.
func (s *Service) RollbackDeployment(
	ctx context.Context,
	deploymentID string,
	actorID string,
	expectedVersion int64,
) (*domain.Deployment, error) {
	actorID, err := validateActor(actorID)
	if err != nil {
		return nil, err
	}
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" || expectedVersion <= 0 {
		return nil, domain.ErrInvalidDeployment
	}
	current, err := s.repository.GetDeployment(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	if current.RecordVersion != expectedVersion {
		return nil, &domain.DeploymentVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  current.RecordVersion,
		}
	}
	release, err := s.repository.GetRelease(ctx, current.ReleaseID)
	if err != nil {
		return nil, err
	}
	if !release.RollbackAllowed {
		return nil, domain.ErrRollbackNotAllowed
	}
	if current.Status != domain.DeploymentStatusFailed &&
		current.Status != domain.DeploymentStatusSucceeded &&
		current.Status != domain.DeploymentStatusPendingVerify {
		return nil, domain.ErrDeploymentStateConflict
	}
	targetReleaseID := strings.TrimSpace(release.RollbackReleaseID)
	if targetReleaseID == "" {
		return nil, domain.ErrRollbackNotAllowed
	}
	targetRelease, err := s.repository.GetRelease(ctx, targetReleaseID)
	if err != nil {
		return nil, err
	}
	if targetRelease.HardwareRevision != release.HardwareRevision {
		return nil, domain.ErrRollbackNotAllowed
	}
	if targetRelease.Status != domain.ReleaseStatusPublished {
		return nil, domain.ErrRollbackNotAllowed
	}
	now := s.timeSource.Now().UTC()
	rollback := &domain.Deployment{
		ID:                     uuid.NewString(),
		ReleaseID:              targetRelease.ID,
		RollbackReleaseID:      targetRelease.ID,
		RollbackOfDeploymentID: current.ID,
		DeviceID:               current.DeviceID,
		GroupID:                targetRelease.Target.GroupID,
		Status:                 domain.DeploymentStatusQueued,
		RequestedBy:            actorID,
		RequestID:              "rollback:" + current.ID + ":" + targetRelease.ID,
		BytesTotal:             targetRelease.SizeBytes,
		RecordVersion:          1,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := domain.ValidateDeployment(rollback); err != nil {
		return nil, err
	}
	return s.repository.CreateDeployment(ctx, rollback)
}

// RecordEvent records device-authenticated installation progress.
func (s *Service) RecordEvent(
	ctx context.Context,
	input domain.EventInput,
) (*domain.Deployment, bool, error) {
	if strings.TrimSpace(input.DeviceID) == "" {
		return nil, false, domain.ErrDeviceNotFound
	}
	deployment, err := s.repository.GetDeployment(
		ctx,
		strings.TrimSpace(input.DeploymentID),
	)
	if err != nil {
		return nil, false, err
	}
	if deployment.DeviceID != strings.TrimSpace(input.DeviceID) {
		return nil, false, domain.ErrDeviceNotEligible
	}
	status := domain.StatusForEvent(input.Type)
	if status == "" {
		return nil, false, domain.ErrInvalidEvent
	}
	now := s.timeSource.Now().UTC()
	reportedAt := input.ReportedAt.UTC()
	if reportedAt.IsZero() {
		reportedAt = now
	}
	event := &domain.Event{
		ID:              uuid.NewString(),
		DeploymentID:    strings.TrimSpace(input.DeploymentID),
		EventID:         strings.TrimSpace(input.EventID),
		Type:            input.Type,
		Sequence:        input.Sequence,
		Status:          status,
		ProgressPercent: input.ProgressPercent,
		BytesReceived:   input.BytesReceived,
		BytesTotal:      input.BytesTotal,
		ErrorCode:       strings.TrimSpace(input.ErrorCode),
		Message:         strings.TrimSpace(input.Message),
		Detail:          cloneDetail(input.Detail),
		ReportedAt:      reportedAt,
		ReceivedAt:      now,
	}
	if err := domain.ValidateEvent(event); err != nil {
		return nil, false, err
	}
	return s.repository.RecordEvent(ctx, event)
}

func validateActor(actorID string) (string, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" || len(actorID) > 128 {
		return "", domain.ErrInvalidActor
	}
	return actorID, nil
}

func validateReleaseTarget(
	ctx context.Context,
	repository repository.Repository,
	release *domain.Release,
) error {
	if release == nil {
		return domain.ErrInvalidRelease
	}
	if err := release.Target.Validate(); err != nil {
		return err
	}
	switch release.Target.Scope {
	case domain.TargetScopeGroup:
		group, err := repository.GetGroup(ctx, release.Target.GroupID)
		if err != nil {
			return err
		}
		if group.HardwareRevision != release.HardwareRevision {
			return domain.ErrInvalidTarget
		}
	case domain.TargetScopeDevice:
		device, err := repository.GetDeviceContext(ctx, release.Target.DeviceID)
		if err != nil {
			return err
		}
		if device.HardwareRevision != release.HardwareRevision {
			return domain.ErrInvalidTarget
		}
	}
	return nil
}

func validateGroup(group *domain.Group) error {
	if group == nil ||
		strings.TrimSpace(group.Name) == "" ||
		len([]rune(group.Name)) > 128 ||
		len([]rune(group.Description)) > 1000 ||
		!group.Channel.Valid() ||
		strings.TrimSpace(group.HardwareRevision) == "" ||
		len(group.HardwareRevision) > 128 {
		return domain.ErrInvalidGroup
	}
	return nil
}

func validateRollbackReference(
	ctx context.Context,
	repository repository.Repository,
	release *domain.Release,
) error {
	if release == nil || strings.TrimSpace(release.RollbackReleaseID) == "" {
		return nil
	}
	rollbackRelease, err := repository.GetRelease(
		ctx,
		strings.TrimSpace(release.RollbackReleaseID),
	)
	if err != nil {
		return err
	}
	if rollbackRelease.Status == domain.ReleaseStatusWithdrawn ||
		rollbackRelease.Status != domain.ReleaseStatusPublished ||
		rollbackRelease.HardwareRevision != release.HardwareRevision {
		return domain.ErrInvalidRelease
	}
	return nil
}

func validUUID(value string) bool {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil && parsed.String() != "00000000-0000-0000-0000-000000000000"
}

func releaseIdentity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return uuid.NewString()
	}
	return value
}

func normalizeReleaseFilter(filter domain.ReleaseFilter) domain.ReleaseFilter {
	filter.Version = strings.TrimSpace(filter.Version)
	filter.HardwareRevision = strings.TrimSpace(filter.HardwareRevision)
	filter.GroupID = strings.TrimSpace(filter.GroupID)
	filter.DeviceID = strings.TrimSpace(filter.DeviceID)
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	return filter
}

func normalizeDeploymentFilter(
	filter domain.DeploymentFilter,
) domain.DeploymentFilter {
	filter.ReleaseID = strings.TrimSpace(filter.ReleaseID)
	filter.DeviceID = strings.TrimSpace(filter.DeviceID)
	filter.GroupID = strings.TrimSpace(filter.GroupID)
	filter.Version = strings.TrimSpace(filter.Version)
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	return filter
}

func normalizePage(page int, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func cloneDetail(detail map[string]any) map[string]any {
	if detail == nil {
		return map[string]any{}
	}
	copy := make(map[string]any, len(detail))
	for key, value := range detail {
		copy[key] = value
	}
	return copy
}

// Package service owns camera/vision task lifecycle and result use cases.
//
// Vision is guardian-gated: every task creation re-checks image-upload
// consent. Tasks carry only bounded symbolic metadata, and results expire on a
// short retention window. Image bytes never pass through this service.
package service

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

var (
	deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
	labelPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.:/-]{0,63}$`)
)

// ConsentReader reports whether a device's guardian has granted image upload
// consent. It is implemented by the privacy module; the vision module depends
// only on this narrow surface.
type ConsentReader interface {
	ImageUploadConsentForDevice(ctx context.Context, deviceID string) (bool, error)
}

// DeviceReader resolves the parent account that owns a device.
type DeviceReader interface {
	OwnsDevice(ctx context.Context, parentAccountID string, deviceID string) (bool, error)
}

// Service coordinates vision tasks and results.
type Service struct {
	repository    repository.Repository
	consentReader ConsentReader
	deviceReader  DeviceReader
	timeSource    clock.Clock
}

// Options contains vision service dependencies.
type Options struct {
	Repository    repository.Repository
	ConsentReader ConsentReader
	DeviceReader  DeviceReader
	Clock         clock.Clock
}

// New creates the vision service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("vision repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		repository:    options.Repository,
		consentReader: options.ConsentReader,
		deviceReader:  options.DeviceReader,
		timeSource:    timeSource,
	}, nil
}

// Capability returns the guardian-facing camera capability state for one
// device. A missing consent reader is treated as no consent, never as consent.
func (s *Service) Capability(
	ctx context.Context,
	deviceID string,
) (*domain.CapabilityState, error) {
	deviceID = strings.TrimSpace(deviceID)
	if !deviceIDPattern.MatchString(deviceID) {
		return nil, domain.ErrInvalidTask
	}
	granted := false
	if s.consentReader != nil {
		consent, err := s.consentReader.ImageUploadConsentForDevice(ctx, deviceID)
		if err != nil {
			return nil, err
		}
		granted = consent
	}
	return &domain.CapabilityState{
		Supported:      true,
		ConsentGranted: granted,
		ReconsentPath:  "/settings/privacy",
		RetentionHours: int(domain.ResultRetention.Hours()),
	}, nil
}

// CreateTask validates guardian consent and creates a pending vision task.
func (s *Service) CreateTask(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
	taskType domain.TaskType,
	prompt string,
) (*domain.Task, error) {
	deviceID = strings.TrimSpace(deviceID)
	if !deviceIDPattern.MatchString(deviceID) || !domain.TaskTypeIsValid(taskType) {
		return nil, domain.ErrInvalidTask
	}
	prompt = strings.TrimSpace(prompt)
	if len([]rune(prompt)) > 500 {
		return nil, domain.ErrInvalidTask
	}
	if s.deviceReader != nil {
		owned, err := s.deviceReader.OwnsDevice(ctx, parentAccountID, deviceID)
		if err != nil {
			return nil, err
		}
		if !owned {
			return nil, domain.ErrInvalidTask
		}
	}
	if !s.imageConsentGranted(ctx, deviceID) {
		return nil, domain.ErrPrivacyNotGranted
	}
	now := s.timeSource.Now().UTC()
	task := &domain.Task{
		ID:          uuid.NewString(),
		DeviceID:    deviceID,
		TaskType:    taskType,
		Status:      domain.TaskStatusPending,
		RequestedBy: strings.TrimSpace(parentAccountID),
		Prompt:      prompt,
		CreatedAt:   now,
		ExpiresAt:   now.Add(domain.DefaultTaskTTL),
	}
	if err := s.repository.CreateTask(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

// MarkDispatched records that the device acknowledged and started a task.
// Consent is re-checked before a task leaves the platform so a revocation
// between creation and dispatch fails the task instead of sending a frame.
func (s *Service) MarkDispatched(
	ctx context.Context,
	taskID string,
) (*domain.Task, error) {
	task, err := s.repository.GetTask(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}
	if task.Status != domain.TaskStatusPending {
		return nil, domain.ErrTaskNotPending
	}
	if !s.imageConsentGranted(ctx, task.DeviceID) {
		_ = s.repository.UpdateTaskStatus(
			ctx,
			task.ID,
			domain.TaskStatusFailed,
			domain.FailureConsentRevoked,
			s.timeSource.Now().UTC(),
		)
		return nil, domain.ErrPrivacyNotGranted
	}
	if err := s.repository.UpdateTaskStatus(
		ctx,
		task.ID,
		domain.TaskStatusDispatched,
		"",
		s.timeSource.Now().UTC(),
	); err != nil {
		return nil, err
	}
	return s.repository.GetTask(ctx, task.ID)
}

// CompleteTask stores one bounded recognition result and marks the task done.
func (s *Service) CompleteTask(
	ctx context.Context,
	taskID string,
	width int,
	height int,
	byteSize int,
	labels []domain.ResultLabel,
	summary string,
) (*domain.Result, error) {
	taskID = strings.TrimSpace(taskID)
	task, err := s.repository.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != domain.TaskStatusPending &&
		task.Status != domain.TaskStatusDispatched {
		return nil, domain.ErrTaskNotPending
	}
	if !frameIsValid(width, height, byteSize) {
		return nil, domain.ErrInvalidTask
	}
	normalized, err := normalizeLabels(labels)
	if err != nil {
		return nil, err
	}
	summary = strings.TrimSpace(summary)
	if len([]rune(summary)) > 500 {
		return nil, domain.ErrInvalidTask
	}
	now := s.timeSource.Now().UTC()
	task.FrameWidth = width
	task.FrameHeight = height
	task.FrameBytes = byteSize
	result := &domain.Result{
		TaskID:    task.ID,
		DeviceID:  task.DeviceID,
		TaskType:  task.TaskType,
		Labels:    normalized,
		Summary:   summary,
		CreatedAt: now,
		ExpiresAt: now.Add(domain.ResultRetention),
	}
	if err := s.repository.SaveResult(ctx, result); err != nil {
		return nil, err
	}
	if err := s.repository.UpdateTaskStatus(
		ctx,
		task.ID,
		domain.TaskStatusCompleted,
		"",
		now,
	); err != nil {
		return nil, err
	}
	return result, nil
}

// FailTask records a bounded failure for a task.
func (s *Service) FailTask(
	ctx context.Context,
	taskID string,
	failureCode string,
) (*domain.Task, error) {
	taskID = strings.TrimSpace(taskID)
	failureCode = strings.TrimSpace(failureCode)
	if !failureCodeIsValid(failureCode) {
		return nil, domain.ErrInvalidTask
	}
	if err := s.repository.UpdateTaskStatus(
		ctx,
		taskID,
		domain.TaskStatusFailed,
		failureCode,
		s.timeSource.Now().UTC(),
	); err != nil {
		return nil, err
	}
	return s.repository.GetTask(ctx, taskID)
}

// CancelTask cancels a pending or dispatched task.
func (s *Service) CancelTask(
	ctx context.Context,
	taskID string,
) (*domain.Task, error) {
	taskID = strings.TrimSpace(taskID)
	if err := s.repository.UpdateTaskStatus(
		ctx,
		taskID,
		domain.TaskStatusCancelled,
		"",
		s.timeSource.Now().UTC(),
	); err != nil {
		return nil, err
	}
	return s.repository.GetTask(ctx, taskID)
}

// ListTasks returns recent tasks for one device owned by the caller.
func (s *Service) ListTasks(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
	limit int,
) ([]domain.Task, error) {
	deviceID = strings.TrimSpace(deviceID)
	if !deviceIDPattern.MatchString(deviceID) {
		return nil, domain.ErrInvalidTask
	}
	if err := s.requireOwnership(ctx, parentAccountID, deviceID); err != nil {
		return nil, err
	}
	return s.repository.ListTasksByDevice(ctx, deviceID, limit)
}

// ListResults returns short-lived results for one device owned by the caller.
func (s *Service) ListResults(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
	limit int,
) ([]domain.Result, error) {
	deviceID = strings.TrimSpace(deviceID)
	if !deviceIDPattern.MatchString(deviceID) {
		return nil, domain.ErrInvalidTask
	}
	if err := s.requireOwnership(ctx, parentAccountID, deviceID); err != nil {
		return nil, err
	}
	return s.repository.ListResultsByDevice(ctx, deviceID, limit)
}

// GetResult returns one result, only when the caller owns the device.
func (s *Service) GetResult(
	ctx context.Context,
	parentAccountID string,
	taskID string,
) (*domain.Result, error) {
	taskID = strings.TrimSpace(taskID)
	task, err := s.repository.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := s.requireOwnership(ctx, parentAccountID, task.DeviceID); err != nil {
		return nil, err
	}
	return s.repository.GetResult(ctx, taskID)
}

// ListPendingTasksForDevice returns open tasks for a device that has already
// authenticated over its device session. Ownership is enforced by the device
// session verification in transport, so this path is not parent-scoped.
func (s *Service) ListPendingTasksForDevice(
	ctx context.Context,
	deviceID string,
	limit int,
) ([]domain.Task, error) {
	deviceID = strings.TrimSpace(deviceID)
	if !deviceIDPattern.MatchString(deviceID) {
		return nil, domain.ErrInvalidTask
	}
	tasks, err := s.repository.ListTasksByDevice(ctx, deviceID, limit)
	if err != nil {
		return nil, err
	}
	pending := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if task.Status == domain.TaskStatusPending {
			pending = append(pending, task)
		}
	}
	return pending, nil
}

// AdminStats returns a bounded, windowed fleet summary for the admin console.
// A non-positive or oversized window falls back to the default so an operator
// cannot request an unbounded scan.
func (s *Service) AdminStats(
	ctx context.Context,
	windowDays int,
) (*domain.AdminStats, error) {
	if windowDays <= 0 || windowDays > 90 {
		windowDays = 7
	}
	since := s.timeSource.Now().UTC().AddDate(0, 0, -windowDays)
	stats, err := s.repository.GetAdminStats(ctx, since)
	if err != nil {
		return nil, err
	}
	stats.WindowDays = windowDays
	return stats, nil
}

// PruneExpired prunes stale tasks and expired results. It is safe to call from
// a periodic maintenance job.
func (s *Service) PruneExpired(ctx context.Context) (int64, int64, error) {
	now := s.timeSource.Now().UTC()
	expiredTasks, err := s.repository.ExpireStaleTasks(ctx, now)
	if err != nil {
		return 0, 0, err
	}
	deletedResults, err := s.repository.DeleteExpiredResults(ctx, now)
	if err != nil {
		return 0, 0, err
	}
	return expiredTasks, deletedResults, nil
}

func (s *Service) imageConsentGranted(ctx context.Context, deviceID string) bool {
	if s.consentReader == nil {
		return false
	}
	granted, err := s.consentReader.ImageUploadConsentForDevice(ctx, deviceID)
	if err != nil {
		return false
	}
	return granted
}

// requireOwnership denies access to a device that the caller does not own. When
// no device reader is configured the check fails closed.
func (s *Service) requireOwnership(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
) error {
	if s.deviceReader == nil {
		return domain.ErrInvalidTask
	}
	owned, err := s.deviceReader.OwnsDevice(
		ctx,
		strings.TrimSpace(parentAccountID),
		deviceID,
	)
	if err != nil {
		return err
	}
	if !owned {
		return domain.ErrInvalidTask
	}
	return nil
}

func frameIsValid(width, height, byteSize int) bool {
	if width < 0 || width > domain.MaxFrameDimension {
		return false
	}
	if height < 0 || height > domain.MaxFrameDimension {
		return false
	}
	return byteSize >= 0 && byteSize <= domain.MaxFrameBytes
}

func normalizeLabels(labels []domain.ResultLabel) ([]domain.ResultLabel, error) {
	if len(labels) > domain.MaxResultLabels {
		return nil, domain.ErrInvalidTask
	}
	normalized := make([]domain.ResultLabel, 0, len(labels))
	for _, label := range labels {
		value := strings.TrimSpace(label.Label)
		if !labelPattern.MatchString(value) {
			return nil, domain.ErrInvalidTask
		}
		confidence := label.Confidence
		if confidence < 0 {
			confidence = 0
		}
		if confidence > domain.MaxConfidence {
			confidence = domain.MaxConfidence
		}
		normalized = append(normalized, domain.ResultLabel{
			Label:      value,
			Confidence: confidence,
		})
	}
	return normalized, nil
}

func failureCodeIsValid(code string) bool {
	switch code {
	case domain.FailureConsentRevoked,
		domain.FailureTimeout,
		domain.FailureModelRejected,
		domain.FailureCaptureFailed,
		domain.FailureInvalidFrame,
		domain.FailureUnsupported:
		return true
	default:
		return false
	}
}

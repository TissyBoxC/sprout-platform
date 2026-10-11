package service

import (
	"context"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/domain"
)

type memoryRepository struct {
	tasks   map[string]domain.Task
	results map[string]domain.Result
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		tasks:   make(map[string]domain.Task),
		results: make(map[string]domain.Result),
	}
}

func (repository *memoryRepository) CreateTask(
	_ context.Context,
	task *domain.Task,
) error {
	repository.tasks[task.ID] = *task
	return nil
}

func (repository *memoryRepository) GetTask(
	_ context.Context,
	taskID string,
) (*domain.Task, error) {
	task, exists := repository.tasks[taskID]
	if !exists {
		return nil, domain.ErrTaskNotFound
	}
	copy := task
	return &copy, nil
}

func (repository *memoryRepository) UpdateTaskStatus(
	_ context.Context,
	taskID string,
	status domain.TaskStatus,
	failureCode string,
	timestamp time.Time,
) error {
	task, exists := repository.tasks[taskID]
	if !exists {
		return domain.ErrTaskNotFound
	}
	if task.Status != domain.TaskStatusPending &&
		task.Status != domain.TaskStatusDispatched {
		return domain.ErrTaskNotPending
	}
	task.Status = status
	task.FailureCode = failureCode
	switch status {
	case domain.TaskStatusDispatched:
		task.DispatchedAt = &timestamp
	default:
		task.CompletedAt = &timestamp
	}
	repository.tasks[taskID] = task
	return nil
}

func (repository *memoryRepository) ListTasksByDevice(
	_ context.Context,
	deviceID string,
	_ int,
) ([]domain.Task, error) {
	tasks := make([]domain.Task, 0)
	for _, task := range repository.tasks {
		if task.DeviceID == deviceID {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (repository *memoryRepository) ExpireStaleTasks(
	_ context.Context,
	_ time.Time,
) (int64, error) {
	return 0, nil
}

func (repository *memoryRepository) SaveResult(
	_ context.Context,
	result *domain.Result,
) error {
	repository.results[result.TaskID] = *result
	return nil
}

func (repository *memoryRepository) GetResult(
	_ context.Context,
	taskID string,
) (*domain.Result, error) {
	result, exists := repository.results[taskID]
	if !exists {
		return nil, domain.ErrResultNotFound
	}
	copy := result
	return &copy, nil
}

func (repository *memoryRepository) ListResultsByDevice(
	_ context.Context,
	deviceID string,
	_ int,
) ([]domain.Result, error) {
	results := make([]domain.Result, 0)
	for _, result := range repository.results {
		if result.DeviceID == deviceID {
			results = append(results, result)
		}
	}
	return results, nil
}

func (repository *memoryRepository) DeleteExpiredResults(
	_ context.Context,
	_ time.Time,
) (int64, error) {
	return 0, nil
}

type stubConsent struct {
	granted bool
	err     error
}

func (stub stubConsent) ImageUploadConsentForDevice(
	_ context.Context,
	_ string,
) (bool, error) {
	return stub.granted, stub.err
}

type stubDeviceReader struct {
	owned bool
	err   error
}

func (stub stubDeviceReader) OwnsDevice(
	_ context.Context,
	_ string,
	_ string,
) (bool, error) {
	return stub.owned, stub.err
}

func newTestService(
	t *testing.T,
	consent bool,
	owned bool,
) (*Service, *memoryRepository) {
	t.Helper()
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository:    repository,
		ConsentReader: stubConsent{granted: consent},
		DeviceReader:  stubDeviceReader{owned: owned},
	})
	if err != nil {
		t.Fatalf("new vision service: %v", err)
	}
	return service, repository
}

func TestCreateTaskRequiresConsent(t *testing.T) {
	service, _ := newTestService(t, false, true)
	_, err := service.CreateTask(
		context.Background(),
		"parent-001",
		"sprout_device_001",
		domain.TaskTypeObjectRecognition,
		"",
	)
	if err != domain.ErrPrivacyNotGranted {
		t.Fatalf("expected consent error, got %v", err)
	}
}

func TestCreateTaskRejectsUnownedDevice(t *testing.T) {
	service, _ := newTestService(t, true, false)
	_, err := service.CreateTask(
		context.Background(),
		"parent-001",
		"sprout_device_001",
		domain.TaskTypeObjectRecognition,
		"",
	)
	if err != domain.ErrInvalidTask {
		t.Fatalf("expected invalid task error, got %v", err)
	}
}

func TestCreateTaskRejectsUnsupportedType(t *testing.T) {
	service, _ := newTestService(t, true, true)
	_, err := service.CreateTask(
		context.Background(),
		"parent-001",
		"sprout_device_001",
		domain.TaskType("unknown"),
		"",
	)
	if err != domain.ErrInvalidTask {
		t.Fatalf("expected invalid task error, got %v", err)
	}
}

func TestCompleteTaskStoresBoundedResult(t *testing.T) {
	service, _ := newTestService(t, true, true)
	task, err := service.CreateTask(
		context.Background(),
		"parent-001",
		"sprout_device_001",
		domain.TaskTypePictureBook,
		"这是什么",
	)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.Status != domain.TaskStatusPending {
		t.Fatalf("expected pending task, got %q", task.Status)
	}
	result, err := service.CompleteTask(
		context.Background(),
		task.ID,
		320,
		240,
		2048,
		[]domain.ResultLabel{
			{Label: "teddy bear", Confidence: 12000},
		},
		"一只玩具熊",
	)
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if len(result.Labels) != 1 {
		t.Fatalf("expected one label, got %d", len(result.Labels))
	}
	if result.Labels[0].Confidence != domain.MaxConfidence {
		t.Fatalf(
			"expected confidence clamped to %d, got %d",
			domain.MaxConfidence,
			result.Labels[0].Confidence,
		)
	}
}

func TestCompleteTaskRejectsTooManyLabels(t *testing.T) {
	service, _ := newTestService(t, true, true)
	task, err := service.CreateTask(
		context.Background(),
		"parent-001",
		"sprout_device_001",
		domain.TaskTypeObjectRecognition,
		"",
	)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	labels := make([]domain.ResultLabel, 0, domain.MaxResultLabels+1)
	for index := 0; index <= domain.MaxResultLabels; index++ {
		labels = append(labels, domain.ResultLabel{
			Label:      "object",
			Confidence: 100,
		})
	}
	_, err = service.CompleteTask(
		context.Background(),
		task.ID,
		320,
		240,
		2048,
		labels,
		"",
	)
	if err != domain.ErrInvalidTask {
		t.Fatalf("expected invalid task error, got %v", err)
	}
}

func TestMarkDispatchedFailsWhenConsentRevoked(t *testing.T) {
	repository := newMemoryRepository()
	consent := &mutableConsent{granted: true}
	service, err := New(Options{
		Repository:    repository,
		ConsentReader: consent,
		DeviceReader:  stubDeviceReader{owned: true},
	})
	if err != nil {
		t.Fatalf("new vision service: %v", err)
	}
	task, err := service.CreateTask(
		context.Background(),
		"parent-001",
		"sprout_device_001",
		domain.TaskTypeCapture,
		"",
	)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	consent.granted = false
	_, err = service.MarkDispatched(context.Background(), task.ID)
	if err != domain.ErrPrivacyNotGranted {
		t.Fatalf("expected consent error, got %v", err)
	}
	stored, err := repository.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if stored.Status != domain.TaskStatusFailed ||
		stored.FailureCode != domain.FailureConsentRevoked {
		t.Fatalf("expected consent-revoked failure, got %+v", stored)
	}
}

type mutableConsent struct {
	granted bool
}

func (consent *mutableConsent) ImageUploadConsentForDevice(
	_ context.Context,
	_ string,
) (bool, error) {
	return consent.granted, nil
}

func TestCapabilityReportsConsent(t *testing.T) {
	service, _ := newTestService(t, false, true)
	state, err := service.Capability(context.Background(), "sprout_device_001")
	if err != nil {
		t.Fatalf("capability: %v", err)
	}
	if !state.Supported || state.ConsentGranted {
		t.Fatalf("unexpected capability state: %+v", state)
	}
	if state.RetentionHours != int(domain.ResultRetention.Hours()) {
		t.Fatalf("unexpected retention hours: %d", state.RetentionHours)
	}
}

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/repository"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type memoryRepository struct {
	requests []domain.Request
}

func (r *memoryRepository) CreateRequest(
	_ context.Context,
	request *domain.Request,
) error {
	r.requests = append(r.requests, *request)
	return nil
}

func (r *memoryRepository) ListRequests(
	_ context.Context,
	familyID string,
	query domain.RequestQuery,
) ([]domain.Request, error) {
	result := make([]domain.Request, 0, len(r.requests))
	for _, request := range r.requests {
		if request.FamilyID != familyID {
			continue
		}
		if query.DeviceID != "" && request.DeviceID != query.DeviceID {
			continue
		}
		if query.Status != "" && request.Status != query.Status {
			continue
		}
		result = append(result, request)
	}
	if query.Limit > 0 && len(result) > query.Limit {
		result = result[:query.Limit]
	}
	return result, nil
}

func (r *memoryRepository) GetRequest(
	_ context.Context,
	familyID string,
	requestID string,
) (*domain.Request, error) {
	for index := range r.requests {
		if r.requests[index].FamilyID == familyID &&
			r.requests[index].ID == requestID {
			copyRequest := r.requests[index]
			return &copyRequest, nil
		}
	}
	return nil, domain.ErrRequestNotFound
}

func (r *memoryRepository) CancelRequest(
	_ context.Context,
	familyID string,
	requestID string,
) (*domain.Request, error) {
	for index := range r.requests {
		if r.requests[index].FamilyID != familyID ||
			r.requests[index].ID != requestID {
			continue
		}
		if r.requests[index].Status != domain.StatusPending {
			copyRequest := r.requests[index]
			return &copyRequest, domain.ErrInvalidRequestState
		}
		r.requests[index].Status = domain.StatusCancelled
		copyRequest := r.requests[index]
		return &copyRequest, nil
	}
	return nil, domain.ErrRequestNotFound
}

func (r *memoryRepository) ListPendingForDevice(
	_ context.Context,
	deviceID string,
) ([]domain.Request, error) {
	result := make([]domain.Request, 0, len(r.requests))
	for _, request := range r.requests {
		if request.DeviceID != deviceID {
			continue
		}
		if request.Status != domain.StatusPending &&
			request.Status != domain.StatusAcknowledged {
			continue
		}
		result = append(result, request)
	}
	return result, nil
}

func (r *memoryRepository) AcknowledgeRequest(
	_ context.Context,
	deviceID string,
	requestID string,
) (*domain.Request, error) {
	for index := range r.requests {
		if r.requests[index].DeviceID != deviceID ||
			r.requests[index].ID != requestID {
			continue
		}
		if r.requests[index].Status != domain.StatusPending {
			return nil, domain.ErrRequestNotFound
		}
		r.requests[index].Status = domain.StatusAcknowledged
		copyRequest := r.requests[index]
		return &copyRequest, nil
	}
	return nil, domain.ErrRequestNotFound
}

func (r *memoryRepository) CompleteRequest(
	_ context.Context,
	deviceID string,
	requestID string,
	outcome string,
) (*domain.Request, error) {
	for index := range r.requests {
		if r.requests[index].DeviceID != deviceID ||
			r.requests[index].ID != requestID {
			continue
		}
		if domain.IsTerminal(r.requests[index].Status) {
			return nil, domain.ErrRequestNotFound
		}
		status := domain.StatusCompleted
		if outcome == domain.OutcomeError {
			status = domain.StatusFailed
		}
		r.requests[index].Status = status
		r.requests[index].Outcome = outcome
		copyRequest := r.requests[index]
		return &copyRequest, nil
	}
	return nil, domain.ErrRequestNotFound
}

func (r *memoryRepository) HasPendingRequest(
	_ context.Context,
	familyID string,
	deviceID string,
	packageID string,
) (bool, error) {
	for _, request := range r.requests {
		if request.FamilyID != familyID ||
			request.DeviceID != deviceID ||
			request.PackageID != packageID {
			continue
		}
		if request.Status == domain.StatusPending ||
			request.Status == domain.StatusAcknowledged {
			return true, nil
		}
	}
	return false, nil
}

type stubDeviceReader struct {
	deviceIDs []string
	err       error
}

func (r stubDeviceReader) FamilyDeviceIDs(
	_ context.Context,
	_ string,
) ([]string, error) {
	return r.deviceIDs, r.err
}

type stubPackageReader struct {
	published map[string]bool
}

func (r stubPackageReader) IsPublished(
	_ context.Context,
	packageID string,
) (bool, error) {
	return r.published[packageID], nil
}

func newTestService(repository repository.Repository) *Service {
	service, err := New(Options{
		Repository: repository,
		Clock: fixedClock{
			now: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC),
		},
		DeviceReader: stubDeviceReader{deviceIDs: []string{"device-1"}},
		PackageReader: stubPackageReader{
			published: map[string]bool{"lesson_story": true},
		},
	})
	if err != nil {
		panic(err)
	}
	return service
}

func TestCreateStoresPendingRequest(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(repository)

	request, err := service.Create(context.Background(), "family-1", domain.RequestInput{
		DeviceID:  "device-1",
		PackageID: "lesson_story",
		Note:      "睡前播放",
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if request.Status != domain.StatusPending {
		t.Fatalf("status = %q, want pending", request.Status)
	}
	if request.Source != domain.SourceGuardian {
		t.Fatalf("source = %q, want guardian", request.Source)
	}
	if request.ID == "" {
		t.Fatal("expected a generated request id")
	}
	if len(repository.requests) != 1 {
		t.Fatalf("stored = %d, want 1", len(repository.requests))
	}
}

func TestCreateRejectsUnownedDevice(t *testing.T) {
	service := newTestService(&memoryRepository{})

	_, err := service.Create(context.Background(), "family-1", domain.RequestInput{
		DeviceID:  "device-9",
		PackageID: "lesson_story",
	})
	if !errors.Is(err, domain.ErrInvalidDeviceID) {
		t.Fatalf("err = %v, want ErrInvalidDeviceID", err)
	}
}

func TestCreateRejectsUnpublishedPackage(t *testing.T) {
	service := newTestService(&memoryRepository{})

	_, err := service.Create(context.Background(), "family-1", domain.RequestInput{
		DeviceID:  "device-1",
		PackageID: "lesson_draft",
	})
	if !errors.Is(err, domain.ErrInvalidPackageID) {
		t.Fatalf("err = %v, want ErrInvalidPackageID", err)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	service := newTestService(&memoryRepository{})
	cases := []domain.RequestInput{
		{DeviceID: "", PackageID: "lesson_story"},
		{DeviceID: "device-1", PackageID: "Bad!"},
		{DeviceID: "device-1", PackageID: "lesson_story", Note: string(make([]byte, 201))},
	}
	for _, input := range cases {
		if _, err := service.Create(context.Background(), "family-1", input); err == nil {
			t.Fatalf("input %+v = nil error, want rejection", input)
		}
	}
}

func TestCreateRejectsDuplicatePending(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(repository)
	input := domain.RequestInput{DeviceID: "device-1", PackageID: "lesson_story"}

	if _, err := service.Create(context.Background(), "family-1", input); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := service.Create(context.Background(), "family-1", input)
	if !errors.Is(err, domain.ErrDuplicateRequest) {
		t.Fatalf("err = %v, want ErrDuplicateRequest", err)
	}
}

func TestListClampsLimitAndValidatesStatus(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(repository)
	if _, err := service.Create(context.Background(), "family-1", domain.RequestInput{
		DeviceID: "device-1", PackageID: "lesson_story",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	requests, err := service.List(context.Background(), "family-1", domain.RequestQuery{
		Limit: 9999,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}

	if _, err := service.List(context.Background(), "family-1", domain.RequestQuery{
		Status: "bogus",
	}); !errors.Is(err, domain.ErrInvalidRequestState) {
		t.Fatalf("err = %v, want ErrInvalidRequestState", err)
	}
}

func TestListIsFamilyScoped(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(repository)
	if _, err := service.Create(context.Background(), "family-1", domain.RequestInput{
		DeviceID: "device-1", PackageID: "lesson_story",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	requests, err := service.List(context.Background(), "family-2", domain.RequestQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("requests = %d, want 0 for another family", len(requests))
	}
}

func TestCancelOnlyPending(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(repository)
	created, err := service.Create(context.Background(), "family-1", domain.RequestInput{
		DeviceID: "device-1", PackageID: "lesson_story",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cancelled, err := service.Cancel(context.Background(), "family-1", created.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	if _, err := service.Cancel(context.Background(), "family-1", created.ID); !errors.Is(err, domain.ErrInvalidRequestState) {
		t.Fatalf("second cancel err = %v, want ErrInvalidRequestState", err)
	}
}

func TestDeviceLifecycleAcknowledgeThenComplete(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(repository)
	created, err := service.Create(context.Background(), "family-1", domain.RequestInput{
		DeviceID: "device-1", PackageID: "lesson_story",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	pending, err := service.ListPendingForDevice(context.Background(), "device-1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}

	acknowledged, err := service.Acknowledge(context.Background(), "device-1", created.ID)
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if acknowledged.Status != domain.StatusAcknowledged {
		t.Fatalf("status = %q, want acknowledged", acknowledged.Status)
	}

	completed, err := service.Complete(
		context.Background(),
		"device-1",
		created.ID,
		domain.OutcomePlayed,
	)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.Status != domain.StatusCompleted {
		t.Fatalf("status = %q, want completed", completed.Status)
	}

	// A request addressed to another device is invisible.
	if _, err := service.Acknowledge(context.Background(), "device-9", created.ID); !errors.Is(err, domain.ErrRequestNotFound) {
		t.Fatalf("err = %v, want ErrRequestNotFound", err)
	}
}

func TestCompleteDefaultsToPlayedAndRejectsBadOutcome(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(repository)
	created, err := service.Create(context.Background(), "family-1", domain.RequestInput{
		DeviceID: "device-1", PackageID: "lesson_story",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := service.Complete(context.Background(), "device-1", created.ID, "bogus"); !errors.Is(err, domain.ErrInvalidOutcome) {
		t.Fatalf("err = %v, want ErrInvalidOutcome", err)
	}

	completed, err := service.Complete(context.Background(), "device-1", created.ID, "")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.Outcome != domain.OutcomePlayed {
		t.Fatalf("outcome = %q, want played", completed.Outcome)
	}
}

func TestCreateFailsClosedWithoutOwnershipAndPackageReaders(t *testing.T) {
	for name, options := range map[string]Options{
		"device reader": {
			Repository:    &memoryRepository{},
			PackageReader: stubPackageReader{published: map[string]bool{"lesson_story": true}},
		},
		"package reader": {
			Repository:   &memoryRepository{},
			DeviceReader: stubDeviceReader{deviceIDs: []string{"device-1"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			service, err := New(options)
			if err != nil {
				t.Fatalf("new service: %v", err)
			}
			_, err = service.Create(context.Background(), "family-1", domain.RequestInput{
				DeviceID:  "device-1",
				PackageID: "lesson_story",
			})
			if err == nil {
				t.Fatal("Create() succeeded without a required reader")
			}
		})
	}
}

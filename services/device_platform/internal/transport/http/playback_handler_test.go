package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	playbackdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/domain"
	playbackservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/service"
)

// playbackTestRepository is an in-memory double so the handler tests exercise
// the real service validation without a database.
type playbackTestRepository struct {
	requests []playbackdomain.Request
}

func (r *playbackTestRepository) CreateRequest(
	_ context.Context,
	request *playbackdomain.Request,
) error {
	r.requests = append(r.requests, *request)
	return nil
}

func (r *playbackTestRepository) ListRequests(
	_ context.Context,
	familyID string,
	query playbackdomain.RequestQuery,
) ([]playbackdomain.Request, error) {
	result := make([]playbackdomain.Request, 0, len(r.requests))
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
	return result, nil
}

func (r *playbackTestRepository) GetRequest(
	_ context.Context,
	familyID string,
	requestID string,
) (*playbackdomain.Request, error) {
	for index := range r.requests {
		if r.requests[index].FamilyID == familyID &&
			r.requests[index].ID == requestID {
			copyRequest := r.requests[index]
			return &copyRequest, nil
		}
	}
	return nil, playbackdomain.ErrRequestNotFound
}

func (r *playbackTestRepository) CancelRequest(
	_ context.Context,
	familyID string,
	requestID string,
) (*playbackdomain.Request, error) {
	for index := range r.requests {
		if r.requests[index].FamilyID != familyID ||
			r.requests[index].ID != requestID {
			continue
		}
		if r.requests[index].Status != playbackdomain.StatusPending {
			return nil, playbackdomain.ErrInvalidRequestState
		}
		r.requests[index].Status = playbackdomain.StatusCancelled
		copyRequest := r.requests[index]
		return &copyRequest, nil
	}
	return nil, playbackdomain.ErrRequestNotFound
}

func (r *playbackTestRepository) ListPendingForDevice(
	_ context.Context,
	deviceID string,
) ([]playbackdomain.Request, error) {
	result := make([]playbackdomain.Request, 0, len(r.requests))
	for _, request := range r.requests {
		if request.DeviceID != deviceID {
			continue
		}
		if request.Status != playbackdomain.StatusPending &&
			request.Status != playbackdomain.StatusAcknowledged {
			continue
		}
		result = append(result, request)
	}
	return result, nil
}

func (r *playbackTestRepository) AcknowledgeRequest(
	_ context.Context,
	deviceID string,
	requestID string,
) (*playbackdomain.Request, error) {
	for index := range r.requests {
		if r.requests[index].DeviceID != deviceID ||
			r.requests[index].ID != requestID {
			continue
		}
		if r.requests[index].Status != playbackdomain.StatusPending {
			return nil, playbackdomain.ErrRequestNotFound
		}
		r.requests[index].Status = playbackdomain.StatusAcknowledged
		copyRequest := r.requests[index]
		return &copyRequest, nil
	}
	return nil, playbackdomain.ErrRequestNotFound
}

func (r *playbackTestRepository) CompleteRequest(
	_ context.Context,
	deviceID string,
	requestID string,
	outcome string,
) (*playbackdomain.Request, error) {
	for index := range r.requests {
		if r.requests[index].DeviceID != deviceID ||
			r.requests[index].ID != requestID {
			continue
		}
		if playbackdomain.IsTerminal(r.requests[index].Status) {
			return nil, playbackdomain.ErrRequestNotFound
		}
		status := playbackdomain.StatusCompleted
		if outcome == playbackdomain.OutcomeError {
			status = playbackdomain.StatusFailed
		}
		r.requests[index].Status = status
		r.requests[index].Outcome = outcome
		copyRequest := r.requests[index]
		return &copyRequest, nil
	}
	return nil, playbackdomain.ErrRequestNotFound
}

func (r *playbackTestRepository) HasPendingRequest(
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
		if request.Status == playbackdomain.StatusPending ||
			request.Status == playbackdomain.StatusAcknowledged {
			return true, nil
		}
	}
	return false, nil
}

type playbackTestDeviceReader struct{}

func (playbackTestDeviceReader) FamilyDeviceIDs(
	_ context.Context,
	_ string,
) ([]string, error) {
	return []string{"device-1"}, nil
}

type playbackTestPackageReader struct{}

func (playbackTestPackageReader) IsPublished(
	_ context.Context,
	packageID string,
) (bool, error) {
	return packageID == "lesson_story", nil
}

func newPlaybackHandlerForTest(
	t *testing.T,
) (playbackHandler, *playbackTestRepository) {
	t.Helper()
	repository := &playbackTestRepository{}
	service, err := playbackservice.New(playbackservice.Options{
		Repository:    repository,
		DeviceReader:  playbackTestDeviceReader{},
		PackageReader: playbackTestPackageReader{},
		Clock:         playbackTestClock{},
	})
	if err != nil {
		t.Fatalf("create playback service: %v", err)
	}
	return playbackHandler{service: service}, repository
}

type playbackTestClock struct{}

func (playbackTestClock) Now() time.Time {
	return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
}

func playbackRequest(
	method string,
	target string,
	body string,
	accountID string,
) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if accountID == "" {
		return request
	}
	return request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		accountID,
	))
}

func TestPlaybackCreateRequiresAuthentication(t *testing.T) {
	handler, _ := newPlaybackHandlerForTest(t)
	request := playbackRequest(
		http.MethodPost,
		"/api/v1/playback/requests",
		`{"device_id":"device-1","package_id":"lesson_story"}`,
		"",
	)
	recorder := httptest.NewRecorder()
	handler.create(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestPlaybackCreateStoresRequest(t *testing.T) {
	handler, repository := newPlaybackHandlerForTest(t)
	request := playbackRequest(
		http.MethodPost,
		"/api/v1/playback/requests",
		`{"device_id":"device-1","package_id":"lesson_story","note":"睡前播放"}`,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.create(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if len(repository.requests) != 1 {
		t.Fatalf("stored = %d, want 1", len(repository.requests))
	}
	var envelope struct {
		Data struct {
			Request struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"request"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Request.Status != playbackdomain.StatusPending {
		t.Fatalf("status = %q, want pending", envelope.Data.Request.Status)
	}
}

func TestPlaybackCreateRejectsUnpublishedPackage(t *testing.T) {
	handler, _ := newPlaybackHandlerForTest(t)
	request := playbackRequest(
		http.MethodPost,
		"/api/v1/playback/requests",
		`{"device_id":"device-1","package_id":"lesson_draft"}`,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.create(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
}

func TestPlaybackCreateRejectsDuplicate(t *testing.T) {
	handler, _ := newPlaybackHandlerForTest(t)
	body := `{"device_id":"device-1","package_id":"lesson_story"}`
	first := playbackRequest(
		http.MethodPost, "/api/v1/playback/requests", body, "family-1")
	handler.create(httptest.NewRecorder(), first)

	second := playbackRequest(
		http.MethodPost, "/api/v1/playback/requests", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.create(recorder, second)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusConflict, recorder.Body.String())
	}
}

func TestPlaybackListIsFamilyScoped(t *testing.T) {
	handler, repository := newPlaybackHandlerForTest(t)
	repository.requests = []playbackdomain.Request{
		{
			ID:          "req-1",
			FamilyID:    "family-1",
			DeviceID:    "device-1",
			PackageID:   "lesson_story",
			Source:      playbackdomain.SourceGuardian,
			Status:      playbackdomain.StatusPending,
			RequestedAt: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC),
		},
		{
			ID:          "req-2",
			FamilyID:    "family-2",
			DeviceID:    "device-2",
			PackageID:   "lesson_story",
			Source:      playbackdomain.SourceGuardian,
			Status:      playbackdomain.StatusPending,
			RequestedAt: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC),
		},
	}
	request := playbackRequest(
		http.MethodGet, "/api/v1/playback/requests", "", "family-1")
	recorder := httptest.NewRecorder()
	handler.list(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var envelope struct {
		Data struct {
			Requests []map[string]any `json:"requests"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.Requests) != 1 {
		t.Fatalf("requests = %d, want family-1 only", len(envelope.Data.Requests))
	}
}

func TestPlaybackGetNotFound(t *testing.T) {
	handler, _ := newPlaybackHandlerForTest(t)
	request := playbackRequest(
		http.MethodGet, "/api/v1/playback/requests/req-missing", "", "family-1")
	request.SetPathValue("request_id", "req-missing")
	recorder := httptest.NewRecorder()
	handler.get(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPlaybackCancelPendingThenConflict(t *testing.T) {
	handler, repository := newPlaybackHandlerForTest(t)
	repository.requests = []playbackdomain.Request{
		{
			ID:          "req-1",
			FamilyID:    "family-1",
			DeviceID:    "device-1",
			PackageID:   "lesson_story",
			Source:      playbackdomain.SourceGuardian,
			Status:      playbackdomain.StatusPending,
			RequestedAt: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC),
		},
	}
	request := playbackRequest(
		http.MethodPost,
		"/api/v1/playback/requests/req-1/cancel",
		"",
		"family-1",
	)
	request.SetPathValue("request_id", "req-1")
	recorder := httptest.NewRecorder()
	handler.cancel(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	second := playbackRequest(
		http.MethodPost,
		"/api/v1/playback/requests/req-1/cancel",
		"",
		"family-1",
	)
	second.SetPathValue("request_id", "req-1")
	conflictRecorder := httptest.NewRecorder()
	handler.cancel(conflictRecorder, second)
	if conflictRecorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", conflictRecorder.Code, http.StatusConflict)
	}
}

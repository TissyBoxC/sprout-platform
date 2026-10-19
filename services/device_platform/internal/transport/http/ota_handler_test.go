package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
	otarepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/repository"
	otaservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/service"
)

type rollbackDeviceSessionVerifier struct{}

func (rollbackDeviceSessionVerifier) VerifyDeviceSession(context.Context, string) (string, error) {
	return "device-1", nil
}

type rollbackOTARepository struct {
	otarepository.Repository
	release    *domain.Release
	deployment *domain.Deployment
}

type deviceEventOTARepository struct {
	otarepository.Repository
	deployment *domain.Deployment
	events     map[string]*domain.Event
}

func (repository rollbackOTARepository) ListDeployments(
	context.Context,
	domain.DeploymentFilter,
) (*domain.DeploymentPage, error) {
	return &domain.DeploymentPage{Items: []domain.Deployment{*repository.deployment}}, nil
}

func (repository rollbackOTARepository) GetRelease(
	context.Context,
	string,
) (*domain.Release, error) {
	copy := *repository.release
	return &copy, nil
}

func (repository rollbackOTARepository) GetDeviceContext(
	context.Context,
	string,
) (*domain.DeviceContext, error) {
	return &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: repository.release.HardwareRevision,
		CurrentVersion:   "1.3.0",
	}, nil
}

func (repository *deviceEventOTARepository) GetDeployment(
	context.Context,
	string,
) (*domain.Deployment, error) {
	copy := *repository.deployment
	return &copy, nil
}

func (repository *deviceEventOTARepository) ListDeployments(
	context.Context,
	domain.DeploymentFilter,
) (*domain.DeploymentPage, error) {
	return &domain.DeploymentPage{}, nil
}

func (repository *deviceEventOTARepository) GetDeviceContext(
	context.Context,
	string,
) (*domain.DeviceContext, error) {
	return &domain.DeviceContext{
		DeviceID:         "device-1",
		HardwareRevision: "sprout-v1",
		CurrentVersion:   "0.24.0",
	}, nil
}

func (repository *deviceEventOTARepository) ListPublishedReleases(
	context.Context,
	domain.Channel,
	string,
) ([]domain.Release, error) {
	return []domain.Release{}, nil
}

func (repository *deviceEventOTARepository) RecordEvent(
	_ context.Context,
	event *domain.Event,
) (*domain.Deployment, bool, error) {
	key := event.DeploymentID + ":" + event.EventID
	if existing, ok := repository.events[key]; ok {
		if existing.Type != event.Type ||
			existing.Sequence != event.Sequence ||
			existing.ProgressPercent != event.ProgressPercent {
			return nil, false, domain.ErrEventConflict
		}
		copy := *repository.deployment
		return &copy, false, nil
	}
	if event.Sequence <= repository.deployment.LastEventSequence {
		return nil, false, domain.ErrInvalidEvent
	}
	if !domain.CanTransitionDeployment(repository.deployment.Status, event.Status) {
		return nil, false, domain.ErrDeploymentStateConflict
	}
	repository.events[key] = event
	repository.deployment.Status = event.Status
	repository.deployment.ProgressPercent = event.ProgressPercent
	repository.deployment.LastEventSequence = event.Sequence
	repository.deployment.RecordVersion++
	copy := *repository.deployment
	return &copy, true, nil
}

type deviceEventSessionVerifier struct {
	deviceID string
}

func (verifier deviceEventSessionVerifier) VerifyDeviceSession(
	context.Context,
	string,
) (string, error) {
	return verifier.deviceID, nil
}

func newDeviceEventHandlerForTest(t *testing.T) (otaHandler, *deviceEventOTARepository) {
	t.Helper()
	repository := &deviceEventOTARepository{
		deployment: &domain.Deployment{
			ID:                "ota_deployment_001",
			ReleaseID:         "ota_release_001",
			DeviceID:          "device-1",
			Status:            domain.DeploymentStatusPendingVerify,
			LastEventSequence: 4,
			RecordVersion:     1,
		},
		events: map[string]*domain.Event{},
	}
	service, err := otaservice.New(otaservice.Options{Repository: repository})
	if err != nil {
		t.Fatalf("create OTA service: %v", err)
	}
	return newOTAHandler(
		service,
		deviceEventSessionVerifier{deviceID: "device-1"},
		nil,
	), repository
}

func postDeviceEvent(handler otaHandler, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/devices/device-1/ota/events",
		strings.NewReader(body),
	)
	request.SetPathValue("device_id", "device-1")
	request.Header.Set("Authorization", "Bearer device-session")
	response := httptest.NewRecorder()
	handler.recordDeviceEvent(response, request)
	return response
}

func deviceEventPayload(eventType string, sequence int64) string {
	payload, err := json.Marshal(map[string]any{
		"schema_version":   "1.0.0",
		"event_id":         "ota_evt_" + eventType,
		"deployment_id":    "ota_deployment_001",
		"release_id":       "ota_release_001",
		"event_type":       eventType,
		"sequence":         sequence,
		"progress_percent": 80,
		"bytes_received":   819200,
		"bytes_total":      1024000,
		"detail":           map[string]any{},
		"occurred_at":      "2026-10-18T12:00:00Z",
		"firmware_version": "0.24.0",
		"artifact_sha256":  strings.Repeat("a", 64),
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func TestValidateOTAReleaseMutationRequiresCurrentSignaturePayloadVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		version   string
		releaseID string
		wantErr   bool
	}{
		{name: "current", version: domain.OTAReleaseSignaturePayloadVersion, releaseID: "00000000-0000-4000-8000-000000000001"},
		{name: "missing", version: "", wantErr: true},
		{name: "legacy digest-only", version: "1", wantErr: true},
		{name: "arbitrary", version: "3", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateOTAReleaseMutation(otaReleaseMutation{
				ReleaseID:               test.releaseID,
				SignaturePayloadVersion: test.version,
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("validateOTAReleaseMutation() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestDeviceReleaseResponseIncludesPublishedAt(t *testing.T) {
	t.Parallel()
	publishedAt := time.Date(2026, 10, 18, 8, 9, 10, 0, time.FixedZone("SGT", 8*60*60))
	response := otaDeviceReleaseResponse(domain.DeviceRelease{
		ReleaseID:   "00000000-0000-4000-8000-000000000001",
		Version:     "0.13.0",
		PublishedAt: &publishedAt,
	})
	if response["published_at"] != "2026-10-18T00:09:10Z" {
		t.Fatalf("published_at = %v", response["published_at"])
	}
}

func TestCurrentDeviceUpdateStatusIsRollbackAvailableForRollback(t *testing.T) {
	t.Parallel()
	status := currentDeviceUpdateStatus(&otaservice.CurrentDeviceUpdate{
		Deployment: domain.Deployment{Status: domain.DeploymentStatusQueued},
		Rollback:   true,
	})
	if status != "rollback_available" {
		t.Fatalf("status = %q, want rollback_available", status)
	}
}

func TestDeviceUpdateHTTPMarksAuthorizedRollback(t *testing.T) {
	t.Parallel()
	release := &domain.Release{
		ID:                 "00000000-0000-4000-8000-000000000001",
		Version:            "1.2.0",
		Channel:            domain.ChannelStable,
		HardwareRevision:   "sprout-v1",
		ArtifactURL:        "https://download.clarkhub.cn/firmware/1.2.0.bin",
		ArtifactKey:        "firmware/1.2.0.bin",
		SHA256:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SizeBytes:          1024,
		SignatureKeyID:     "firmware-prod-2026",
		SignatureAlgorithm: "ed25519",
		Signature:          "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==",
		RollbackAllowed:    true,
		Status:             domain.ReleaseStatusPublished,
		Target:             domain.Target{Scope: domain.TargetScopeAll},
		RecordVersion:      1,
	}
	deployment := &domain.Deployment{
		ID:                     "00000000-0000-4000-8000-000000000002",
		ReleaseID:              release.ID,
		RollbackReleaseID:      release.ID,
		RollbackOfDeploymentID: "00000000-0000-4000-8000-000000000003",
		DeviceID:               "device-1",
		Status:                 domain.DeploymentStatusQueued,
		RequestID:              "rollback-http",
		RecordVersion:          1,
	}
	service, err := otaservice.New(otaservice.Options{
		Repository: rollbackOTARepository{
			release:    release,
			deployment: deployment,
		},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	handler := newOTAHandler(service, rollbackDeviceSessionVerifier{}, nil)
	request := httptest.NewRequest(
		"GET",
		"/api/v1/devices/device-1/ota",
		nil,
	)
	request.SetPathValue("device_id", "device-1")
	request.Header.Set("Authorization", "Bearer device-session")
	response := httptest.NewRecorder()
	handler.deviceUpdate(response, request)
	if response.Code != 200 {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data["status"] != "rollback_available" {
		t.Fatalf("status = %v", envelope.Data["status"])
	}
	if envelope.Data["schema_version"] != domain.OTAReleaseSchemaVersion {
		t.Fatalf("schema_version = %v", envelope.Data["schema_version"])
	}
	if envelope.Data["rollback_available"] != true {
		t.Fatalf("rollback_available = %v", envelope.Data["rollback_available"])
	}
	if envelope.Data["update_available"] != true {
		t.Fatalf("update_available = %v", envelope.Data["update_available"])
	}
	if envelope.Data["target_version"] != "1.2.0" {
		t.Fatalf("target_version = %v", envelope.Data["target_version"])
	}
	if envelope.Data["rollback_release_id"] != release.ID ||
		envelope.Data["rollback_of_deployment_id"] != deployment.RollbackOfDeploymentID {
		t.Fatalf("rollback ids = %#v", envelope.Data)
	}
	releaseResponse, ok := envelope.Data["release"].(map[string]any)
	if !ok || releaseResponse["release_id"] != release.ID {
		t.Fatalf("release = %#v", envelope.Data["release"])
	}
}

func TestDeviceEventAcceptsFirmwarePayloadAndCompletesRollback(t *testing.T) {
	t.Parallel()
	handler, repository := newDeviceEventHandlerForTest(t)

	started := postDeviceEvent(handler, deviceEventPayload("rollback_started", 5))
	if started.Code != http.StatusOK {
		t.Fatalf(
			"rollback_started status = %d, want 200: %s",
			started.Code,
			started.Body.String(),
		)
	}
	if repository.deployment.Status != domain.DeploymentStatusInstalling {
		t.Fatalf(
			"rollback_started status = %q, want %q",
			repository.deployment.Status,
			domain.DeploymentStatusInstalling,
		)
	}
	if repository.deployment.Status == domain.DeploymentStatusRolledBack {
		t.Fatal("rollback_started must remain non-terminal")
	}

	rolledBack := postDeviceEvent(handler, deviceEventPayload("rolled_back", 6))
	if rolledBack.Code != http.StatusOK {
		t.Fatalf(
			"rolled_back status = %d, want 200: %s",
			rolledBack.Code,
			rolledBack.Body.String(),
		)
	}
	if repository.deployment.Status != domain.DeploymentStatusRolledBack {
		t.Fatalf(
			"rolled_back status = %q, want %q",
			repository.deployment.Status,
			domain.DeploymentStatusRolledBack,
		)
	}
	afterTerminal := postDeviceEvent(handler, deviceEventPayload("started", 7))
	if afterTerminal.Code != http.StatusConflict {
		t.Fatalf(
			"event after rolled_back status = %d, want 409: %s",
			afterTerminal.Code,
			afterTerminal.Body.String(),
		)
	}
}

func TestDeviceUpdateResponseIncludesSchemaVersion(t *testing.T) {
	t.Parallel()
	handler, _ := newDeviceEventHandlerForTest(t)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/ota",
		nil,
	)
	request.SetPathValue("device_id", "device-1")
	request.Header.Set("Authorization", "Bearer device-session")
	response := httptest.NewRecorder()
	handler.deviceUpdate(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data["schema_version"] != "1.0.0" {
		t.Fatalf(
			"schema_version = %v, want 1.0.0",
			envelope.Data["schema_version"],
		)
	}
}

func TestDeviceEventReplayIsIdempotent(t *testing.T) {
	t.Parallel()
	handler, _ := newDeviceEventHandlerForTest(t)
	body := deviceEventPayload("rollback_started", 5)
	if response := postDeviceEvent(handler, body); response.Code != http.StatusOK {
		t.Fatalf("first status = %d: %s", response.Code, response.Body.String())
	}
	response := postDeviceEvent(handler, body)
	if response.Code != http.StatusOK {
		t.Fatalf("second status = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Accepted  bool `json:"accepted"`
			Duplicate bool `json:"duplicate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Accepted || !envelope.Data.Duplicate {
		t.Fatalf("response data = %+v, want accepted duplicate", envelope.Data)
	}
}

func TestDeviceEventRejectsMissingRequiredFieldsAndUnknownFields(t *testing.T) {
	t.Parallel()
	handler, _ := newDeviceEventHandlerForTest(t)
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing deployment",
			body: `{"schema_version":"1.0.0","event_id":"ota_evt_missing","release_id":"ota_release_001","event_type":"started","sequence":5,"progress_percent":1,"bytes_received":0,"bytes_total":1024,"occurred_at":"2026-10-18T12:00:00Z","firmware_version":"0.24.0"}`,
		},
		{
			name: "missing release",
			body: `{"schema_version":"1.0.0","event_id":"ota_evt_missing","deployment_id":"ota_deployment_001","event_type":"started","sequence":5,"progress_percent":1,"bytes_received":0,"bytes_total":1024,"occurred_at":"2026-10-18T12:00:00Z","firmware_version":"0.24.0"}`,
		},
		{
			name: "missing sequence",
			body: `{"schema_version":"1.0.0","event_id":"ota_evt_missing","deployment_id":"ota_deployment_001","release_id":"ota_release_001","event_type":"started","progress_percent":1,"bytes_received":0,"bytes_total":1024,"occurred_at":"2026-10-18T12:00:00Z","firmware_version":"0.24.0"}`,
		},
		{
			name: "unknown field",
			body: `{"schema_version":"1.0.0","event_id":"ota_evt_unknown","deployment_id":"ota_deployment_001","release_id":"ota_release_001","event_type":"started","sequence":5,"progress_percent":1,"bytes_received":0,"bytes_total":1024,"occurred_at":"2026-10-18T12:00:00Z","firmware_version":"0.24.0","unexpected":true}`,
		},
		{
			name: "invalid firmware version",
			body: `{"schema_version":"1.0.0","event_id":"ota_evt_invalid_version","deployment_id":"ota_deployment_001","release_id":"ota_release_001","event_type":"started","sequence":5,"progress_percent":1,"bytes_received":0,"bytes_total":1024,"occurred_at":"2026-10-18T12:00:00Z","firmware_version":"latest"}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := postDeviceEvent(handler, test.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf(
					"status = %d, want 400: %s",
					response.Code,
					response.Body.String(),
				)
			}
		})
	}
}

func TestDeviceEventRejectsUnknownEventType(t *testing.T) {
	t.Parallel()
	handler, _ := newDeviceEventHandlerForTest(t)
	response := postDeviceEvent(handler, deviceEventPayload("download_started", 5))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body.String())
	}
}

func TestOTAReleaseMutationRequiresCanonicalReleaseUUID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		releaseID string
		wantErr   bool
	}{
		{name: "empty draft id", wantErr: true},
		{name: "canonical", releaseID: "00000000-0000-4000-8000-000000000001"},
		{name: "uppercase", releaseID: "00000000-0000-4000-8000-00000000000A", wantErr: true},
		{name: "not uuid", releaseID: "release_1", wantErr: true},
		{name: "nil uuid", releaseID: "00000000-0000-0000-0000-000000000000", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateOTAReleaseMutation(otaReleaseMutation{
				ReleaseID:               test.releaseID,
				SignaturePayloadVersion: domain.OTAReleaseSignaturePayloadVersion,
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("validateOTAReleaseMutation() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

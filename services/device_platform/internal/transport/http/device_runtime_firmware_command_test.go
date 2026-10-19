package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	runtimeDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	runtimeRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/repository"
	runtimeService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	otaDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
)

const firmwareCommandReleaseID = "00000000-0000-4000-8000-000000000001"

type firmwareCommandRepository struct {
	runtimeRepository.Repository
	command     runtimeDomain.Command
	createError error
}

func (repository *firmwareCommandRepository) CreateCommand(
	_ context.Context,
	command *runtimeDomain.Command,
) error {
	if repository.createError != nil {
		return repository.createError
	}
	repository.command = *command
	return nil
}

type firmwareCommandBindingReader struct{}

func (firmwareCommandBindingReader) VerifyDeviceSession(
	context.Context,
	string,
) (string, error) {
	return "device_test_001", nil
}

func (firmwareCommandBindingReader) GetByDeviceID(
	context.Context,
	string,
) (*bindingdomain.Binding, error) {
	return nil, bindingdomain.ErrDeviceNotFound
}

func (firmwareCommandBindingReader) List(
	context.Context,
	string,
) ([]bindingdomain.Binding, error) {
	return nil, nil
}

func (firmwareCommandBindingReader) ListAllForAdmin(
	context.Context,
) ([]bindingdomain.Binding, error) {
	return nil, nil
}

type firmwareCommandAuthorizer struct {
	releaseID string
	deviceID  string
	err       error
}

func (authorizer *firmwareCommandAuthorizer) AuthorizeRuntimeFirmwareUpdate(
	_ context.Context,
	deviceID string,
	releaseID string,
) (*runtimeService.FirmwareUpdateAuthorization, error) {
	authorizer.deviceID = deviceID
	authorizer.releaseID = releaseID
	if authorizer.err != nil {
		return nil, authorizer.err
	}
	return &runtimeService.FirmwareUpdateAuthorization{
		ReleaseID:       releaseID,
		FirmwareVersion: "0.14.0",
		DeploymentID:    "00000000-0000-4000-8000-000000000002",
		DeploymentKind:  "install",
	}, nil
}

func newFirmwareCommandHandler(
	t *testing.T,
	authorizer runtimeService.FirmwareUpdateAuthorizer,
) (deviceRuntimeHandler, *firmwareCommandRepository) {
	t.Helper()
	repository := &firmwareCommandRepository{}
	service, err := runtimeService.New(runtimeService.Options{
		Repository:               repository,
		BindingService:           firmwareCommandBindingReader{},
		FirmwareUpdateAuthorizer: authorizer,
		Clock:                    firmwareFixedClock{now: time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}
	return deviceRuntimeHandler{service: service}, repository
}

type firmwareFixedClock struct {
	now time.Time
}

func (clock firmwareFixedClock) Now() time.Time {
	return clock.now
}

func firmwareCommandRequest(body string) *http.Request {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/devices/device_test_001/commands",
		strings.NewReader(body),
	)
	request.SetPathValue("device_id", "device_test_001")
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"admin_test_001",
	))
	return request
}

func TestCreateFirmwareUpdateCommandRequiresAuthorizedRelease(t *testing.T) {
	t.Parallel()
	authorizer := &firmwareCommandAuthorizer{}
	handler, repository := newFirmwareCommandHandler(t, authorizer)
	response := httptest.NewRecorder()
	handler.createCommand(
		response,
		firmwareCommandRequest(`{"command_type":"firmware_update","release_id":"`+firmwareCommandReleaseID+`"}`),
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if authorizer.deviceID != "device_test_001" ||
		authorizer.releaseID != firmwareCommandReleaseID {
		t.Fatalf("unexpected OTA authorization call: %#v", authorizer)
	}
	if repository.command.Type != runtimeDomain.CommandFirmwareUpdate ||
		repository.command.Payload["release_id"] != firmwareCommandReleaseID {
		t.Fatalf("unexpected persisted command: %#v", repository.command)
	}

	var envelope struct {
		Data struct {
			CommandID   string `json:"command_id"`
			CommandType string `json:"command_type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.CommandType != string(runtimeDomain.CommandFirmwareUpdate) {
		t.Fatalf("unexpected response data: %#v", envelope.Data)
	}
}

func TestCreateFirmwareUpdateCommandRejectsBadPayloadBeforePersistence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       string
		authorizer runtimeService.FirmwareUpdateAuthorizer
		status     int
		code       string
	}{
		{
			name:       "missing release",
			body:       `{"command_type":"firmware_update"}`,
			authorizer: &firmwareCommandAuthorizer{},
			status:     http.StatusUnprocessableEntity,
			code:       "invalid_device_command",
		},
		{
			name:       "unknown version field",
			body:       `{"command_type":"firmware_update","release_id":"` + firmwareCommandReleaseID + `","firmware_version":"9.9.9"}`,
			authorizer: &firmwareCommandAuthorizer{},
			status:     http.StatusBadRequest,
			code:       "invalid_request",
		},
		{
			name:       "release for another device",
			body:       `{"command_type":"firmware_update","release_id":"` + firmwareCommandReleaseID + `"}`,
			authorizer: &firmwareCommandAuthorizer{err: otaDomain.ErrDeploymentNotFound},
			status:     http.StatusNotFound,
			code:       "firmware_deployment_not_found",
		},
		{
			name:       "incompatible device",
			body:       `{"command_type":"firmware_update","release_id":"` + firmwareCommandReleaseID + `"}`,
			authorizer: &firmwareCommandAuthorizer{err: otaDomain.ErrDeviceNotEligible},
			status:     http.StatusForbidden,
			code:       "firmware_update_not_authorized",
		},
		{
			name:       "rollback not allowed",
			body:       `{"command_type":"firmware_update","release_id":"` + firmwareCommandReleaseID + `"}`,
			authorizer: &firmwareCommandAuthorizer{err: otaDomain.ErrRollbackNotAllowed},
			status:     http.StatusForbidden,
			code:       "firmware_rollback_not_authorized",
		},
		{
			name:       "deployment already ended",
			body:       `{"command_type":"firmware_update","release_id":"` + firmwareCommandReleaseID + `"}`,
			authorizer: &firmwareCommandAuthorizer{err: otaDomain.ErrDeploymentStateConflict},
			status:     http.StatusConflict,
			code:       "firmware_update_state_conflict",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler, repository := newFirmwareCommandHandler(t, test.authorizer)
			response := httptest.NewRecorder()
			handler.createCommand(response, firmwareCommandRequest(test.body))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d, body=%s", response.Code, test.status, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"`+test.code+`"`) {
				t.Fatalf("response body missing error code %q: %s", test.code, response.Body.String())
			}
			if repository.command.ID != "" {
				t.Fatal("invalid firmware command must not be persisted")
			}
		})
	}
}

func TestDeviceCommandResponseProjectsReleaseIDTopLevel(t *testing.T) {
	t.Parallel()
	response := deviceCommandResponse(&runtimeDomain.Command{
		ID:       "00000000-0000-4000-8000-000000000003",
		DeviceID: "device_test_001",
		Type:     runtimeDomain.CommandFirmwareUpdate,
		Status:   runtimeDomain.CommandStatusPending,
		Payload: map[string]any{
			"release_id": firmwareCommandReleaseID,
		},
	})
	if response["release_id"] != firmwareCommandReleaseID {
		t.Fatalf("top-level release_id = %#v", response["release_id"])
	}
	payload, ok := response["payload"].(map[string]any)
	if !ok || payload["release_id"] != firmwareCommandReleaseID {
		t.Fatalf("nested payload = %#v", response["payload"])
	}
}

func TestCreateNonFirmwareCommandRejectsReleaseID(t *testing.T) {
	t.Parallel()
	handler, repository := newFirmwareCommandHandler(t, &firmwareCommandAuthorizer{})
	response := httptest.NewRecorder()
	handler.createCommand(
		response,
		firmwareCommandRequest(`{"command_type":"resync_time","release_id":"`+firmwareCommandReleaseID+`"}`),
	)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if repository.command.ID != "" {
		t.Fatal("unexpected command persisted")
	}
}

func TestCreateFirmwareUpdateWithoutOtaAuthorityFailsClosed(t *testing.T) {
	t.Parallel()
	handler, repository := newFirmwareCommandHandler(t, nil)
	response := httptest.NewRecorder()
	handler.createCommand(
		response,
		firmwareCommandRequest(`{"command_type":"firmware_update","release_id":"`+firmwareCommandReleaseID+`"}`),
	)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if repository.command.ID != "" {
		t.Fatal("command must not be persisted without OTA authority")
	}
}

func TestCreateFirmwareUpdateDuplicateActiveCommandReturnsConflict(t *testing.T) {
	t.Parallel()
	handler, _ := newFirmwareCommandHandler(t, &firmwareCommandAuthorizer{})
	repository := &firmwareCommandRepository{
		createError: runtimeDomain.ErrFirmwareUpdateAlreadyQueued,
	}
	service, err := runtimeService.New(runtimeService.Options{
		Repository:               repository,
		BindingService:           firmwareCommandBindingReader{},
		FirmwareUpdateAuthorizer: &firmwareCommandAuthorizer{},
		Clock:                    firmwareFixedClock{now: time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}
	handler.service = service
	response := httptest.NewRecorder()
	handler.createCommand(
		response,
		firmwareCommandRequest(`{"command_type":"firmware_update","release_id":"`+firmwareCommandReleaseID+`"}`),
	)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"firmware_update_already_queued"`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

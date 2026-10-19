package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

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

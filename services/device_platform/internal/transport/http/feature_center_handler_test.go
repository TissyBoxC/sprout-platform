package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	featuredomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
)

type featureCenterTestService struct {
	features []featuredomain.Feature
	err      error
	updates  []featureCenterUpdate
}

type featureCenterUpdate struct {
	FeatureID string
	Values    map[string]any
	ActorID   string
	Version   int64
}

func (service *featureCenterTestService) List(
	_ context.Context,
) ([]featuredomain.Feature, error) {
	if service.err != nil {
		return nil, service.err
	}
	return service.features, nil
}

func (service *featureCenterTestService) Get(
	_ context.Context,
	featureID string,
) (*featuredomain.Feature, error) {
	if service.err != nil {
		return nil, service.err
	}
	for index := range service.features {
		if service.features[index].ID == featureID {
			return &service.features[index], nil
		}
	}
	return nil, featuredomain.ErrFeatureNotFound
}

func (service *featureCenterTestService) UpdateConfig(
	_ context.Context,
	featureID string,
	values map[string]any,
	actorID string,
	expectedVersion int64,
) (*featuredomain.Feature, error) {
	if service.err != nil {
		return nil, service.err
	}
	service.updates = append(service.updates, featureCenterUpdate{
		FeatureID: featureID,
		Values:    values,
		ActorID:   actorID,
		Version:   expectedVersion,
	})
	current, err := service.Get(context.Background(), featureID)
	if err != nil {
		return nil, err
	}
	return current, nil
}

func (service *featureCenterTestService) CheckHealth(
	_ context.Context,
	featureID string,
) (*featuredomain.Feature, error) {
	if service.err != nil {
		return nil, service.err
	}
	current, err := service.Get(context.Background(), featureID)
	if err != nil {
		return nil, err
	}
	current.Health = featuredomain.HealthHealthy
	return current, nil
}

func TestFeatureCenterListUsesSuccessEnvelope(t *testing.T) {
	service := &featureCenterTestService{
		features: []featuredomain.Feature{{
			ID:     "voice_gateway",
			Name:   "语音网关",
			Health: featuredomain.HealthUnknown,
		}},
	}
	handler := NewFeatureCenterHandler(service)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/feature-center",
		nil,
	)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Data          struct {
			Features []featuredomain.Feature `json:"features"`
		} `json:"data"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.SchemaVersion != httpapi.SchemaVersion ||
		len(envelope.Data.Features) != 1 ||
		envelope.Error != nil {
		t.Fatalf("unexpected envelope: %s", recorder.Body.String())
	}
}

func TestFeatureCenterUpdateConfigUsesAuthenticatedActorAndVersion(t *testing.T) {
	service := &featureCenterTestService{
		features: []featuredomain.Feature{{
			ID:            "voice_gateway",
			ConfigVersion: 3,
		}},
	}
	handler := NewFeatureCenterHandler(service)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/feature-center/voice_gateway/config",
		strings.NewReader(`{
			"values":{"enabled":true},
			"expected_version":3
		}`),
	)
	request.SetPathValue("feature", "voice_gateway")
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"admin-001",
	))
	recorder := httptest.NewRecorder()
	handler.UpdateConfig(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if len(service.updates) != 1 ||
		service.updates[0].ActorID != "admin-001" ||
		service.updates[0].Version != 3 ||
		service.updates[0].Values["enabled"] != true {
		t.Fatalf("unexpected delegated update: %+v", service.updates)
	}
}

func TestFeatureCenterUpdateConfigMapsVersionConflict(t *testing.T) {
	service := &featureCenterTestService{
		err: &featuredomain.VersionConflictError{
			ExpectedVersion: 1,
			CurrentVersion:  2,
		},
	}
	handler := NewFeatureCenterHandler(service)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/feature-center/voice_gateway/config",
		strings.NewReader(`{"values":{},"expected_version":1}`),
	)
	request.SetPathValue("feature", "voice_gateway")
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"admin-001",
	))
	recorder := httptest.NewRecorder()
	handler.UpdateConfig(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Error *httpapi.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error == nil ||
		envelope.Error.Code != "config_version_conflict" {
		t.Fatalf("unexpected error envelope: %s", recorder.Body.String())
	}
}

func TestFeatureCenterGetExposesSecretConfiguredWithoutPlaintext(t *testing.T) {
	service := &featureCenterTestService{
		features: []featuredomain.Feature{{
			ID: "voice_gateway",
			Values: map[string]any{
				"session_secret": map[string]any{
					"configured": true,
					"mask":       "********cret",
				},
			},
		}},
	}
	handler := NewFeatureCenterHandler(service)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/feature-center/voice_gateway",
		nil,
	)
	request.SetPathValue("feature", "voice_gateway")
	recorder := httptest.NewRecorder()
	handler.Get(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Feature struct {
				SecretConfigured map[string]bool `json:"secret_configured"`
			} `json:"feature"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Feature.SecretConfigured["session_secret"] {
		t.Fatalf("secret configuration missing: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "plain-secret-value") {
		t.Fatalf("secret plaintext leaked: %s", recorder.Body.String())
	}
}

func TestFeatureCenterErrorsDoNotExposeInternalMessages(t *testing.T) {
	service := &featureCenterTestService{
		err: errors.New("database password leaked"),
	}
	handler := NewFeatureCenterHandler(service)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/feature-center/voice_gateway",
		nil,
	)
	request.SetPathValue("feature", "voice_gateway")
	recorder := httptest.NewRecorder()
	handler.Get(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "database password") {
		t.Fatalf("internal error leaked: %s", recorder.Body.String())
	}
}

func TestFeatureCenterHealthCheckUnavailableReturnsStableCode(t *testing.T) {
	service := &featureCenterTestService{
		err: featuredomain.ErrHealthCheckUnavailable,
	}
	handler := NewFeatureCenterHandler(service)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/feature-center/notification/health-check",
		nil,
	)
	request.SetPathValue("feature", "notification")
	recorder := httptest.NewRecorder()
	handler.HealthCheck(recorder, request)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Error *httpapi.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error == nil ||
		envelope.Error.Code != "health_check_unavailable" {
		t.Fatalf("unexpected error envelope: %s", recorder.Body.String())
	}
}

func TestFeatureCenterRegisterAdminRoutesUsesSuppliedMiddleware(t *testing.T) {
	handler := NewFeatureCenterHandler(&featureCenterTestService{})
	mux := http.NewServeMux()
	middlewareCalled := false
	handler.RegisterAdminRoutes(mux, func(next http.HandlerFunc) http.HandlerFunc {
		return func(response http.ResponseWriter, request *http.Request) {
			middlewareCalled = true
			next(response, request)
		}
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/feature-center",
		nil,
	)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if !middlewareCalled {
		t.Fatal("admin middleware was not used")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

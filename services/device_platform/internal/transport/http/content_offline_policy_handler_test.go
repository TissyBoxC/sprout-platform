package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	contentofflinepolicydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/domain"
)

// contentOfflinePolicyTestService is an in-memory double so the handler tests
// exercise the real policy validation without a database.
type contentOfflinePolicyTestService struct {
	policy   contentofflinepolicydomain.Policy
	revision int
}

func (s *contentOfflinePolicyTestService) Current(
	_ context.Context,
) (contentofflinepolicydomain.Policy, error) {
	if s.policy.Revision == 0 && s.revision == 0 {
		return contentofflinepolicydomain.Default(), nil
	}
	policy := s.policy
	policy.Revision = s.revision
	return policy, nil
}

func (s *contentOfflinePolicyTestService) Publish(
	_ context.Context,
	input contentofflinepolicydomain.Input,
) (contentofflinepolicydomain.Policy, error) {
	normalized, err := contentofflinepolicydomain.ValidateInput(input)
	if err != nil {
		return contentofflinepolicydomain.Policy{}, err
	}
	s.revision += 1
	s.policy = contentofflinepolicydomain.Policy{
		OfflineEnabled:       normalized.OfflineEnabled,
		MaxCachedPackages:    normalized.MaxCachedPackages,
		MaxCacheBytes:        normalized.MaxCacheBytes,
		CacheTTLHours:        normalized.CacheTTLHours,
		DownloadOverWifiOnly: normalized.DownloadOverWifiOnly,
		Revision:             s.revision,
	}
	return s.policy, nil
}

func newContentOfflinePolicyHandlerForTest() contentOfflinePolicyHandler {
	return contentOfflinePolicyHandler{
		service: &contentOfflinePolicyTestService{},
	}
}

func TestContentOfflinePolicyCurrentRequiresAuthentication(t *testing.T) {
	handler := newContentOfflinePolicyHandlerForTest()
	request := englishRequest(
		http.MethodGet,
		"/api/v1/content/offline-policy",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestContentOfflinePolicyCurrentReturnsDefaultWhenUnset(t *testing.T) {
	handler := newContentOfflinePolicyHandlerForTest()
	request := englishRequest(
		http.MethodGet,
		"/api/v1/content/offline-policy",
		"",
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.current(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	payload := decodePolicyPayload(t, recorder.Body.Bytes())
	if payload.MaxCachedPackages != contentofflinepolicydomain.DefaultCachedPackages {
		t.Fatalf("max_cached_packages = %d, want %d",
			payload.MaxCachedPackages,
			contentofflinepolicydomain.DefaultCachedPackages)
	}
}

func TestContentOfflinePolicyPublishAndReadBack(t *testing.T) {
	handler := newContentOfflinePolicyHandlerForTest()
	body := `{"offline_enabled":true,"max_cached_packages":30,"max_cache_bytes":268435456,"cache_ttl_hours":48,"download_over_wifi_only":false}`
	publish := englishRequest(
		http.MethodPut,
		"/api/v1/content/offline-policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, publish)
	if recorder.Code != http.StatusOK {
		t.Fatalf("publish status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	published := decodePolicyPayload(t, recorder.Body.Bytes())
	if published.MaxCachedPackages != 30 || published.CacheTTLHours != 48 {
		t.Fatalf("unexpected published policy: %+v", published)
	}
	if published.Revision != 1 {
		t.Fatalf("revision = %d, want 1", published.Revision)
	}
	if published.DownloadOverWifiOnly {
		t.Fatal("download_over_wifi_only should have been published as false")
	}

	read := englishRequest(
		http.MethodGet,
		"/api/v1/content/offline-policy",
		"",
		"family-1",
	)
	recorder = httptest.NewRecorder()
	handler.current(recorder, read)
	if recorder.Code != http.StatusOK {
		t.Fatalf("read status = %d, want %d", recorder.Code, http.StatusOK)
	}
	current := decodePolicyPayload(t, recorder.Body.Bytes())
	if current.MaxCacheBytes != 268435456 {
		t.Fatalf("max_cache_bytes = %d, want 268435456", current.MaxCacheBytes)
	}
}

func TestContentOfflinePolicyPublishRejectsOutOfRange(t *testing.T) {
	handler := newContentOfflinePolicyHandlerForTest()
	body := `{"offline_enabled":true,"max_cached_packages":9999,"max_cache_bytes":1024,"cache_ttl_hours":0,"download_over_wifi_only":true}`
	request := englishRequest(
		http.MethodPut,
		"/api/v1/content/offline-policy",
		body,
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.publish(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
}

type policyPayload struct {
	OfflineEnabled       bool  `json:"offline_enabled"`
	MaxCachedPackages    int   `json:"max_cached_packages"`
	MaxCacheBytes        int64 `json:"max_cache_bytes"`
	CacheTTLHours        int   `json:"cache_ttl_hours"`
	DownloadOverWifiOnly bool  `json:"download_over_wifi_only"`
	Revision             int   `json:"revision"`
}

func decodePolicyPayload(t *testing.T, body []byte) policyPayload {
	t.Helper()
	var envelope struct {
		Data policyPayload `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode response: %v (%s)", err, string(body))
	}
	return envelope.Data
}

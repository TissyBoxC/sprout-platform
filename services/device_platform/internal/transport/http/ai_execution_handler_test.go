package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/packages/go/observability"
	gatewaydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
	aiexecution "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/execution"
	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
)

type fakeAIExecutionStore struct {
	mu         sync.Mutex
	ticket     aiexecution.Ticket
	consumeErr error
	issueErr   error
	leaseErr   error
	releases   int
}

func (store *fakeAIExecutionStore) Issue(
	_ context.Context,
	ticket aiexecution.Ticket,
	_ time.Duration,
) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.issueErr != nil {
		return "", store.issueErr
	}
	store.ticket = ticket
	return "ticket-token", nil
}

func (store *fakeAIExecutionStore) Consume(
	_ context.Context,
	_ string,
) (aiexecution.Ticket, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.consumeErr != nil {
		return aiexecution.Ticket{}, store.consumeErr
	}
	return store.ticket, nil
}

func (store *fakeAIExecutionStore) AcquireDeviceLease(
	_ context.Context,
	_ string,
	_ int,
	_ time.Duration,
) (string, func(), error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.leaseErr != nil {
		return "", nil, store.leaseErr
	}
	return "lease-token", func() {
		store.mu.Lock()
		defer store.mu.Unlock()
		store.releases++
	}, nil
}

type fakeAIExecutionAuthorizer struct {
	credential *gatewaydomain.Credential
	err        error
	parentID   *string
}

func (authorizer fakeAIExecutionAuthorizer) AuthorizeExecution(
	_ context.Context,
	parentAccountID string,
	_ string,
) (*gatewaydomain.Credential, error) {
	if authorizer.parentID != nil {
		*authorizer.parentID = parentAccountID
	}
	return authorizer.credential, authorizer.err
}

var _ aiExecutionAuthorizer = fakeAIExecutionAuthorizer{}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return roundTrip(request)
}

type fakeBindingRepository struct {
	binding *bindingdomain.Binding
}

func (repository fakeBindingRepository) CreateToken(
	context.Context,
	*bindingdomain.BindingToken,
) error {
	return nil
}

func (repository fakeBindingRepository) GetTokenByHash(
	context.Context,
	string,
) (*bindingdomain.BindingToken, error) {
	return nil, bindingdomain.ErrTokenNotFound
}

func (repository fakeBindingRepository) UpsertBinding(
	context.Context,
	*bindingdomain.Binding,
) error {
	return nil
}

func (repository fakeBindingRepository) ListByParentAccountID(
	context.Context,
	string,
) ([]bindingdomain.Binding, error) {
	return nil, nil
}

func (repository fakeBindingRepository) ListAllBindings(
	context.Context,
) ([]bindingdomain.Binding, error) {
	return nil, nil
}

func (repository fakeBindingRepository) GetByDeviceID(
	context.Context,
	string,
) (*bindingdomain.Binding, error) {
	return repository.binding, nil
}

func (repository fakeBindingRepository) Delete(
	context.Context,
	string,
	string,
) error {
	return nil
}

func (repository fakeBindingRepository) RevokeDeviceSessionsAndDeleteBinding(
	context.Context,
	string,
	string,
) error {
	return nil
}

func (repository fakeBindingRepository) RevokeDeviceSessions(
	context.Context,
	string,
	bool,
) error {
	return nil
}

func (repository fakeBindingRepository) CreateRegistrationToken(
	context.Context,
	*bindingdomain.RegistrationToken,
) error {
	return nil
}

func (repository fakeBindingRepository) GetRegistrationTokenByHash(
	context.Context,
	string,
) (*bindingdomain.RegistrationToken, error) {
	return nil, bindingdomain.ErrRegistrationTokenNotFound
}

func (repository fakeBindingRepository) ConsumeRegistrationTokenAndUpsertCredential(
	context.Context,
	string,
	*bindingdomain.DeviceCredential,
) error {
	return nil
}

func (repository fakeBindingRepository) GetDeviceCredential(
	context.Context,
	string,
) (*bindingdomain.DeviceCredential, error) {
	return nil, bindingdomain.ErrDeviceNotFound
}

func (repository fakeBindingRepository) CreateDeviceChallenge(
	context.Context,
	*bindingdomain.DeviceChallenge,
) error {
	return nil
}

func (repository fakeBindingRepository) GetDeviceChallengeByNonceHash(
	context.Context,
	string,
) (*bindingdomain.DeviceChallenge, error) {
	return nil, bindingdomain.ErrDeviceChallengeNotFound
}

func (repository fakeBindingRepository) CompleteDeviceAuthentication(
	context.Context,
	string,
	string,
	*bindingdomain.DeviceSession,
) error {
	return nil
}

func (repository fakeBindingRepository) GetDeviceSessionByTokenHash(
	context.Context,
	string,
) (*bindingdomain.DeviceSession, error) {
	return nil, bindingdomain.ErrDeviceSessionNotFound
}

func (repository fakeBindingRepository) ConsumeTokenAndUpsertBinding(
	context.Context,
	string,
	*bindingdomain.Binding,
) error {
	return nil
}

func newAIExecutionBindingService(t *testing.T) *bindingservice.Service {
	return newAIExecutionBindingServiceForParent(t, "parent-account-0001")
}

func newAIExecutionBindingServiceForParent(
	t *testing.T,
	parentAccountID string,
) *bindingservice.Service {
	t.Helper()
	bindingService, err := bindingservice.New(bindingservice.Options{
		Repository: fakeBindingRepository{
			binding: &bindingdomain.Binding{
				DeviceID:        "device-0001",
				ParentAccountID: parentAccountID,
			},
		},
		ProofVerifier: fakeProofVerifier{},
		TokenTTL:      time.Minute,
	})
	if err != nil {
		t.Fatalf("create binding service: %v", err)
	}
	return bindingService
}

type fakeProofVerifier struct{}

func (fakeProofVerifier) ValidatePublicKey(string) error {
	return nil
}

func (fakeProofVerifier) Verify(string, []byte, []byte) error {
	return nil
}

func newAIExecutionTestHandler(
	t *testing.T,
	upstream http.Handler,
) (internalHandler, *fakeAIExecutionStore) {
	t.Helper()
	store := &fakeAIExecutionStore{
		ticket: aiexecution.Ticket{
			DeviceID:        "device-0001",
			ParentAccountID: "parent-account-0001",
			SessionID:       "session-0001",
			Project:         "voice",
			Model:           "model-a",
			Concurrency:     2,
		},
	}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	return internalHandler{
		bindingService: newAIExecutionBindingService(t),
		aiService: fakeAIExecutionAuthorizer{
			credential: &gatewaydomain.Credential{
				APIKey: "upstream-secret-key",
			},
		},
		executionStore:    store,
		executionLeaseTTL: time.Minute,
		aiUpstreamURL:     server.URL,
		aiHTTPClient:      server.Client(),
	}, store
}

func TestAIExecutionStreamsSSEAndOverridesUntrustedHeaders(t *testing.T) {
	var gotRequest *http.Request
	handler, store := newAIExecutionTestHandler(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		gotRequest = request.Clone(context.Background())
		gotBody, _ := io.ReadAll(request.Body)
		gotRequest.Body = io.NopCloser(bytes.NewReader(gotBody))
		response.Header().Set("Content-Type", "text/event-stream")
		response.Header().Set("Cache-Control", "no-cache")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("data: first\n\n"))
		response.(http.Flusher).Flush()
		_, _ = response.Write([]byte("data: [DONE]\n\n"))
	}))
	defer closeAIExecutionTestHandler(handler)

	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/ai-execution?ticket=ticket-token",
		strings.NewReader(`{"model":"model-a","stream":true,"messages":[{"role":"user","content":"你好"}]}`),
	)
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-Sprout-Tenant-ID", "attacker-tenant")
	request.Header.Set("X-Sprout-Device-ID", "attacker-device")
	request.Header.Set("X-Sprout-Session-ID", "attacker-session")
	request.Header.Set("X-Sprout-Project", "vision")
	request.Header.Set(observability.PolicyVersionHeader, "age_3_4")
	recorder := httptest.NewRecorder()

	handler.requireHTTPSUpstream = false
	handler.aiExecution(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Body.String(); got != "data: first\n\ndata: [DONE]\n\n" {
		t.Fatalf("stream body = %q", got)
	}
	if gotRequest.Header.Get("Authorization") != "Bearer upstream-secret-key" {
		t.Fatalf("authorization = %q", gotRequest.Header.Get("Authorization"))
	}
	if gotRequest.Header.Get(observability.TenantIDHeader) != "parent-account-0001" {
		t.Fatalf("tenant header = %q", gotRequest.Header.Get(observability.TenantIDHeader))
	}
	if gotRequest.Header.Get(observability.DeviceIDHeader) != "device-0001" {
		t.Fatalf("device header = %q", gotRequest.Header.Get(observability.DeviceIDHeader))
	}
	if gotRequest.Header.Get(observability.SessionIDHeader) != "session-0001" {
		t.Fatalf("session header = %q", gotRequest.Header.Get(observability.SessionIDHeader))
	}
	if gotRequest.Header.Get("X-Sprout-Project") != "voice" {
		t.Fatalf("project header = %q", gotRequest.Header.Get("X-Sprout-Project"))
	}
	if gotRequest.Header.Get(observability.PolicyVersionHeader) != "age_3_4" {
		t.Fatalf("policy header = %q", gotRequest.Header.Get(observability.PolicyVersionHeader))
	}
	store.mu.Lock()
	releases := store.releases
	store.mu.Unlock()
	if releases != 1 {
		t.Fatalf("lease releases = %d, want 1", releases)
	}
}

func TestAIExecutionRejectsModelMismatchWithoutCallingUpstream(t *testing.T) {
	called := false
	handler, store := newAIExecutionTestHandler(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		called = true
		response.WriteHeader(http.StatusOK)
	}))
	defer closeAIExecutionTestHandler(handler)

	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/ai-execution?ticket=ticket-token",
		strings.NewReader(`{"model":"model-b","stream":true,"messages":[{"role":"user","content":"你好"}]}`),
	)
	recorder := httptest.NewRecorder()
	handler.requireHTTPSUpstream = false
	handler.aiExecution(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if called {
		t.Fatal("model mismatch must not call the upstream provider")
	}
	store.mu.Lock()
	releases := store.releases
	store.mu.Unlock()
	if releases != 0 {
		t.Fatalf("lease releases = %d, want 0 before acquisition", releases)
	}
}

func TestAIExecutionRejectsExpiredTicket(t *testing.T) {
	handler, store := newAIExecutionTestHandler(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.WriteHeader(http.StatusOK)
	}))
	defer closeAIExecutionTestHandler(handler)
	store.mu.Lock()
	store.consumeErr = aiexecution.ErrTicketInvalid
	store.mu.Unlock()

	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/ai-execution?ticket=spent",
		strings.NewReader(`{"model":"model-a","stream":true,"messages":[{"role":"user","content":"你好"}]}`),
	)
	recorder := httptest.NewRecorder()
	handler.requireHTTPSUpstream = false
	handler.aiExecution(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "invalid_execution_ticket") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestAIExecutionRejectsTicketAfterDeviceRebind(t *testing.T) {
	called := false
	handler, _ := newAIExecutionTestHandler(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		called = true
		response.WriteHeader(http.StatusOK)
	}))
	defer closeAIExecutionTestHandler(handler)
	handler.bindingService = newAIExecutionBindingServiceForParent(
		t,
		"parent-account-0002",
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/ai-execution?ticket=ticket-token",
		strings.NewReader(`{"model":"model-a","stream":true,"messages":[{"role":"user","content":"你好"}]}`),
	)
	recorder := httptest.NewRecorder()
	handler.requireHTTPSUpstream = false
	handler.aiExecution(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", recorder.Code, recorder.Body.String())
	}
	if called {
		t.Fatal("a rebound device must not reach the provider")
	}
}

func TestAIExecutionConvertsProviderErrorsWithoutLeakingBodyOrKey(t *testing.T) {
	handler, _ := newAIExecutionTestHandler(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"error":"upstream-secret-key"}`))
	}))
	defer closeAIExecutionTestHandler(handler)

	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/ai-execution?ticket=ticket-token",
		strings.NewReader(`{"model":"model-a","stream":true,"messages":[{"role":"user","content":"你好"}]}`),
	)
	recorder := httptest.NewRecorder()
	handler.requireHTTPSUpstream = false
	handler.aiExecution(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "upstream-secret-key") {
		t.Fatal("provider key leaked through the error response")
	}
	var envelope struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Error == nil ||
		envelope.Error.Code != "ai_credential_rejected" {
		t.Fatalf("unexpected error envelope: %s", recorder.Body.String())
	}
}

func TestAIExecutionReturns429WhenDeviceLeaseIsFull(t *testing.T) {
	handler, store := newAIExecutionTestHandler(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.WriteHeader(http.StatusOK)
	}))
	defer closeAIExecutionTestHandler(handler)
	store.mu.Lock()
	store.leaseErr = aiexecution.ErrTicketConcurrencyExceeded
	store.mu.Unlock()

	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/ai-execution?ticket=ticket-token",
		strings.NewReader(`{"model":"model-a","stream":true,"messages":[{"role":"user","content":"你好"}]}`),
	)
	recorder := httptest.NewRecorder()
	handler.requireHTTPSUpstream = false
	handler.aiExecution(recorder, request)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After for a concurrency rejection")
	}
}

func TestAIExecutionRejectsUnknownBodyFields(t *testing.T) {
	handler, _ := newAIExecutionTestHandler(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		response.WriteHeader(http.StatusOK)
	}))
	defer closeAIExecutionTestHandler(handler)

	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/ai-execution?ticket=ticket-token",
		strings.NewReader(`{"model":"model-a","stream":true,"messages":[{"role":"user","content":"你好"}],"tools":[{"name":"shell"}]`),
	)
	recorder := httptest.NewRecorder()
	handler.requireHTTPSUpstream = false
	handler.aiExecution(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

func closeAIExecutionTestHandler(handler internalHandler) {
	if handler.aiHTTPClient != nil {
		handler.aiHTTPClient.CloseIdleConnections()
	}
}

func TestAIUpstreamURLRequiresHTTPSScheme(t *testing.T) {
	if _, err := resolveUpstreamChatURL(
		"http://sub2api:8080",
		true,
	); err == nil {
		t.Fatal("production upstream accepted a cleartext HTTP URL")
	}
	parsed, err := resolveUpstreamChatURL(
		"https://sub2api:8443",
		true,
	)
	if err != nil {
		t.Fatalf("valid HTTPS upstream rejected: %v", err)
	}
	if parsed.Path != "/v1/chat/completions" {
		t.Fatalf("path = %q", parsed.Path)
	}
	if _, err := resolveUpstreamChatURL(
		"https://user:password@sub2api:8443",
		true,
	); err == nil {
		t.Fatal("upstream accepted embedded credentials")
	}
}

func TestAIExecutionReleasesLeaseWhenClientCancels(t *testing.T) {
	started := make(chan struct{})
	store := &fakeAIExecutionStore{
		ticket: aiexecution.Ticket{
			DeviceID:        "device-0001",
			ParentAccountID: "parent-account-0001",
			SessionID:       "session-0001",
			Project:         "voice",
			Model:           "model-a",
			Concurrency:     2,
		},
	}
	handler := internalHandler{
		bindingService: newAIExecutionBindingService(t),
		aiService: fakeAIExecutionAuthorizer{
			credential: &gatewaydomain.Credential{
				APIKey: "upstream-secret-key",
			},
		},
		executionStore:    store,
		executionLeaseTTL: time.Minute,
		aiUpstreamURL:     "http://sub2api.test",
		aiHTTPClient: &http.Client{
			Transport: roundTripFunc(func(
				request *http.Request,
			) (*http.Response, error) {
				close(started)
				<-request.Context().Done()
				return nil, request.Context().Err()
			}),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/ai-execution?ticket=ticket-token",
		strings.NewReader(`{"model":"model-a","stream":true,"messages":[{"role":"user","content":"你好"}]}`),
	).WithContext(ctx)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.requireHTTPSUpstream = false
		handler.aiExecution(recorder, request)
		close(done)
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("execution did not return after client cancellation")
	}
	store.mu.Lock()
	releases := store.releases
	store.mu.Unlock()
	if releases != 1 {
		t.Fatalf("lease releases = %d, want 1", releases)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context error = %v, want canceled", ctx.Err())
	}
}

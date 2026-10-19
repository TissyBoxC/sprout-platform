package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	authrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/repository"
	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/security"
)

// permissionFakeRepository satisfies the auth repository interface through
// embedding; only GetParentAccountByID is used by the authorization path.
type permissionFakeRepository struct {
	authrepository.Repository
	account *authdomain.ParentAccount
}

func (repository *permissionFakeRepository) GetActiveSessionByID(
	_ context.Context,
	_ string,
	_ string,
) (*authdomain.Session, error) {
	return &authdomain.Session{}, nil
}

func (repository *permissionFakeRepository) GetParentAccountByID(
	_ context.Context,
	accountID string,
) (*authdomain.ParentAccount, error) {
	if repository.account == nil || repository.account.ID != accountID {
		return nil, authdomain.ErrAccountNotFound
	}
	return repository.account, nil
}

func TestRequireAdminPermissionGrantsExplicitCapability(t *testing.T) {
	handler, token := newPermissionMiddleware(t, &authdomain.ParentAccount{
		ID:               "admin-1",
		Role:             authdomain.RoleAdmin,
		Status:           "active",
		AdminPermissions: []string{authdomain.AdminPermissionDevicesRead},
	})

	reached := false
	next := func(response http.ResponseWriter, _ *http.Request) {
		reached = true
		response.WriteHeader(http.StatusOK)
	}
	request := authenticatedRequest(token)
	recorder := httptest.NewRecorder()

	handler.requireAdminPermission(authdomain.AdminPermissionDevicesRead, next)(
		recorder,
		request,
	)

	if !reached || recorder.Code != http.StatusOK {
		t.Fatalf("granted request blocked: reached=%v code=%d", reached, recorder.Code)
	}
}

func TestRequireAdminPermissionDeniesMissingCapability(t *testing.T) {
	audit := &recordingAudit{}
	handler, token := newPermissionMiddlewareWithAudit(t, &authdomain.ParentAccount{
		ID:               "admin-1",
		Role:             authdomain.RoleAdmin,
		Status:           "active",
		AdminPermissions: []string{authdomain.AdminPermissionDevicesRead},
	}, audit)

	reached := false
	next := func(http.ResponseWriter, *http.Request) { reached = true }
	request := authenticatedRequest(token)
	recorder := httptest.NewRecorder()

	handler.requireAdminPermission(authdomain.AdminPermissionServiceUpgrade, next)(
		recorder,
		request,
	)

	if reached {
		t.Fatal("a missing capability must not reach the handler")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if len(audit.events) != 1 || audit.events[0].action != "admin.permission.denied" {
		t.Fatalf("expected a denial audit event, got %+v", audit.events)
	}
	if reason, _ := audit.events[0].detail["reason"].(string); reason == "" {
		t.Fatal("denial audit must record the reason")
	}
}

func TestRequireAuthenticationFailsClosedWhenDenialAuditFails(t *testing.T) {
	handler, token := newPermissionMiddlewareWithAudit(
		t,
		&authdomain.ParentAccount{
			ID:               "admin-1",
			Role:             authdomain.RoleAdmin,
			Status:           "active",
			AdminPermissions: []string{authdomain.AdminPermissionWildcard},
		},
		failingAudit{},
	)
	request := authenticatedRequest(token)
	request.Header.Del("Authorization")
	recorder := httptest.NewRecorder()
	reached := false

	handler.requireAuthentication(func(http.ResponseWriter, *http.Request) {
		reached = true
	})(recorder, request)

	if reached {
		t.Fatal("an unauthenticated request must not reach the handler")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusUnauthorized,
		)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode denial response: %v", err)
	}
	if envelope.Error.Code != "unauthenticated" {
		t.Fatalf("error code = %q", envelope.Error.Code)
	}
}

func TestRequireAdminPermissionFailsClosedWhenDenialAuditFails(t *testing.T) {
	handler, token := newPermissionMiddlewareWithAudit(
		t,
		&authdomain.ParentAccount{
			ID:               "admin-1",
			Role:             authdomain.RoleAdmin,
			Status:           "active",
			AdminPermissions: []string{authdomain.AdminPermissionDevicesRead},
		},
		failingAudit{},
	)
	request := authenticatedRequest(token)
	recorder := httptest.NewRecorder()
	reached := false

	handler.requireAdminPermission(
		authdomain.AdminPermissionServiceUpgrade,
		func(http.ResponseWriter, *http.Request) { reached = true },
	)(recorder, request)

	if reached {
		t.Fatal("a missing capability must not reach the handler")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusForbidden,
		)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode denial response: %v", err)
	}
	if envelope.Error.Code != "insufficient_permission" {
		t.Fatalf("error code = %q", envelope.Error.Code)
	}
}

type failingAudit struct{}

func (failingAudit) RecordAudit(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ map[string]any,
) error {
	return errors.New("audit storage unavailable")
}

func TestRequireAdminPermissionGrantsWildcard(t *testing.T) {
	handler, token := newPermissionMiddleware(t, &authdomain.ParentAccount{
		ID:               "admin-1",
		Role:             authdomain.RoleAdmin,
		Status:           "active",
		AdminPermissions: []string{authdomain.AdminPermissionWildcard},
	})

	reached := false
	next := func(response http.ResponseWriter, _ *http.Request) {
		reached = true
		response.WriteHeader(http.StatusOK)
	}
	recorder := httptest.NewRecorder()

	handler.requireAdminPermission(authdomain.AdminPermissionServiceUpgrade, next)(
		recorder,
		authenticatedRequest(token),
	)

	if !reached {
		t.Fatal("the wildcard must grant every capability")
	}
}

func TestAuditAfterSuccessReturnsAcceptedNoticeWithoutLosingData(t *testing.T) {
	handler, token := newPermissionMiddlewareWithAudit(
		t,
		&authdomain.ParentAccount{
			ID:               "admin-1",
			Role:             authdomain.RoleAdmin,
			Status:           "active",
			AdminPermissions: []string{authdomain.AdminPermissionWildcard},
		},
		failingAudit{},
	)
	request := authenticatedRequest(token)
	request.Method = http.MethodPost
	request.URL.Path = "/api/v1/admin/example"
	recorder := httptest.NewRecorder()

	handler.auditAfterSuccess(
		"admin.example.updated",
		func(response http.ResponseWriter, request *http.Request) {
			writeSuccess(
				response,
				request,
				http.StatusOK,
				map[string]any{"updated": true},
			)
		},
	)(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want accepted: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data   map[string]any `json:"data"`
		Error  any            `json:"error"`
		Notice *struct {
			Code string `json:"code"`
		} `json:"notice"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data["updated"] != true ||
		envelope.Error != nil ||
		envelope.Notice == nil ||
		envelope.Notice.Code != "audit_write_failed" {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "audit storage unavailable") {
		t.Fatal("internal audit error leaked into the response")
	}
}

func TestAuditAfterSuccessForwardsMarkedStreamingWriter(t *testing.T) {
	handler := authHandler{audit: failingAudit{}}
	request := httptest.NewRequest(http.MethodPost, "/large-upload", nil)
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"admin-1",
	))
	recorder := httptest.NewRecorder()
	called := false

	handler.auditAfterSuccess(
		"admin.upload.completed",
		func(response http.ResponseWriter, _ *http.Request) {
			called = true
			if _, ok := response.(*bufferedResponseRecorder); ok {
				t.Fatal("streaming-marked response was buffered")
			}
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(`{"uploaded":true}`))
		},
	)(wrapStreamingResponseWriter(recorder), request)

	if !called {
		t.Fatal("streaming handler was not called")
	}
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", recorder.Code)
	}
	if body := recorder.Body.String(); body != `{"uploaded":true}` {
		t.Fatalf("body = %q", body)
	}
}

func TestRequireAdminPermissionDeniesGuardian(t *testing.T) {
	handler, token := newPermissionMiddleware(t, &authdomain.ParentAccount{
		ID:               "parent-1",
		Role:             authdomain.RoleParent,
		Status:           "active",
		AdminPermissions: []string{authdomain.AdminPermissionWildcard},
	})

	reached := false
	next := func(http.ResponseWriter, *http.Request) { reached = true }
	recorder := httptest.NewRecorder()

	handler.requireAdminPermission(authdomain.AdminPermissionDevicesRead, next)(
		recorder,
		authenticatedRequest(token),
	)

	if reached {
		t.Fatal("a guardian must never reach a management handler")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestRequireAdminPermissionRequiresToken(t *testing.T) {
	handler, _ := newPermissionMiddleware(t, &authdomain.ParentAccount{
		ID:               "admin-1",
		Role:             authdomain.RoleAdmin,
		Status:           "active",
		AdminPermissions: []string{authdomain.AdminPermissionWildcard},
	})

	reached := false
	next := func(http.ResponseWriter, *http.Request) { reached = true }
	recorder := httptest.NewRecorder()

	handler.requireAdminPermission(authdomain.AdminPermissionDevicesRead, next)(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/admin/devices", nil),
	)

	if reached || recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: reached=%v code=%d", reached, recorder.Code)
	}
}

func TestFeatureCenterPermissionSplitsReadAndWrite(t *testing.T) {
	readRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/feature-center", nil)
	if got := featureCenterPermission(readRequest); got != authdomain.AdminPermissionFeatureCenterRead {
		t.Fatalf("read route permission = %q", got)
	}
	healthRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/feature-center/notification/health-check",
		nil,
	)
	if got := featureCenterPermission(healthRequest); got != authdomain.AdminPermissionFeatureCenterRead {
		t.Fatalf("health route permission = %q", got)
	}
	writeRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/feature-center/voice_gateway/config",
		nil,
	)
	if got := featureCenterPermission(writeRequest); got != authdomain.AdminPermissionFeatureCenterWrite {
		t.Fatalf("write route permission = %q", got)
	}
}

func TestAccountResponseIncludesAdminPermissions(t *testing.T) {
	response := accountResponse(&authdomain.ParentAccount{
		ID:               "admin-1",
		Email:            "admin@sprout.local",
		Role:             authdomain.RoleAdmin,
		Status:           "active",
		AdminPermissions: []string{authdomain.AdminPermissionFeatureCenterRead},
	}, nil)

	account, ok := response["account"].(map[string]any)
	if !ok {
		t.Fatalf("account response = %#v", response["account"])
	}
	permissions, ok := account["admin_permissions"].([]string)
	if !ok || len(permissions) != 1 ||
		permissions[0] != authdomain.AdminPermissionFeatureCenterRead {
		t.Fatalf("admin_permissions = %#v", account["admin_permissions"])
	}
}

func TestOTAPermissionSplitsReadAndWrite(t *testing.T) {
	readRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ota/releases", nil)
	if got := otaPermission(readRequest); got != authdomain.AdminPermissionOTARead {
		t.Fatalf("read route permission = %q", got)
	}
	publishRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/ota/releases/release-1/publish",
		nil,
	)
	if got := otaPermission(publishRequest); got != authdomain.AdminPermissionOTAManage {
		t.Fatalf("write route permission = %q", got)
	}
}

func newPermissionMiddleware(
	t *testing.T,
	account *authdomain.ParentAccount,
) (authHandler, string) {
	t.Helper()
	return newPermissionMiddlewareWithAudit(t, account, nil)
}

func TestRequireAuthenticationRejectsDisabledAccount(t *testing.T) {
	handler, token := newPermissionMiddleware(t, &authdomain.ParentAccount{
		ID:     "parent-disabled",
		Role:   authdomain.RoleParent,
		Status: "disabled",
	})
	request := authenticatedRequest(token)
	recorder := httptest.NewRecorder()
	handler.requireAuthentication(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		t.Fatal("disabled account must not reach the protected handler")
	})(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
}

func newPermissionMiddlewareWithAudit(
	t *testing.T,
	account *authdomain.ParentAccount,
	audit auditRecorder,
) (authHandler, string) {
	t.Helper()
	issuer, err := security.NewHMACTokenIssuer("permission-test-secret-0123456789abcdef")
	if err != nil {
		t.Fatalf("create token issuer: %v", err)
	}
	service, err := authservice.New(authservice.Options{
		Repository:  &permissionFakeRepository{account: account},
		TokenIssuer: issuer,
		AccessTTL:   time.Minute,
		RefreshTTL:  time.Hour,
	})
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	token, err := issuer.Issue(security.TokenClaims{
		Subject:   account.ID,
		SessionID: "test-session",
		TokenID:   "test-token",
		IssuedAt:  time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return authHandler{
		service: service,
		audit:   audit,
		limiter: authRateLimiter{store: permittedRateLimitStore{}},
	}, token
}

func authenticatedRequest(token string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/devices", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

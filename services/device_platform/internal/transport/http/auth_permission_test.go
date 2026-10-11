package http

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	writeRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/feature-center/voice_gateway/config",
		nil,
	)
	if got := featureCenterPermission(writeRequest); got != authdomain.AdminPermissionFeatureCenterWrite {
		t.Fatalf("write route permission = %q", got)
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
		TokenID:   "test-token",
		IssuedAt:  time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return authHandler{service: service, audit: audit}, token
}

func authenticatedRequest(token string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/devices", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

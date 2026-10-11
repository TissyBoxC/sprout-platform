package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
)

// stubPermissionService records the calls the HTTP layer makes so the handler
// contract can be verified without a database.
type stubPermissionService struct {
	accounts    []authdomain.AdminPermissionAccount
	updated     authdomain.AdminPermissionAccount
	lastUpdate  []string
	lastAccount string
	err         error
}

func (stub *stubPermissionService) AdminPermissionCatalog() []string {
	return []string{authdomain.AdminPermissionDevicesRead, authdomain.AdminPermissionAuditRead}
}

func (stub *stubPermissionService) ListAdminPermissions(
	_ context.Context,
) ([]authdomain.AdminPermissionAccount, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	return stub.accounts, nil
}

func (stub *stubPermissionService) UpdateAdminPermissions(
	_ context.Context,
	accountID string,
	permissions []string,
) (authdomain.AdminPermissionAccount, error) {
	stub.lastAccount = accountID
	stub.lastUpdate = permissions
	if stub.err != nil {
		return authdomain.AdminPermissionAccount{}, stub.err
	}
	return stub.updated, nil
}

func TestAdminPermissionListReturnsCatalogAndAccounts(t *testing.T) {
	service := &stubPermissionService{accounts: []authdomain.AdminPermissionAccount{
		{
			AccountID:   "admin-1",
			Email:       "ops@sprout.local",
			DisplayName: "运营管理员",
			Status:      "active",
			CreatedAt:   time.Now().UTC(),
			Permissions: []string{authdomain.AdminPermissionDevicesRead},
		},
	}}
	handler := adminPermissionHandler{service: service}
	recorder := httptest.NewRecorder()

	handler.list(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/permissions", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var envelope struct {
		Data struct {
			Catalog  []string `json:"catalog"`
			Accounts []struct {
				AccountID   string   `json:"account_id"`
				Permissions []string `json:"permissions"`
			} `json:"accounts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.Catalog) != 2 || len(envelope.Data.Accounts) != 1 {
		t.Fatalf("unexpected payload: %s", recorder.Body.String())
	}
	if envelope.Data.Accounts[0].AccountID != "admin-1" {
		t.Fatalf("unexpected account: %s", recorder.Body.String())
	}
}

func TestAdminPermissionUpdatePersistsAndAudits(t *testing.T) {
	service := &stubPermissionService{updated: authdomain.AdminPermissionAccount{
		AccountID:   "admin-2",
		Email:       "review@sprout.local",
		DisplayName: "内容审核",
		Status:      "active",
		Permissions: []string{authdomain.AdminPermissionContentRead},
	}}
	audit := &recordingAudit{}
	handler := adminPermissionHandler{service: service, audit: audit}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/permissions/admin-2",
		strings.NewReader(`{"permissions":["content.read"]}`),
	)
	request.SetPathValue("account_id", "admin-2")
	recorder := httptest.NewRecorder()

	handler.update(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.lastAccount != "admin-2" || len(service.lastUpdate) != 1 {
		t.Fatalf("unexpected service call: %+v", service)
	}
	if len(audit.events) != 1 || audit.events[0].action != "admin.permission.updated" {
		t.Fatalf("expected an audit event, got %+v", audit.events)
	}
	if audit.events[0].targetAccountID != "admin-2" {
		t.Fatalf("audit target = %q", audit.events[0].targetAccountID)
	}
}

func TestAdminPermissionUpdateRejectsUnknownPermission(t *testing.T) {
	service := &stubPermissionService{err: authdomain.ErrUnknownAdminPermission}
	handler := adminPermissionHandler{service: service}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/permissions/admin-2",
		strings.NewReader(`{"permissions":["devices.destroy"]}`),
	)
	request.SetPathValue("account_id", "admin-2")
	recorder := httptest.NewRecorder()

	handler.update(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestAdminPermissionUpdateRejectsMalformedBody(t *testing.T) {
	handler := adminPermissionHandler{service: &stubPermissionService{}}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/permissions/admin-2",
		strings.NewReader(`{"permissions":"content.read"}`),
	)
	request.SetPathValue("account_id", "admin-2")
	recorder := httptest.NewRecorder()

	handler.update(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

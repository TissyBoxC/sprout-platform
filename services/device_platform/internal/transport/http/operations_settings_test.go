package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
)

type settingsAdminService struct {
	updateSettings *operationsdomain.Settings
	updateActorID  string
	updateVersion  int64
	updateErr      error
	familyPage     int
	familyPageSize int
	releasePage    int
	releaseSize    int
	listFamilyErr  error
}

func (*settingsAdminService) Settings(
	context.Context,
) (*operationsdomain.Settings, int64, error) {
	return &operationsdomain.Settings{}, 1, nil
}

func (service *settingsAdminService) UpdateSettings(
	_ context.Context,
	settings *operationsdomain.Settings,
	actorAccountID string,
	expectedVersion int64,
) (*operationsdomain.Settings, int64, error) {
	service.updateSettings = settings
	service.updateActorID = actorAccountID
	service.updateVersion = expectedVersion
	if service.updateErr != nil {
		return nil, 0, service.updateErr
	}
	return settings, expectedVersion + 1, nil
}

func (*settingsAdminService) Overview(context.Context) (*operationsdomain.Overview, error) {
	return nil, nil
}

func (service *settingsAdminService) ListFamilyAccounts(
	_ context.Context,
	page int,
	pageSize int,
) (operationsdomain.FamilyAccountPage, error) {
	service.familyPage = page
	service.familyPageSize = pageSize
	if service.listFamilyErr != nil {
		return operationsdomain.FamilyAccountPage{}, service.listFamilyErr
	}
	return operationsdomain.FamilyAccountPage{
		Accounts: []operationsdomain.FamilyAccount{},
		Pagination: operationsdomain.NewPagination(
			operationsdomain.NormalizePageRequest(page, pageSize),
			45,
		),
	}, nil
}

func TestListFamiliesRejectsInvalidPageSizeBeforeService(t *testing.T) {
	service := &settingsAdminService{}
	handler := adminHandler{operationsService: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families?page=1&page_size=101",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.listFamilies(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
	if service.familyPage != 0 {
		t.Fatal("invalid page size must not reach the service")
	}
}

func (service *settingsAdminService) ListReleases(
	_ context.Context,
	page int,
	pageSize int,
) (operationsdomain.ReleasePage, error) {
	service.releasePage = page
	service.releaseSize = pageSize
	return operationsdomain.ReleasePage{
		Releases: []operationsdomain.Release{},
		Pagination: operationsdomain.NewPagination(
			operationsdomain.NormalizePageRequest(page, pageSize),
			40,
		),
	}, nil
}

func (*settingsAdminService) CreateRelease(context.Context, operationsdomain.ReleaseInput) (*operationsdomain.Release, error) {
	return nil, nil
}

func (*settingsAdminService) PublishRelease(context.Context, string, string) error {
	return nil
}

func (*settingsAdminService) DeleteRelease(context.Context, string) error {
	return nil
}

func (*settingsAdminService) FindReleaseArtifact(context.Context, string, string, string) (*operationsdomain.ReleaseArtifact, error) {
	return nil, nil
}

func (*settingsAdminService) AppUpdate(context.Context, string, string, string) (*operationsdomain.AppUpdate, error) {
	return nil, nil
}

func TestUpdateSettingsRejectsUnwrappedDocument(t *testing.T) {
	service := &settingsAdminService{}
	handler := adminHandler{operationsService: service}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		strings.NewReader(`{"ai":{"default_balance_usd":1}}`),
	)
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"admin-001",
	))
	recorder := httptest.NewRecorder()

	handler.updateSettings(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
	if service.updateSettings != nil {
		t.Fatal("unwrapped settings must not reach the service")
	}
}

func TestUpdateSettingsPassesExpectedVersionAndMapsConflict(t *testing.T) {
	service := &settingsAdminService{}
	handler := adminHandler{operationsService: service}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		strings.NewReader(`{
			"settings": {"ai":{"default_balance_usd":1}},
			"expected_version": 4
		}`),
	)
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"admin-001",
	))

	successRecorder := httptest.NewRecorder()
	handler.updateSettings(successRecorder, request)
	if successRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", successRecorder.Code, successRecorder.Body.String())
	}
	if service.updateVersion != 4 || service.updateSettings == nil {
		t.Fatalf("unexpected delegated update: version=%d settings=%v", service.updateVersion, service.updateSettings)
	}

	service.updateErr = &operationsdomain.SettingsVersionConflictError{
		ExpectedVersion: 4,
		CurrentVersion:  5,
	}
	conflictRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		strings.NewReader(`{
			"settings": {"ai":{"default_balance_usd":1}},
			"expected_version": 4
		}`),
	)
	conflictRequest = conflictRequest.WithContext(context.WithValue(
		conflictRequest.Context(),
		authenticatedAccountKey{},
		"admin-001",
	))
	conflictRecorder := httptest.NewRecorder()
	handler.updateSettings(conflictRecorder, conflictRequest)
	if conflictRecorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", conflictRecorder.Code, conflictRecorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(conflictRecorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if envelope.Error.Code != "settings_version_conflict" {
		t.Fatalf("unexpected conflict code: %q", envelope.Error.Code)
	}
}

func TestSettingsVersionConflictUsesErrorsIs(t *testing.T) {
	err := &operationsdomain.SettingsVersionConflictError{}
	if !errors.Is(err, operationsdomain.ErrSettingsVersionConflict) {
		t.Fatal("expected settings version conflict error to unwrap to the stable sentinel")
	}
}

func TestListFamiliesPassesPaginationAndReturnsMetadata(t *testing.T) {
	service := &settingsAdminService{}
	handler := adminHandler{operationsService: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families?page=2&page_size=10",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.listFamilies(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if service.familyPage != 2 || service.familyPageSize != 10 {
		t.Fatalf(
			"delegated page = %d/%d, want 2/10",
			service.familyPage,
			service.familyPageSize,
		)
	}
	var envelope struct {
		Data struct {
			Accounts   []operationsdomain.FamilyAccount `json:"accounts"`
			Pagination operationsdomain.Pagination      `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Pagination.Total != 45 ||
		envelope.Data.Pagination.Page != 2 ||
		envelope.Data.Pagination.NextPage == nil ||
		*envelope.Data.Pagination.NextPage != 3 {
		t.Fatalf("unexpected pagination: %+v", envelope.Data.Pagination)
	}
}

func TestListReleasesDefaultsToFirstBoundedPage(t *testing.T) {
	service := &settingsAdminService{}
	handler := adminHandler{operationsService: service}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/releases", nil)
	recorder := httptest.NewRecorder()

	handler.listReleases(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if service.releasePage != 1 ||
		service.releaseSize != operationsdomain.DefaultListPageSize {
		t.Fatalf(
			"delegated page = %d/%d, want 1/%d",
			service.releasePage,
			service.releaseSize,
			operationsdomain.DefaultListPageSize,
		)
	}
}

func TestListReleasesRejectsInvalidPagination(t *testing.T) {
	cases := []string{
		"/api/v1/admin/releases?page=0",
		"/api/v1/admin/releases?page=two",
		"/api/v1/admin/releases?page_size=0",
		"/api/v1/admin/releases?page_size=101",
	}
	for _, target := range cases {
		target := target
		t.Run(target, func(t *testing.T) {
			service := &settingsAdminService{}
			handler := adminHandler{operationsService: service}
			request := httptest.NewRequest(http.MethodGet, target, nil)
			recorder := httptest.NewRecorder()

			handler.listReleases(recorder, request)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf(
					"status = %d, want 400: %s",
					recorder.Code,
					recorder.Body.String(),
				)
			}
			if service.releasePage != 0 {
				t.Fatal("invalid pagination must not reach the service")
			}
		})
	}
}

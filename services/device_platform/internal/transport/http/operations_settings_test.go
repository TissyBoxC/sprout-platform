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
	publishedID    string
	deletedID      string
	artifactLookup operationsdomain.ReleaseIdentity
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

func (service *settingsAdminService) PublishRelease(
	_ context.Context,
	releaseID string,
	_ string,
) error {
	service.publishedID = releaseID
	return nil
}

func (service *settingsAdminService) DeleteRelease(
	_ context.Context,
	releaseID string,
) error {
	service.deletedID = releaseID
	return nil
}

func (service *settingsAdminService) FindReleaseArtifact(
	_ context.Context,
	version string,
	kind string,
	platform string,
	channel string,
) (*operationsdomain.ReleaseArtifact, error) {
	service.artifactLookup = operationsdomain.ReleaseIdentity{
		Version:  version,
		Channel:  channel,
		Kind:     kind,
		Platform: platform,
	}
	return &operationsdomain.ReleaseArtifact{
		Version:     version,
		Channel:     channel,
		Kind:        kind,
		Platform:    platform,
		DownloadURL: "https://download.example.test/app.apk",
		SHA256:      strings.Repeat("a", 64),
	}, nil
}

func (*settingsAdminService) AppUpdate(
	context.Context,
	string,
	string,
	string,
	string,
) (*operationsdomain.AppUpdate, error) {
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
	if !service.updateSettings.Safety.MinorModeDefault {
		t.Fatal("omitted minor mode must be materialized as the enforced true value")
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

func TestUpdateSettingsRejectsDisabledMinorMode(t *testing.T) {
	service := &settingsAdminService{}
	handler := adminHandler{operationsService: service}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		strings.NewReader(`{
			"settings": {"safety":{"minor_mode_default":false}},
			"expected_version": 4
		}`),
	)
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"admin-001",
	))
	recorder := httptest.NewRecorder()

	handler.updateSettings(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", recorder.Code, recorder.Body.String())
	}
	if service.updateSettings != nil {
		t.Fatal("disabled minor mode must not reach the service")
	}
}

func TestUpdateSettingsRejectsRawAudioRetention(t *testing.T) {
	service := &settingsAdminService{}
	handler := adminHandler{operationsService: service}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		strings.NewReader(`{
			"settings": {"retention":{"audio_days":7}},
			"expected_version": 4
		}`),
	)
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"admin-001",
	))
	recorder := httptest.NewRecorder()

	handler.updateSettings(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", recorder.Code, recorder.Body.String())
	}
	if service.updateSettings != nil {
		t.Fatal("raw audio retention must not reach the service")
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

func TestPublishAndDeleteReleaseUseRecordIdentity(t *testing.T) {
	t.Parallel()
	service := &settingsAdminService{}
	handler := adminHandler{operationsService: service}
	releaseID := "72453d1e-dce3-401a-96db-e96db54c9fd5"
	publishRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/releases/"+releaseID+"/publish",
		nil,
	)
	publishRequest.SetPathValue("release_id", releaseID)
	publishRequest = publishRequest.WithContext(context.WithValue(
		publishRequest.Context(),
		authenticatedAccountKey{},
		"admin-001",
	))
	publishResponse := httptest.NewRecorder()

	handler.publishRelease(publishResponse, publishRequest)

	if publishResponse.Code != http.StatusOK || service.publishedID != releaseID {
		t.Fatalf(
			"publish status=%d delegated id=%q",
			publishResponse.Code,
			service.publishedID,
		)
	}

	deleteRequest := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/admin/releases/"+releaseID,
		nil,
	)
	deleteRequest.SetPathValue("release_id", releaseID)
	deleteResponse := httptest.NewRecorder()

	handler.deleteRelease(deleteResponse, deleteRequest)

	if deleteResponse.Code != http.StatusOK || service.deletedID != releaseID {
		t.Fatalf(
			"delete status=%d delegated id=%q",
			deleteResponse.Code,
			service.deletedID,
		)
	}
}

func TestGetReleaseArtifactPassesExactChannelAndIdentity(t *testing.T) {
	t.Parallel()
	service := &settingsAdminService{}
	handler := adminHandler{operationsService: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/release-artifacts/0.29.0?kind=client&platform=android&channel=beta",
		nil,
	)
	request.SetPathValue("version", "0.29.0")
	response := httptest.NewRecorder()

	handler.getReleaseArtifact(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Artifact struct {
				Channel string `json:"channel"`
			} `json:"artifact"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode artifact response: %v", err)
	}
	if envelope.Data.Artifact.Channel != "beta" {
		t.Fatalf("artifact channel = %q, want beta", envelope.Data.Artifact.Channel)
	}
	if service.artifactLookup != (operationsdomain.ReleaseIdentity{
		Version:  "0.29.0",
		Channel:  "beta",
		Kind:     "client",
		Platform: "android",
	}) {
		t.Fatalf("artifact lookup = %+v", service.artifactLookup)
	}
}

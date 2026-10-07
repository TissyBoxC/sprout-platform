package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	contentdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	contentservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/service"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/security"
)

type contentTestRepository struct {
	mu       sync.Mutex
	versions map[string]*contentdomain.PackageVersion
}

func newContentTestRepository() *contentTestRepository {
	return &contentTestRepository{
		versions: make(map[string]*contentdomain.PackageVersion),
	}
}

func (repository *contentTestRepository) List(
	_ context.Context,
	filter contentdomain.PackageListFilter,
) (contentdomain.PackagePage, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	packages := make([]contentdomain.PackageVersion, 0, len(repository.versions))
	for _, version := range repository.versions {
		packages = append(packages, *version)
	}
	return contentdomain.PackagePage{
		Packages: packages,
		Page:     filter.Page,
		PageSize: filter.PageSize,
		Total:    len(packages),
	}, nil
}

func (repository *contentTestRepository) GetDetail(
	_ context.Context,
	packageID string,
) (*contentdomain.PackageDetail, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	versions := make([]contentdomain.PackageVersion, 0)
	for _, version := range repository.versions {
		if version.PackageID == packageID {
			versions = append(versions, *version)
		}
	}
	if len(versions) == 0 {
		return nil, contentdomain.ErrPackageNotFound
	}
	return &contentdomain.PackageDetail{
		PackageID: packageID,
		Versions:  versions,
	}, nil
}

func (repository *contentTestRepository) GetVersion(
	_ context.Context,
	packageID string,
	packageVersion int,
) (*contentdomain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	version := repository.versions[contentTestKey(packageID, packageVersion)]
	if version == nil {
		return nil, contentdomain.ErrPackageNotFound
	}
	return cloneContentTestVersion(version), nil
}

func (repository *contentTestRepository) CreateDraft(
	_ context.Context,
	input contentdomain.MetadataInput,
	now time.Time,
) (*contentdomain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	version := &contentdomain.PackageVersion{
		PackageID:      input.PackageID,
		PackageVersion: 1,
		Title:          input.Title,
		Category:       input.Category,
		AgeTiers:       input.AgeTiers,
		AssetKey:       input.AssetKey,
		SHA256:         input.SHA256,
		SizeBytes:      input.SizeBytes,
		Status:         contentdomain.StatusDraft,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	repository.versions[contentTestKey(input.PackageID, 1)] = version
	return cloneContentTestVersion(version), nil
}

func (repository *contentTestRepository) CreateVersion(
	_ context.Context,
	input contentdomain.MetadataInput,
	packageVersion int,
	now time.Time,
) (*contentdomain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	version := &contentdomain.PackageVersion{
		PackageID:      input.PackageID,
		PackageVersion: packageVersion,
		Title:          input.Title,
		Category:       input.Category,
		AgeTiers:       input.AgeTiers,
		AssetKey:       input.AssetKey,
		SHA256:         input.SHA256,
		SizeBytes:      input.SizeBytes,
		Status:         contentdomain.StatusDraft,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	repository.versions[contentTestKey(input.PackageID, packageVersion)] = version
	return cloneContentTestVersion(version), nil
}

func (repository *contentTestRepository) UpdateDraft(
	_ context.Context,
	packageID string,
	packageVersion int,
	_ contentdomain.MetadataInput,
	_ time.Time,
) (*contentdomain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	version := repository.versions[contentTestKey(packageID, packageVersion)]
	if version == nil {
		return nil, contentdomain.ErrPackageNotFound
	}
	return cloneContentTestVersion(version), nil
}

func (repository *contentTestRepository) ApplyTransition(
	_ context.Context,
	transition contentdomain.Transition,
) (*contentdomain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	version := repository.versions[contentTestKey(
		transition.PackageID,
		transition.PackageVersion,
	)]
	if version == nil {
		return nil, contentdomain.ErrPackageNotFound
	}
	version.Status = transition.ToStatus
	return cloneContentTestVersion(version), nil
}

func (repository *contentTestRepository) Catalog(
	_ context.Context,
	_ contentdomain.CatalogQuery,
) (*contentdomain.Catalog, error) {
	return &contentdomain.Catalog{Revision: 1}, nil
}

func (repository *contentTestRepository) PublishedDownload(
	_ context.Context,
	packageID string,
) (*contentdomain.PackageVersion, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	version := repository.versions[contentTestKey(packageID, 1)]
	if version == nil {
		return nil, contentdomain.ErrPackageNotFound
	}
	publishedAt := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	version.PublishedAt = &publishedAt
	return cloneContentTestVersion(version), nil
}

type contentTestAssetReader struct {
	asset *contentdomain.Asset
}

func (reader contentTestAssetReader) FindContentAsset(
	_ context.Context,
	_ string,
) (*contentdomain.Asset, error) {
	return reader.asset, nil
}

type contentTestBindingService struct {
	sessionDeviceID string
	err             error
}

func (service contentTestBindingService) VerifyDeviceSession(
	_ context.Context,
	_ string,
) (string, error) {
	if service.err != nil {
		return "", service.err
	}
	return service.sessionDeviceID, nil
}

func newContentHandlerForTest(t *testing.T) contentHandler {
	t.Helper()
	service, err := contentservice.New(contentservice.Options{
		Repository:  newContentTestRepository(),
		AssetReader: contentTestAssetReader{},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	return contentHandler{service: service}
}

func TestContentCreateRejectsInvalidMetadata(t *testing.T) {
	handler := newContentHandlerForTest(t)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/content/packages",
		strings.NewReader(`{"package_id":"bad id","title":"故事","category":"story","age_tiers":["age_3_4"],"asset_key":"content/story/a.bin","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size_bytes":1}`),
	)
	recorder := httptest.NewRecorder()
	handler.createPackage(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestContentRejectRequiresReasonAndUsesAdminActor(t *testing.T) {
	handler := newContentHandlerForTest(t)
	repository := newContentTestRepository()
	repository.versions[contentTestKey("content_story_001", 1)] = &contentdomain.PackageVersion{
		PackageID:      "content_story_001",
		PackageVersion: 1,
		Status:         contentdomain.StatusInReview,
	}
	service, err := contentservice.New(contentservice.Options{
		Repository:  repository,
		AssetReader: contentTestAssetReader{},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	handler = contentHandler{service: service}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/content/packages/content_story_001/versions/1/reject",
		strings.NewReader(`{"reason":""}`),
	)
	request.SetPathValue("package_id", "content_story_001")
	request.SetPathValue("package_version", "1")
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"admin-001",
	))
	recorder := httptest.NewRecorder()
	handler.rejectPackage(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestContentCatalogRejectsNegativeRevision(t *testing.T) {
	handler := newContentHandlerForTest(t)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/content/catalog?since_revision=-1",
		nil,
	)
	recorder := httptest.NewRecorder()
	handler.catalog(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestContentDownloadReturnsVerifiedAsset(t *testing.T) {
	repository := newContentTestRepository()
	sha256 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repository.versions[contentTestKey("content_story_001", 1)] = &contentdomain.PackageVersion{
		PackageID:      "content_story_001",
		PackageVersion: 1,
		Title:          "月亮晚安故事",
		AssetKey:       "content/story/moon-night-v1.bin",
		SHA256:         sha256,
		Status:         contentdomain.StatusPublished,
	}
	service, err := contentservice.New(contentservice.Options{
		Repository: repository,
		AssetReader: contentTestAssetReader{
			asset: &contentdomain.Asset{
				Key:         "content/story/moon-night-v1.bin",
				SHA256:      sha256,
				SizeBytes:   1024,
				DownloadURL: "https://download.example.test/content/story/moon-night-v1.bin",
			},
		},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	handler := contentHandler{service: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/content/packages/content_story_001/download",
		nil,
	)
	request.SetPathValue("package_id", "content_story_001")
	recorder := httptest.NewRecorder()
	handler.download(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Download struct {
				SHA256      string `json:"sha256"`
				DownloadURL string `json:"download_url"`
			} `json:"download"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Download.SHA256 != sha256 ||
		envelope.Data.Download.DownloadURL == "" {
		t.Fatalf("unexpected download response: %s", recorder.Body.String())
	}
}

func TestDeviceContentCatalogRequiresMatchingDeviceSession(t *testing.T) {
	handler := deviceContentHandler{
		contentHandler: newContentHandlerForTest(t),
		bindingService: contentTestBindingService{
			sessionDeviceID: "sprout_device_001",
		},
	}
	for _, test := range []struct {
		name       string
		token      string
		pathDevice string
		wantCode   int
	}{
		{
			name:       "matching session",
			token:      "device-session-token",
			pathDevice: "sprout_device_001",
			wantCode:   http.StatusOK,
		},
		{
			name:       "missing token",
			token:      "",
			pathDevice: "sprout_device_001",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "other device path",
			token:      "device-session-token",
			pathDevice: "sprout_device_002",
			wantCode:   http.StatusUnauthorized,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/devices/"+test.pathDevice+"/content/catalog",
				nil,
			)
			request.SetPathValue("device_id", test.pathDevice)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			recorder := httptest.NewRecorder()
			handler.catalog(recorder, request)
			if recorder.Code != test.wantCode {
				t.Fatalf(
					"status = %d, want %d: %s",
					recorder.Code,
					test.wantCode,
					recorder.Body.String(),
				)
			}
		})
	}
}

func TestDeviceContentCatalogRejectsInvalidSession(t *testing.T) {
	handler := deviceContentHandler{
		contentHandler: newContentHandlerForTest(t),
		bindingService: contentTestBindingService{
			err: errors.New("session expired"),
		},
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/sprout_device_001/content/catalog",
		nil,
	)
	request.SetPathValue("device_id", "sprout_device_001")
	request.Header.Set("Authorization", "Bearer device-session-token")
	recorder := httptest.NewRecorder()
	handler.catalog(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRouterMountsDeviceContentRoutes(t *testing.T) {
	contentService, err := contentservice.New(contentservice.Options{
		Repository:  newContentTestRepository(),
		AssetReader: contentTestAssetReader{},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	bindingRepository := newVoiceTokenRepository()
	bindingService, err := bindingservice.New(bindingservice.Options{
		Repository:    bindingRepository,
		ProofVerifier: security.ECDSAProofVerifier{},
		TokenTTL:      time.Minute,
	})
	if err != nil {
		t.Fatalf("create binding service: %v", err)
	}
	options := newTestRouterOptions()
	options.ContentService = contentService
	options.BindingService = bindingService
	router := NewRouter(options)
	for _, path := range []string{
		"/api/v1/devices/sprout_device_001/content/catalog",
		"/api/v1/devices/sprout_device_001/content/packages/content_story_001/download",
	} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf(
					"status = %d, want %d: %s",
					recorder.Code,
					http.StatusUnauthorized,
					recorder.Body.String(),
				)
			}
		})
	}
}

func contentTestKey(packageID string, packageVersion int) string {
	return fmt.Sprintf("%s/%d", packageID, packageVersion)
}

func cloneContentTestVersion(
	value *contentdomain.PackageVersion,
) *contentdomain.PackageVersion {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.AgeTiers = append([]contentdomain.AgeTier(nil), value.AgeTiers...)
	if value.PublishedAt != nil {
		publishedAt := *value.PublishedAt
		cloned.PublishedAt = &publishedAt
	}
	return &cloned
}

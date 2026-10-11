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
	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/security"
)

type contentTestRepository struct {
	mu         sync.Mutex
	versions   map[string]*contentdomain.PackageVersion
	favorites  map[string]contentdomain.Favorite
	history    []contentdomain.PlayRecord
	themePacks map[string]contentdomain.ThemePack
	revision   int64
}

func newContentTestRepository() *contentTestRepository {
	return &contentTestRepository{
		versions:   make(map[string]*contentdomain.PackageVersion),
		favorites:  make(map[string]contentdomain.Favorite),
		themePacks: make(map[string]contentdomain.ThemePack),
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
	query contentdomain.CatalogQuery,
) (*contentdomain.Catalog, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	packages := make([]contentdomain.PackageVersion, 0)
	for _, version := range repository.versions {
		if version.Status != contentdomain.StatusPublished {
			continue
		}
		if query.Category != "" && string(version.Category) != query.Category {
			continue
		}
		if len(query.AllowedCategories) > 0 {
			allowed := false
			for _, category := range query.AllowedCategories {
				if string(version.Category) == category {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		packages = append(packages, *cloneContentTestVersion(version))
	}
	return &contentdomain.Catalog{Revision: 1, Packages: packages}, nil
}

func (repository *contentTestRepository) CatalogRevision(
	_ context.Context,
) (int64, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return 1, nil
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

type contentTestRuntimeService struct {
	familyID string
	err      error
}

func (service contentTestRuntimeService) ResolveDeviceFamily(
	_ context.Context,
	_ string,
	_ string,
) (string, error) {
	if service.err != nil {
		return "", service.err
	}
	return service.familyID, nil
}

type contentTestPolicyReader struct {
	categories []string
}

func (reader contentTestPolicyReader) GetEffective(
	_ context.Context,
	_ string,
) (*policydomain.EffectivePolicy, error) {
	return &policydomain.EffectivePolicy{
		DailyLimitMinutes: 60,
		AllowedCategories: append([]string(nil), reader.categories...),
	}, nil
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
		PolicyReader: contentTestPolicyReader{
			categories: []string{"story"},
		},
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
		runtimeService: contentTestRuntimeService{familyID: "family_001"},
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

// TestDeviceContentCatalogFiltersByFamilyPolicy proves the device-facing
// catalog excludes packages whose category the family policy does not allow,
// even though the admin catalog still lists them.
func TestDeviceContentCatalogFiltersByFamilyPolicy(t *testing.T) {
	repository := newContentTestRepository()
	repository.versions[contentTestKey("content_story_001", 1)] = &contentdomain.PackageVersion{
		PackageID:      "content_story_001",
		PackageVersion: 1,
		Title:          "月亮晚安故事",
		Category:       contentdomain.CategoryStory,
		Status:         contentdomain.StatusPublished,
	}
	repository.versions[contentTestKey("content_english_001", 1)] = &contentdomain.PackageVersion{
		PackageID:      "content_english_001",
		PackageVersion: 1,
		Title:          "早安英语",
		Category:       contentdomain.CategoryEnglish,
		Status:         contentdomain.StatusPublished,
	}
	service, err := contentservice.New(contentservice.Options{
		Repository:  repository,
		AssetReader: contentTestAssetReader{},
		PolicyReader: contentTestPolicyReader{
			categories: []string{"story"},
		},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	handler := deviceContentHandler{
		contentHandler: contentHandler{service: service},
		bindingService: contentTestBindingService{
			sessionDeviceID: "sprout_device_001",
		},
		runtimeService: contentTestRuntimeService{familyID: "family_001"},
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
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Packages []struct {
				PackageID string `json:"package_id"`
				Category  string `json:"category"`
			} `json:"packages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data.Packages) != 1 ||
		payload.Data.Packages[0].PackageID != "content_story_001" {
		t.Fatalf("device catalog was not filtered: %s", recorder.Body.String())
	}
}

func TestDeviceContentCatalogRejectsInvalidSession(t *testing.T) {
	handler := deviceContentHandler{
		contentHandler: newContentHandlerForTest(t),
		bindingService: contentTestBindingService{
			err: errors.New("session expired"),
		},
		runtimeService: contentTestRuntimeService{familyID: "family_001"},
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

func TestContentFavoriteLifecycleEndpoints(t *testing.T) {
	repository := newContentTestRepository()
	repository.versions[contentTestKey("content_story_001", 1)] = &contentdomain.PackageVersion{
		PackageID:      "content_story_001",
		PackageVersion: 1,
		Title:          "月亮晚安故事",
		Category:       contentdomain.CategoryStory,
		AgeTiers:       []contentdomain.AgeTier{contentdomain.AgeTier3To4},
		Status:         contentdomain.StatusPublished,
	}
	service, err := contentservice.New(contentservice.Options{
		Repository:  repository,
		AssetReader: contentTestAssetReader{},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	handler := contentHandler{service: service}

	authed := func(method string, target string) *http.Request {
		request := httptest.NewRequest(method, target, nil)
		request.SetPathValue("package_id", "content_story_001")
		return request.WithContext(context.WithValue(
			request.Context(),
			authenticatedAccountKey{},
			"parent-001",
		))
	}

	// Add.
	recorder := httptest.NewRecorder()
	handler.addFavorite(recorder, authed(http.MethodPut, "/api/v1/content/favorites/content_story_001"))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("add favorite status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	// Repeat add is idempotent (200, not 409).
	recorder = httptest.NewRecorder()
	handler.addFavorite(recorder, authed(http.MethodPut, "/api/v1/content/favorites/content_story_001"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("repeat favorite status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	// List.
	recorder = httptest.NewRecorder()
	handler.listFavorites(recorder, authed(http.MethodGet, "/api/v1/content/favorites"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list favorite status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var listEnvelope struct {
		Data struct {
			Favorites []struct {
				PackageID string `json:"package_id"`
				Available bool   `json:"available"`
			} `json:"favorites"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listEnvelope.Data.Favorites) != 1 ||
		!listEnvelope.Data.Favorites[0].Available {
		t.Fatalf("unexpected favorite list: %s", recorder.Body.String())
	}

	// Remove.
	recorder = httptest.NewRecorder()
	handler.removeFavorite(recorder, authed(http.MethodDelete, "/api/v1/content/favorites/content_story_001"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("remove favorite status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestContentFavoriteRequiresAuthentication(t *testing.T) {
	handler := newContentHandlerForTest(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/content/favorites", nil)
	recorder := httptest.NewRecorder()
	handler.listFavorites(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestDevicePlaybackRequiresMatchingDeviceSession(t *testing.T) {
	handler := deviceContentHandler{
		contentHandler: newContentHandlerForTest(t),
		bindingService: contentTestBindingService{sessionDeviceID: "sprout_device_001"},
		runtimeService: contentTestRuntimeService{familyID: "family-001"},
	}
	for _, path := range []string{
		"/api/v1/devices/sprout_device_001/content/playback",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.SetPathValue("device_id", "sprout_device_002")
		recorder := httptest.NewRecorder()
		handler.listPlayHistory(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
		}
	}
}

// contentTestDeviceLister returns a fixed set of owned devices.
type contentTestDeviceLister struct {
	deviceIDs []string
	err       error
}

func (lister contentTestDeviceLister) DeviceIDsForParent(
	_ context.Context,
	_ string,
) ([]string, error) {
	if lister.err != nil {
		return nil, lister.err
	}
	return lister.deviceIDs, nil
}

func TestGuardianPlayHistoryMergesOwnedDevices(t *testing.T) {
	repository := newContentTestRepository()
	service, err := contentservice.New(contentservice.Options{
		Repository:  repository,
		AssetReader: contentTestAssetReader{},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	record := func(deviceID string, seconds int) {
		t.Helper()
		if _, err := service.RecordPlayback(
			context.Background(),
			contentdomain.PlayRecordInput{
				DeviceID:       deviceID,
				PackageID:      "content_story_001",
				PackageVersion: 1,
				Outcome:        contentdomain.PlayOutcomeCompleted,
				PlayedSeconds:  seconds,
			},
		); err != nil {
			t.Fatalf("record playback: %v", err)
		}
	}
	record("sprout_device_001", 1)
	record("sprout_device_002", 2)
	record("sprout_device_009", 9)

	handler := contentHandler{
		service: service,
		guardianDevices: contentTestDeviceLister{
			deviceIDs: []string{"sprout_device_001", "sprout_device_002"},
		},
	}
	authed := func(target string) *http.Request {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		return request.WithContext(context.WithValue(
			request.Context(),
			authenticatedAccountKey{},
			"parent-001",
		))
	}
	decodeHistory := func(body []byte) []struct {
		DeviceID      string `json:"device_id"`
		PlayedSeconds int    `json:"played_seconds"`
	} {
		t.Helper()
		var envelope struct {
			Data struct {
				History []struct {
					DeviceID      string `json:"device_id"`
					PlayedSeconds int    `json:"played_seconds"`
				} `json:"history"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatalf("decode history response: %v", err)
		}
		return envelope.Data.History
	}

	// No device filter: both owned devices merge and the unrelated one is gone.
	recorder := httptest.NewRecorder()
	handler.listPlayHistoryForGuardian(recorder, authed("/api/v1/content/play-history"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	history := decodeHistory(recorder.Body.Bytes())
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2: %s", len(history), recorder.Body.String())
	}
	for _, item := range history {
		if item.DeviceID == "sprout_device_009" {
			t.Fatalf("history leaked unowned device: %s", recorder.Body.String())
		}
	}

	// A device filter narrows to a single owned device.
	recorder = httptest.NewRecorder()
	handler.listPlayHistoryForGuardian(
		recorder,
		authed("/api/v1/content/play-history?device_id=sprout_device_001"),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("filtered status = %d, want %d", recorder.Code, http.StatusOK)
	}
	filtered := decodeHistory(recorder.Body.Bytes())
	if len(filtered) != 1 || filtered[0].DeviceID != "sprout_device_001" {
		t.Fatalf("filtered history = %s, want only sprout_device_001", recorder.Body.String())
	}

	// An unowned device filter is rejected instead of silently returning empty.
	recorder = httptest.NewRecorder()
	handler.listPlayHistoryForGuardian(
		recorder,
		authed("/api/v1/content/play-history?device_id=sprout_device_009"),
	)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unowned device status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestGuardianPlayHistoryRequiresAuthentication(t *testing.T) {
	handler := newContentHandlerForTest(t)
	handler.guardianDevices = contentTestDeviceLister{deviceIDs: []string{"sprout_device_001"}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/content/play-history", nil)
	recorder := httptest.NewRecorder()
	handler.listPlayHistoryForGuardian(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestThemePackAdminLifecycleEndpoints(t *testing.T) {
	repository := newContentTestRepository()
	repository.versions[contentTestKey("content_story_001", 1)] = &contentdomain.PackageVersion{
		PackageID:      "content_story_001",
		PackageVersion: 1,
		Title:          "月亮晚安故事",
		Category:       contentdomain.CategoryStory,
		AgeTiers:       []contentdomain.AgeTier{contentdomain.AgeTier3To4},
		Status:         contentdomain.StatusPublished,
	}
	service, err := contentservice.New(contentservice.Options{
		Repository:  repository,
		AssetReader: contentTestAssetReader{},
	})
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	handler := contentHandler{service: service}

	upsert := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/content/theme-packs/theme_ocean",
		strings.NewReader(
			`{"theme_id":"theme_ocean","title":"海洋主题",`+
				`"summary":"海底世界","age_tiers":["age_3_4"],`+
				`"package_ids":["content_story_001"]}`,
		),
	)
	recorder := httptest.NewRecorder()
	handler.upsertThemePack(recorder, upsert)
	if recorder.Code != http.StatusOK {
		t.Fatalf("upsert theme status = %d: %s", recorder.Code, recorder.Body.String())
	}

	publish := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/content/theme-packs/theme_ocean/publish",
		nil,
	)
	publish.SetPathValue("theme_id", "theme_ocean")
	recorder = httptest.NewRecorder()
	handler.publishThemePack(recorder, publish)
	if recorder.Code != http.StatusOK {
		t.Fatalf("publish theme status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var publishEnvelope struct {
		Data struct {
			ThemePack struct {
				Status string `json:"status"`
			} `json:"theme_pack"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &publishEnvelope); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	if publishEnvelope.Data.ThemePack.Status != "published" {
		t.Fatalf("published theme status = %q", publishEnvelope.Data.ThemePack.Status)
	}

	// The guardian surface lists only published packs.
	guardianRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/content/theme-packs",
		nil,
	)
	recorder = httptest.NewRecorder()
	handler.listPublishedThemePacks(recorder, guardianRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("guardian list status = %d", recorder.Code)
	}
	var listEnvelope struct {
		Data struct {
			ThemePacks []struct {
				ThemeID string `json:"theme_id"`
			} `json:"theme_packs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("decode guardian list: %v", err)
	}
	if len(listEnvelope.Data.ThemePacks) != 1 ||
		listEnvelope.Data.ThemePacks[0].ThemeID != "theme_ocean" {
		t.Fatalf("guardian theme list = %s", recorder.Body.String())
	}

	// A published pack cannot be deleted, so withdraw it first.
	deleteRequest := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/admin/content/theme-packs/theme_ocean",
		nil,
	)
	deleteRequest.SetPathValue("theme_id", "theme_ocean")
	recorder = httptest.NewRecorder()
	handler.deleteThemePack(recorder, deleteRequest)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("delete published theme status = %d, want 404", recorder.Code)
	}
}

func contentFavoriteKey(parentAccountID string, packageID string) string {
	return parentAccountID + "\x00" + packageID
}

func (repository *contentTestRepository) AddFavorite(
	_ context.Context,
	parentAccountID string,
	packageID string,
	now time.Time,
) (contentdomain.Favorite, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := contentFavoriteKey(parentAccountID, packageID)
	if _, exists := repository.favorites[key]; exists {
		return contentdomain.Favorite{}, contentdomain.ErrFavoriteExists
	}
	favorite := contentdomain.Favorite{PackageID: packageID, CreatedAt: now}
	repository.favorites[key] = favorite
	return favorite, nil
}

func (repository *contentTestRepository) RemoveFavorite(
	_ context.Context,
	parentAccountID string,
	packageID string,
) (bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := contentFavoriteKey(parentAccountID, packageID)
	if _, exists := repository.favorites[key]; !exists {
		return false, nil
	}
	delete(repository.favorites, key)
	return true, nil
}

func (repository *contentTestRepository) ListFavorites(
	_ context.Context,
	parentAccountID string,
) ([]contentdomain.FavoriteView, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	views := make([]contentdomain.FavoriteView, 0)
	for key, favorite := range repository.favorites {
		if !strings.HasPrefix(key, parentAccountID+"\x00") {
			continue
		}
		view := contentdomain.FavoriteView{
			PackageID: favorite.PackageID,
			CreatedAt: favorite.CreatedAt,
			AgeTiers:  []contentdomain.AgeTier{},
		}
		for _, version := range repository.versions {
			if version.PackageID == favorite.PackageID &&
				version.Status == contentdomain.StatusPublished {
				view.Title = version.Title
				view.Category = version.Category
				view.AgeTiers = append([]contentdomain.AgeTier(nil), version.AgeTiers...)
				view.PackageVersion = version.PackageVersion
				view.Available = true
				break
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func (repository *contentTestRepository) RecordPlayback(
	_ context.Context,
	input contentdomain.PlayRecordInput,
	limit int,
	now time.Time,
) (contentdomain.PlayRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if limit < 1 {
		limit = 50
	}
	record := contentdomain.PlayRecord{
		ID:             fmt.Sprintf("play-%d", len(repository.history)+1),
		DeviceID:       input.DeviceID,
		PackageID:      input.PackageID,
		PackageVersion: input.PackageVersion,
		Outcome:        input.Outcome,
		PlayedSeconds:  input.PlayedSeconds,
		PlayedAt:       now,
	}
	repository.history = append([]contentdomain.PlayRecord{record}, repository.history...)
	deviceRecords := make([]contentdomain.PlayRecord, 0, len(repository.history))
	for _, item := range repository.history {
		if item.DeviceID == input.DeviceID {
			deviceRecords = append(deviceRecords, item)
		}
	}
	if len(deviceRecords) > limit {
		deviceRecords = deviceRecords[:limit]
	}
	kept := make([]contentdomain.PlayRecord, 0, len(repository.history))
	for _, item := range repository.history {
		if item.DeviceID != input.DeviceID {
			kept = append(kept, item)
		}
	}
	repository.history = append(kept, deviceRecords...)
	return record, nil
}

func (repository *contentTestRepository) ListPlayHistory(
	_ context.Context,
	query contentdomain.PlayHistoryQuery,
) ([]contentdomain.PlayRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	limit := query.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}
	records := make([]contentdomain.PlayRecord, 0)
	for _, item := range repository.history {
		if item.DeviceID != query.DeviceID {
			continue
		}
		records = append(records, item)
		if len(records) >= limit {
			break
		}
	}
	return records, nil
}

func (repository *contentTestRepository) ListThemePacks(
	_ context.Context,
	query contentdomain.ThemePackQuery,
) ([]contentdomain.ThemePack, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	packs := make([]contentdomain.ThemePack, 0, len(repository.themePacks))
	for _, pack := range repository.themePacks {
		if query.Status != "" && string(pack.Status) != query.Status {
			continue
		}
		packs = append(packs, cloneContentTestThemePack(pack))
	}
	return packs, nil
}

func (repository *contentTestRepository) GetThemePack(
	_ context.Context,
	themeID string,
) (*contentdomain.ThemePack, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	pack, exists := repository.themePacks[themeID]
	if !exists {
		return nil, contentdomain.ErrThemePackNotFound
	}
	cloned := cloneContentTestThemePack(pack)
	return &cloned, nil
}

func (repository *contentTestRepository) UpsertThemePack(
	_ context.Context,
	input contentdomain.ThemePackInput,
	now time.Time,
) (*contentdomain.ThemePack, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	existing, exists := repository.themePacks[input.ThemeID]
	if exists && existing.Status != contentdomain.ThemePackDraft {
		return nil, contentdomain.ErrThemeTransitionInvalid
	}
	pack := contentdomain.ThemePack{
		ThemeID:    input.ThemeID,
		Title:      input.Title,
		Summary:    input.Summary,
		AgeTiers:   append([]contentdomain.AgeTier(nil), input.AgeTiers...),
		PackageIDs: append([]string(nil), input.PackageIDs...),
		Status:     contentdomain.ThemePackDraft,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if exists {
		pack.CreatedAt = existing.CreatedAt
	}
	repository.themePacks[input.ThemeID] = pack
	cloned := cloneContentTestThemePack(pack)
	return &cloned, nil
}

func (repository *contentTestRepository) TransitionThemePack(
	_ context.Context,
	themeID string,
	action contentdomain.Action,
	now time.Time,
) (*contentdomain.ThemePack, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	pack, exists := repository.themePacks[themeID]
	if !exists {
		return nil, contentdomain.ErrThemePackNotFound
	}
	switch action {
	case contentdomain.ActionPublish:
		if pack.Status != contentdomain.ThemePackDraft {
			return nil, contentdomain.ErrThemeTransitionInvalid
		}
		publishedAt := now
		pack.Status = contentdomain.ThemePackPublished
		pack.CatalogRevision = repository.revision
		pack.PublishedAt = &publishedAt
	case contentdomain.ActionWithdraw:
		if pack.Status != contentdomain.ThemePackPublished {
			return nil, contentdomain.ErrThemeTransitionInvalid
		}
		pack.Status = contentdomain.ThemePackWithdrawn
		pack.CatalogRevision = 0
	default:
		return nil, contentdomain.ErrThemeTransitionInvalid
	}
	pack.UpdatedAt = now
	repository.themePacks[themeID] = pack
	cloned := cloneContentTestThemePack(pack)
	return &cloned, nil
}

func (repository *contentTestRepository) DeleteThemePack(
	_ context.Context,
	themeID string,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	pack, exists := repository.themePacks[themeID]
	if !exists || pack.Status == contentdomain.ThemePackPublished {
		return contentdomain.ErrThemePackNotFound
	}
	delete(repository.themePacks, themeID)
	return nil
}

func cloneContentTestThemePack(
	pack contentdomain.ThemePack,
) contentdomain.ThemePack {
	cloned := pack
	cloned.AgeTiers = append([]contentdomain.AgeTier(nil), pack.AgeTiers...)
	cloned.PackageIDs = append([]string(nil), pack.PackageIDs...)
	if pack.PublishedAt != nil {
		publishedAt := *pack.PublishedAt
		cloned.PublishedAt = &publishedAt
	}
	return cloned
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

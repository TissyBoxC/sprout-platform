package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ui_textdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
	ui_textservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/service"
)

// uiTextTestRepository is an in-memory double so handler tests exercise the
// real service validation and revision bookkeeping without a database.
type uiTextTestRepository struct {
	mu       sync.Mutex
	packages map[string]*ui_textdomain.PackageDetail
	revision int64
}

func newUITextTestRepository() *uiTextTestRepository {
	return &uiTextTestRepository{packages: map[string]*ui_textdomain.PackageDetail{}}
}

func uiTextTestKey(
	packageID string,
	language string,
	platform ui_textdomain.Platform,
	version int,
) string {
	return packageID + "|" + language + "|" + string(platform) + "|" + itoaUIText(version)
}

func itoaUIText(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func (repository *uiTextTestRepository) NextVersion(
	_ context.Context,
	packageID string,
	language string,
	platform ui_textdomain.Platform,
) (int, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	highest := 0
	for _, detail := range repository.packages {
		if detail.Package.PackageID == packageID &&
			detail.Package.Language == language &&
			detail.Package.Platform == platform &&
			detail.Package.PackageVersion > highest {
			highest = detail.Package.PackageVersion
		}
	}
	return highest + 1, nil
}

func (repository *uiTextTestRepository) CreateVersion(
	_ context.Context,
	input ui_textdomain.DraftInput,
	packageVersion int,
	actorID string,
	now time.Time,
) (*ui_textdomain.PackageDetail, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	detail := &ui_textdomain.PackageDetail{
		Package: ui_textdomain.Package{
			PackageID:        input.PackageID,
			PackageVersion:   packageVersion,
			Language:         input.Language,
			Platform:         input.Platform,
			MinClientVersion: input.MinClientVersion,
			Status:           ui_textdomain.StatusDraft,
			CreatedBy:        actorID,
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		Entries: append([]ui_textdomain.Entry(nil), input.Entries...),
	}
	repository.packages[uiTextTestKey(
		input.PackageID,
		input.Language,
		input.Platform,
		packageVersion,
	)] = detail
	return detail, nil
}

func (repository *uiTextTestRepository) UpdateDraft(
	_ context.Context,
	input ui_textdomain.DraftInput,
	packageVersion int,
	_ time.Time,
) (*ui_textdomain.PackageDetail, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	detail, ok := repository.packages[uiTextTestKey(
		input.PackageID,
		input.Language,
		input.Platform,
		packageVersion,
	)]
	if !ok {
		return nil, ui_textdomain.ErrPackageNotFound
	}
	if detail.Package.Status != ui_textdomain.StatusDraft {
		return nil, ui_textdomain.ErrInvalidTransition
	}
	detail.Entries = append([]ui_textdomain.Entry(nil), input.Entries...)
	detail.Package.MinClientVersion = input.MinClientVersion
	return detail, nil
}

func (repository *uiTextTestRepository) GetVersion(
	_ context.Context,
	packageID string,
	language string,
	platform ui_textdomain.Platform,
	packageVersion int,
) (*ui_textdomain.PackageDetail, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	detail, ok := repository.packages[uiTextTestKey(
		packageID,
		language,
		platform,
		packageVersion,
	)]
	if !ok {
		return nil, ui_textdomain.ErrPackageNotFound
	}
	copied := *detail
	copied.Entries = append([]ui_textdomain.Entry(nil), detail.Entries...)
	return &copied, nil
}

func (repository *uiTextTestRepository) ListVersions(
	_ context.Context,
	filter ui_textdomain.VersionFilter,
) ([]ui_textdomain.PackageVersion, int, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	items := make([]ui_textdomain.PackageVersion, 0, len(repository.packages))
	for _, detail := range repository.packages {
		if filter.Status != "" && detail.Package.Status != filter.Status {
			continue
		}
		items = append(items, ui_textdomain.PackageVersion{
			Package:    detail.Package,
			EntryCount: len(detail.Entries),
		})
	}
	return items, len(items), nil
}

func (repository *uiTextTestRepository) ApplyTransition(
	_ context.Context,
	transition ui_textdomain.TransitionInput,
	now time.Time,
) (*ui_textdomain.Package, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	detail, ok := repository.packages[uiTextTestKey(
		transition.PackageID,
		transition.Language,
		transition.Platform,
		transition.PackageVersion,
	)]
	if !ok {
		return nil, ui_textdomain.ErrPackageNotFound
	}
	switch transition.Action {
	case ui_textdomain.ActionSubmit:
		detail.Package.Status = ui_textdomain.StatusInReview
		detail.Package.SubmittedAt = &now
	case ui_textdomain.ActionApprove:
		detail.Package.ApprovedAt = &now
		detail.Package.ApprovedBy = transition.ActorID
	case ui_textdomain.ActionReject:
		detail.Package.Status = ui_textdomain.StatusDraft
	case ui_textdomain.ActionPublish:
		for _, other := range repository.packages {
			if other.Package.PackageID == transition.PackageID &&
				other.Package.Language == transition.Language &&
				other.Package.Platform == transition.Platform &&
				other.Package.Status == ui_textdomain.StatusPublished {
				other.Package.Status = ui_textdomain.StatusWithdrawn
				other.Package.WithdrawnAt = &now
				repository.revision++
				other.Package.Revision = repository.revision
			}
		}
		repository.revision++
		detail.Package.Status = ui_textdomain.StatusPublished
		detail.Package.PublishedAt = &now
		detail.Package.PublishedBy = transition.ActorID
		detail.Package.SignatureKeyID = transition.SignatureKeyID
		detail.Package.Signature = transition.Signature
		detail.Package.Revision = repository.revision
	case ui_textdomain.ActionWithdraw:
		repository.revision++
		detail.Package.Status = ui_textdomain.StatusWithdrawn
		detail.Package.WithdrawnAt = &now
		detail.Package.Revision = repository.revision
	}
	detail.Package.UpdatedAt = now
	copied := detail.Package
	return &copied, nil
}

func (repository *uiTextTestRepository) CurrentRevision(
	_ context.Context,
) (int64, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.revision, nil
}

func (repository *uiTextTestRepository) ListPublished(
	_ context.Context,
	sinceRevision int64,
	_ int,
) (ui_textdomain.RevisionPage, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if sinceRevision > repository.revision {
		return ui_textdomain.RevisionPage{}, ui_textdomain.ErrRevisionAhead
	}
	page := ui_textdomain.RevisionPage{Revision: repository.revision}
	for _, detail := range repository.packages {
		if detail.Package.Status == ui_textdomain.StatusPublished &&
			detail.Package.Revision > sinceRevision {
			page.Packages = append(page.Packages, detail.Package)
		}
	}
	return page, nil
}

func (repository *uiTextTestRepository) PublishedPackage(
	_ context.Context,
	packageID string,
	language string,
	platform ui_textdomain.Platform,
) (*ui_textdomain.PublishedPackage, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, detail := range repository.packages {
		if detail.Package.PackageID == packageID &&
			detail.Package.Language == language &&
			detail.Package.Platform == platform &&
			detail.Package.Status == ui_textdomain.StatusPublished {
			return &ui_textdomain.PublishedPackage{
				Package: detail.Package,
				Entries: append([]ui_textdomain.Entry(nil), detail.Entries...),
			}, nil
		}
	}
	return nil, ui_textdomain.ErrPackageNotFound
}

func newUITextHandlerForTest(t *testing.T) (uiTextHandler, *uiTextTestRepository) {
	t.Helper()
	repository := newUITextTestRepository()
	service, err := ui_textservice.New(ui_textservice.Options{
		Repository: repository,
		Verifier:   passthroughUITextVerifier{},
	})
	if err != nil {
		t.Fatalf("create ui text service: %v", err)
	}
	return uiTextHandler{service: service}, repository
}

// passthroughUITextVerifier accepts any well-formed signature so handler tests
// exercise the publish path without generating real Ed25519 material.
type passthroughUITextVerifier struct{}

func (passthroughUITextVerifier) Verify(
	_ string,
	_ string,
	_ string,
	_ string,
) error {
	return nil
}

func uiTextRequest(
	method string,
	target string,
	body string,
	accountID string,
	pathValues map[string]string,
) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	for key, value := range pathValues {
		request.SetPathValue(key, value)
	}
	if accountID == "" {
		return request
	}
	return request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		accountID,
	))
}

const uiTextDraftBody = `{
	"package_id": "home_welcome",
	"language": "zh-CN",
	"platform": "parent_app",
	"min_client_version": "1.0.0",
	"entries": [
		{"ui_text_key": "home.title", "value": "欢迎回家", "max_length": 40, "variables": []}
	]
}`

func TestUITextAdminLifecycleReachesPublished(t *testing.T) {
	handler, _ := newUITextHandlerForTest(t)

	create := httptest.NewRecorder()
	handler.createDraft(
		create,
		uiTextRequest(
			http.MethodPost,
			"/api/v1/admin/ui-text/packages/drafts",
			uiTextDraftBody,
			"account-1",
			nil,
		),
	)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s",
			create.Code, http.StatusCreated, create.Body.String())
	}

	pathValues := map[string]string{
		"package_id":      "home_welcome",
		"language":        "zh-CN",
		"platform":        "parent_app",
		"package_version": "1",
	}
	for _, action := range []string{"submit", "approve"} {
		recorder := httptest.NewRecorder()
		request := uiTextRequest(
			http.MethodPost,
			"/api/v1/admin/ui-text/packages/home_welcome/zh-CN/parent_app/1/"+action,
			"",
			"account-1",
			pathValues,
		)
		handler.applyAction(recorder, request, ui_textdomain.Action(action))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d: %s",
				action, recorder.Code, http.StatusOK, recorder.Body.String())
		}
	}

	publish := httptest.NewRecorder()
	handler.applyAction(
		publish,
		uiTextRequest(
			http.MethodPost,
			"/api/v1/admin/ui-text/packages/home_welcome/zh-CN/parent_app/1/publish",
			`{"signature_key_id":"release-2026","signature":"c2lnbmF0dXJl"}`,
			"account-1",
			pathValues,
		),
		ui_textdomain.ActionPublish,
	)
	if publish.Code != http.StatusOK {
		t.Fatalf("publish status = %d, want %d: %s",
			publish.Code, http.StatusOK, publish.Body.String())
	}

	// A client pull at revision 0 must report the freshly published package.
	page := httptest.NewRecorder()
	handler.published(
		page,
		uiTextRequest(
			http.MethodGet,
			"/api/v1/ui-text/packages?since_revision=0",
			"",
			"account-1",
			nil,
		),
	)
	if page.Code != http.StatusOK {
		t.Fatalf("published status = %d, want %d: %s",
			page.Code, http.StatusOK, page.Body.String())
	}
	var payload struct {
		Data ui_textdomain.RevisionPage `json:"data"`
	}
	if err := json.Unmarshal(page.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode published page: %v", err)
	}
	if len(payload.Data.Packages) != 1 {
		t.Fatalf("published packages = %d, want 1", len(payload.Data.Packages))
	}
}

func TestUITextPublishRequiresSignature(t *testing.T) {
	handler, repository := newUITextHandlerForTest(t)
	repository.packages[uiTextTestKey(
		"home_welcome",
		"zh-CN",
		ui_textdomain.PlatformParentApp,
		1,
	)] = &ui_textdomain.PackageDetail{
		Package: ui_textdomain.Package{
			PackageID:      "home_welcome",
			PackageVersion: 1,
			Language:       "zh-CN",
			Platform:       ui_textdomain.PlatformParentApp,
			Status:         ui_textdomain.StatusInReview,
		},
	}
	recorder := httptest.NewRecorder()
	handler.applyAction(
		recorder,
		uiTextRequest(
			http.MethodPost,
			"/api/v1/admin/ui-text/packages/home_welcome/zh-CN/parent_app/1/publish",
			`{"signature_key_id":"","signature":""}`,
			"account-1",
			map[string]string{
				"package_id":      "home_welcome",
				"language":        "zh-CN",
				"platform":        "parent_app",
				"package_version": "1",
			},
		),
		ui_textdomain.ActionPublish,
	)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
}

func TestUITextCreateRejectsInvalidEntry(t *testing.T) {
	handler, _ := newUITextHandlerForTest(t)
	recorder := httptest.NewRecorder()
	handler.createDraft(
		recorder,
		uiTextRequest(
			http.MethodPost,
			"/api/v1/admin/ui-text/packages/drafts",
			`{"package_id":"home_welcome","language":"zh-CN","platform":"parent_app",
			  "entries":[{"ui_text_key":"bad","value":"x","max_length":5,"variables":[]}]}`,
			"account-1",
			nil,
		),
	)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
}

func TestUITextPublishedRejectsRevisionAhead(t *testing.T) {
	handler, _ := newUITextHandlerForTest(t)
	recorder := httptest.NewRecorder()
	handler.published(
		recorder,
		uiTextRequest(
			http.MethodGet,
			"/api/v1/ui-text/packages?since_revision=99",
			"",
			"account-1",
			nil,
		),
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

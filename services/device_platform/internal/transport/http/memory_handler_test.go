package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	memorydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/domain"
	memoryrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/repository"
	memoryservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/service"
)

// memoryServiceStub is a scripted memory service so the handler tests exercise
// the wire shape without a database.
type memoryServiceStub struct {
	memories      []memorydomain.ChildMemory
	page          memorydomain.MemoryPage
	summaries     []memoryrepository.FamilyMemorySummary
	stored        domainMemoryPointer
	revision      int64
	deletedCount  int64
	err           error
	lastFamilyID  string
	lastChildID   string
	lastMemoryKey string
	lastSince     int64
	lastInputs    []memorydomain.MemoryInput
}

// domainMemoryPointer keeps the pointer field in one place so the stub stays
// readable.
type domainMemoryPointer = *memorydomain.ChildMemory

func (stub *memoryServiceStub) ListForGuardian(
	_ context.Context,
	familyID string,
	childID string,
) ([]memorydomain.ChildMemory, error) {
	stub.lastFamilyID = familyID
	stub.lastChildID = childID
	return stub.memories, stub.err
}

func (stub *memoryServiceStub) SummariesForFamily(
	_ context.Context,
	familyID string,
) ([]memoryrepository.FamilyMemorySummary, error) {
	stub.lastFamilyID = familyID
	return stub.summaries, stub.err
}

func (stub *memoryServiceStub) SetForGuardian(
	_ context.Context,
	familyID string,
	childID string,
	input memorydomain.MemoryInput,
) (*memorydomain.ChildMemory, error) {
	stub.lastFamilyID = familyID
	stub.lastChildID = childID
	stub.lastInputs = append(stub.lastInputs, input)
	if stub.err != nil {
		return nil, stub.err
	}
	return stub.stored, nil
}

func (stub *memoryServiceStub) DeleteForGuardian(
	_ context.Context,
	familyID string,
	childID string,
	memoryKey string,
) (int64, error) {
	stub.lastFamilyID = familyID
	stub.lastChildID = childID
	stub.lastMemoryKey = memoryKey
	return stub.revision, stub.err
}

func (stub *memoryServiceStub) DeleteAllForGuardian(
	_ context.Context,
	familyID string,
	childID string,
) (int64, error) {
	stub.lastFamilyID = familyID
	stub.lastChildID = childID
	return stub.deletedCount, stub.err
}

func (stub *memoryServiceStub) SyncForDevice(
	_ context.Context,
	familyID string,
	childID string,
	sinceRevision int64,
) (memorydomain.MemoryPage, error) {
	stub.lastFamilyID = familyID
	stub.lastChildID = childID
	stub.lastSince = sinceRevision
	return stub.page, stub.err
}

func (stub *memoryServiceStub) UpsertForDevice(
	_ context.Context,
	familyID string,
	childID string,
	inputs []memorydomain.MemoryInput,
) ([]memorydomain.ChildMemory, error) {
	stub.lastFamilyID = familyID
	stub.lastChildID = childID
	stub.lastInputs = append(stub.lastInputs, inputs...)
	return stub.memories, stub.err
}

func (stub *memoryServiceStub) CurrentRevisionForDevice(
	_ context.Context,
	_ string,
	_ string,
) (int64, error) {
	return stub.revision, stub.err
}

type memoryFamilyStub struct {
	familyID string
	err      error
}

func (stub memoryFamilyStub) ResolveDeviceFamily(
	_ context.Context,
	_ string,
	_ string,
) (string, error) {
	return stub.familyID, stub.err
}

type memoryChildStub struct {
	childID string
	err     error
}

func (stub memoryChildStub) ResolveFamilyChild(
	_ context.Context,
	_ string,
	_ string,
) (string, error) {
	return stub.childID, stub.err
}

func sampleMemory(childID string) memorydomain.ChildMemory {
	moment := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	return memorydomain.ChildMemory{
		ID:               "memory-1",
		FamilyID:         "family-1",
		ChildID:          childID,
		MemoryKey:        "pet_name",
		MemoryValue:      "咪咪",
		Confidence:       1,
		Source:           memorydomain.SourceGuardian,
		Revision:         3,
		CreatedAt:        moment,
		UpdatedAt:        moment,
		LastReinforcedAt: moment,
	}
}

func TestMemoryListRequiresFamilyOwnership(t *testing.T) {
	stub := &memoryServiceStub{memories: []memorydomain.ChildMemory{sampleMemory("child-1")}}
	handler := memoryHandler{service: stub}
	// The caller is authenticated as family-1 but asks for family-2.
	request := englishRequest(
		http.MethodGet,
		"/api/v1/families/family-2/children/child-1/memories",
		"",
		"family-1",
	)
	recorder := httptest.NewRecorder()

	handler.list(recorder, request, "family-2")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for another family", recorder.Code)
	}
	if stub.lastFamilyID != "" {
		t.Fatal("the service must not be called for another family")
	}
}

func TestMemoryListReturnsValuesForTheOwnFamily(t *testing.T) {
	stub := &memoryServiceStub{memories: []memorydomain.ChildMemory{sampleMemory("child-1")}}
	handler := memoryHandler{service: stub}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/families/family-1/children/child-1/memories",
		"",
		"family-1",
	)
	request.SetPathValue("child_id", "child-1")
	recorder := httptest.NewRecorder()

	handler.list(recorder, request, "family-1")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			ChildID  string `json:"child_id"`
			Memories []struct {
				MemoryKey   string `json:"memory_key"`
				MemoryValue string `json:"memory_value"`
				Revision    int64  `json:"revision"`
			} `json:"memories"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.ChildID != "child-1" || len(payload.Data.Memories) != 1 {
		t.Fatalf("unexpected payload: %+v", payload.Data)
	}
	if payload.Data.Memories[0].MemoryValue != "咪咪" {
		t.Fatal("the guardian view must include the stored value")
	}
}

func TestMemoryOverviewNeverReturnsValues(t *testing.T) {
	stub := &memoryServiceStub{
		summaries: []memoryrepository.FamilyMemorySummary{
			{
				ChildID: "child-1",
				Memories: []memorydomain.MemorySummary{
					{MemoryKey: "pet_name", Source: memorydomain.SourceGuardian, Confidence: 1},
				},
			},
		},
	}
	handler := memoryHandler{service: stub}
	request := englishRequest(http.MethodGet, "/api/v1/families/family-1/memories", "", "family-1")
	recorder := httptest.NewRecorder()

	handler.overview(recorder, request, "family-1")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, _ := payload["data"].(map[string]any)
	children, _ := data["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("children = %v, want one child", children)
	}
	child, _ := children[0].(map[string]any)
	memories, _ := child["memories"].([]any)
	first, _ := memories[0].(map[string]any)
	if _, ok := first["memory_value"]; ok {
		t.Fatal("the family overview must never expose memory values")
	}
}

func TestMemorySetPassesPathChildAndBodyOnly(t *testing.T) {
	stored := sampleMemory("child-1")
	stub := &memoryServiceStub{stored: &stored}
	handler := memoryHandler{service: stub}
	request := englishRequest(
		http.MethodPut,
		"/api/v1/families/family-1/children/child-1/memories",
		`{"memory_key":"pet_name","memory_value":"咪咪"}`,
		"family-1",
	)
	request.SetPathValue("child_id", "child-1")
	recorder := httptest.NewRecorder()

	handler.set(recorder, request, "family-1")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if stub.lastChildID != "child-1" || stub.lastFamilyID != "family-1" {
		t.Fatalf("last ids = %s/%s", stub.lastFamilyID, stub.lastChildID)
	}
	if len(stub.lastInputs) != 1 || stub.lastInputs[0].MemoryKey != "pet_name" {
		t.Fatalf("inputs = %+v", stub.lastInputs)
	}
}

func TestMemoryLimitExceededReturnsStableCode(t *testing.T) {
	stub := &memoryServiceStub{err: memorydomain.ErrMemoryLimitExceeded}
	handler := memoryHandler{service: stub}
	request := englishRequest(
		http.MethodPut,
		"/api/v1/families/family-1/children/child-1/memories",
		`{"memory_key":"pet_name","memory_value":"咪咪"}`,
		"family-1",
	)
	request.SetPathValue("child_id", "child-1")
	recorder := httptest.NewRecorder()

	handler.set(recorder, request, "family-1")

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	if !containsErrorCode(recorder.Body.Bytes(), "memory_limit_exceeded") {
		t.Fatalf("body = %s, want memory_limit_exceeded", recorder.Body.String())
	}
}

func TestMemoryDeleteReturnsAdvancedRevision(t *testing.T) {
	stub := &memoryServiceStub{revision: 9}
	handler := memoryHandler{service: stub}
	request := englishRequest(
		http.MethodDelete,
		"/api/v1/families/family-1/children/child-1/memories/pet_name",
		"",
		"family-1",
	)
	request.SetPathValue("child_id", "child-1")
	request.SetPathValue("memory_key", "pet_name")
	recorder := httptest.NewRecorder()

	handler.remove(recorder, request, "family-1")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var payload struct {
		Data struct {
			Deleted  bool  `json:"deleted"`
			Revision int64 `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Data.Deleted || payload.Data.Revision != 9 {
		t.Fatalf("payload = %+v", payload.Data)
	}
}

func TestMemoryDeleteMissingKeyIsNotFound(t *testing.T) {
	stub := &memoryServiceStub{err: memorydomain.ErrMemoryNotFound}
	handler := memoryHandler{service: stub}
	request := englishRequest(
		http.MethodDelete,
		"/api/v1/families/family-1/children/child-1/memories/pet_name",
		"",
		"family-1",
	)
	request.SetPathValue("child_id", "child-1")
	request.SetPathValue("memory_key", "pet_name")
	recorder := httptest.NewRecorder()

	handler.remove(recorder, request, "family-1")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestDeviceSyncResolvesChildFromBinding(t *testing.T) {
	stub := &memoryServiceStub{
		page: memorydomain.MemoryPage{
			ChildID:      "child-1",
			NextRevision: 4,
			Memories:     []memorydomain.ChildMemory{sampleMemory("child-1")},
		},
	}
	handler := deviceMemoryHandler{
		handler:        stub,
		familyResolver: memoryFamilyStub{familyID: "family-1"},
		childResolver:  memoryChildStub{childID: "child-1"},
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/device_abc123/memories?since_revision=2",
		nil,
	)
	request.Header.Set("Authorization", "Bearer session-token")
	recorder := httptest.NewRecorder()

	handler.syncForDevice(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if stub.lastFamilyID != "family-1" || stub.lastChildID != "child-1" || stub.lastSince != 2 {
		t.Fatalf("resolved = %s/%s since %d", stub.lastFamilyID, stub.lastChildID, stub.lastSince)
	}
}

func TestDeviceSyncRejectsMalformedRevision(t *testing.T) {
	handler := deviceMemoryHandler{
		handler:        &memoryServiceStub{},
		familyResolver: memoryFamilyStub{familyID: "family-1"},
		childResolver:  memoryChildStub{childID: "child-1"},
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/device_abc123/memories?since_revision=abc",
		nil,
	)
	request.Header.Set("Authorization", "Bearer session-token")
	recorder := httptest.NewRecorder()

	handler.syncForDevice(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
}

func TestDeviceSyncWithoutChildIsNotFound(t *testing.T) {
	handler := deviceMemoryHandler{
		handler:        &memoryServiceStub{},
		familyResolver: memoryFamilyStub{familyID: "family-1"},
		childResolver:  memoryChildStub{},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices/device_abc123/memories", nil)
	request.Header.Set("Authorization", "Bearer session-token")
	recorder := httptest.NewRecorder()

	handler.syncForDevice(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestDeviceSyncAmbiguousFamilyIsConflict(t *testing.T) {
	handler := deviceMemoryHandler{
		handler:        &memoryServiceStub{},
		familyResolver: memoryFamilyStub{familyID: "family-1"},
		childResolver:  memoryChildStub{err: memoryrepository.ErrDeviceChildAmbiguous},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices/device_abc123/memories", nil)
	request.Header.Set("Authorization", "Bearer session-token")
	recorder := httptest.NewRecorder()

	handler.syncForDevice(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
}

func TestDeviceUpsertRejectsEmptyBatch(t *testing.T) {
	stub := &memoryServiceStub{}
	handler := deviceMemoryHandler{
		handler:        stub,
		familyResolver: memoryFamilyStub{familyID: "family-1"},
		childResolver:  memoryChildStub{childID: "child-1"},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/devices/device_abc123/memories",
		jsonBody(`{"memories":[]}`),
	)
	request.Header.Set("Authorization", "Bearer session-token")
	recorder := httptest.NewRecorder()

	handler.upsertForDevice(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
	if len(stub.lastInputs) != 0 {
		t.Fatal("an empty batch must not reach the service")
	}
}

func TestDeviceUpsertStoresBatchAndReturnsRevision(t *testing.T) {
	stored := sampleMemory("child-1")
	stub := &memoryServiceStub{memories: []memorydomain.ChildMemory{stored}, revision: 7}
	handler := deviceMemoryHandler{
		handler:        stub,
		familyResolver: memoryFamilyStub{familyID: "family-1"},
		childResolver:  memoryChildStub{childID: "child-1"},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/devices/device_abc123/memories",
		jsonBody(`{"memories":[{"memory_key":"pet_name","memory_value":"咪咪","confidence":0.8}]}`),
	)
	request.Header.Set("Authorization", "Bearer session-token")
	recorder := httptest.NewRecorder()

	handler.upsertForDevice(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if len(stub.lastInputs) != 1 || stub.lastInputs[0].Confidence == nil ||
		*stub.lastInputs[0].Confidence != 0.8 {
		t.Fatalf("inputs = %+v", stub.lastInputs)
	}
	var payload struct {
		Data struct {
			Revision int64 `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.Revision != 7 {
		t.Fatalf("revision = %d, want 7", payload.Data.Revision)
	}
}

func TestMemoryRoutesRequireAuthentication(t *testing.T) {
	for _, route := range []struct {
		method string
		target string
	}{
		{"GET", "/api/v1/families/family-1/memories"},
		{"GET", "/api/v1/families/family-1/children/child-1/memories"},
		{"PUT", "/api/v1/families/family-1/children/child-1/memories"},
		{"DELETE", "/api/v1/families/family-1/children/child-1/memories"},
		{"DELETE", "/api/v1/families/family-1/children/child-1/memories/pet_name"},
		{"GET", "/api/v1/devices/device_abc123/memories"},
		{"POST", "/api/v1/devices/device_abc123/memories"},
	} {
		options := newTestRouterOptions()
		options.AuthService = &authservice.Service{}
		options.MemoryService = &memoryservice.Service{}
		options.MemoryChildResolver = memoryChildStub{childID: "child-1"}
		// Device routes are mounted only when a device session verifier exists;
		// without one they answer 404 exactly like an older deployment would.
		options.BindingService = &bindingservice.Service{}
		options.RuntimeService = &runtimeservice.Service{}
		router := NewRouter(options)
		request := httptest.NewRequest(route.method, route.target, nil)
		recorder := httptest.NewRecorder()

		router.ServeHTTP(recorder, request)

		if recorder.Code == http.StatusNotFound || recorder.Code == http.StatusMethodNotAllowed {
			t.Fatalf("%s %s is not registered (status %d)", route.method, route.target, recorder.Code)
		}
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401 without a session", route.method, route.target, recorder.Code)
		}
	}
}

// jsonBody builds a JSON request body without repeating the reader dance in
// every test.
func jsonBody(body string) io.Reader {
	return strings.NewReader(body)
}

// containsErrorCode reports whether the response body carries the stable error
// code. The code is part of the client contract, so it is asserted directly.
func containsErrorCode(body []byte, code string) bool {
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return strings.Contains(string(body), code)
	}
	return payload.Error.Code == code
}

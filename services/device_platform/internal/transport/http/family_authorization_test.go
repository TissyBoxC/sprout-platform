package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

type familyAuthorizerStub struct {
	access    familydomain.Access
	err       error
	calls     int
	authorize func(action familydomain.Action) error
}

type guardianDeviceVerifierStub struct {
	parentID string
	deviceID string
	err      error
	calls    int
}

func (stub *guardianDeviceVerifierStub) DeviceBelongsToParent(
	_ context.Context,
	parentAccountID string,
	deviceID string,
) (bool, error) {
	stub.calls++
	stub.parentID = parentAccountID
	stub.deviceID = deviceID
	if stub.err != nil {
		return false, stub.err
	}
	return parentAccountID == "owner-1" && deviceID == "device-1", nil
}

// withOwnerFamilyContext mirrors the context installed by the family
// authorization middleware for fixture requests that invoke a protected
// handler directly.
func withOwnerFamilyContext(
	request *http.Request,
	accountID string,
) *http.Request {
	requestContext := context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		accountID,
	)
	requestContext = context.WithValue(
		requestContext,
		familyAccessKey{},
		familydomain.Access{
			FamilyID:    accountID,
			ActorUserID: accountID,
			OwnerUserID: accountID,
			Role:        familydomain.RoleOwner,
		},
	)
	return request.WithContext(requestContext)
}

func (stub *familyAuthorizerStub) Authorize(
	_ context.Context,
	_ string,
	action familydomain.Action,
) (familydomain.Access, error) {
	stub.calls++
	if stub.authorize != nil {
		if err := stub.authorize(action); err != nil {
			return familydomain.Access{}, err
		}
	}
	return stub.access, stub.err
}

func TestParentScopedForUsesResolvedOwnerAccount(t *testing.T) {
	authorizer := &familyAuthorizerStub{
		access: familydomain.Access{
			FamilyID:    "family-1",
			ActorUserID: "guardian-1",
			OwnerUserID: "owner-1",
			Role:        familydomain.RoleGuardian,
		},
	}
	var ownerAccountID string
	var resolvedAccess familydomain.Access
	handler := parentScopedFor(
		authorizer,
		familydomain.ActionDeviceRead,
		func(
			_ http.ResponseWriter,
			request *http.Request,
			ownerID string,
		) {
			ownerAccountID = ownerID
			resolvedAccess, _ = authenticatedFamilyAccess(request)
		},
	)
	request := englishRequest(http.MethodGet, "/api/v1/devices", "", "guardian-1")
	recorder := httptest.NewRecorder()

	handler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if ownerAccountID != "owner-1" {
		t.Fatalf("owner account = %q, want owner-1", ownerAccountID)
	}
	if resolvedAccess.FamilyID != "family-1" ||
		resolvedAccess.ActorUserID != "guardian-1" ||
		resolvedAccess.Role != familydomain.RoleGuardian {
		t.Fatalf("unexpected access context: %+v", resolvedAccess)
	}
	if authorizer.calls != 1 {
		t.Fatalf("authorize calls = %d, want 1", authorizer.calls)
	}
}

func TestParentScopedForReturnsForbiddenForViewerWrite(t *testing.T) {
	authorizer := &familyAuthorizerStub{err: familydomain.ErrPermissionDenied}
	called := false
	handler := parentScopedFor(
		authorizer,
		familydomain.ActionVisionExecute,
		func(http.ResponseWriter, *http.Request, string) {
			called = true
		},
	)
	request := englishRequest(
		http.MethodPost,
		"/api/v1/devices/device-1/vision/tasks",
		`{"task_type":"describe"}`,
		"viewer-1",
	)
	recorder := httptest.NewRecorder()

	handler(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if called {
		t.Fatal("viewer request reached the protected handler")
	}
}

func TestParentScopedForRequiresAuthentication(t *testing.T) {
	authorizer := &familyAuthorizerStub{}
	handler := parentScopedFor(
		authorizer,
		familydomain.ActionDeviceRead,
		func(http.ResponseWriter, *http.Request, string) {
			t.Fatal("unauthenticated request reached the protected handler")
		},
	)
	recorder := httptest.NewRecorder()

	handler(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if authorizer.calls != 0 {
		t.Fatalf("authorize calls = %d, want 0", authorizer.calls)
	}
}

func TestParentScopedForFailsClosedWithoutAuthorizer(t *testing.T) {
	called := false
	handler := parentScopedFor(
		nil,
		familydomain.ActionDeviceRead,
		func(http.ResponseWriter, *http.Request, string) {
			called = true
		},
	)
	request := englishRequest(http.MethodGet, "/api/v1/devices", "", "guardian-1")
	recorder := httptest.NewRecorder()

	handler(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if called {
		t.Fatal("request reached the handler without a family authorizer")
	}
}

func TestIsFamilyPermissionDenied(t *testing.T) {
	if !isFamilyPermissionDenied(familydomain.ErrPermissionDenied) {
		t.Fatal("permission error was not recognized")
	}
	if isFamilyPermissionDenied(errors.New("other")) {
		t.Fatal("unrelated error was classified as permission denied")
	}
}

func TestFamilyAccessRejectsIncompleteContext(t *testing.T) {
	request := englishRequest(http.MethodGet, "/api/v1/devices", "", "viewer-1")
	request = request.WithContext(context.WithValue(
		request.Context(),
		familyAccessKey{},
		familydomain.Access{
			FamilyID:    "family-1",
			ActorUserID: "viewer-1",
			Role:        familydomain.RoleViewer,
		},
	))

	if _, ok := authenticatedFamilyAccess(request); ok {
		t.Fatal("incomplete family access was accepted")
	}
}

func TestRequireFamilyActionInvitedGuardianUsesFamilyAndOwnerIDs(t *testing.T) {
	handler, service := newFamilyHandlerForTest()
	service.members["guardian-1"] = familydomain.Member{
		ID:        "member-guardian",
		FamilyID:  "family-1",
		UserID:    "guardian-1",
		Role:      familydomain.RoleGuardian,
		CreatedAt: time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC),
	}
	var access familydomain.Access
	next := func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		resolved, ok := authenticatedFamilyAccess(request)
		if !ok {
			t.Error("family access context is missing")
		}
		access = resolved
		response.WriteHeader(http.StatusNoContent)
	}
	request := englishRequest(http.MethodGet, "/api/v1/devices", "", "guardian-1")
	recorder := httptest.NewRecorder()

	handler.requireFamilyAction(familydomain.ActionDeviceRead, next)(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", recorder.Code, recorder.Body.String())
	}
	if access.FamilyID != "family-1" {
		t.Fatalf("family id = %q, want family-1", access.FamilyID)
	}
	if access.ActorUserID != "guardian-1" {
		t.Fatalf("actor id = %q, want guardian-1", access.ActorUserID)
	}
	if access.OwnerUserID != "owner-1" {
		t.Fatalf("owner id = %q, want owner-1", access.OwnerUserID)
	}
}

func TestGuardianOTAusesFamilyOwnerAndDeviceAccessContext(t *testing.T) {
	verifier := &guardianDeviceVerifierStub{}
	handler := otaHandler{guardianDevices: verifier}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/ota/status",
		nil,
	)
	request.SetPathValue("device_id", "device-1")
	request = request.WithContext(context.WithValue(
		request.Context(),
		familyAccessKey{},
		familydomain.Access{
			FamilyID:    "family-1",
			ActorUserID: "guardian-1",
			OwnerUserID: "owner-1",
			Role:        familydomain.RoleGuardian,
		},
	))
	recorder := httptest.NewRecorder()

	deviceID, parentID, ok := handler.verifyGuardianDevice(recorder, request)

	if !ok || recorder.Code != http.StatusOK {
		t.Fatalf(
			"guardian device verification failed: status=%d body=%s",
			recorder.Code,
			recorder.Body.String(),
		)
	}
	if deviceID != "device-1" || parentID != "owner-1" {
		t.Fatalf("resolved device=%q parent=%q", deviceID, parentID)
	}
	if verifier.calls != 1 ||
		verifier.parentID != "owner-1" ||
		verifier.deviceID != "device-1" {
		t.Fatalf("unexpected verifier call: %+v", verifier)
	}
}

func TestGuardianOTAFailsClosedWithoutFamilyAccess(t *testing.T) {
	verifier := &guardianDeviceVerifierStub{}
	handler := otaHandler{guardianDevices: verifier}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/ota/status",
		nil,
	)
	request.SetPathValue("device_id", "device-1")
	recorder := httptest.NewRecorder()

	if _, _, ok := handler.verifyGuardianDevice(recorder, request); ok {
		t.Fatal("request without family authorization reached device verification")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if verifier.calls != 0 {
		t.Fatalf("verifier calls = %d, want 0", verifier.calls)
	}
}

func TestFamilyRoutesAllowViewerReadAndRejectViewerWrite(t *testing.T) {
	authHandler, token := newPermissionMiddleware(t, &authdomain.ParentAccount{
		ID:     "viewer-1",
		Role:   authdomain.RoleParent,
		Status: "active",
	})
	authorizer := &familyAuthorizerStub{
		access: familydomain.Access{
			FamilyID:    "family-1",
			ActorUserID: "viewer-1",
			OwnerUserID: "owner-1",
			Role:        familydomain.RoleViewer,
		},
	}
	authorizer.authorize = func(
		_ familydomain.Action,
	) error {
		return nil
	}
	mux := http.NewServeMux()
	registerFamilyRead := authHandler.requireAuthentication(
		parentScopedFor(
			authorizer,
			familydomain.ActionChildRead,
			func(
				response http.ResponseWriter,
				request *http.Request,
				_ string,
			) {
				response.WriteHeader(http.StatusNoContent)
			},
		),
	)
	registerFamilyWrite := authHandler.requireAuthentication(
		parentScopedFor(
			authorizer,
			familydomain.ActionChildManage,
			func(
				response http.ResponseWriter,
				request *http.Request,
				_ string,
			) {
				response.WriteHeader(http.StatusNoContent)
			},
		),
	)
	mux.HandleFunc("GET /read", registerFamilyRead)
	mux.HandleFunc("POST /write", registerFamilyWrite)

	readRequest := authenticatedRequest(token)
	readRequest.Method = http.MethodGet
	readRequest.URL.Path = "/read"
	readRecorder := httptest.NewRecorder()
	mux.ServeHTTP(readRecorder, readRequest)
	if readRecorder.Code != http.StatusNoContent {
		t.Fatalf("viewer read status = %d, want 204", readRecorder.Code)
	}

	writeRequest := authenticatedRequest(token)
	writeRequest.Method = http.MethodPost
	writeRequest.URL.Path = "/write"
	writeRecorder := httptest.NewRecorder()
	authorizer.authorize = func(
		action familydomain.Action,
	) error {
		if action == familydomain.ActionChildManage {
			return familydomain.ErrPermissionDenied
		}
		return nil
	}
	mux.ServeHTTP(writeRecorder, writeRequest)
	if writeRecorder.Code != http.StatusForbidden {
		t.Fatalf("viewer write status = %d, want 403", writeRecorder.Code)
	}
}

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	deviceruntimedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
)

// stubAdminParentService records the calls the transport makes so the handler
// contract can be verified without a database or a full auth service.
type stubAdminParentService struct {
	account *authdomain.ParentAccount

	updateProfileCalls int
	lastProfileUpdate  authservice.ProfileUpdate

	resetPasswordCalls int
	lastResetPassword  string

	err error
}

func (stub *stubAdminParentService) CreateParent(
	_ context.Context,
	_ authdomain.RegisterInput,
) (*authdomain.ParentAccount, *authdomain.AIAccountSummary, error) {
	return nil, nil, stub.err
}

func (stub *stubAdminParentService) GetIdentity(
	_ context.Context,
	_ string,
) (*authdomain.ParentAccount, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	return stub.account, nil
}

func (stub *stubAdminParentService) UpdateParentProfile(
	_ context.Context,
	_ string,
	update authservice.ProfileUpdate,
) (*authdomain.ParentAccount, error) {
	stub.updateProfileCalls++
	stub.lastProfileUpdate = update
	if stub.err != nil {
		return nil, stub.err
	}
	account := *stub.account
	account.DisplayName = update.DisplayName
	account.GuardianFamilyName = update.GuardianFamilyName
	account.ChildNickname = update.ChildNickname
	account.ChildBirthday = update.ChildBirthday
	return &account, nil
}

func (stub *stubAdminParentService) ResetParentPassword(
	_ context.Context,
	_ string,
	password string,
) error {
	stub.resetPasswordCalls++
	stub.lastResetPassword = password
	return stub.err
}

// stubAdminDeviceStatusService returns a fixed parent-scoped device list.
type stubAdminDeviceStatusService struct {
	parentAccountID string
	statuses        []deviceruntimedomain.DeviceStatus
	err             error
}

type stubAdminDeviceManagementService struct {
	parentAccountID string
	deviceID        string
	err             error
}

func (stub *stubAdminDeviceStatusService) ListDeviceStatuses(
	_ context.Context,
	parentAccountID string,
) ([]deviceruntimedomain.DeviceStatus, error) {
	stub.parentAccountID = parentAccountID
	if stub.err != nil {
		return nil, stub.err
	}
	return stub.statuses, nil
}

func (stub *stubAdminDeviceManagementService) Delete(
	_ context.Context,
	parentAccountID string,
	deviceID string,
) error {
	stub.parentAccountID = parentAccountID
	stub.deviceID = deviceID
	return stub.err
}

func TestUpdateParentProfileHandlerPassesEditableFields(t *testing.T) {
	parentService := &stubAdminParentService{
		account: &authdomain.ParentAccount{ID: "parent-001"},
	}
	handler := adminHandler{parentService: parentService}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/families/parent-001/profile",
		strings.NewReader(
			`{"display_name":"林家长","guardian_family_name":"林",`+
				`"child_nickname":"小芽","child_birthday":"2020-05-20"}`,
		),
	)
	request.SetPathValue("parent_account_id", "parent-001")
	recorder := httptest.NewRecorder()

	handler.updateParentProfile(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if parentService.updateProfileCalls != 1 {
		t.Fatalf("expected one profile update, got %d", parentService.updateProfileCalls)
	}
	update := parentService.lastProfileUpdate
	if update.DisplayName != "林家长" ||
		update.GuardianFamilyName != "林" ||
		update.ChildNickname != "小芽" ||
		update.ChildBirthday != "2020-05-20" {
		t.Fatalf("unexpected profile update: %+v", update)
	}
}

func TestResetParentPasswordHandlerRejectsWeakPassword(t *testing.T) {
	parentService := &stubAdminParentService{
		account: &authdomain.ParentAccount{ID: "parent-001"},
		err:     authdomain.ErrWeakPassword,
	}
	handler := adminHandler{parentService: parentService}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/families/parent-001/password",
		strings.NewReader(`{"password":"weak"}`),
	)
	request.SetPathValue("parent_account_id", "parent-001")
	recorder := httptest.NewRecorder()

	handler.resetParentPassword(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status %d, got %d", http.StatusUnprocessableEntity, recorder.Code)
	}
	if parentService.lastResetPassword != "weak" {
		t.Fatalf("expected the submitted password to reach the service")
	}
}

func TestListParentDevicesHandlerReturnsOnlineState(t *testing.T) {
	deviceService := &stubAdminDeviceStatusService{
		statuses: []deviceruntimedomain.DeviceStatus{
			{
				DeviceID:   "sprout_device_001",
				DeviceName: "初芽",
				Runtime: &deviceruntimedomain.RuntimeStatus{
					DeviceID: "sprout_device_001",
					IsOnline: true,
				},
			},
		},
	}
	handler := adminHandler{deviceStatusService: deviceService}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families/parent-001/devices",
		nil,
	)
	request.SetPathValue("parent_account_id", "parent-001")
	recorder := httptest.NewRecorder()

	handler.listParentDevices(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if deviceService.parentAccountID != "parent-001" {
		t.Fatalf("expected the device lookup to be parent-scoped, got %q", deviceService.parentAccountID)
	}
	var envelope struct {
		Data struct {
			Devices []struct {
				DeviceID string `json:"device_id"`
				Runtime  *struct {
					IsOnline bool `json:"is_online"`
				} `json:"runtime"`
			} `json:"devices"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode devices response: %v", err)
	}
	if len(envelope.Data.Devices) != 1 ||
		envelope.Data.Devices[0].Runtime == nil ||
		!envelope.Data.Devices[0].Runtime.IsOnline {
		t.Fatalf("expected one online device, got %s", recorder.Body.String())
	}
}

func TestListParentDevicesHandlerReportsServiceUnavailableWithoutRuntime(t *testing.T) {
	handler := adminHandler{}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families/parent-001/devices",
		nil,
	)
	request.SetPathValue("parent_account_id", "parent-001")
	recorder := httptest.NewRecorder()

	handler.listParentDevices(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

func TestUnbindParentDeviceHandlerVerifiesFamilyScope(t *testing.T) {
	deviceService := &stubAdminDeviceManagementService{}
	handler := adminHandler{deviceBindingService: deviceService}
	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/admin/families/parent-001/devices/sprout_device_001",
		nil,
	)
	request.SetPathValue("parent_account_id", "parent-001")
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.unbindParentDevice(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if deviceService.parentAccountID != "parent-001" ||
		deviceService.deviceID != "sprout_device_001" {
		t.Fatalf(
			"expected parent-scoped delete, got parent=%q device=%q",
			deviceService.parentAccountID,
			deviceService.deviceID,
		)
	}
}

func TestUnbindParentDeviceHandlerReportsMissingDevice(t *testing.T) {
	deviceService := &stubAdminDeviceManagementService{
		err: bindingdomain.ErrDeviceNotFound,
	}
	handler := adminHandler{deviceBindingService: deviceService}
	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/admin/families/parent-001/devices/sprout_device_001",
		nil,
	)
	request.SetPathValue("parent_account_id", "parent-001")
	request.SetPathValue("device_id", "sprout_device_001")
	recorder := httptest.NewRecorder()

	handler.unbindParentDevice(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

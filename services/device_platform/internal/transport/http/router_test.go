package http

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/config"
	gatewayservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/service"
	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	releasestoreservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/service"
)

func TestHealthEndpoint(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	NewRouter(newTestRouterOptions()).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusOK, recorder.Code)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID response header")
	}
}

func TestHealthEndpointReturnsTraceParent(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	NewRouter(newTestRouterOptions()).ServeHTTP(recorder, request)

	if recorder.Header().Get("traceparent") == "" {
		t.Fatal("expected traceparent response header")
	}
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
}

func TestInternalAPIIsHiddenWhenDisabled(t *testing.T) {
	request := httptest.NewRequest(stdhttp.MethodGet, "/internal/v1/runtime", nil)
	recorder := httptest.NewRecorder()

	NewRouter(newTestRouterOptions()).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusNotFound, recorder.Code)
	}
}

func TestInternalAPIRejectsMissingServiceToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}

	request := httptest.NewRequest(stdhttp.MethodGet, "/internal/v1/runtime", nil)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
	envelope := decodeEnvelope(t, recorder)
	if envelope.Error == nil || envelope.Error.Code != "unauthenticated" {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
}

func TestInternalAPIReturnsRuntimeEnvelope(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}

	request := httptest.NewRequest(stdhttp.MethodGet, "/internal/v1/runtime", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusOK, recorder.Code)
	}
	envelope := decodeEnvelope(t, recorder)
	if envelope.SchemaVersion != httpapi.SchemaVersion {
		t.Fatalf("unexpected schema version: %q", envelope.SchemaVersion)
	}
	if envelope.RequestID == "" {
		t.Fatal("expected response request ID")
	}
}

func TestInternalAPIContractDocumentsRuntimeEndpoint(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	if !bytes.Contains(contract, []byte("/internal/v1/runtime")) {
		t.Fatal("expected runtime endpoint in internal API contract")
	}
}

// The relay resolves AI credentials by device id, so the contract must keep the
// device-scoped endpoint documented alongside the runtime probe.
func TestInternalAPIContractDocumentsDeviceCredentialEndpoint(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	expected := "/internal/v1/devices/{device_id}/ai-credential"
	if !bytes.Contains(contract, []byte(expected)) {
		t.Fatalf("expected %s in internal API contract", expected)
	}
}

func TestInternalAPIContractDocumentsEffectiveParentPolicyEndpoint(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	expected := "/internal/v1/parent-policies"
	if !bytes.Contains(contract, []byte(expected)) {
		t.Fatalf("expected %s in internal API contract", expected)
	}
	if !bytes.Contains(contract, []byte("EffectiveParentPolicyEnvelope")) {
		t.Fatal("expected effective parent policy schema in internal API contract")
	}
}

func TestFamilyContractDocumentsMembershipEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	expectedPaths := []string{
		"/api/v1/family/members:",
		"/api/v1/family/members/{user_id}:",
		"/api/v1/family/invitations:",
		"/api/v1/family/invitations/mine:",
		"/api/v1/family/invitations/{invitation_id}:",
		"/api/v1/families/{family_id}/invitations/{invitation_id}/accept:",
		"/api/v1/family/capability:",
	}
	for _, path := range expectedPaths {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected family path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"FamilyMember:",
		"FamilyInvitation:",
		"FamilyMemberRoleRequest:",
		"FamilyInvitationRequest:",
		"FamilyCapability:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected family schema %s in internal API contract", schema)
		}
	}
}

func TestChildContractDocumentsProfileAndPolicyEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/children:",
		"/api/v1/children/{child_id}:",
		"/api/v1/children/{child_id}/policy:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected child path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"ChildProfileRequest:",
		"Child:",
		"ParentPolicyRequest:",
		"ParentPolicy:",
		"ParentPolicyVoiceConversation:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected child schema %s in internal API contract", schema)
		}
	}
}

func TestAuthContractDocumentsIdentityAndPermissionEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/auth/register:",
		"/api/v1/auth/login:",
		"/api/v1/auth/phone-verification:",
		"/api/v1/auth/refresh:",
		"/api/v1/auth/logout:",
		"/api/v1/auth/email:",
		"/api/v1/auth/profile:",
		"/api/v1/auth/overview:",
		"/api/v1/auth/me:",
		"/api/v1/auth/ai-models:",
		"/api/v1/admin/auth/login:",
		"/api/v1/admin/auth/mfa:",
		"/api/v1/admin/permissions:",
		"/api/v1/admin/permissions/{account_id}:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected identity path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"RegisterRequest:",
		"LoginRequest:",
		"AdminMFARequest:",
		"UserAccount:",
		"AIAccountSummary:",
		"AuthSessionEnvelope:",
		"AdminPermissionAccountEnvelope:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected identity schema %s in internal API contract", schema)
		}
	}
}

func TestDeviceBindingContractDocumentsLifecycleEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/internal/v1/device-registration-tokens:",
		"/api/v1/devices/register:",
		"/api/v1/devices/auth/challenge:",
		"/api/v1/devices/auth/complete:",
		"/api/v1/devices/{device_id}/provisioning-token:",
		"/api/v1/devices/{device_id}/binding-status:",
		"/api/v1/devices/{device_id}/voice-token:",
		"/api/v1/devices/bind:",
		"/api/v1/devices:",
		"/api/v1/devices/{device_id}:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected device binding path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"RegisterDeviceRequest:",
		"DeviceAuthenticationCompleteRequest:",
		"DeviceBinding:",
		"DeviceCredentialEnvelope:",
		"VoiceTokenEnvelope:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected device binding schema %s in internal API contract", schema)
		}
	}
}

func TestDeviceRuntimeContractDocumentsStatusAndCommandEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/devices/status:",
		"/api/v1/devices/{device_id}/status:",
		"/api/v1/devices/{device_id}/runtime/heartbeat:",
		"/api/v1/devices/{device_id}/runtime/commands:",
		"/api/v1/devices/{device_id}/runtime/commands/{command_id}/ack:",
		"/api/v1/devices/{device_id}/runtime/parent-policy:",
		"/api/v1/devices/{device_id}/runtime/usage:",
		"/api/v1/usage-reports:",
		"/api/v1/admin/devices:",
		"/api/v1/admin/devices/{device_id}/commands:",
		"/api/v1/admin/devices/{device_id}/sessions/revoke:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected device runtime path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"DeviceHeartbeatRequest:",
		"DeviceRuntimeStatus:",
		"DeviceStatus:",
		"DeviceCommand:",
		"DeviceUsageUploadRequest:",
		"UsageReport:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected device runtime schema %s in internal API contract", schema)
		}
	}
}

func TestDisplayContractDocumentsPolicyAndStatusEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/display/policy:",
		"/api/v1/display/devices:",
		"/api/v1/devices/{device_id}/display/capability:",
		"/api/v1/devices/{device_id}/display/status:",
		"/api/v1/devices/{device_id}/display/policy:",
		"/api/v1/admin/display/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected display path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"DisplayPolicyRequest:",
		"DisplayPolicy:",
		"DisplayCapability:",
		"DisplayStatusReportRequest:",
		"DisplayAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected display schema %s in internal API contract", schema)
		}
	}
}

func TestScreenContractDocumentsPolicyDictionaryAndPlaybackEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/screen/policy:",
		"/api/v1/screen/devices:",
		"/api/v1/screen/point-read:",
		"/api/v1/screen/point-read/{entry_id}:",
		"/api/v1/devices/{device_id}/screen/capability:",
		"/api/v1/devices/{device_id}/screen/playback:",
		"/api/v1/devices/{device_id}/screen/status:",
		"/api/v1/devices/{device_id}/screen/policy:",
		"/api/v1/admin/screen/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected screen path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"ScreenPolicyRequest:",
		"ScreenPolicyCatalogEnvelope:",
		"ScreenDictionaryEntryRequest:",
		"ScreenPlaybackRequest:",
		"ScreenCapability:",
		"ScreenStatusReportRequest:",
		"ScreenAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected screen schema %s in internal API contract", schema)
		}
	}
}

func TestTouchContractDocumentsConfigAndStatusEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/touch/config:",
		"/api/v1/touch/devices:",
		"/api/v1/devices/{device_id}/touch/capability:",
		"/api/v1/devices/{device_id}/touch/status:",
		"/api/v1/devices/{device_id}/touch/policy:",
		"/api/v1/admin/touch/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected touch path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"TouchConfigRequest:",
		"TouchConfig:",
		"TouchCapability:",
		"TouchStatusReportRequest:",
		"TouchAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected touch schema %s in internal API contract", schema)
		}
	}
}

func TestConnectivityContractDocumentsPolicyAndStatusEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/connectivity/policy:",
		"/api/v1/connectivity/devices:",
		"/api/v1/devices/{device_id}/connectivity/capability:",
		"/api/v1/devices/{device_id}/connectivity/status:",
		"/api/v1/devices/{device_id}/connectivity/policy:",
		"/api/v1/admin/connectivity/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected connectivity path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"ConnectivityPolicyRequest:",
		"ConnectivityPolicy:",
		"ConnectivityDevicePolicy:",
		"ConnectivityCapability:",
		"ConnectivityStatusReportRequest:",
		"ConnectivityAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected connectivity schema %s in internal API contract", schema)
		}
	}
}

func TestPowerContractDocumentsPolicyAndStatusEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/power/policy:",
		"/api/v1/power/devices:",
		"/api/v1/devices/{device_id}/power/capability:",
		"/api/v1/devices/{device_id}/power/status:",
		"/api/v1/devices/{device_id}/power/policy:",
		"/api/v1/admin/power/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected power path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"PowerPolicyRequest:",
		"PowerPolicy:",
		"PowerCapability:",
		"PowerStatusReportRequest:",
		"PowerAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected power schema %s in internal API contract", schema)
		}
	}
}

func TestMotionContractDocumentsPolicyAndStatusEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/motion/policy:",
		"/api/v1/motion/devices:",
		"/api/v1/devices/{device_id}/motion/capability:",
		"/api/v1/devices/{device_id}/motion/status:",
		"/api/v1/devices/{device_id}/motion/policy:",
		"/api/v1/admin/motion/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected motion path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"MotionPolicyRequest:",
		"MotionPolicy:",
		"MotionCapability:",
		"MotionStatusReportRequest:",
		"MotionAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected motion schema %s in internal API contract", schema)
		}
	}
}

func TestFormFactorContractDocumentsProfileEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/form-factor/devices:",
		"/api/v1/devices/{device_id}/form-factor/capability:",
		"/api/v1/devices/{device_id}/form-factor/report:",
		"/api/v1/admin/form-factor/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected form-factor path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"FormFactorProfile:",
		"FormFactorCapabilitySummary:",
		"FormFactorReportRequest:",
		"FormFactorAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected form-factor schema %s in internal API contract", schema)
		}
	}
}

func TestAlarmReminderContractDocumentsAlarmAndEventEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/alarm-reminders:",
		"/api/v1/alarm-reminders/{alarm_id}:",
		"/api/v1/alarm-reminders/events:",
		"/api/v1/devices/{device_id}/alarm-reminders:",
		"/api/v1/devices/{device_id}/alarm-reminders/{alarm_id}/events:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected alarm reminder path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"AlarmReminderRequest:",
		"AlarmReminder:",
		"AlarmEventRequest:",
		"AlarmEvent:",
		"DeviceAlarm:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected alarm reminder schema %s in internal API contract", schema)
		}
	}
}

func TestBluetoothAudioContractDocumentsPolicyAndStatusEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/bluetooth-audio/policy:",
		"/api/v1/bluetooth-audio/devices:",
		"/api/v1/devices/{device_id}/bluetooth-audio/capability:",
		"/api/v1/devices/{device_id}/bluetooth-audio/policy:",
		"/api/v1/devices/{device_id}/bluetooth-audio/status:",
		"/api/v1/admin/bluetooth-audio/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected bluetooth-audio path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"BluetoothAudioPolicyRequest:",
		"BluetoothAudioPolicyCatalogEnvelope:",
		"BluetoothAudioCapability:",
		"BluetoothAudioStatusReportRequest:",
		"BluetoothAudioAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected bluetooth-audio schema %s in internal API contract", schema)
		}
	}
}

func TestWearableContractDocumentsPolicyLocationAndSOSEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/wearable/policy:",
		"/api/v1/wearable/contacts:",
		"/api/v1/wearable/contacts/{contact_id}:",
		"/api/v1/wearable/geofence:",
		"/api/v1/wearable/devices:",
		"/api/v1/devices/{device_id}/wearable/capability:",
		"/api/v1/wearable/sos:",
		"/api/v1/wearable/sos/active:",
		"/api/v1/wearable/sos/{event_id}/cancel:",
		"/api/v1/devices/{device_id}/wearable/location:",
		"/api/v1/devices/{device_id}/wearable/policy:",
		"/api/v1/devices/{device_id}/wearable/sos:",
		"/api/v1/admin/wearable/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected wearable path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"WearablePolicyRequest:",
		"WearableDevicePolicy:",
		"WearableContactRequest:",
		"WearableGeofenceRequest:",
		"WearableDeviceStatus:",
		"WearableSOSEvent:",
		"WearableLocationReportRequest:",
		"WearableAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected wearable schema %s in internal API contract", schema)
		}
	}
}

func TestContentContractDocumentsCatalogFavoritesHistoryAndPolicies(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/content/catalog:",
		"/api/v1/content/packages/{package_id}/download:",
		"/api/v1/content/favorites:",
		"/api/v1/content/favorites/{package_id}:",
		"/api/v1/content/play-history:",
		"/api/v1/content/theme-packs:",
		"/api/v1/content/recommendations:",
		"/api/v1/content/offline-policy:",
		"/api/v1/content/offline-policy/acknowledgements:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected content path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"ContentCatalog:",
		"ContentPackageVersion:",
		"ContentFavorite:",
		"ContentPlayRecord:",
		"ContentThemePack:",
		"ContentRecommendation:",
		"ContentOfflinePolicy:",
		"ContentOfflinePolicyAcknowledgement:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected content schema %s in internal API contract", schema)
		}
	}
}

func TestDeviceMeshContractDocumentsGroupsMessagesAndContentSync(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/device-mesh/groups:",
		"/api/v1/device-mesh/groups/{group_id}:",
		"/api/v1/device-mesh/groups/{group_id}/members:",
		"/api/v1/device-mesh/groups/{group_id}/members/{device_id}:",
		"/api/v1/device-mesh/messages:",
		"/api/v1/device-mesh/content:",
		"/api/v1/admin/device-mesh/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected device-mesh path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"DeviceMeshGroupRequest:",
		"DeviceMeshMemberRequest:",
		"DeviceMeshMessageRequest:",
		"DeviceMeshSyncRequest:",
		"DeviceMeshContentState:",
		"DeviceMeshAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected device-mesh schema %s in internal API contract", schema)
		}
	}
}

func TestVideoCompanionContractDocumentsContactsCallsAndDeviceStatus(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/video-companion/contacts:",
		"/api/v1/video-companion/contacts/{contact_id}:",
		"/api/v1/video-companion/calls:",
		"/api/v1/video-companion/calls/{call_id}:",
		"/api/v1/video-companion/devices:",
		"/api/v1/devices/{device_id}/video-companion/capability:",
		"/api/v1/devices/{device_id}/video-companion/status:",
		"/api/v1/admin/video-companion/stats:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected video-companion path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"VideoCompanionContactRequest:",
		"VideoCompanionCallRequest:",
		"VideoCompanionCallStateRequest:",
		"VideoCompanionDeviceStatus:",
		"VideoCompanionCapability:",
		"VideoCompanionStatusRequest:",
		"VideoCompanionAdminStats:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected video-companion schema %s in internal API contract", schema)
		}
	}
}

func TestCompanionshipContractDocumentsGrowthRecordsAndHabitReminders(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/companionship/growth-records:",
		"/api/v1/companionship/growth-records/{record_id}:",
		"/api/v1/companionship/habit-reminders:",
		"/api/v1/companionship/habit-reminders/{reminder_id}:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected companionship path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"CompanionshipGrowthCategory:",
		"CompanionshipGrowthRecordRequest:",
		"CompanionshipGrowthRecord:",
		"CompanionshipHabitReminderRequest:",
		"CompanionshipHabitReminder:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected companionship schema %s in internal API contract", schema)
		}
	}
}

func TestNotificationContractDocumentsInboxAndAdminEndpoints(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read internal API contract: %v", err)
	}
	for _, path := range []string{
		"/api/v1/notifications:",
		"/api/v1/notifications/unread-count:",
		"/api/v1/notifications/read-all:",
		"/api/v1/notifications/{notification_id}/read:",
		"/api/v1/notifications/family-messages:",
		"/api/v1/notifications/devices/{device_id}/messages:",
		"/api/v1/admin/notifications:",
		"/api/v1/admin/notifications/stats:",
		"/api/v1/admin/notifications/{notification_id}:",
	} {
		if !bytes.Contains(contract, []byte(path)) {
			t.Fatalf("expected notification path %s in internal API contract", path)
		}
	}
	for _, schema := range []string{
		"NotificationCategory:",
		"NotificationSeverity:",
		"NotificationInboxItem:",
		"NotificationInboxEnvelope:",
		"NotificationUnreadCountEnvelope:",
		"NotificationReadEnvelope:",
		"NotificationFamilyMessageRequest:",
		"NotificationDeviceMessage:",
		"NotificationDeviceMessagesEnvelope:",
		"NotificationAdministratorRequest:",
		"NotificationAdminItem:",
		"NotificationAdminPageEnvelope:",
		"NotificationAdminStatsEnvelope:",
	} {
		if !bytes.Contains(contract, []byte(schema)) {
			t.Fatalf("expected notification schema %s in internal API contract", schema)
		}
	}
}

func TestInternalCredentialRouteIsHiddenWhenDisabled(t *testing.T) {
	request := httptest.NewRequest(
		stdhttp.MethodGet,
		"/internal/v1/devices/device-0001/ai-credential",
		nil,
	)
	recorder := httptest.NewRecorder()

	NewRouter(newTestRouterOptions()).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusNotFound, recorder.Code)
	}
}

func TestInternalCredentialRouteRequiresServiceToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	options.BindingService = &bindingservice.Service{}
	options.AIService = &gatewayservice.Service{}

	request := httptest.NewRequest(
		stdhttp.MethodGet,
		"/internal/v1/devices/device-0001/ai-credential",
		nil,
	)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
}

func TestInternalReleaseUploadRequiresReleaseToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:            true,
		AuthToken:          strings.Repeat("t", 32),
		ReleaseUploadToken: strings.Repeat("r", 32),
	}
	options.ReleaseStoreService = new(releasestoreservice.Service)

	request := newInternalReleaseUploadRequest(t)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
}

func TestInternalReleaseUploadRejectsGeneralServiceToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:            true,
		AuthToken:          strings.Repeat("t", 32),
		ReleaseUploadToken: strings.Repeat("r", 32),
	}
	options.ReleaseStoreService = new(releasestoreservice.Service)

	request := newInternalReleaseUploadRequest(t)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
}

func TestInternalReleaseUploadRouteIsHiddenWithoutReleaseToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:   true,
		AuthToken: strings.Repeat("t", 32),
	}
	options.ReleaseStoreService = new(releasestoreservice.Service)

	request := newInternalReleaseUploadRequest(t)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusNotFound {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusNotFound, recorder.Code)
	}
}

func TestPublicReleasePublicationRouteRequiresReleaseToken(t *testing.T) {
	options := newTestRouterOptions()
	options.InternalAPIConfig = config.InternalAPIConfig{
		Enabled:            true,
		AuthToken:          strings.Repeat("t", 32),
		ReleaseUploadToken: strings.Repeat("r", 32),
	}
	options.ReleaseStoreService = new(releasestoreservice.Service)

	request := newInternalReleaseUploadRequest(t)
	request.URL.Path = "/api/v1/release-publication/files"
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", stdhttp.StatusUnauthorized, recorder.Code)
	}
}

type testEnvelope struct {
	SchemaVersion string             `json:"schema_version"`
	RequestID     string             `json:"request_id"`
	Error         *httpapi.ErrorBody `json:"error"`
}

func newInternalReleaseUploadRequest(t *testing.T) *stdhttp.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("version", "0.12.4"); err != nil {
		t.Fatalf("write version field: %v", err)
	}
	if err := writer.WriteField("channel", "stable"); err != nil {
		t.Fatalf("write channel field: %v", err)
	}
	if err := writer.WriteField("platform", "any"); err != nil {
		t.Fatalf("write platform field: %v", err)
	}
	if err := writer.WriteField("kind", "resource"); err != nil {
		t.Fatalf("write kind field: %v", err)
	}
	if err := writer.WriteField("filename", "resource.bin"); err != nil {
		t.Fatalf("write filename field: %v", err)
	}
	filePart, err := writer.CreateFormFile("file", "resource.bin")
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := filePart.Write([]byte("release bytes")); err != nil {
		t.Fatalf("write file field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(
		stdhttp.MethodPost,
		"/internal/v1/release-files",
		&body,
	)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) testEnvelope {
	t.Helper()

	var envelope testEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response envelope: %v", err)
	}
	return envelope
}

func newTestRouterOptions() RouterOptions {
	return RouterOptions{Logger: newTestLogger()}
}

// recordingAudit captures the audit events a handler writes so tests can
// assert the denial and success audit paths without a database.
type recordingAudit struct {
	events []auditEvent
}

type auditEvent struct {
	actorAccountID  string
	targetAccountID string
	action          string
	detail          map[string]any
}

func (audit *recordingAudit) RecordAudit(
	_ context.Context,
	actorAccountID string,
	targetAccountID string,
	action string,
	detail map[string]any,
) error {
	audit.events = append(audit.events, auditEvent{
		actorAccountID:  actorAccountID,
		targetAccountID: targetAccountID,
		action:          action,
		detail:          detail,
	})
	return nil
}

func TestRequireAuthenticationAuditsDeniedRequest(t *testing.T) {
	audit := &recordingAudit{}
	options := newTestRouterOptions()
	options.AuthService = &authservice.Service{}
	options.AuditRecorder = audit

	request := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/auth/me", nil)
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, stdhttp.StatusUnauthorized)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	if audit.events[0].action != "auth.denied" {
		t.Fatalf("audit action = %q, want auth.denied", audit.events[0].action)
	}
	if _, leaked := audit.events[0].detail["token"]; leaked {
		t.Fatalf("denial audit must not record the token")
	}
}

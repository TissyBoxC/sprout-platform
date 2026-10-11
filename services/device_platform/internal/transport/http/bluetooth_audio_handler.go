package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	bluetoothaudiodomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/bluetooth_audio/domain"
	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
)

// bluetoothAudioService is the guardian-facing Bluetooth audio surface. The HTTP
// layer depends on this narrow interface so the module stays removable.
type bluetoothAudioService interface {
	Catalog() ([]string, []string, []string, []string)
	Current(
		ctx context.Context,
		familyID string,
	) (bluetoothaudiodomain.Policy, error)
	Publish(
		ctx context.Context,
		familyID string,
		input bluetoothaudiodomain.Input,
	) (bluetoothaudiodomain.Policy, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (bluetoothaudiodomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input bluetoothaudiodomain.StatusInput,
	) (bluetoothaudiodomain.Status, error)
	DeviceStatuses(
		ctx context.Context,
		familyID string,
	) ([]bluetoothaudiodomain.DeviceStatus, error)
	AdminStats(ctx context.Context, windowDays int) (bluetoothaudiodomain.AdminStats, error)
}

type bluetoothAudioHandler struct {
	service bluetoothAudioService
}

// deviceBluetoothAudioHandler serves the family policy and records bounded
// adapter status for a bound device that holds a device session token.
type deviceBluetoothAudioHandler struct {
	handler        *bluetoothAudioHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type bluetoothAudioPolicyPayload struct {
	Enabled            bool   `json:"enabled"`
	SpeakerModeAllowed bool   `json:"speaker_mode_allowed"`
	PairingAllowed     bool   `json:"pairing_allowed"`
	AutoReconnect      bool   `json:"auto_reconnect"`
	PreferredCodec     string `json:"preferred_codec"`
	MaxVolumePercent   int    `json:"max_volume_percent"`
	DisplayName        string `json:"display_name"`
}

type bluetoothAudioStatusPayload struct {
	BluetoothSupported   bool   `json:"bluetooth_supported"`
	SpeakerMode          bool   `json:"speaker_mode"`
	AdapterState         string `json:"adapter_state"`
	PairedDeviceCount    int    `json:"paired_device_count"`
	ConnectedDeviceCount int    `json:"connected_device_count"`
	ActiveCodec          string `json:"active_codec"`
	VolumePercent        int    `json:"volume_percent"`
	LastDisconnectReason string `json:"last_disconnect_reason"`
	PolicyRevision       int    `json:"policy_revision"`
}

// current returns the guardian's Bluetooth audio policy for the authenticated
// family together with the fixed codec, state and reason catalogs.
func (handler bluetoothAudioHandler) current(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	policy, err := handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeBluetoothAudioError(response, request, err)
		return
	}
	preferredCodecs, activeCodecs, adapterStates, reasons := handler.service.Catalog()
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"policy":             bluetoothAudioPolicyResponse(policy),
		"preferred_codecs":   preferredCodecs,
		"active_codecs":      activeCodecs,
		"adapter_states":     adapterStates,
		"disconnect_reasons": reasons,
	})
}

// publish validates and stores a new guardian Bluetooth audio policy.
func (handler bluetoothAudioHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload bluetoothAudioPolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查蓝牙音箱设置")
		return
	}
	policy, err := handler.service.Publish(
		request.Context(),
		familyID,
		bluetoothaudiodomain.Input{
			Enabled:            payload.Enabled,
			SpeakerModeAllowed: payload.SpeakerModeAllowed,
			PairingAllowed:     payload.PairingAllowed,
			AutoReconnect:      payload.AutoReconnect,
			PreferredCodec:     payload.PreferredCodec,
			MaxVolumePercent:   payload.MaxVolumePercent,
			DisplayName:        payload.DisplayName,
		},
	)
	if err != nil {
		writeBluetoothAudioError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, bluetoothAudioPolicyResponse(policy))
}

// statuses returns each family device's latest bounded adapter status.
func (handler bluetoothAudioHandler) statuses(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	devices, err := handler.service.DeviceStatuses(request.Context(), familyID)
	if err != nil {
		writeBluetoothAudioError(response, request, err)
		return
	}
	if devices == nil {
		devices = []bluetoothaudiodomain.DeviceStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"devices": devices})
}

// capability returns the Bluetooth audio capability for one family device.
func (handler bluetoothAudioHandler) capability(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	capability, err := handler.service.Capability(
		request.Context(),
		familyID,
		deviceID,
	)
	if err != nil {
		writeBluetoothAudioError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// adminStats returns the bounded fleet Bluetooth audio summary.
func (handler bluetoothAudioHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseBluetoothAudioWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeBluetoothAudioError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

// devicePolicy returns the family Bluetooth audio policy to a bound device over
// its device session token so the firmware can clamp and apply it.
func (handler deviceBluetoothAudioHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	policy, err := handler.handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeBluetoothAudioError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK,
		bluetoothAudioPolicyResponse(policy))
}

// reportStatus records one bound device's adapter status.
func (handler deviceBluetoothAudioHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload bluetoothAudioStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		bluetoothaudiodomain.StatusInput{
			BluetoothSupported:   payload.BluetoothSupported,
			SpeakerMode:          payload.SpeakerMode,
			AdapterState:         payload.AdapterState,
			PairedDeviceCount:    payload.PairedDeviceCount,
			ConnectedDeviceCount: payload.ConnectedDeviceCount,
			ActiveCodec:          payload.ActiveCodec,
			VolumePercent:        payload.VolumePercent,
			LastDisconnectReason: payload.LastDisconnectReason,
			PolicyRevision:       payload.PolicyRevision,
		},
	)
	if err != nil {
		writeBluetoothAudioError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":       status.DeviceID,
		"adapter_state":   status.AdapterState,
		"policy_revision": status.PolicyRevision,
		"recorded":        true,
	})
}

func (handler deviceBluetoothAudioHandler) verifyDevice(
	response http.ResponseWriter,
	request *http.Request,
) (string, string, bool) {
	if handler.bindingService == nil || handler.runtimeService == nil {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", "", false
	}
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", "", false
	}
	pathDeviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionDeviceID, err := handler.bindingService.VerifyDeviceSession(
		request.Context(),
		deviceSessionToken,
	)
	if err != nil || sessionDeviceID != pathDeviceID {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", "", false
	}
	familyID, err := handler.runtimeService.ResolveDeviceFamily(
		request.Context(),
		deviceSessionToken,
		pathDeviceID,
	)
	if err != nil {
		writeBluetoothAudioError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func bluetoothAudioPolicyResponse(
	policy bluetoothaudiodomain.Policy,
) map[string]any {
	return map[string]any{
		"schema_version":       policy.SchemaVersion,
		"enabled":              policy.Enabled,
		"speaker_mode_allowed": policy.SpeakerModeAllowed,
		"pairing_allowed":      policy.PairingAllowed,
		"auto_reconnect":       policy.AutoReconnect,
		"preferred_codec":      policy.PreferredCodec,
		"max_volume_percent":   policy.MaxVolumePercent,
		"display_name":         policy.DisplayName,
		"revision":             policy.Revision,
	}
}

func parseBluetoothAudioWindowDays(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 7
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > 90 {
		return 7
	}
	return value
}

func writeBluetoothAudioError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, bluetoothaudiodomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "蓝牙音箱设置超出允许范围")
	case errors.Is(err, bluetoothaudiodomain.ErrInvalidInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查上报内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

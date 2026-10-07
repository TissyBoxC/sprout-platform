package http

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
)

type deviceBindingHandler struct {
	service           *bindingservice.Service
	voiceTokenIssuer  bindingservice.VoiceTokenIssuer
	voiceWebSocketURL string
}

type createRegistrationTokenRequest struct {
	DeviceID string `json:"device_id"`
}

type registerDeviceRequest struct {
	RegistrationToken string   `json:"registration_token"`
	DeviceID          string   `json:"device_id"`
	HardwareModel     string   `json:"hardware_model"`
	FirmwareVersion   string   `json:"firmware_version"`
	Capabilities      []string `json:"capabilities"`
	PublicKey         string   `json:"public_key"`
}

type startDeviceAuthenticationRequest struct {
	DeviceID string `json:"device_id"`
}

type completeDeviceAuthenticationRequest struct {
	DeviceID  string `json:"device_id"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
}

type bindDeviceRequest struct {
	Token      string `json:"token"`
	DeviceName string `json:"device_name"`
}

// createRegistrationToken is an operator/manufacturing endpoint protected by
// the internal service token, never by a parent session.
func (handler deviceBindingHandler) createRegistrationToken(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload createRegistrationTokenRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查设备信息")
		return
	}
	token, details, err := handler.service.CreateRegistrationToken(
		request.Context(),
		payload.DeviceID,
	)
	if err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"registration_token": token,
		"device_id":          details.DeviceID,
		"expires_at":         details.ExpiresAt,
	})
}

// registerDevice consumes a manufacturing registration grant and stores the
// device public key. It is deliberately not authenticated by a parent session.
func (handler deviceBindingHandler) registerDevice(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload registerDeviceRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查设备信息")
		return
	}
	credential, err := handler.service.RegisterDevice(
		request.Context(),
		payload.RegistrationToken,
		bindingdomain.DeviceRegistrationInput{
			DeviceID:        payload.DeviceID,
			HardwareModel:   payload.HardwareModel,
			FirmwareVersion: payload.FirmwareVersion,
			Capabilities:    payload.Capabilities,
			PublicKey:       payload.PublicKey,
		},
	)
	if err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"device_id":        credential.DeviceID,
		"hardware_model":   credential.HardwareModel,
		"firmware_version": credential.FirmwareVersion,
		"capabilities":     credential.CapabilitySet,
		"registered_at":    credential.RegisteredAt,
	})
}

// startDeviceAuthentication issues a short-lived nonce to a registered device.
func (handler deviceBindingHandler) startDeviceAuthentication(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload startDeviceAuthenticationRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查设备信息")
		return
	}
	nonce, challenge, err := handler.service.StartDeviceAuthentication(
		request.Context(),
		payload.DeviceID,
	)
	if err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"nonce":      nonce,
		"expires_at": challenge.ExpiresAt,
	})
}

// completeDeviceAuthentication verifies the device signature and returns a
// short-lived device session token. The token is never logged or persisted raw.
func (handler deviceBindingHandler) completeDeviceAuthentication(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload completeDeviceAuthenticationRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查设备信息")
		return
	}
	signature, err := decodeDeviceSignature(payload.Signature)
	if err != nil {
		writeDeviceBindingError(
			response,
			request,
			bindingdomain.ErrInvalidDeviceProof,
		)
		return
	}
	sessionToken, err := handler.service.CompleteDeviceAuthentication(
		request.Context(),
		payload.DeviceID,
		payload.Nonce,
		signature,
	)
	if err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_session_token": sessionToken,
	})
}

func decodeDeviceSignature(value string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
}

// createDeviceProvisioningToken is called by an authenticated device to put a
// single-use binding token into its QR code or BLE advertisement.
func (handler deviceBindingHandler) createDeviceProvisioningToken(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return
	}
	token, details, err := handler.service.CreateDeviceProvisioningToken(
		request.Context(),
		deviceID,
		sessionToken,
	)
	if err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"token":      token,
		"device_id":  details.DeviceID,
		"expires_at": details.ExpiresAt,
	})
}

func (handler deviceBindingHandler) bind(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload bindDeviceRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查设备信息")
		return
	}
	binding, err := handler.service.Bind(
		request.Context(),
		accountID,
		payload.Token,
		payload.DeviceName,
	)
	if err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, bindingResponse(binding))
}

// bindingStatus is polled by an authenticated device after it presents a
// provisioning code. It never reveals which guardian account completed binding.
func (handler deviceBindingHandler) bindingStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return
	}
	isBound, err := handler.service.BindingStatus(
		request.Context(),
		deviceID,
		sessionToken,
	)
	if err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id": deviceID,
		"is_bound":  isBound,
	})
}

// createVoiceToken issues a short-lived signed credential for one bound device.
// The device session must belong to the exact path device, and the binding
// lookup prevents an unbound but valid device session from reaching the voice
// gateway. A missing signer is a safe 503 rather than an unsigned token.
func (handler deviceBindingHandler) createVoiceToken(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return
	}
	sessionDeviceID, err := handler.service.VerifyDeviceSession(
		request.Context(),
		sessionToken,
	)
	if err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	if sessionDeviceID != deviceID {
		writeDeviceBindingError(response, request, bindingdomain.ErrInvalidDeviceProof)
		return
	}
	if _, err := handler.service.GetByDeviceID(request.Context(), sessionDeviceID); err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	if handler.voiceTokenIssuer == nil || handler.voiceWebSocketURL == "" {
		writeError(response, request, http.StatusServiceUnavailable, "voice_unavailable", "实时语音暂时不可用，请稍后重试")
		return
	}
	issue, err := handler.voiceTokenIssuer.Issue(sessionDeviceID)
	if err != nil || strings.TrimSpace(issue.Token) == "" || issue.ExpiresAt.IsZero() {
		writeError(response, request, http.StatusServiceUnavailable, "voice_unavailable", "实时语音暂时不可用，请稍后重试")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"websocket_url": handler.voiceWebSocketURL,
		"token":         issue.Token,
		"expires_at":    issue.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (handler deviceBindingHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	bindings, err := handler.service.List(request.Context(), accountID)
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取设备")
		return
	}
	result := make([]map[string]any, 0, len(bindings))
	for index := range bindings {
		result = append(result, bindingResponse(&bindings[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"devices": result})
}

func (handler deviceBindingHandler) remove(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	if err := handler.service.Delete(request.Context(), accountID, deviceID); err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"removed": true})
}

func bindingResponse(binding *bindingdomain.Binding) map[string]any {
	return map[string]any{
		"device_id":        binding.DeviceID,
		"device_name":      binding.DeviceName,
		"hardware_model":   binding.HardwareModel,
		"firmware_version": binding.FirmwareVersion,
		"capabilities":     binding.Capabilities,
		"bound_at":         binding.BoundAt,
		"updated_at":       binding.UpdatedAt,
	}
}

func writeDeviceBindingError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, bindingdomain.ErrInvalidDeviceID):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_device", "设备信息不正确")
	case errors.Is(err, bindingdomain.ErrTokenNotFound):
		writeError(response, request, http.StatusNotFound, "token_not_found", "绑定码无效，请在设备上重新生成")
	case errors.Is(err, bindingdomain.ErrTokenExpired):
		writeError(response, request, http.StatusGone, "token_expired", "绑定码已过期，请在设备上重新生成")
	case errors.Is(err, bindingdomain.ErrTokenConsumed):
		writeError(response, request, http.StatusConflict, "token_used", "这个绑定码已经使用，请在设备上重新生成")
	case errors.Is(err, bindingdomain.ErrDeviceNotFound):
		writeError(response, request, http.StatusNotFound, "device_not_found", "没有找到这台设备")
	case errors.Is(err, bindingdomain.ErrDeviceDisabled):
		writeError(response, request, http.StatusForbidden, "device_disabled", "这台设备当前无法连接")
	case errors.Is(err, bindingdomain.ErrRegistrationTokenNotFound):
		writeError(response, request, http.StatusNotFound, "registration_not_found", "设备注册信息无效")
	case errors.Is(err, bindingdomain.ErrRegistrationTokenExpired):
		writeError(response, request, http.StatusGone, "registration_expired", "设备注册已过期，请重新获取")
	case errors.Is(err, bindingdomain.ErrRegistrationTokenConsumed):
		writeError(response, request, http.StatusConflict, "registration_used", "设备已经完成注册")
	case errors.Is(err, bindingdomain.ErrDeviceChallengeNotFound),
		errors.Is(err, bindingdomain.ErrDeviceChallengeExpired),
		errors.Is(err, bindingdomain.ErrDeviceChallengeConsumed):
		writeError(response, request, http.StatusUnauthorized, "device_auth_expired", "设备验证已过期，请重新连接")
	case errors.Is(err, bindingdomain.ErrInvalidDeviceProof):
		writeError(response, request, http.StatusUnauthorized, "device_auth_failed", "设备验证没有通过")
	case errors.Is(err, bindingdomain.ErrDeviceSessionNotFound),
		errors.Is(err, bindingdomain.ErrDeviceSessionExpired):
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	screendomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/screen/domain"
)

// screenService is the guardian-facing screen surface. The HTTP layer depends
// on this narrow interface so the module stays removable.
type screenService interface {
	Catalog() ([]string, []string)
	Current(ctx context.Context, familyID string) (screendomain.Policy, error)
	Publish(
		ctx context.Context,
		familyID string,
		input screendomain.Input,
	) (screendomain.Policy, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (screendomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input screendomain.StatusInput,
	) (screendomain.Status, error)
	DeviceStatuses(ctx context.Context, familyID string) ([]screendomain.DeviceStatus, error)
	Dictionary(ctx context.Context, familyID string) ([]screendomain.DictionaryEntry, error)
	PutEntry(
		ctx context.Context,
		familyID string,
		input screendomain.DictionaryInput,
	) (screendomain.DictionaryEntry, error)
	DeleteEntry(ctx context.Context, familyID string, entryID string) error
	ResolvePlayback(
		ctx context.Context,
		familyID string,
		deviceID string,
		requestedBitrateKBS int,
		requestedFPS int,
	) (screendomain.PlaybackPlan, error)
	Playback(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (screendomain.PlaybackPlan, error)
	AdminStats(ctx context.Context, windowDays int) (screendomain.AdminStats, error)
}

type screenHandler struct {
	service screenService
}

// deviceScreenHandler records bounded screen status from a bound device.
type deviceScreenHandler struct {
	handler        *screenHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type screenPolicyPayload struct {
	DefaultExpression  string `json:"default_expression"`
	AnimationEnabled   bool   `json:"animation_enabled"`
	PointReadEnabled   bool   `json:"point_read_enabled"`
	VideoEnabled       bool   `json:"video_enabled"`
	VideoMaxFPS        int    `json:"video_max_fps"`
	VideoMaxBitrateKBS int    `json:"video_max_bitrate_kbps"`
	AnimationFrameMS   int    `json:"animation_frame_ms"`
}

type screenStatusPayload struct {
	ScreenSupported      bool   `json:"screen_supported"`
	ExpressionsSupported bool   `json:"expressions_supported"`
	AnimationSupported   bool   `json:"animation_supported"`
	PointReadSupported   bool   `json:"point_read_supported"`
	VideoSupported       bool   `json:"video_supported"`
	Expression           string `json:"expression"`
	Animation            string `json:"animation"`
	VideoState           string `json:"video_state"`
	PolicyRevision       int    `json:"policy_revision"`
}

type screenDictionaryPayload struct {
	Term string `json:"term"`
	Text string `json:"text"`
}

type screenPlaybackPayload struct {
	BitrateKBS int `json:"bitrate_kbps"`
	FPS        int `json:"fps"`
}

// current returns the guardian's screen policy for the authenticated family.
func (handler screenHandler) current(
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
		writeScreenError(response, request, err)
		return
	}
	expressions, animations := handler.service.Catalog()
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"policy":      screenPolicyResponse(policy),
		"expressions": expressions,
		"animations":  animations,
	})
}

// publish validates and stores a new guardian screen policy.
func (handler screenHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload screenPolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查屏幕设置")
		return
	}
	policy, err := handler.service.Publish(request.Context(), familyID, screendomain.Input{
		DefaultExpression:  payload.DefaultExpression,
		AnimationEnabled:   payload.AnimationEnabled,
		PointReadEnabled:   payload.PointReadEnabled,
		VideoEnabled:       payload.VideoEnabled,
		VideoMaxFPS:        payload.VideoMaxFPS,
		VideoMaxBitrateKBS: payload.VideoMaxBitrateKBS,
		AnimationFrameMS:   payload.AnimationFrameMS,
	})
	if err != nil {
		writeScreenError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, screenPolicyResponse(policy))
}

// capability returns the screen capability for one family device.
func (handler screenHandler) capability(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	capability, err := handler.service.Capability(request.Context(), familyID, deviceID)
	if err != nil {
		writeScreenError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// statuses returns each family device's latest bounded screen status.
func (handler screenHandler) statuses(
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
		writeScreenError(response, request, err)
		return
	}
	if devices == nil {
		devices = []screendomain.DeviceStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"devices": devices})
}

// dictionary returns the family point-to-read dictionary.
func (handler screenHandler) dictionary(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	entries, err := handler.service.Dictionary(request.Context(), familyID)
	if err != nil {
		writeScreenError(response, request, err)
		return
	}
	if entries == nil {
		entries = []screendomain.DictionaryEntry{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"entries": entries})
}

// putDictionaryEntry upserts one point-to-read entry.
func (handler screenHandler) putDictionaryEntry(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload screenDictionaryPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查点读内容")
		return
	}
	entry, err := handler.service.PutEntry(request.Context(), familyID, screendomain.DictionaryInput{
		Term: payload.Term,
		Text: payload.Text,
	})
	if err != nil {
		writeScreenError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, entry)
}

// deleteDictionaryEntry removes one point-to-read entry.
func (handler screenHandler) deleteDictionaryEntry(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	entryID := strings.TrimSpace(request.PathValue("entry_id"))
	if err := handler.service.DeleteEntry(request.Context(), familyID, entryID); err != nil {
		writeScreenError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"deleted": true})
}

// playback returns the stored video plan for one family device.
func (handler screenHandler) playback(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	plan, err := handler.service.Playback(request.Context(), familyID, deviceID)
	if err != nil {
		writeScreenError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, plan)
}

// resolvePlayback clamps and stores a requested video plan.
func (handler screenHandler) resolvePlayback(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	var payload screenPlaybackPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查视频播放参数")
		return
	}
	plan, err := handler.service.ResolvePlayback(
		request.Context(),
		familyID,
		deviceID,
		payload.BitrateKBS,
		payload.FPS,
	)
	if err != nil {
		writeScreenError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, plan)
}

// reportStatus records one bound device's screen status.
func (handler deviceScreenHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload screenStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		screendomain.StatusInput{
			ScreenSupported:      payload.ScreenSupported,
			ExpressionsSupported: payload.ExpressionsSupported,
			AnimationSupported:   payload.AnimationSupported,
			PointReadSupported:   payload.PointReadSupported,
			VideoSupported:       payload.VideoSupported,
			Expression:           payload.Expression,
			Animation:            payload.Animation,
			VideoState:           payload.VideoState,
			PolicyRevision:       payload.PolicyRevision,
		},
	)
	if err != nil {
		writeScreenError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":       status.DeviceID,
		"policy_revision": status.PolicyRevision,
		"recorded":        true,
	})
}

// devicePolicy returns the family screen policy to a bound device over its
// device session token so the firmware can clamp and apply it, mirroring the
// other device policy endpoints.
func (handler deviceScreenHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	policy, err := handler.handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeScreenError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, screenPolicyResponse(policy))
}

// adminStats returns the bounded fleet screen summary.
func (handler screenHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseScreenWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeScreenError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

func (handler deviceScreenHandler) verifyDevice(
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
		writeScreenError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func screenPolicyResponse(policy screendomain.Policy) map[string]any {
	return map[string]any{
		"schema_version":         policy.SchemaVersion,
		"default_expression":     policy.DefaultExpression,
		"animation_enabled":      policy.AnimationEnabled,
		"point_read_enabled":     policy.PointReadEnabled,
		"video_enabled":          policy.VideoEnabled,
		"video_max_fps":          policy.VideoMaxFPS,
		"video_max_bitrate_kbps": policy.VideoMaxBitrateKBS,
		"animation_frame_ms":     policy.AnimationFrameMS,
		"revision":               policy.Revision,
	}
}

func parseScreenWindowDays(raw string) int {
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

func writeScreenError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, screendomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "屏幕设置超出允许范围")
	case errors.Is(err, screendomain.ErrInvalidDictionaryEntry):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_entry", "请检查点读内容")
	case errors.Is(err, screendomain.ErrDictionaryFull):
		writeError(response, request, http.StatusUnprocessableEntity, "dictionary_full", "点读词条已达到上限")
	case errors.Is(err, screendomain.ErrDictionaryNotFound):
		writeError(response, request, http.StatusNotFound, "entry_not_found", "点读词条不存在")
	case errors.Is(err, screendomain.ErrInvalidPlayback):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_playback", "当前不允许播放视频")
	case errors.Is(err, screendomain.ErrInvalidInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查上报内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

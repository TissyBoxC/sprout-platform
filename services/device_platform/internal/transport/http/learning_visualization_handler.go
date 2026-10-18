package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	learningdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/learning_visualization/domain"
)

// learningVisualizationService is the guardian-facing learning-visualization
// surface. The HTTP layer depends on this narrow interface so the module stays
// removable.
type learningVisualizationService interface {
	Catalog() ([]string, []string, []string)
	Current(
		ctx context.Context,
		familyID string,
	) (learningdomain.Policy, error)
	Publish(
		ctx context.Context,
		familyID string,
		input learningdomain.Input,
	) (learningdomain.Policy, error)
	Capability(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (learningdomain.Capability, error)
	ReportStatus(
		ctx context.Context,
		familyID string,
		deviceID string,
		input learningdomain.StatusInput,
	) (learningdomain.Status, error)
	DeviceStatuses(
		ctx context.Context,
		familyID string,
	) ([]learningdomain.DeviceStatus, error)
	AdminStats(ctx context.Context, windowDays int) (learningdomain.AdminStats, error)
}

type learningVisualizationHandler struct {
	service learningVisualizationService
}

// deviceLearningVisualizationHandler serves the family visualization policy and
// records bounded visualization status for a bound device that holds a device
// session token.
type deviceLearningVisualizationHandler struct {
	handler        *learningVisualizationHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type learningVisualizationPolicyPayload struct {
	Enabled              bool   `json:"enabled"`
	FeedbackStyle        string `json:"feedback_style"`
	AnimationLevel       string `json:"animation_level"`
	TouchFeedbackEnabled bool   `json:"touch_feedback_enabled"`
	RewardEffectsEnabled bool   `json:"reward_effects_enabled"`
	LessonLinkageEnabled bool   `json:"lesson_linkage_enabled"`
	MaxSessionMinutes    int    `json:"max_session_minutes"`
}

type learningVisualizationStatusPayload struct {
	VisualizationSupported bool   `json:"visualization_supported"`
	TouchSupported         bool   `json:"touch_supported"`
	LinkedLessonID         string `json:"linked_lesson_id"`
	VisualState            string `json:"visual_state"`
	FeedbackStyle          string `json:"feedback_style"`
	AnimationLevel         string `json:"animation_level"`
	FeedbackEvents         int    `json:"feedback_events"`
	SessionMinutes         int    `json:"session_minutes"`
	PolicyRevision         int    `json:"policy_revision"`
}

// current returns the guardian's learning-visualization policy for the
// authenticated family together with the fixed catalogs.
func (handler learningVisualizationHandler) current(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	policy, err := handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeLearningVisualizationError(response, request, err)
		return
	}
	styles, animations, states := handler.service.Catalog()
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"policy":          learningVisualizationPolicyResponse(policy),
		"feedback_styles": styles,
		"animation_levels": animations,
		"visual_states":   states,
	})
}

// publish validates and stores a new guardian visualization policy.
func (handler learningVisualizationHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload learningVisualizationPolicyPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查学习可视化设置")
		return
	}
	policy, err := handler.service.Publish(
		request.Context(),
		familyID,
		learningdomain.Input{
			Enabled:              payload.Enabled,
			FeedbackStyle:        payload.FeedbackStyle,
			AnimationLevel:       payload.AnimationLevel,
			TouchFeedbackEnabled: payload.TouchFeedbackEnabled,
			RewardEffectsEnabled: payload.RewardEffectsEnabled,
			LessonLinkageEnabled: payload.LessonLinkageEnabled,
			MaxSessionMinutes:    payload.MaxSessionMinutes,
		},
	)
	if err != nil {
		writeLearningVisualizationError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, learningVisualizationPolicyResponse(policy))
}

// statuses returns each family device's latest bounded visualization status.
func (handler learningVisualizationHandler) statuses(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	devices, err := handler.service.DeviceStatuses(request.Context(), familyID)
	if err != nil {
		writeLearningVisualizationError(response, request, err)
		return
	}
	if devices == nil {
		devices = []learningdomain.DeviceStatus{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"devices": devices})
}

// capability returns the visualization capability for one family device.
func (handler learningVisualizationHandler) capability(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
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
		writeLearningVisualizationError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, capability)
}

// adminStats returns the bounded fleet learning-visualization summary.
func (handler learningVisualizationHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseLearningVisualizationWindowDays(
		request.URL.Query().Get("window_days"),
	)
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeLearningVisualizationError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

// devicePolicy returns the family visualization policy to a bound device over
// its device session token so the firmware can clamp and apply it.
func (handler deviceLearningVisualizationHandler) devicePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	_, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	policy, err := handler.handler.service.Current(request.Context(), familyID)
	if err != nil {
		writeLearningVisualizationError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK,
		learningVisualizationPolicyResponse(policy))
}

// reportStatus records one bound device's visualization status.
func (handler deviceLearningVisualizationHandler) reportStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, familyID, ok := handler.verifyDevice(response, request)
	if !ok {
		return
	}
	var payload learningVisualizationStatusPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报内容")
		return
	}
	status, err := handler.handler.service.ReportStatus(
		request.Context(),
		familyID,
		deviceID,
		learningdomain.StatusInput{
			VisualizationSupported: payload.VisualizationSupported,
			TouchSupported:         payload.TouchSupported,
			LinkedLessonID:         payload.LinkedLessonID,
			VisualState:            payload.VisualState,
			FeedbackStyle:          payload.FeedbackStyle,
			AnimationLevel:         payload.AnimationLevel,
			FeedbackEvents:         payload.FeedbackEvents,
			SessionMinutes:         payload.SessionMinutes,
			PolicyRevision:         payload.PolicyRevision,
		},
	)
	if err != nil {
		writeLearningVisualizationError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":       status.DeviceID,
		"policy_revision": status.PolicyRevision,
		"recorded":        true,
	})
}

func (handler deviceLearningVisualizationHandler) verifyDevice(
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
		writeLearningVisualizationError(response, request, err)
		return "", "", false
	}
	return pathDeviceID, familyID, true
}

func learningVisualizationPolicyResponse(
	policy learningdomain.Policy,
) map[string]any {
	return map[string]any{
		"schema_version":         policy.SchemaVersion,
		"enabled":                policy.Enabled,
		"feedback_style":         policy.FeedbackStyle,
		"animation_level":        policy.AnimationLevel,
		"touch_feedback_enabled": policy.TouchFeedbackEnabled,
		"reward_effects_enabled": policy.RewardEffectsEnabled,
		"lesson_linkage_enabled": policy.LessonLinkageEnabled,
		"max_session_minutes":    policy.MaxSessionMinutes,
		"revision":               policy.Revision,
	}
}

func parseLearningVisualizationWindowDays(raw string) int {
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

func writeLearningVisualizationError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, learningdomain.ErrInvalidPolicy):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_policy", "学习可视化设置超出允许范围")
	case errors.Is(err, learningdomain.ErrInvalidInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查上报内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

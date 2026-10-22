package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	emotiondomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/domain"
	emotionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/service"
)

// guardianEmotionHandler serves the guardian side of mood sensing: the family's
// setting and the aggregate summary.
//
// There is deliberately no handler that returns individual observations. The
// guardian contract is a distribution over a period, and the absence of a detail
// route is part of the product decision rather than an unfinished feature.
type guardianEmotionHandler struct {
	service *emotionservice.Service
}

// deviceEmotionHandler accepts samples reported by a device.
type deviceEmotionHandler struct {
	service *emotionservice.Service
}

// updateSettingsRequest is the guardian's configuration change.
//
// Both fields are pointers so an omitted retention keeps the stored window
// instead of being read as zero, which would otherwise trip the range check.
type updateSettingsRequest struct {
	IsEnabled     *bool `json:"is_enabled"`
	RetentionDays *int  `json:"retention_days"`
}

// settings returns the family's mood sensing configuration.
func (handler guardianEmotionHandler) settings(
	response http.ResponseWriter,
	request *http.Request,
) {
	settings, err := handler.service.GetSettings(
		request.Context(),
		request.PathValue("family_id"),
	)
	if err != nil {
		writeEmotionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, emotionSettingsResponse(settings))
}

// updateSettings applies the guardian's configuration change.
func (handler guardianEmotionHandler) updateSettings(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload updateSettingsRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	if payload.IsEnabled == nil {
		// The switch is the whole point of the screen. A request that omits it
		// is ambiguous, and guessing would let a stale client turn sensing on.
		writeError(response, request, http.StatusBadRequest,
			"emotion_invalid_request", "请选择是否开启情绪感知")
		return
	}
	retentionDays := 0
	if payload.RetentionDays != nil {
		// Zero is a value the guardian could send by mistake, not a way to say
		// "unchanged": the UI keeps one day as the smallest window, and a zero
		// would otherwise be read as "keep the stored value" and silently
		// succeed while changing nothing.
		if *payload.RetentionDays == 0 {
			writeError(response, request, http.StatusUnprocessableEntity,
				"emotion_invalid_retention", "保留天数需要在 1 到 90 天之间")
			return
		}
		retentionDays = *payload.RetentionDays
	}
	settings, err := handler.service.UpdateSettings(
		request.Context(),
		request.PathValue("family_id"),
		*payload.IsEnabled,
		retentionDays,
	)
	if err != nil {
		writeEmotionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, emotionSettingsResponse(settings))
}

// summary returns the guardian-visible aggregate for a window.
//
// The response is built from a domain.Summary, which has no field for an
// observation instant, a device, or an observation identifier: the aggregate is
// the only shape this endpoint can produce.
func (handler guardianEmotionHandler) summary(
	response http.ResponseWriter,
	request *http.Request,
) {
	days := 0
	if raw := request.URL.Query().Get("days"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			writeError(response, request, http.StatusBadRequest,
				"emotion_invalid_request", "请选择 1 到 90 天之间的时间段")
			return
		}
		days = parsed
	}
	summary, err := handler.service.Summary(
		request.Context(),
		request.PathValue("family_id"),
		request.URL.Query().Get("child_id"),
		days,
	)
	if err != nil {
		writeEmotionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, emotionSummaryResponse(summary))
}

// record stores one sample reported by the device.
func (handler deviceEmotionHandler) record(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		ChildID    string `json:"child_id"`
		Label      string `json:"label"`
		Confidence *int   `json:"confidence"`
		ObservedAt string `json:"observed_at"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	if payload.Confidence == nil {
		writeError(response, request, http.StatusUnprocessableEntity,
			"emotion_invalid_confidence", "请检查情绪的置信度")
		return
	}
	observedAt := time.Time{}
	if payload.ObservedAt != "" {
		parsed, parseErr := time.Parse(time.RFC3339, payload.ObservedAt)
		if parseErr != nil {
			writeError(response, request, http.StatusUnprocessableEntity,
				"emotion_invalid_request", "请检查情绪的观测时间")
			return
		}
		observedAt = parsed
	}
	observation, err := handler.service.Record(
		request.Context(),
		emotiondomain.RecordInput{
			DeviceID:   request.PathValue("device_id"),
			ChildID:    payload.ChildID,
			Label:      payload.Label,
			Confidence: *payload.Confidence,
			ObservedAt: observedAt,
		},
	)
	if err != nil {
		writeEmotionError(response, request, err)
		return
	}
	// The acknowledgement carries no instant and no text. A device that got this
	// answer knows the sample was accepted; nothing about the child goes back.
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"accepted":    true,
		"label":       string(observation.Label),
		"confidence":  observation.Confidence,
		"received_at": observation.CreatedAt,
	})
}

// emotionSettingsResponse exposes the family's configuration.
func emotionSettingsResponse(settings emotiondomain.Settings) map[string]any {
	return map[string]any{
		"is_enabled":     settings.Enabled,
		"retention_days": settings.RetentionDays,
		"updated_at":     settings.UpdatedAt,
		// The UI explains what the switch means without asking the client to
		// hard-code the rule, so a wording change ships with the backend.
		"summary_default_days": emotiondomain.DefaultSummaryDays,
		"max_retention_days":   emotiondomain.MaxRetentionDays,
	}
}

// emotionSummaryResponse exposes the aggregate.
//
// It carries a window, counts, percentages, and trends. No field here can locate
// a specific moment, name a device, or identify an observation, which is what
// makes the aggregate-only promise structural rather than conventional.
func emotionSummaryResponse(summary emotiondomain.Summary) map[string]any {
	labels := make([]map[string]any, 0, len(summary.Labels))
	for _, entry := range summary.Labels {
		labels = append(labels, map[string]any{
			"label":      string(entry.Label),
			"count":      entry.Count,
			"percentage": emotiondomain.Percent(entry.Count, summary.TotalSamples),
			"trend":      string(summary.Trends[entry.Label]),
		})
	}
	return map[string]any{
		"child_id":               summary.ChildID,
		"days":                   summary.Days,
		"period_start":           summary.PeriodStart,
		"period_end":             summary.PeriodEnd,
		"total_samples":          summary.TotalSamples,
		"average_confidence":     summary.AverageConfidence,
		"labels":                 labels,
		"previous_total_samples": summary.PreviousTotalSamples,
		"is_enabled":             summary.Enabled,
		"retention_days":         summary.RetentionDays,
	}
}

// writeEmotionError maps mood sensing failures to stable codes with parent-facing
// copy.
//
// Every message is written for a parent and contains no internal detail. A
// cross-family identifier answers exactly like a missing one, so the endpoints
// cannot be used to discover which devices or families exist.
func writeEmotionError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, emotionservice.ErrDisabled):
		writeError(response, request, http.StatusForbidden,
			"emotion_disabled", "还没有开启情绪感知，请先在设置里开启")
	case errors.Is(err, emotionservice.ErrDeviceNotFound):
		writeError(response, request, http.StatusNotFound,
			"emotion_device_not_found", "没有找到这台设备")
	case errors.Is(err, emotionservice.ErrChildNotBound):
		writeError(response, request, http.StatusUnprocessableEntity,
			"emotion_child_not_bound", "这台设备当前没有绑定这个孩子")
	case errors.Is(err, emotiondomain.ErrInvalidLabel):
		writeError(response, request, http.StatusUnprocessableEntity,
			"emotion_invalid_label", "请检查情绪类型")
	case errors.Is(err, emotiondomain.ErrInvalidConfidence):
		writeError(response, request, http.StatusUnprocessableEntity,
			"emotion_invalid_confidence", "请检查情绪的置信度")
	case errors.Is(err, emotiondomain.ErrObservationFuture):
		writeError(response, request, http.StatusUnprocessableEntity,
			"emotion_future_observation", "观测时间不能晚于现在")
	case errors.Is(err, emotiondomain.ErrObservationTooOld):
		writeError(response, request, http.StatusUnprocessableEntity,
			"emotion_stale_observation", "观测时间太久以前，无法记录")
	case errors.Is(err, emotiondomain.ErrInvalidRetention):
		writeError(response, request, http.StatusUnprocessableEntity,
			"emotion_invalid_retention", "保留天数需要在 1 到 90 天之间")
	case errors.Is(err, emotiondomain.ErrInvalidDays):
		writeError(response, request, http.StatusUnprocessableEntity,
			"emotion_invalid_days", "请选择 1 到 90 天之间的时间段")
	case errors.Is(err, emotiondomain.ErrInvalidRequest):
		writeError(response, request, http.StatusUnprocessableEntity,
			"emotion_invalid_request", "请检查提交的内容")
	case errors.Is(err, emotionservice.ErrSummariseFailed):
		writeError(response, request, http.StatusServiceUnavailable,
			"emotion_summary_unavailable", "暂时无法汇总情绪数据，请稍后再试")
	default:
		writeError(response, request, http.StatusInternalServerError,
			"emotion_failed", "情绪感知暂时无法使用，请稍后再试")
	}
}

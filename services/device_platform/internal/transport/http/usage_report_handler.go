package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
	usageservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/service"
)

// usageReportHandler translates usage uploads and report reads to the service.
type usageReportHandler struct {
	service        *usageservice.Service
	runtimeService usageReportRuntimeService
}

// usageReportRuntimeService resolves the authenticated device and its family.
type usageReportRuntimeService interface {
	ResolveDeviceFamily(
		ctx context.Context,
		deviceSessionToken string,
		pathDeviceID string,
	) (string, error)
}

type deviceUsageUploadRequest struct {
	SchemaVersion         string                    `json:"schema_version"`
	ReportDate            string                    `json:"report_date"`
	TimezoneOffsetMinutes int                       `json:"timezone_offset_minutes"`
	ActiveSeconds         int                       `json:"active_seconds"`
	ConversationCount     int                       `json:"conversation_count"`
	ConversationSeconds   int                       `json:"conversation_seconds"`
	ContentPlayCount      int                       `json:"content_play_count"`
	ContentSeconds        int                       `json:"content_seconds"`
	Categories            []usageCategoryRequest    `json:"categories"`
	Blocked               usageBlockedCountsRequest `json:"blocked"`
}

type usageCategoryRequest struct {
	Category  string `json:"category"`
	PlayCount int    `json:"play_count"`
	Seconds   int    `json:"seconds"`
}

type usageBlockedCountsRequest struct {
	DisabledPeriod int `json:"disabled_period"`
	DailyLimit     int `json:"daily_limit"`
	CategoryDenied int `json:"category_denied"`
	TimeUntrusted  int `json:"time_untrusted"`
}

func (handler usageReportHandler) recordDeviceUsage(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil || handler.runtimeService == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "使用记录暂时无法保存")
		return
	}
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return
	}
	var payload deviceUsageUploadRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "使用记录格式不正确")
		return
	}
	if payload.SchemaVersion != "1.0.0" {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_usage_report", "使用记录需要重新同步")
		return
	}
	parentAccountID, err := handler.runtimeService.ResolveDeviceFamily(
		request.Context(),
		deviceSessionToken,
		strings.TrimSpace(request.PathValue("device_id")),
	)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	categories := make([]domain.CategoryUsage, 0, len(payload.Categories))
	for _, category := range payload.Categories {
		categories = append(categories, domain.CategoryUsage{
			Category:  category.Category,
			PlayCount: category.PlayCount,
			Seconds:   category.Seconds,
		})
	}
	usage, err := handler.service.RecordDeviceUsage(
		request.Context(),
		parentAccountID,
		strings.TrimSpace(request.PathValue("device_id")),
		domain.UsageUpload{
			ReportDate:            payload.ReportDate,
			TimezoneOffsetMinutes: payload.TimezoneOffsetMinutes,
			ActiveSeconds:         payload.ActiveSeconds,
			ConversationCount:     payload.ConversationCount,
			ConversationSeconds:   payload.ConversationSeconds,
			ContentPlayCount:      payload.ContentPlayCount,
			ContentSeconds:        payload.ContentSeconds,
			Categories:            categories,
			Blocked: domain.BlockedCounts{
				DisabledPeriod: payload.Blocked.DisabledPeriod,
				DailyLimit:     payload.Blocked.DailyLimit,
				CategoryDenied: payload.Blocked.CategoryDenied,
				TimeUntrusted:  payload.Blocked.TimeUntrusted,
			},
		},
	)
	if err != nil {
		writeUsageReportError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"report_date": usage.ReportDate,
		"updated_at":  usage.UpdatedAt.UTC(),
	})
}

// listGuardianUsageReports returns report days for the authenticated family.
func (handler usageReportHandler) listGuardianUsageReports(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "使用报告暂时无法读取")
		return
	}
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	days, err := usageReportDays(request)
	if err != nil {
		writeUsageReportError(response, request, err)
		return
	}
	reports, err := handler.service.ListForFamily(request.Context(), accountID, days)
	if err != nil {
		writeUsageReportError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"reports": usageReportResponses(reports),
	})
}

// listAdminFamilyUsageReports returns one family's report days to support
// staff. The admin middleware enforces the role before this handler runs.
func (handler usageReportHandler) listAdminFamilyUsageReports(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "使用报告暂时无法读取")
		return
	}
	parentAccountID := strings.TrimSpace(request.PathValue("parent_account_id"))
	if parentAccountID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择家长账号")
		return
	}
	days, err := usageReportDays(request)
	if err != nil {
		writeUsageReportError(response, request, err)
		return
	}
	reports, err := handler.service.ListForFamily(request.Context(), parentAccountID, days)
	if err != nil {
		writeUsageReportError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"reports": usageReportResponses(reports),
	})
}

func usageReportDays(request *http.Request) (int, error) {
	value := strings.TrimSpace(request.URL.Query().Get("days"))
	if value == "" {
		return 30, nil
	}
	days, err := strconv.Atoi(value)
	if err != nil || days < 1 || days > 90 {
		return 0, domain.ErrInvalidUsageReport
	}
	return days, nil
}

func usageReportResponses(reports []domain.Report) []map[string]any {
	result := make([]map[string]any, 0, len(reports))
	for index := range reports {
		result = append(result, usageReportResponse(&reports[index]))
	}
	return result
}

func usageReportResponse(report *domain.Report) map[string]any {
	if report == nil {
		return nil
	}
	categories := make([]map[string]any, 0, len(report.Categories))
	for _, category := range report.Categories {
		categories = append(categories, map[string]any{
			"category":   category.Category,
			"play_count": category.PlayCount,
			"minutes":    secondsToReportMinutes(category.Seconds),
		})
	}
	devices := make([]map[string]any, 0, len(report.Devices))
	for _, device := range report.Devices {
		devices = append(devices, map[string]any{
			"device_id":          device.DeviceID,
			"device_name":        device.DeviceName,
			"active_minutes":     device.ActiveMinutes,
			"conversation_count": device.ConversationCount,
			"content_play_count": device.ContentPlayCount,
		})
	}
	return map[string]any{
		"schema_version":          report.SchemaVersion,
		"report_date":             report.ReportDate,
		"timezone_offset_minutes": report.TimezoneOffsetMinutes,
		"active_minutes":          report.ActiveMinutes,
		"conversation_count":      report.ConversationCount,
		"conversation_minutes":    report.ConversationMinutes,
		"content_play_count":      report.ContentPlayCount,
		"content_minutes":         report.ContentMinutes,
		"daily_limit_minutes":     report.DailyLimitMinutes,
		"remaining_minutes":       report.RemainingMinutes,
		"limit_reached":           report.LimitReached,
		"categories":              categories,
		"blocked": map[string]any{
			"disabled_period": report.Blocked.DisabledPeriod,
			"daily_limit":     report.Blocked.DailyLimit,
			"category_denied": report.Blocked.CategoryDenied,
			"time_untrusted":  report.Blocked.TimeUntrusted,
		},
		"devices":    devices,
		"updated_at": report.UpdatedAt.UTC(),
	}
}

func secondsToReportMinutes(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 59) / 60
}

func writeUsageReportError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, domain.ErrInvalidUsageReport):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_usage_report", "使用记录需要重新同步")
	case errors.Is(err, domain.ErrUsageReportNotFound):
		writeError(response, request, http.StatusNotFound, "usage_report_not_found", "没有找到使用报告")
	default:
		writeDeviceRuntimeError(response, request, err)
	}
}

// Package http exposes growth reports to guardians and the console.
//
// Boundary: every response here contains aggregates only. No route returns a
// conversation, a memory value or a child's name alongside content, so a
// guardian can share a report without exposing what the child said.
package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/growthreport/domain"
)

// ErrReportNotFound is re-exported so the transport layer maps it without
// importing the service package's error value.
var ErrReportNotFound = errors.New("growth report not found")

// guardianReportService is the guardian-facing slice of the report service.
type guardianReportService interface {
	ListForChild(
		ctx context.Context,
		familyID string,
		childID string,
		period domain.PeriodType,
		limit int,
	) ([]domain.Report, error)
	GetForGuardian(
		ctx context.Context,
		familyID string,
		childID string,
		reportID string,
	) (domain.Report, error)
}

// adminReportService is the console-facing slice.
type adminReportService interface {
	ListForFamily(
		ctx context.Context,
		familyID string,
		limit int,
	) ([]domain.Report, error)
}

// guardianHandler serves one child's reports.
type guardianHandler struct {
	service guardianReportService
}

// list answers with a child's reports newest first.
func (handler guardianHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID := request.PathValue("family_id")
	childID := request.PathValue("child_id")
	period := domain.PeriodType(request.URL.Query().Get("period_type"))
	limit := parseLimit(request.URL.Query().Get("limit"))

	reports, err := handler.service.ListForChild(
		request.Context(),
		familyID,
		childID,
		period,
		limit,
	)
	if err != nil {
		writeReportError(response, request, err, http.StatusBadRequest)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"reports": reportResponses(reports),
	})
}

// get answers with one report owned by the family and child.
func (handler guardianHandler) get(
	response http.ResponseWriter,
	request *http.Request,
) {
	report, err := handler.service.GetForGuardian(
		request.Context(),
		request.PathValue("family_id"),
		request.PathValue("child_id"),
		request.PathValue("report_id"),
	)
	if err != nil {
		if errors.Is(err, ErrReportNotFound) ||
			errors.Is(err, domain.ErrInvalidReport) {
			writeError(
				response,
				request,
				http.StatusNotFound,
				"growth_report_not_found",
				"这份成长报告不存在",
			)
			return
		}
		writeError(
			response,
			request,
			http.StatusServiceUnavailable,
			"service_unavailable",
			"成长报告暂时无法读取，请稍后重试",
		)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"report": reportResponse(report),
	})
}

// adminGrowthReportHandler serves a family's reports to the console.
type adminGrowthReportHandler struct {
	service adminReportService
}

// list answers with a family's reports newest first.
func (handler adminGrowthReportHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	reports, err := handler.service.ListForFamily(
		request.Context(),
		request.PathValue("family_id"),
		parseLimit(request.URL.Query().Get("limit")),
	)
	if err != nil {
		writeReportError(response, request, err, http.StatusBadRequest)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"reports": reportResponses(reports),
	})
}

// reportResponse shapes one report for the wire.
//
// The summary carries a child id but never a child name: the guardian app holds
// the name locally, and putting it in a report body would spread identifying
// data into anything that logs a response.
func reportResponse(report domain.Report) map[string]any {
	return map[string]any{
		"id":           report.ID,
		"family_id":    report.FamilyID,
		"child_id":     report.ChildID,
		"period_type":  string(report.PeriodType),
		"period_start": report.PeriodStart.Format("2006-01-02"),
		"period_end":   report.PeriodEnd.Format("2006-01-02"),
		"metrics":      metricsResponse(report.Metrics),
		"summary":      report.Summary,
		"generated_at": report.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// metricsResponse flattens the comparison into the numbers a screen draws.
func metricsResponse(metrics domain.Metrics) map[string]any {
	return map[string]any{
		"practice": map[string]any{
			"attempts":      metrics.Practice.Attempts,
			"passed":        metrics.Practice.Passed,
			"average_score": metrics.Practice.AverageScore,
			"accuracy":      metrics.Practice.Accuracy(),
		},
		"previous_practice": map[string]any{
			"attempts":      metrics.PreviousPractice.Attempts,
			"passed":        metrics.PreviousPractice.Passed,
			"average_score": metrics.PreviousPractice.AverageScore,
			"accuracy":      metrics.PreviousPractice.Accuracy(),
		},
		"usage": map[string]any{
			"active_seconds":       metrics.Usage.ActiveSeconds,
			"conversation_count":   metrics.Usage.ConversationCount,
			"conversation_seconds": metrics.Usage.ConversationSeconds,
		},
		"counts": map[string]any{
			"growth_records": metrics.Counts.GrowthRecords,
			"memories":       metrics.Counts.Memories,
		},
		"deltas": map[string]any{
			"practice":       metrics.PracticeDelta,
			"accuracy":       metrics.AccuracyDelta,
			"active_seconds": metrics.ActiveSecondsDiff,
		},
		"accuracy_phrase": domain.AccuracyPhrase(metrics),
	}
}

// reportResponses shapes a list.
func reportResponses(reports []domain.Report) []map[string]any {
	items := make([]map[string]any, 0, len(reports))
	for index := range reports {
		items = append(items, reportResponse(reports[index]))
	}
	return items
}

// parseLimit reads a page size, letting the service apply its own bounds.
func parseLimit(raw string) int {
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return limit
}

// writeReportError maps a service failure to a stable code and safe message.
func writeReportError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
	invalidStatus int,
) {
	if errors.Is(err, domain.ErrInvalidReport) {
		writeError(
			response,
			request,
			invalidStatus,
			"invalid_growth_report_request",
			"请选择要查看的成长报告周期",
		)
		return
	}
	writeError(
		response,
		request,
		http.StatusServiceUnavailable,
		"service_unavailable",
		"成长报告暂时无法读取，请稍后重试",
	)
}

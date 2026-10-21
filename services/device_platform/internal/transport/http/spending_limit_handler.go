package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	billingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	billingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/service"
	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

// spendingLimitService is the narrow cap surface the transport depends on.
//
// It exists so route tests can drive the handlers without a database and so the
// transport never reaches into the billing repository. Every amount decision,
// including whether a checkout is over the cap, stays in the service.
type spendingLimitService interface {
	GetSpendingLimit(ctx context.Context, familyID string) (billingservice.SpendingLimitView, error)
	SetSpendingLimit(
		ctx context.Context,
		familyID string,
		actorAccountID string,
		monthlyLimitCents billingdomain.Cents,
	) (billingservice.SpendingLimitView, error)
	ListSpendingLimitRejections(
		ctx context.Context,
		familyID string,
		moment time.Time,
		page billingservice.Page,
	) ([]billingdomain.SpendingLimitRejection, int, billingservice.MonthPeriod, error)
}

// spendingLimitHandler exposes the family monthly spending cap to guardians and
// operators.
//
// Boundary: HTTP shape only. A cap is always echoed as an integer cent count and
// the current month's window is resolved by the service, so no client can move
// the accounting period.
type spendingLimitHandler struct {
	service spendingLimitService
}

// spendingLimitResponse is one family's cap and this month's spend.
//
// The remaining figure is derived on the server so every client renders the
// same number; -1 marks an unlimited cap rather than a magic zero that would
// read as "fully spent".
type spendingLimitResponse struct {
	FamilyID          string     `json:"family_id"`
	MonthlyLimitCents int64      `json:"monthly_limit_cents"`
	ConsumedCents     int64      `json:"consumed_cents"`
	RemainingCents    int64      `json:"remaining_cents"`
	IsUnlimited       bool       `json:"is_unlimited"`
	Period            string     `json:"period"`
	UpdatedBy         string     `json:"updated_by,omitempty"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
}

// spendingLimitRejectionResponse is one refused checkout. Amounts are cents.
type spendingLimitRejectionResponse struct {
	RejectionID       string    `json:"rejection_id"`
	MonthlyLimitCents int64     `json:"monthly_limit_cents"`
	ConsumedCents     int64     `json:"consumed_cents"`
	RequestedCents    int64     `json:"requested_cents"`
	PlanCode          string    `json:"plan_code"`
	CreatedAt         time.Time `json:"created_at"`
}

type updateSpendingLimitRequest struct {
	// MonthlyLimitCents is required explicitly: a missing field must not be
	// decoded as 0 and silently remove the guardian's cap.
	MonthlyLimitCents *int64 `json:"monthly_limit_cents"`
}

// getLimit returns the calling family's cap. The route resolves the family from
// the caller's membership, so the path id is only a cross-check.
func (handler spendingLimitHandler) getLimit(
	response http.ResponseWriter,
	request *http.Request,
	familyID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "消费限额暂时无法读取，请稍后重试")
		return
	}
	access, ok := authenticatedFamilyAccess(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	if !spendingLimitFamilyMatches(response, request, familyID, access) {
		return
	}
	view, err := handler.service.GetSpendingLimit(request.Context(), familyID)
	if err != nil {
		writeSpendingLimitError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, spendingLimitResponseOf(view))
}

// setLimit stores a new cap for the calling family.
func (handler spendingLimitHandler) setLimit(
	response http.ResponseWriter,
	request *http.Request,
	familyID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "消费限额暂时无法保存，请稍后重试")
		return
	}
	access, ok := authenticatedFamilyAccess(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	if !spendingLimitFamilyMatches(response, request, familyID, access) {
		return
	}
	var payload updateSpendingLimitRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请输入每月的消费上限")
		return
	}
	if payload.MonthlyLimitCents == nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请输入每月的消费上限")
		return
	}
	view, err := handler.service.SetSpendingLimit(
		request.Context(),
		familyID,
		access.ActorUserID,
		billingdomain.Cents(*payload.MonthlyLimitCents),
	)
	if err != nil {
		writeSpendingLimitError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, spendingLimitResponseOf(view))
}

// adminGetLimit returns one explicit target family's cap for the operator
// console. The administrator is not a member of that family, so the permission
// route, not family membership, is the boundary.
func (handler spendingLimitHandler) adminGetLimit(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "消费限额暂时无法读取，请稍后重试")
		return
	}
	familyID := strings.TrimSpace(request.PathValue("family_id"))
	if familyID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择要查看的家庭")
		return
	}
	view, err := handler.service.GetSpendingLimit(request.Context(), familyID)
	if err != nil {
		writeSpendingLimitError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, spendingLimitResponseOf(view))
}

// adminListRejections returns one family's over-limit refusals for the month
// named by the optional period query parameter (YYYY-MM), defaulting to the
// current month.
func (handler spendingLimitHandler) adminListRejections(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "超限记录暂时无法读取，请稍后重试")
		return
	}
	familyID := strings.TrimSpace(request.PathValue("family_id"))
	if familyID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择要查看的家庭")
		return
	}
	moment, ok := spendingLimitPeriodOf(response, request)
	if !ok {
		return
	}
	page, ok := billingPageOf(response, request)
	if !ok {
		return
	}
	rejections, total, period, err := handler.service.ListSpendingLimitRejections(
		request.Context(),
		familyID,
		moment,
		page,
	)
	if err != nil {
		writeSpendingLimitError(response, request, err)
		return
	}
	items := make([]spendingLimitRejectionResponse, 0, len(rejections))
	for _, rejection := range rejections {
		items = append(items, spendingLimitRejectionResponse{
			RejectionID:       rejection.ID,
			MonthlyLimitCents: int64(rejection.MonthlyLimitCents),
			ConsumedCents:     int64(rejection.ConsumedCents),
			RequestedCents:    int64(rejection.RequestedCents),
			PlanCode:          rejection.PlanCode,
			CreatedAt:         rejection.CreatedAt,
		})
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"family_id":  familyID,
		"period":     period.Start().Format("2006-01"),
		"rejections": items,
		"total":      total,
	})
}

// spendingLimitFamilyMatches refuses a request whose path family differs from
// the caller's own family. A mismatch is answered as "not found" so the route
// cannot be used to probe which family ids exist.
func spendingLimitFamilyMatches(
	response http.ResponseWriter,
	request *http.Request,
	familyID string,
	access familydomain.Access,
) bool {
	if !validFamilyAccess(access) {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return false
	}
	if strings.TrimSpace(familyID) != access.FamilyID {
		writeError(response, request, http.StatusNotFound, "family_not_found", "没有找到这个家庭")
		return false
	}
	return true
}

// writeSpendingLimitError maps a cap failure to a guardian-safe response.
func writeSpendingLimitError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, billingdomain.ErrInvalidSpendingLimit):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_spending_limit", "请输入 0 到 100000 元之间的每月消费上限")
	case errors.Is(err, billingservice.ErrInvalidRequest):
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择要设置的家庭")
	case errors.Is(err, billingdomain.ErrSpendingLimitExceeded):
		writeError(response, request, http.StatusConflict, "spending_limit_exceeded", "本月消费已达上限，请先调整限额或次月再试")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

// spendingLimitPeriodOf parses the optional period=YYYY-MM filter. An absent
// value means the current month; an invalid one is rejected rather than
// silently defaulting, because a report showing the wrong month would be read
// as fact.
func spendingLimitPeriodOf(
	response http.ResponseWriter,
	request *http.Request,
) (time.Time, bool) {
	raw := strings.TrimSpace(request.URL.Query().Get("period"))
	if raw == "" {
		return time.Time{}, true
	}
	parsed, err := time.Parse("2006-01", raw)
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择有效的月份，例如 2026-10")
		return time.Time{}, false
	}
	return parsed, true
}

// spendingLimitResponseOf projects the service view onto the wire shape.
func spendingLimitResponseOf(view billingservice.SpendingLimitView) spendingLimitResponse {
	limit := view.Limit
	result := spendingLimitResponse{
		FamilyID:          view.Spending.FamilyID,
		MonthlyLimitCents: int64(limit.MonthlyLimitCents),
		ConsumedCents:     int64(view.Spending.ConsumedCents),
		RemainingCents: int64(view.Spending.RemainingCents(
			limit.MonthlyLimitCents,
		)),
		IsUnlimited: limit.IsUnlimited(),
		Period:      view.Spending.Period.Format("2006-01"),
		UpdatedBy:   limit.UpdatedBy,
	}
	if !limit.UpdatedAt.IsZero() {
		updatedAt := limit.UpdatedAt
		result.UpdatedAt = &updatedAt
	}
	return result
}

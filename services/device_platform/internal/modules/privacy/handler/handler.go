// Package handler maps guardian privacy controls and the administrator audit
// query onto the privacy service.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/domain"
)

// Service is the privacy use-case surface consumed by HTTP transport.
type Service interface {
	Status(ctx context.Context, parentAccountID string) (*domain.Status, error)
	Export(ctx context.Context, parentAccountID string) (*domain.DataExport, error)
	RequestDeletion(
		ctx context.Context,
		parentAccountID string,
		reason string,
	) (*domain.DeletionRequest, error)
	CancelDeletion(
		ctx context.Context,
		parentAccountID string,
	) (*domain.DeletionRequest, error)
	WithdrawConsent(
		ctx context.Context,
		parentAccountID string,
		consentType string,
		version string,
	) (*domain.Status, error)
	GrantConsent(
		ctx context.Context,
		parentAccountID string,
		consentType string,
		version string,
	) (*domain.Status, error)
	QueryAudit(
		ctx context.Context,
		filter domain.AuditFilter,
	) (*domain.AuditPage, error)
}

// Handler exposes guardian privacy and administrator audit endpoints.
type Handler struct {
	service Service
}

// New creates a privacy HTTP handler.
func New(service Service) *Handler {
	return &Handler{service: service}
}

type deletionRequest struct {
	Confirmation string `json:"confirmation"`
	Reason       string `json:"reason"`
}

type consentRequest struct {
	ConsentType  string `json:"consent_type"`
	Version      string `json:"version"`
	Confirmation string `json:"confirmation"`
}

// Status returns the guardian-facing privacy control state.
func (handler *Handler) Status(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	status, err := handler.service.Status(request.Context(), parentAccountID)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, status)
}

// Export returns a redacted, guardian-owned data copy.
func (handler *Handler) Export(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	export, err := handler.service.Export(request.Context(), parentAccountID)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, export)
}

// RequestDeletion starts the cancellable grace period. Production clients
// must echo the explicit destructive confirmation.
func (handler *Handler) RequestDeletion(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	var payload deletionRequest
	if err := decodeJSON(request, &payload); err != nil ||
		strings.TrimSpace(payload.Confirmation) != "DELETE" {
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"confirmation_required",
			"请确认要注销账号",
		)
		return
	}
	deletion, err := handler.service.RequestDeletion(
		request.Context(),
		parentAccountID,
		payload.Reason,
	)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusAccepted, map[string]any{
		"deletion": deletion,
	})
}

// CancelDeletion cancels the open request.
func (handler *Handler) CancelDeletion(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	deletion, err := handler.service.CancelDeletion(
		request.Context(),
		parentAccountID,
	)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"deletion": deletion,
	})
}

// WithdrawConsent records withdrawal, stops AI processing, and revokes the
// guardian's active sessions.
func (handler *Handler) WithdrawConsent(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	var payload consentRequest
	if err := decodeJSON(request, &payload); err != nil ||
		strings.TrimSpace(payload.Confirmation) != "WITHDRAW" {
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"confirmation_required",
			"请确认要撤回授权",
		)
		return
	}
	status, err := handler.service.WithdrawConsent(
		request.Context(),
		parentAccountID,
		payload.ConsentType,
		payload.Version,
	)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, status)
}

// GrantConsent restores a previously withdrawn authorization.
func (handler *Handler) GrantConsent(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	var payload consentRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	status, err := handler.service.GrantConsent(
		request.Context(),
		parentAccountID,
		payload.ConsentType,
		payload.Version,
	)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, status)
}

// AdminAudit returns the administrator-only operation projection.
func (handler *Handler) AdminAudit(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	filter, err := parseAuditFilter(request)
	if err != nil {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_query", "请检查筛选条件")
		return
	}
	page, err := handler.service.QueryAudit(request.Context(), filter)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, page)
}

func parseAuditFilter(request *http.Request) (domain.AuditFilter, error) {
	query := request.URL.Query()
	filter := domain.AuditFilter{
		Action:          strings.TrimSpace(query.Get("action")),
		ActorAccountID:  strings.TrimSpace(query.Get("actor_account_id")),
		TargetAccountID: strings.TrimSpace(query.Get("target_account_id")),
	}
	if value := strings.TrimSpace(query.Get("page")); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page < 1 {
			return domain.AuditFilter{}, domain.ErrAuditQueryInvalid
		}
		filter.Page = page
	}
	if value := strings.TrimSpace(query.Get("page_size")); value != "" {
		pageSize, err := strconv.Atoi(value)
		if err != nil || pageSize < 1 || pageSize > domain.MaxAuditPageSize {
			return domain.AuditFilter{}, domain.ErrAuditQueryInvalid
		}
		filter.PageSize = pageSize
	}
	if value := strings.TrimSpace(query.Get("from")); value != "" {
		from, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return domain.AuditFilter{}, domain.ErrAuditQueryInvalid
		}
		filter.From = &from
	}
	if value := strings.TrimSpace(query.Get("to")); value != "" {
		to, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return domain.AuditFilter{}, domain.ErrAuditQueryInvalid
		}
		filter.To = &to
	}
	return filter, nil
}

func handlerUnavailable(response http.ResponseWriter, request *http.Request) {
	writeError(
		response,
		request,
		http.StatusServiceUnavailable,
		"service_unavailable",
		"服务暂时不可用，请稍后重试",
	)
}

func writeServiceError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, domain.ErrInvalidConsentVersion):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_consent", "授权信息不正确")
	case errors.Is(err, domain.ErrDeletionPending):
		writeError(response, request, http.StatusConflict, "deletion_pending", "已经提交过注销申请")
	case errors.Is(err, domain.ErrDeletionNotPending):
		writeError(response, request, http.StatusConflict, "deletion_not_pending", "当前没有可撤销的注销申请")
	case errors.Is(err, domain.ErrInvalidDeletionReason):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_reason", "注销原因不能超过 500 个字")
	case errors.Is(err, domain.ErrAccountUnavailable):
		writeError(response, request, http.StatusNotFound, "account_not_found", "没有找到这个账号")
	case errors.Is(err, domain.ErrAuditQueryInvalid):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_query", "请检查筛选条件")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

func decodeJSON(request *http.Request, destination any) error {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func writeSuccess(
	response http.ResponseWriter,
	request *http.Request,
	status int,
	data any,
) {
	httpapi.WriteSuccess(response, request, status, data)
}

func writeError(
	response http.ResponseWriter,
	request *http.Request,
	status int,
	code string,
	message string,
) {
	httpapi.WriteError(response, request, status, code, message, false)
}

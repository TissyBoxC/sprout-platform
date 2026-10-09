// Package handler maps guardian notification and administrator notification
// management endpoints onto the notification service.
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
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
)

// Service is the notification use-case surface consumed by HTTP transport.
type Service interface {
	Inbox(
		ctx context.Context,
		parentAccountID string,
		limit int,
		cursor string,
		category string,
	) (*domain.InboxPage, error)
	UnreadCount(ctx context.Context, parentAccountID string) (int, error)
	MarkRead(ctx context.Context, parentAccountID string, notificationID string) error
	MarkAllRead(ctx context.Context, parentAccountID string) error
	SendFamilyMessage(
		ctx context.Context,
		parentAccountID string,
		input domain.GuardianMessageInput,
	) (*domain.InboxItem, error)
	DeviceMessages(
		ctx context.Context,
		parentAccountID string,
		deviceID string,
		limit int,
	) ([]domain.DeviceMessage, error)
	AdminList(ctx context.Context, filter domain.AdminFilter) (*domain.AdminPage, error)
	AdminGet(ctx context.Context, notificationID string) (*domain.AdminItem, error)
	AdminDelete(ctx context.Context, actorAccountID string, notificationID string) error
	AdminStats(ctx context.Context) (*domain.AdminStats, error)
	CreateAdministratorNotice(
		ctx context.Context,
		actorAccountID string,
		input domain.AdministratorInput,
	) (*domain.AdminItem, error)
}

// Handler exposes guardian notification endpoints.
type Handler struct {
	service Service
}

// New creates a notification HTTP handler.
func New(service Service) *Handler {
	return &Handler{service: service}
}

// AdminHandler exposes administrator notification management endpoints.
type AdminHandler struct {
	service Service
}

// NewAdmin creates an administrator notification HTTP handler.
func NewAdmin(service Service) *AdminHandler {
	return &AdminHandler{service: service}
}

type familyMessageRequest struct {
	DeviceID               string `json:"device_id"`
	Body                   string `json:"body"`
	DisplayDurationSeconds int    `json:"display_duration_seconds"`
	SendToDevice           bool   `json:"send_to_device"`
}

type adminNoticeRequest struct {
	Category               string   `json:"category"`
	Severity               string   `json:"severity"`
	Title                  string   `json:"title"`
	Body                   string   `json:"body"`
	ActionPath             string   `json:"action_path"`
	ActionLabel            string   `json:"action_label"`
	Audience               string   `json:"audience"`
	Channels               []string `json:"channels"`
	ParentAccountID        string   `json:"parent_account_id"`
	DeviceID               string   `json:"device_id"`
	DisplayDurationSeconds int      `json:"display_duration_seconds"`
	ExpiresAt              *string  `json:"expires_at"`
}

// Inbox returns one bounded page of the guardian notification inbox.
func (handler *Handler) Inbox(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	query := request.URL.Query()
	limit := 0
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > domain.MaxPageSize {
			writeError(response, request, http.StatusUnprocessableEntity, "invalid_query", "请检查筛选条件")
			return
		}
		limit = parsed
	}
	page, err := handler.service.Inbox(
		request.Context(),
		parentAccountID,
		limit,
		strings.TrimSpace(query.Get("cursor")),
		strings.TrimSpace(query.Get("category")),
	)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, page)
}

// UnreadCount returns the guardian unread badge value.
func (handler *Handler) UnreadCount(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	count, err := handler.service.UnreadCount(request.Context(), parentAccountID)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"unread_count": count,
	})
}

// MarkRead marks one guardian-owned notification as read.
func (handler *Handler) MarkRead(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	if err := handler.service.MarkRead(
		request.Context(),
		parentAccountID,
		strings.TrimSpace(request.PathValue("notification_id")),
	); err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"read": true})
}

// MarkAllRead marks every unread notification for the guardian.
func (handler *Handler) MarkAllRead(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	if err := handler.service.MarkAllRead(request.Context(), parentAccountID); err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"read": true})
}

// SendFamilyMessage stores and optionally delivers a guardian message.
func (handler *Handler) SendFamilyMessage(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	var payload familyMessageRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请填写要发送的内容")
		return
	}
	item, err := handler.service.SendFamilyMessage(
		request.Context(),
		parentAccountID,
		domain.GuardianMessageInput{
			DeviceID:               payload.DeviceID,
			Body:                   payload.Body,
			DisplayDurationSeconds: payload.DisplayDurationSeconds,
			SendToDevice:           payload.SendToDevice,
		},
	)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"notification": item,
	})
}

// DeviceMessages returns pending device display messages for one session.
func (handler *Handler) DeviceMessages(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	limit := 0
	if value := strings.TrimSpace(request.URL.Query().Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 20 {
			writeError(response, request, http.StatusUnprocessableEntity, "invalid_query", "请检查筛选条件")
			return
		}
		limit = parsed
	}
	messages, err := handler.service.DeviceMessages(
		request.Context(),
		parentAccountID,
		strings.TrimSpace(request.PathValue("device_id")),
		limit,
	)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"messages": messages,
	})
}

// AdminList returns a filtered, paged operator projection.
func (handler *AdminHandler) AdminList(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	filter, err := parseAdminFilter(request)
	if err != nil {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_query", "请检查筛选条件")
		return
	}
	page, err := handler.service.AdminList(request.Context(), filter)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, page)
}

// AdminGet returns one operator projection.
func (handler *AdminHandler) AdminGet(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	item, err := handler.service.AdminGet(
		request.Context(),
		strings.TrimSpace(request.PathValue("notification_id")),
	)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"notification": item,
	})
}

// AdminCreate authors and fans out one operator notification.
func (handler *AdminHandler) AdminCreate(
	response http.ResponseWriter,
	request *http.Request,
	actorAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	if actorAccountID == "" {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload adminNoticeRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查填写的内容")
		return
	}
	var expiresAt *time.Time
	if payload.ExpiresAt != nil && strings.TrimSpace(*payload.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*payload.ExpiresAt))
		if err != nil {
			writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查填写的内容")
			return
		}
		expiresAt = &parsed
	}
	item, err := handler.service.CreateAdministratorNotice(
		request.Context(),
		actorAccountID,
		domain.AdministratorInput{
			Category:               payload.Category,
			Severity:               payload.Severity,
			Title:                  payload.Title,
			Body:                   payload.Body,
			ActionPath:             payload.ActionPath,
			ActionLabel:            payload.ActionLabel,
			Audience:               payload.Audience,
			Channels:               payload.Channels,
			ParentAccountID:        payload.ParentAccountID,
			DeviceID:               payload.DeviceID,
			DisplayDurationSeconds: payload.DisplayDurationSeconds,
			ExpiresAt:              expiresAt,
		},
	)
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"notification": item,
	})
}

// AdminDelete removes one notification.
func (handler *AdminHandler) AdminDelete(
	response http.ResponseWriter,
	request *http.Request,
	actorAccountID string,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	if err := handler.service.AdminDelete(
		request.Context(),
		actorAccountID,
		strings.TrimSpace(request.PathValue("notification_id")),
	); err != nil {
		writeServiceError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

// AdminStats returns the operator counters.
func (handler *AdminHandler) AdminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler == nil || handler.service == nil {
		handlerUnavailable(response, request)
		return
	}
	stats, err := handler.service.AdminStats(request.Context())
	if err != nil {
		writeServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

func parseAdminFilter(request *http.Request) (domain.AdminFilter, error) {
	query := request.URL.Query()
	filter := domain.AdminFilter{
		Category: strings.TrimSpace(query.Get("category")),
		Audience: strings.TrimSpace(query.Get("audience")),
		Source:   strings.TrimSpace(query.Get("source")),
		Query:    strings.TrimSpace(query.Get("query")),
	}
	if value := strings.TrimSpace(query.Get("page")); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page < 1 {
			return domain.AdminFilter{}, domain.ErrInvalidPage
		}
		filter.Page = page
	}
	if value := strings.TrimSpace(query.Get("page_size")); value != "" {
		pageSize, err := strconv.Atoi(value)
		if err != nil || pageSize < 1 || pageSize > domain.MaxListLimit {
			return domain.AdminFilter{}, domain.ErrInvalidPage
		}
		filter.PageSize = pageSize
	}
	return filter, nil
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
	case errors.Is(err, domain.ErrInvalidNotification),
		errors.Is(err, domain.ErrInvalidPage):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查填写的内容")
	case errors.Is(err, domain.ErrNotificationNotFound):
		writeError(response, request, http.StatusNotFound, "notification_not_found", "没有找到这条通知")
	case errors.Is(err, domain.ErrNotificationForbidden):
		writeError(response, request, http.StatusForbidden, "notification_forbidden", "没有权限操作这条通知")
	case errors.Is(err, domain.ErrNotificationExpired):
		writeError(response, request, http.StatusGone, "notification_expired", "这条通知已经失效")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	companionshipdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/domain"
	companionshipservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/service"
)

// companionshipService is the guardian-facing companionship surface.
//
// The HTTP layer depends on this narrow interface so the module stays
// removable and tests can substitute a small fake.
type companionshipService interface {
	CreateGrowthRecord(
		ctx context.Context,
		familyID string,
		input companionshipdomain.GrowthRecordInput,
	) (*companionshipdomain.GrowthRecord, error)
	ListGrowthRecords(
		ctx context.Context,
		familyID string,
		query companionshipdomain.GrowthRecordQuery,
	) ([]companionshipdomain.GrowthRecord, error)
	DeleteGrowthRecord(ctx context.Context, familyID string, recordID string) error

	CreateHabitReminder(
		ctx context.Context,
		familyID string,
		input companionshipdomain.HabitReminderInput,
	) (*companionshipdomain.HabitReminder, error)
	UpdateHabitReminder(
		ctx context.Context,
		familyID string,
		reminderID string,
		input companionshipdomain.HabitReminderInput,
	) (*companionshipdomain.HabitReminder, error)
	ListHabitReminders(
		ctx context.Context,
		familyID string,
	) ([]companionshipdomain.HabitReminder, error)
	DeleteHabitReminder(ctx context.Context, familyID string, reminderID string) error
}

type companionshipHandler struct {
	service companionshipService
}

type growthRecordRequest struct {
	ChildID  string `json:"child_id"`
	Category string `json:"category"`
	Title    string `json:"title"`
	Note     string `json:"note"`
}

type habitReminderRequest struct {
	ChildID      string `json:"child_id"`
	Label        string `json:"label"`
	Prompt       string `json:"prompt"`
	ReminderTime string `json:"reminder_time"`
	Weekdays     []int  `json:"weekdays"`
	Enabled      *bool  `json:"enabled"`
}

func (handler companionshipHandler) listGrowthRecords(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	query := companionshipdomain.GrowthRecordQuery{
		ChildID:  strings.TrimSpace(request.URL.Query().Get("child_id")),
		Category: strings.TrimSpace(request.URL.Query().Get("category")),
		Limit:    parsePositiveQueryInt(request.URL.Query().Get("limit"), 0),
	}
	if before := strings.TrimSpace(request.URL.Query().Get("before")); before != "" {
		parsed, err := time.Parse(time.RFC3339, before)
		if err != nil {
			writeError(response, request, http.StatusUnprocessableEntity, "invalid_before", "请检查分页时间")
			return
		}
		query.BeforeTime = &parsed
	}
	records, err := handler.service.ListGrowthRecords(request.Context(), familyID, query)
	if err != nil {
		writeCompanionshipError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(records))
	for index := range records {
		result = append(result, growthRecordResponse(&records[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"growth_records": result,
	})
}

func (handler companionshipHandler) createGrowthRecord(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload growthRecordRequest
	if err := decodeOptionalJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查成长记录内容")
		return
	}
	record, err := handler.service.CreateGrowthRecord(
		request.Context(),
		familyID,
		companionshipdomain.GrowthRecordInput{
			ChildID:  payload.ChildID,
			Category: payload.Category,
			Title:    payload.Title,
			Note:     payload.Note,
		},
	)
	if err != nil {
		writeCompanionshipError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"growth_record": growthRecordResponse(record),
	})
}

func (handler companionshipHandler) deleteGrowthRecord(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	recordID := strings.TrimSpace(request.PathValue("record_id"))
	if err := handler.service.DeleteGrowthRecord(request.Context(), familyID, recordID); err != nil {
		writeCompanionshipError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"record_id": recordID,
	})
}

func (handler companionshipHandler) listHabitReminders(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	reminders, err := handler.service.ListHabitReminders(request.Context(), familyID)
	if err != nil {
		writeCompanionshipError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(reminders))
	for index := range reminders {
		result = append(result, habitReminderResponse(&reminders[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"habit_reminders": result,
	})
}

func (handler companionshipHandler) createHabitReminder(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload habitReminderRequest
	if err := decodeOptionalJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查习惯提醒内容")
		return
	}
	reminder, err := handler.service.CreateHabitReminder(
		request.Context(),
		familyID,
		habitReminderInput(payload),
	)
	if err != nil {
		writeCompanionshipError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"habit_reminder": habitReminderResponse(reminder),
	})
}

func (handler companionshipHandler) updateHabitReminder(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload habitReminderRequest
	if err := decodeOptionalJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查习惯提醒内容")
		return
	}
	reminderID := strings.TrimSpace(request.PathValue("reminder_id"))
	reminder, err := handler.service.UpdateHabitReminder(
		request.Context(),
		familyID,
		reminderID,
		habitReminderInput(payload),
	)
	if err != nil {
		writeCompanionshipError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"habit_reminder": habitReminderResponse(reminder),
	})
}

func (handler companionshipHandler) deleteHabitReminder(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	reminderID := strings.TrimSpace(request.PathValue("reminder_id"))
	if err := handler.service.DeleteHabitReminder(request.Context(), familyID, reminderID); err != nil {
		writeCompanionshipError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"reminder_id": reminderID,
	})
}

// habitReminderInput defaults the enabled flag to true so a client that omits
// the field creates an active reminder instead of a silent disabled one.
func habitReminderInput(payload habitReminderRequest) companionshipdomain.HabitReminderInput {
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	return companionshipdomain.HabitReminderInput{
		ChildID:      payload.ChildID,
		Label:        payload.Label,
		Prompt:       payload.Prompt,
		ReminderTime: payload.ReminderTime,
		Weekdays:     payload.Weekdays,
		Enabled:      enabled,
	}
}

func growthRecordResponse(record *companionshipdomain.GrowthRecord) map[string]any {
	return map[string]any{
		"id":          record.ID,
		"child_id":    record.ChildID,
		"category":    record.Category,
		"title":       record.Title,
		"note":        record.Note,
		"recorded_at": record.RecordedAt.UTC().Format(time.RFC3339),
		"created_at":  record.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func habitReminderResponse(reminder *companionshipdomain.HabitReminder) map[string]any {
	weekdays := make([]int, len(reminder.Weekdays))
	copy(weekdays, reminder.Weekdays)
	return map[string]any{
		"id":            reminder.ID,
		"child_id":      reminder.ChildID,
		"label":         reminder.Label,
		"prompt":        reminder.Prompt,
		"reminder_time": reminder.ReminderTime,
		"weekdays":      weekdays,
		"enabled":       reminder.Enabled,
		"created_at":    reminder.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":    reminder.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func writeCompanionshipError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, companionshipdomain.ErrGrowthRecordNotFound):
		writeError(response, request, http.StatusNotFound, "growth_record_not_found", "没有找到这条成长记录")
	case errors.Is(err, companionshipdomain.ErrHabitReminderNotFound):
		writeError(response, request, http.StatusNotFound, "habit_reminder_not_found", "没有找到这个习惯提醒")
	case errors.Is(err, companionshipdomain.ErrInvalidGrowthCategory):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_growth_category", "请选择有效的成长记录分类")
	case errors.Is(err, companionshipdomain.ErrInvalidGrowthRecord):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_growth_record", "请检查成长记录标题和备注")
	case errors.Is(err, companionshipdomain.ErrInvalidReminderLabel):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_reminder_label", "请填写 1 到 40 个字的提醒名称")
	case errors.Is(err, companionshipdomain.ErrInvalidReminderPrompt):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_reminder_prompt", "提醒内容太长，请精简到 200 字以内")
	case errors.Is(err, companionshipdomain.ErrInvalidReminderTime):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_reminder_time", "请使用 HH:MM 的 24 小时时间")
	case errors.Is(err, companionshipdomain.ErrInvalidReminderWeekday):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_reminder_weekday", "请选择有效的星期")
	case errors.Is(err, companionshipdomain.ErrInvalidHabitReminder):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_habit_reminder", "请检查习惯提醒内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

var _ companionshipService = (*companionshipservice.Service)(nil)

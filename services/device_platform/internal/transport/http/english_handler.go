package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	englishdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
	englishservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/service"
)

// englishService is the guardian-facing English learning surface.
//
// The HTTP layer depends on this narrow interface so the module stays
// removable and tests can substitute a small fake.
type englishService interface {
	ListLessons(ctx context.Context, ageTier string) ([]englishdomain.Lesson, error)
	GetLesson(ctx context.Context, lessonID string) (*englishdomain.Lesson, error)
	LessonCatalog(ctx context.Context, ageTier string) ([]englishdomain.Lesson, error)
	RecordAttempt(
		ctx context.Context,
		familyID string,
		input englishdomain.AttemptInput,
	) (*englishdomain.PracticeAttempt, error)
	ListAttempts(
		ctx context.Context,
		familyID string,
		query englishdomain.AttemptQuery,
	) ([]englishdomain.PracticeAttempt, error)
	Progress(
		ctx context.Context,
		familyID string,
		query englishdomain.AttemptQuery,
	) ([]englishdomain.LessonProgress, []englishdomain.ProgressItem, error)
}

// englishSessionResolver verifies a device session and resolves the owning
// family, so a device can report a practice attempt without a guardian token.
type englishSessionResolver interface {
	VerifyDeviceSession(ctx context.Context, deviceSessionToken string) (string, error)
}

// englishRuntimeResolver resolves the family that owns a verified device
// session. The narrow interface keeps the HTTP handler independent of the
// runtime module and lets tests substitute a small fake.
type englishRuntimeResolver interface {
	ResolveDeviceFamily(
		ctx context.Context,
		deviceSessionToken string,
		pathDeviceID string,
	) (string, error)
}

type englishHandler struct {
	service englishService
}

// deviceEnglishHandler wraps the practice-attempt write with device-session
// verification so firmware can report pronunciation feedback.
type deviceEnglishHandler struct {
	englishHandler *englishHandler
	bindingService englishSessionResolver
	runtimeService englishRuntimeResolver
}

type practiceAttemptRequest struct {
	ChildID        string `json:"child_id"`
	LessonID       string `json:"lesson_id"`
	ItemID         string `json:"item_id"`
	ItemType       string `json:"item_type"`
	TargetText     string `json:"target_text"`
	RecognizedText string `json:"recognized_text"`
	Score          int    `json:"score"`
}

func (handler englishHandler) listLessons(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, ok := authenticatedFamilyOwnerID(request); !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	lessons, err := handler.service.ListLessons(
		request.Context(),
		strings.TrimSpace(request.URL.Query().Get("age_tier")),
	)
	if err != nil {
		writeEnglishError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(lessons))
	for index := range lessons {
		result = append(result, lessonSummaryResponse(&lessons[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"lessons": result,
	})
}

func (handler englishHandler) getLesson(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, ok := authenticatedFamilyOwnerID(request); !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	lesson, err := handler.service.GetLesson(
		request.Context(),
		strings.TrimSpace(request.PathValue("lesson_id")),
	)
	if err != nil {
		writeEnglishError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"lesson": lessonDetailResponse(lesson),
	})
}

func (handler englishHandler) listProgress(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	query := englishdomain.AttemptQuery{
		ChildID:  strings.TrimSpace(request.URL.Query().Get("child_id")),
		LessonID: strings.TrimSpace(request.URL.Query().Get("lesson_id")),
		Limit:    parsePositiveQueryInt(request.URL.Query().Get("limit"), 0),
	}
	lessons, items, err := handler.service.Progress(request.Context(), familyID, query)
	if err != nil {
		writeEnglishError(response, request, err)
		return
	}
	lessonResult := make([]map[string]any, 0, len(lessons))
	for index := range lessons {
		lessonResult = append(lessonResult, lessonProgressResponse(&lessons[index]))
	}
	itemResult := make([]map[string]any, 0, len(items))
	for index := range items {
		itemResult = append(itemResult, progressItemResponse(&items[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"lesson_progress": lessonResult,
		"item_progress":   itemResult,
	})
}

func (handler englishHandler) listAttempts(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	query := englishdomain.AttemptQuery{
		ChildID:  strings.TrimSpace(request.URL.Query().Get("child_id")),
		LessonID: strings.TrimSpace(request.URL.Query().Get("lesson_id")),
		Limit:    parsePositiveQueryInt(request.URL.Query().Get("limit"), 0),
	}
	attempts, err := handler.service.ListAttempts(request.Context(), familyID, query)
	if err != nil {
		writeEnglishError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(attempts))
	for index := range attempts {
		result = append(result, attemptResponse(&attempts[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"attempts": result,
	})
}

// recordAttempt stores one device practice attempt after verifying the session
// token belongs to the path device.
func (handler deviceEnglishHandler) recordAttempt(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := handler.verifyDeviceFamily(response, request)
	if !ok {
		return
	}
	var payload practiceAttemptRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查练习数据")
		return
	}
	attempt, err := handler.englishHandler.service.RecordAttempt(
		request.Context(),
		familyID,
		englishdomain.AttemptInput{
			ChildID:        payload.ChildID,
			LessonID:       payload.LessonID,
			ItemID:         payload.ItemID,
			ItemType:       payload.ItemType,
			TargetText:     payload.TargetText,
			RecognizedText: payload.RecognizedText,
			Score:          payload.Score,
		},
	)
	if err != nil {
		writeEnglishError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"attempt": attemptResponse(attempt),
	})
}

// listLessons returns the published English lesson catalogue for the path
// device so the firmware can cache it and run the learning flow offline. The
// catalogue is global published content, but the device session is still
// verified so the manifest never leaks to an unauthenticated caller.
func (handler deviceEnglishHandler) listLessons(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, ok := handler.verifyDeviceFamily(response, request); !ok {
		return
	}
	lessons, err := handler.englishHandler.service.LessonCatalog(
		request.Context(),
		strings.TrimSpace(request.URL.Query().Get("age_tier")),
	)
	if err != nil {
		writeEnglishError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(lessons))
	for index := range lessons {
		result = append(result, lessonDetailResponse(&lessons[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"lessons": result,
	})
}

func (handler deviceEnglishHandler) verifyDeviceFamily(
	response http.ResponseWriter,
	request *http.Request,
) (string, bool) {
	if handler.bindingService == nil || handler.runtimeService == nil {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", false
	}
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", false
	}
	pathDeviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionDeviceID, err := handler.bindingService.VerifyDeviceSession(
		request.Context(),
		deviceSessionToken,
	)
	if err != nil || sessionDeviceID != pathDeviceID {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", false
	}
	familyID, err := handler.runtimeService.ResolveDeviceFamily(
		request.Context(),
		deviceSessionToken,
		pathDeviceID,
	)
	if err != nil {
		writeEnglishError(response, request, err)
		return "", false
	}
	return familyID, true
}

func lessonSummaryResponse(lesson *englishdomain.Lesson) map[string]any {
	return map[string]any{
		"lesson_id":      lesson.LessonID,
		"age_tier":       lesson.AgeTier,
		"title":          lesson.Title,
		"summary":        lesson.Summary,
		"sequence":       lesson.Sequence,
		"word_count":     len(lesson.Words),
		"sentence_count": len(lesson.Sentence),
	}
}

func lessonDetailResponse(lesson *englishdomain.Lesson) map[string]any {
	words := make([]map[string]any, 0, len(lesson.Words))
	for index := range lesson.Words {
		word := &lesson.Words[index]
		words = append(words, map[string]any{
			"id":          word.ID,
			"position":    word.Position,
			"term":        word.Term,
			"phonetic":    word.Phonetic,
			"translation": word.Translation,
			"example":     word.Example,
		})
	}
	sentences := make([]map[string]any, 0, len(lesson.Sentence))
	for index := range lesson.Sentence {
		sentence := &lesson.Sentence[index]
		sentences = append(sentences, map[string]any{
			"id":           sentence.ID,
			"position":     sentence.Position,
			"pattern":      sentence.Pattern,
			"prompt":       sentence.Prompt,
			"model_answer": sentence.ModelAnswer,
		})
	}
	return map[string]any{
		"lesson_id": lesson.LessonID,
		"age_tier":  lesson.AgeTier,
		"title":     lesson.Title,
		"summary":   lesson.Summary,
		"sequence":  lesson.Sequence,
		"status":    lesson.Status,
		"words":     words,
		"sentences": sentences,
	}
}

func attemptResponse(attempt *englishdomain.PracticeAttempt) map[string]any {
	return map[string]any{
		"id":              attempt.ID,
		"child_id":        attempt.ChildID,
		"lesson_id":       attempt.LessonID,
		"item_id":         attempt.ItemID,
		"item_type":       attempt.ItemType,
		"target_text":     attempt.TargetText,
		"recognized_text": attempt.RecognizedText,
		"score":           attempt.Score,
		"passed":          attempt.Passed,
		"practiced_at":    attempt.PracticedAt.UTC().Format(time.RFC3339),
	}
}

func lessonProgressResponse(progress *englishdomain.LessonProgress) map[string]any {
	response := map[string]any{
		"lesson_id":     progress.LessonID,
		"attempts":      progress.Attempts,
		"passed_items":  progress.PassedItems,
		"total_items":   progress.TotalItems,
		"best_score":    progress.BestScore,
		"average_score": progress.AverageScore,
	}
	if !progress.LastPracticed.IsZero() {
		response["last_practiced"] = progress.LastPracticed.UTC().Format(time.RFC3339)
	}
	return response
}

func progressItemResponse(item *englishdomain.ProgressItem) map[string]any {
	return map[string]any{
		"lesson_id":   item.LessonID,
		"item_id":     item.ItemID,
		"item_type":   item.ItemType,
		"target_text": item.TargetText,
		"best_score":  item.BestScore,
		"attempts":    item.Attempts,
		"passed":      item.Passed,
		"last_at":     item.LastAt.UTC().Format(time.RFC3339),
	}
}

func writeEnglishError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, englishdomain.ErrLessonNotFound):
		writeError(response, request, http.StatusNotFound, "lesson_not_found", "没有找到这节英语课程")
	case errors.Is(err, englishdomain.ErrInvalidLessonID):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_lesson_id", "请检查课程编号")
	case errors.Is(err, englishdomain.ErrInvalidAgeTier):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_age_tier", "请选择有效的年龄层")
	case errors.Is(err, englishdomain.ErrInvalidItemType):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_item_type", "请检查练习类型")
	case errors.Is(err, englishdomain.ErrInvalidAttempt):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_attempt", "请检查练习内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

var _ englishService = (*englishservice.Service)(nil)

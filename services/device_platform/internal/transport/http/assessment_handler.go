package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	assessmentdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/domain"
	assessmentservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/service"
)

// guardianAssessmentHandler serves the guardian side of a learning assessment.
type guardianAssessmentHandler struct {
	service *assessmentservice.Service
}

// deviceAssessmentHandler serves the device that is running an assessment. The
// device reads the run it was asked to show and reports each answer it observed;
// starting and finishing stay with the guardian, and a guardian can still record
// an answer from the phone when they are the one listening.
type deviceAssessmentHandler struct {
	service *assessmentservice.Service
}

// start opens an assessment on a lesson the family may practise.
func (handler guardianAssessmentHandler) start(
	response http.ResponseWriter,
	request *http.Request,
) {
	guardianID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请先登录")
		return
	}
	var payload struct {
		LessonID string `json:"lesson_id"`
		ChildID  string `json:"child_id"`
		DeviceID string `json:"device_id"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	result, err := handler.service.Start(request.Context(), assessmentdomain.StartRequest{
		FamilyID: request.PathValue("family_id"),
		ChildID:  payload.ChildID,
		LessonID: payload.LessonID,
		DeviceID: payload.DeviceID,
	})
	if err != nil {
		writeAssessmentError(response, request, err)
		return
	}
	body := assessmentResponse(result.Session)
	body["next_item"] = assessmentItemResponse(result.NextItem)
	body["started_by"] = guardianID
	writeSuccess(response, request, http.StatusCreated, body)
}

// get answers with one session owned by the family, together with its answers.
func (handler guardianAssessmentHandler) get(
	response http.ResponseWriter,
	request *http.Request,
) {
	session, answers, err := handler.service.Detail(
		request.Context(),
		request.PathValue("family_id"),
		request.PathValue("session_id"),
	)
	if err != nil {
		writeAssessmentError(response, request, err)
		return
	}
	body := assessmentResponse(session)
	body["answers"] = assessmentAnswersResponse(answers)
	writeSuccess(response, request, http.StatusOK, body)
}

// answer records one answer and answers the device with the next item.
func (handler guardianAssessmentHandler) answer(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		Position   *int   `json:"position"`
		ItemID     string `json:"item_id"`
		ItemType   string `json:"item_type"`
		TargetText string `json:"target_text"`
		Score      int    `json:"score"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	input := assessmentdomain.AnswerInput{
		ItemID:     payload.ItemID,
		ItemType:   payload.ItemType,
		TargetText: payload.TargetText,
		Score:      payload.Score,
	}
	if payload.Position != nil {
		input.Position = *payload.Position
	}
	result, err := handler.service.Answer(
		request.Context(),
		request.PathValue("family_id"),
		request.PathValue("session_id"),
		input,
	)
	if err != nil {
		writeAssessmentError(response, request, err)
		return
	}
	body := assessmentResponse(result.Session)
	if !result.NextItem.Empty() {
		body["next_item"] = assessmentItemResponse(result.NextItem)
	}
	writeSuccess(response, request, http.StatusCreated, body)
}

// finish completes an active session on purpose.
func (handler guardianAssessmentHandler) finish(
	response http.ResponseWriter,
	request *http.Request,
) {
	session, err := handler.service.Finish(
		request.Context(),
		request.PathValue("family_id"),
		request.PathValue("session_id"),
	)
	if err != nil {
		writeAssessmentError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, assessmentResponse(session))
}

// history lists a family's past sessions.
func (handler guardianAssessmentHandler) history(
	response http.ResponseWriter,
	request *http.Request,
) {
	limit := 0
	if raw := request.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	sessions, err := handler.service.History(
		request.Context(),
		request.PathValue("family_id"),
		request.URL.Query().Get("child_id"),
		limit,
	)
	if err != nil {
		writeAssessmentError(response, request, err)
		return
	}
	payload := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		payload = append(payload, assessmentResponse(session))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"sessions": payload})
}

// current answers the device with the assessment it was asked to show.
func (handler deviceAssessmentHandler) current(
	response http.ResponseWriter,
	request *http.Request,
) {
	session, err := handler.service.GetForDevice(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("session_id"),
	)
	if err != nil {
		writeAssessmentError(response, request, err)
		return
	}
	body := assessmentResponse(session)
	if !session.State.IsTerminal() {
		_, item, itemErr := handler.service.CurrentItem(
			request.Context(),
			session.FamilyID,
			session.ID,
		)
		if itemErr == nil && !item.Empty() {
			body["next_item"] = assessmentItemResponse(item)
		}
	}
	writeSuccess(response, request, http.StatusOK, body)
}

// answer records one answer the device observed and replies with the next item.
//
// The device reports what it heard and how well it matched, so practice does not
// depend on a guardian standing by with a phone. It never chooses its own
// position: the next slot is whatever the stored answer count says, which keeps
// a retry from skipping a question or duplicating one.
func (handler deviceAssessmentHandler) answer(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		ItemID     string `json:"item_id"`
		ItemType   string `json:"item_type"`
		TargetText string `json:"target_text"`
		Score      int    `json:"score"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	result, err := handler.service.AnswerForDevice(
		request.Context(),
		request.PathValue("device_id"),
		request.PathValue("session_id"),
		assessmentdomain.AnswerInput{
			ItemID:     payload.ItemID,
			ItemType:   payload.ItemType,
			TargetText: payload.TargetText,
			Score:      payload.Score,
		},
	)
	if err != nil {
		writeAssessmentError(response, request, err)
		return
	}
	body := assessmentResponse(result.Session)
	if !result.NextItem.Empty() {
		body["next_item"] = assessmentItemResponse(result.NextItem)
	}
	writeSuccess(response, request, http.StatusCreated, body)
}

// assessmentResponse exposes the run's lifecycle and progress.
//
// It never carries audio or a transcript. target_text lives in the per-answer
// payload because it is the lesson content the child was asked to say.
func assessmentResponse(session assessmentdomain.Session) map[string]any {
	payload := map[string]any{
		"session_id":     session.ID,
		"child_id":       session.ChildID,
		"lesson_id":      session.LessonID,
		"state":          string(session.State),
		"difficulty":     session.Difficulty,
		"item_index":     session.ItemIndex,
		"correct_count":  session.CorrectCount,
		"answered_count": session.AnsweredCount,
		"streak":         session.Streak,
		"mastery":        session.Mastery(),
		"progress":       session.Progress(),
		"max_items":      assessmentdomain.MaxItemsPerSession,
		"started_at":     session.StartedAt,
		"expires_at":     session.ExpiresAt,
		"finish_reason":  session.FinishReason,
		// Accepting answers tells the client whether to keep offering items, so
		// a reconnecting client never renders a question on a closed session.
		"accepts_answers": session.State.AllowsAnswers() && !session.IsExpired(time.Now().UTC()),
	}
	if session.FinishedAt != nil {
		payload["finished_at"] = session.FinishedAt
	}
	return payload
}

// assessmentItemResponse exposes one course item to render.
func assessmentItemResponse(item assessmentservice.NextItem) map[string]any {
	if item.Empty() {
		return map[string]any{}
	}
	return map[string]any{
		"position":   item.Position,
		"item_id":    item.Item.ID,
		"item_type":  item.Item.Type,
		"text":       item.Item.Text,
		"difficulty": item.Difficulty,
	}
}

// assessmentAnswersResponse exposes the answers already given.
func assessmentAnswersResponse(answers []assessmentdomain.Answer) []map[string]any {
	payload := make([]map[string]any, 0, len(answers))
	for _, answer := range answers {
		payload = append(payload, map[string]any{
			"position":    answer.Position,
			"item_id":     answer.ItemID,
			"item_type":   answer.ItemType,
			"target_text": answer.TargetText,
			"score":       answer.Score,
			"is_correct":  answer.IsCorrect,
			"answered_at": answer.AnsweredAt,
		})
	}
	return payload
}

// writeAssessmentError maps assessment failures to stable codes.
//
// Every message is written for a parent and contains no internal detail: a
// cross-family identifier answers exactly like a missing one, so the endpoint
// cannot be used to discover which sessions exist.
func writeAssessmentError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, assessmentservice.ErrSessionNotFound):
		writeError(response, request, http.StatusNotFound, "assessment_not_found",
			"没有找到这次测评")
	case errors.Is(err, assessmentservice.ErrLessonNotFound):
		writeError(response, request, http.StatusNotFound, "assessment_lesson_not_found",
			"没有找到这节课程")
	case errors.Is(err, assessmentservice.ErrChildNotFound):
		writeError(response, request, http.StatusNotFound, "assessment_child_not_found",
			"没有找到这个孩子的资料")
	case errors.Is(err, assessmentservice.ErrAlreadyActive):
		writeError(response, request, http.StatusConflict, "assessment_already_active",
			"还有一次测评没有结束，请先完成或结束它")
	case errors.Is(err, assessmentservice.ErrConflict):
		writeError(response, request, http.StatusConflict, "assessment_state_changed",
			"这次测评的状态已经变了，请刷新后再试")
	case errors.Is(err, assessmentservice.ErrItemNotFound):
		writeError(response, request, http.StatusUnprocessableEntity, "assessment_item_not_found",
			"这道题不属于当前课程")
	case errors.Is(err, assessmentdomain.ErrRateLimited):
		writeError(response, request, http.StatusTooManyRequests, "assessment_rate_limited",
			"开始测评太频繁了，请休息一会儿再试")
	case errors.Is(err, assessmentdomain.ErrSessionExpired):
		writeError(response, request, http.StatusConflict, "assessment_expired",
			"这次测评已经超时，请重新开始")
	case errors.Is(err, assessmentdomain.ErrInvalidScore):
		writeError(response, request, http.StatusUnprocessableEntity, "assessment_invalid_score",
			"分数需要在 0 到 100 之间")
	case errors.Is(err, assessmentdomain.ErrInvalidItemType):
		writeError(response, request, http.StatusUnprocessableEntity, "assessment_invalid_item_type",
			"请检查这道题的类型")
	case errors.Is(err, assessmentdomain.ErrInvalidAnswer):
		writeError(response, request, http.StatusUnprocessableEntity, "assessment_invalid_answer",
			"请检查这道题的作答内容")
	case errors.Is(err, assessmentdomain.ErrInvalidLessonID),
		errors.Is(err, assessmentdomain.ErrInvalidRequest):
		writeError(response, request, http.StatusUnprocessableEntity, "assessment_invalid_request",
			"请检查课程和孩子的选择")
	default:
		writeError(response, request, http.StatusInternalServerError, "assessment_failed",
			"测评暂时无法进行，请稍后再试")
	}
}

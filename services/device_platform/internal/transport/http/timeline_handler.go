package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/timeline/domain"
	timelineservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/timeline/service"
)

// guardianTimelineHandler serves a child's growth timeline to their family.
//
// It is read-only: the timeline is assembled from records other features
// already store, so there is nothing here to create, edit, or delete.
type guardianTimelineHandler struct {
	service *timelineservice.Service
}

// list answers with one page of the child's timeline, oldest first.
func (handler guardianTimelineHandler) list(response http.ResponseWriter, request *http.Request) {
	limit := 0
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(response, request, http.StatusBadRequest, "timeline_invalid_limit",
				"请检查每页显示的数量")
			return
		}
		limit = parsed
	}
	events, nextCursor, err := handler.service.List(
		request.Context(),
		request.PathValue("family_id"),
		request.URL.Query().Get("child_id"),
		request.URL.Query().Get("before"),
		limit,
	)
	if err != nil {
		writeTimelineError(response, request, err)
		return
	}
	payload := make([]map[string]any, 0, len(events))
	for _, event := range events {
		payload = append(payload, timelineEventResponse(event))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"events": payload,
		// An empty cursor means the guardian has reached the newest entry; the
		// app stops asking for more instead of retrying the same page.
		"next_cursor": nextCursor,
	})
}

// milestones answers with the counters shown above the timeline.
func (handler guardianTimelineHandler) milestones(response http.ResponseWriter, request *http.Request) {
	milestones, err := handler.service.Milestones(
		request.Context(),
		request.PathValue("family_id"),
		request.URL.Query().Get("child_id"),
	)
	if err != nil {
		writeTimelineError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"completed_assessments": milestones.CompletedAssessments,
		"practice_days":         milestones.PracticeDays,
		"current_streak_days":   milestones.CurrentStreakDays,
		"longest_streak_days":   milestones.LongestStreakDays,
		"published_reports":     milestones.PublishedReports,
		"marked_milestones":     milestones.MarkedMilestones,
		"completed_content":     milestones.CompletedContent,
		"total":                 milestones.Total,
	})
}

// timelineEventResponse exposes one card's worth of an event.
//
// It carries a stable kind, a Chinese title and one sentence. It never carries a
// score, a transcript, an audio reference, or the identifier of the row it came
// from: the client renders the line and moves on, and nothing here can be
// replayed or quoted back at the child.
func timelineEventResponse(event domain.Event) map[string]any {
	payload := map[string]any{
		"event_id":    event.ID,
		"kind":        string(event.Kind),
		"occurred_at": event.OccurredAt,
		"title":       event.Title,
		"description": event.Description,
	}
	if event.Subject != "" {
		payload["subject"] = event.Subject
	}
	return payload
}

// writeTimelineError maps timeline failures to stable codes.
//
// Every message explains the situation in a parent's words and offers the next
// step. A cross-family child answers exactly like a missing one, so the endpoint
// cannot be used to discover which child profiles exist.
func writeTimelineError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, timelineservice.ErrChildNotFound):
		writeError(response, request, http.StatusNotFound, "timeline_child_not_found",
			"没有找到这个孩子的资料")
	case errors.Is(err, domain.ErrInvalidCursor):
		writeError(response, request, http.StatusBadRequest, "timeline_invalid_cursor",
			"页面已经刷新过了，请回到时间轴开头重新看看")
	case errors.Is(err, domain.ErrInvalidLimit):
		writeError(response, request, http.StatusBadRequest, "timeline_invalid_limit",
			"每页最多显示 100 条，请调整后再试")
	case errors.Is(err, domain.ErrInvalidScope):
		writeError(response, request, http.StatusBadRequest, "timeline_invalid_request",
			"请先选择要看哪个孩子")
	case errors.Is(err, timelineservice.ErrSourceUnavailable):
		writeError(response, request, http.StatusServiceUnavailable, "timeline_unavailable",
			"成长记录暂时无法查看，请稍后再试")
	default:
		writeError(response, request, http.StatusInternalServerError, "timeline_failed",
			"成长时间轴暂时无法显示，请稍后再试")
	}
}

package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	telemetrydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/telemetry/domain"
)

// telemetryService is the administrator-facing fleet telemetry read surface.
// The HTTP layer depends on this narrow interface so the module stays
// removable and tests can substitute a small fake.
type telemetryService interface {
	Summary(
		ctx context.Context,
		windowDays int,
	) (telemetrydomain.Summary, error)
}

type telemetryHandler struct {
	service telemetryService
}

// summary returns the bounded fleet telemetry projection for administrators.
func (handler telemetryHandler) summary(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays, ok := parseTelemetryWindow(request.URL.Query().Get("window_days"))
	if !ok {
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_window",
			"请选择 1 到 90 天的时间范围",
		)
		return
	}
	summary, err := handler.service.Summary(request.Context(), windowDays)
	if err != nil {
		writeTelemetryError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, summary)
}

// parseTelemetryWindow parses the optional window. An absent value means the
// service default; a present but malformed value is rejected instead of being
// silently replaced by a different report period.
func parseTelemetryWindow(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > telemetrydomain.MaxWindowDays {
		return 0, false
	}
	return value, true
}

func writeTelemetryError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, telemetrydomain.ErrInvalidWindow):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_window",
			"请选择 1 到 90 天的时间范围",
		)
	default:
		writeError(
			response,
			request,
			http.StatusInternalServerError,
			"service_error",
			"操作没有完成，请稍后重试",
		)
	}
}

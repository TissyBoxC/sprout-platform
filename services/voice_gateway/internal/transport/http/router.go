// Package http exposes voice gateway health and internal management endpoints.
package http

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/packages/go/observability"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/usage"
)

// RouterOptions contains dependencies for the voice gateway HTTP transport.
type RouterOptions struct {
	Logger            *slog.Logger
	InternalAPIConfig config.InternalAPIConfig
	UsageRecorder     usage.Recorder
	// RealtimeHandler, when set, is mounted at the device realtime path. The
	// caller owns authentication and upgrade; the router only routes to it.
	RealtimeHandler http.Handler
	RealtimePath    string
}

// NewRouter returns the HTTP router for the voice gateway.
func NewRouter(options RouterOptions) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", readyHandler)

	if options.RealtimeHandler != nil && options.RealtimePath != "" {
		mux.Handle(options.RealtimePath, options.RealtimeHandler)
	}

	if options.InternalAPIConfig.Enabled {
		mux.Handle(
			"GET /internal/v1/runtime",
			requireServiceToken(
				options.InternalAPIConfig.AuthToken,
				http.HandlerFunc(runtimeHandler),
			),
		)
		if options.UsageRecorder != nil {
			usageHandler := usageHandler{recorder: options.UsageRecorder}
			mux.Handle(
				"POST /internal/v1/usage/conversations",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(usageHandler.recordConversation),
				),
			)
		}
	}

	return observability.WithRequestLabels(observability.WithRequestMetadata(
		observability.WithAccessLog(
			mux,
			observability.AccessLogOptions{
				Logger:      logger,
				ServiceName: "voice-gateway",
				Audit:       observability.NewSlogAuditSink(logger),
			},
		),
	))
}

type conversationUsageRequest struct {
	DeviceID   string  `json:"device_id"`
	Model      string  `json:"model"`
	InputSize  int     `json:"input_size"`
	OutputSize int     `json:"output_size"`
	SpentUSD   float64 `json:"spent_usd"`
}

type usageHandler struct {
	recorder usage.Recorder
}

// recordConversation accepts usage only from a trusted server-side caller.
// The source of truth for the owning guardian remains the platform's device
// binding table, which prevents an unbound device from creating billable data.
func (handler usageHandler) recordConversation(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload conversationUsageRequest
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		httpapi.WriteError(
			response,
			request,
			http.StatusBadRequest,
			"invalid_usage",
			"请检查用量记录",
			false,
		)
		return
	}
	payload.DeviceID = strings.TrimSpace(payload.DeviceID)
	payload.Model = strings.TrimSpace(payload.Model)
	if payload.DeviceID == "" ||
		len(payload.DeviceID) > 128 ||
		len(payload.Model) > 128 ||
		payload.InputSize < 0 ||
		payload.OutputSize < 0 ||
		payload.SpentUSD < 0 {
		httpapi.WriteError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_usage",
			"请检查用量记录",
			false,
		)
		return
	}
	if err := handler.recorder.Record(request.Context(), usage.Record{
		DeviceID:   payload.DeviceID,
		Model:      payload.Model,
		InputSize:  payload.InputSize,
		OutputSize: payload.OutputSize,
		SpentUSD:   payload.SpentUSD,
	}); err != nil {
		if errors.Is(err, request.Context().Err()) {
			return
		}
		httpapi.WriteError(
			response,
			request,
			http.StatusInternalServerError,
			"usage_unavailable",
			"用量暂时无法记录，请稍后重试",
			true,
		)
		return
	}
	httpapi.WriteSuccess(response, request, http.StatusOK, map[string]any{
		"recorded": true,
	})
}

func healthHandler(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "voice-gateway",
		"time":    time.Now().UTC(),
	})
}

func readyHandler(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"status": "ready",
	})
}

func runtimeHandler(response http.ResponseWriter, request *http.Request) {
	httpapi.WriteSuccess(response, request, http.StatusOK, map[string]any{
		"service":          "voice-gateway",
		"status":           "running",
		"protocol_version": httpapi.SchemaVersion,
	})
}

func requireServiceToken(expectedToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		providedToken := request.Header.Get("Authorization")
		const bearerPrefix = "Bearer "
		if len(providedToken) <= len(bearerPrefix) ||
			providedToken[:len(bearerPrefix)] != bearerPrefix {
			httpapi.WriteError(
				response,
				request,
				http.StatusUnauthorized,
				"unauthenticated",
				"服务认证失败",
				false,
			)
			return
		}

		providedToken = providedToken[len(bearerPrefix):]
		if subtle.ConstantTimeCompare([]byte(providedToken), []byte(expectedToken)) != 1 {
			httpapi.WriteError(
				response,
				request,
				http.StatusUnauthorized,
				"unauthenticated",
				"服务认证失败",
				false,
			)
			return
		}

		next.ServeHTTP(response, request)
	})
}

func writeJSON(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(payload)
}

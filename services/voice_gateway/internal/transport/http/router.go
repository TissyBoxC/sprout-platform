// Package http exposes voice gateway health and internal management endpoints.
package http

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/packages/go/observability"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/buildinfo"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/intercom"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/session"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/usage"
)

// RouterOptions contains dependencies for the voice gateway HTTP transport.
type RouterOptions struct {
	Logger            *slog.Logger
	BuildVersion      string
	InternalAPIConfig config.InternalAPIConfig
	UsageRecorder     usage.Recorder
	// RealtimeHandler, when set, is mounted at the device realtime path. The
	// caller owns authentication and upgrade; the router only routes to it.
	RealtimeHandler http.Handler
	RealtimePath    string
	// SessionMetrics exposes read-only preprocessing quality for active voice
	// sessions. Values are bounded scalars and never include audio content.
	SessionMetrics interface {
		QualitySnapshots() []session.QualitySnapshot
	}
	// RealtimeEnabled reports whether the device realtime WebSocket endpoint is
	// mounted. Readiness must distinguish "process is up" from "child voice is
	// actually available", otherwise the feature center can show a healthy
	// voice gateway while device audio is unreachable.
	RealtimeEnabled bool
	// VisionService enables the internal camera/vision endpoints. It accepts
	// bounded symbolic task metadata and returns short-lived labels; image
	// bytes never pass through this transport.
	VisionService VisionService
	// IntercomResolver re-reads the authoritative call state from
	// device_platform before a guardian relay may carry audio.
	IntercomResolver IntercomSessionResolver
	// IntercomRelays admits and tracks guardian relays. It is shared with the
	// shutdown path so no relay survives process exit.
	IntercomRelays *intercom.RelayManager
	// IntercomConnector attaches a relay to the device side of a call.
	IntercomConnector intercom.DeviceConnector
	// IntercomTransport bounds the guardian relay endpoint. It is only used
	// when the resolver, relays, and connector are all set.
	IntercomTransport IntercomTransportConfig
	// IntercomEnabled reports whether the guardian intercom feature is
	// configured. Readiness surfaces it so the management plane can tell a
	// disabled relay from a broken one.
	IntercomEnabled bool
}

// NewRouter returns the HTTP router for the voice gateway.
func NewRouter(options RouterOptions) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler(options.BuildVersion))
	mux.HandleFunc("GET /readyz", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]any{
			"status":           "ready",
			"realtime_enabled": options.RealtimeEnabled,
			"intercom_enabled": options.IntercomEnabled,
		})
	})

	if options.RealtimeHandler != nil && options.RealtimePath != "" {
		mux.Handle(options.RealtimePath, withHijackSupport(options.RealtimeHandler))
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
		if options.SessionMetrics != nil {
			mux.Handle(
				"GET /internal/v1/voice/quality",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
						snapshots := options.SessionMetrics.QualitySnapshots()
						if snapshots == nil {
							snapshots = []session.QualitySnapshot{}
						}
						httpapi.WriteSuccess(
							response,
							request,
							http.StatusOK,
							map[string]any{"sessions": snapshots},
						)
					}),
				),
			)
		}
		if options.VisionService != nil {
			vision := visionHandler{service: options.VisionService}
			mux.Handle(
				"POST /internal/v1/vision/tasks",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(vision.analyzeTask),
				),
			)
			mux.Handle(
				"GET /internal/v1/vision/results/{task_id}",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(vision.getResult),
				),
			)
		}
		if options.IntercomResolver != nil &&
			options.IntercomRelays != nil &&
			options.IntercomConnector != nil {
			intercom := intercomHandler{
				resolver:  options.IntercomResolver,
				relays:    options.IntercomRelays,
				connector: options.IntercomConnector,
				logger:    logger,
				config:    options.IntercomTransport,
			}
			// device_platform authorizes the relay after the child accepted.
			mux.Handle(
				"POST /internal/v1/intercom/sessions/{session_id}/relay",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(intercom.authorize),
				),
			)
		}
	}

	// The guardian relay is a public WebSocket route, but it only attaches to
	// a relay the internal authorize endpoint already opened for an active
	// call. It is mounted on its own path so the device conversation endpoint
	// is untouched.
	if options.IntercomResolver != nil &&
		options.IntercomRelays != nil &&
		options.IntercomConnector != nil &&
		options.IntercomTransport.Path != "" {
		guardian := intercomHandler{
			resolver:  options.IntercomResolver,
			relays:    options.IntercomRelays,
			connector: options.IntercomConnector,
			logger:    logger,
			config:    options.IntercomTransport,
		}
		mux.Handle(
			options.IntercomTransport.Path+"/{session_id}",
			withHijackSupport(http.HandlerFunc(guardian.relayGuardian)),
		)
	}

	// The shared access log wraps the response writer to record the status.
	// That wrapper now forwards http.Hijacker, but the gateway keeps its own
	// recovery path as defence in depth: a future middleware that drops the
	// interface again must not silently turn every WebSocket upgrade into 500.
	// The raw server writer is captured before the chain wraps it.
	return withServerConnection(observability.WithRequestLabels(observability.WithRequestMetadata(
		observability.WithAccessLog(
			mux,
			observability.AccessLogOptions{
				Logger:      logger,
				ServiceName: "voice-gateway",
				Audit:       observability.NewSlogAuditSink(logger),
			},
		),
	)))
}

// serverConnectionKey stores the raw server response writer in the request
// context so a downstream handler can recover connection hijacking.
type serverConnectionKey struct{}

// withServerConnection records the raw server writer before middleware wraps it.
func withServerConnection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		contextWithConnection := context.WithValue(request.Context(), serverConnectionKey{}, response)
		next.ServeHTTP(response, request.WithContext(contextWithConnection))
	})
}

// hijackableResponse forwards connection hijacking to the raw server writer
// captured at the start of the middleware chain.
//
// gorilla/websocket needs http.Hijacker to take over the socket. The shared
// access-log middleware swaps in a status-recording writer that does not
// implement it, so a WebSocket route behind that middleware answers 500. The
// raw writer recorded by withServerConnection is the only layer that still
// owns the connection.
type hijackableResponse struct {
	http.ResponseWriter
	connection http.ResponseWriter
}

func (response hijackableResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := response.connection.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("server response writer does not support connection hijacking")
	}
	return hijacker.Hijack()
}

// withHijackSupport restores http.Hijacker on a WebSocket route whose response
// writer was wrapped by a status-recording middleware.
func withHijackSupport(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if _, ok := response.(http.Hijacker); ok {
			next.ServeHTTP(response, request)
			return
		}
		connection, _ := request.Context().Value(serverConnectionKey{}).(http.ResponseWriter)
		if connection == nil {
			connection = response
		}
		next.ServeHTTP(hijackableResponse{ResponseWriter: response, connection: connection}, request)
	})
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

func healthHandler(buildVersion string) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		if strings.TrimSpace(buildVersion) == "" {
			buildVersion = buildinfo.Current()
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"status":  "ok",
			"service": "voice-gateway",
			"version": buildVersion,
			"time":    time.Now().UTC(),
		})
	}
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

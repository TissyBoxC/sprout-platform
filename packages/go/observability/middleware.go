package observability

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const (
	// ServiceField is the canonical service-name log field.
	ServiceField = "service"
	// RequestIDField is the canonical request correlation log field.
	RequestIDField = "request_id"
	// TraceIDField is the canonical distributed trace log field.
	TraceIDField = "trace_id"
)

// AccessLogOptions configures the structured request log.
type AccessLogOptions struct {
	Logger      *slog.Logger
	ServiceName string
	Audit       AuditSink
}

// AuditEvent is the minimum non-sensitive audit record emitted after a mutating
// request. Actor and resource details belong to the owning service.
type AuditEvent struct {
	Service   string
	Method    string
	Path      string
	Status    int
	RequestID string
	TraceID   string
}

// AuditSink accepts audit events without coupling middleware to persistence.
type AuditSink interface {
	RecordAudit(event AuditEvent)
}

// WithAccessLog logs one sanitized record per request and emits audit records
// for mutating HTTP methods.
func WithAccessLog(next http.Handler, options AccessLogOptions) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		recorder := newResponseRecorder(response)

		next.ServeHTTP(recorder, request)

		metadata := MetadataFromContext(request.Context())
		labels := RequestLabelsFromContext(request.Context())
		fields := []any{
			ServiceField, options.ServiceName,
			"method", request.Method,
			"path", request.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			RequestIDField, metadata.RequestID,
			TraceIDField, metadata.TraceID,
		}
		fields = appendRequestLabelFields(fields, labels)
		logger.Info("http request completed", fields...)

		if options.Audit != nil && isMutatingMethod(request.Method) {
			options.Audit.RecordAudit(AuditEvent{
				Service:   options.ServiceName,
				Method:    request.Method,
				Path:      request.URL.Path,
				Status:    recorder.status,
				RequestID: metadata.RequestID,
				TraceID:   metadata.TraceID,
			})
		}
	})
}

func appendRequestLabelFields(fields []any, labels RequestLabels) []any {
	if labels.TenantID != "" {
		fields = append(fields, TenantIDField, labels.TenantID)
	}
	if labels.DeviceID != "" {
		fields = append(fields, DeviceIDField, labels.DeviceID)
	}
	if labels.RequestPurpose != "" {
		fields = append(fields, RequestPurposeField, labels.RequestPurpose)
	}
	if labels.PolicyVersion != "" {
		fields = append(fields, PolicyVersionField, labels.PolicyVersion)
	}
	if labels.SessionID != "" {
		fields = append(fields, SessionIDField, labels.SessionID)
	}
	return fields
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// SlogAuditSink writes audit events through the platform logger without request
// bodies, credentials, child data, or provider payloads.
type SlogAuditSink struct {
	logger *slog.Logger
}

// NewSlogAuditSink creates the default structured audit sink.
func NewSlogAuditSink(logger *slog.Logger) *SlogAuditSink {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlogAuditSink{logger: logger}
}

// RecordAudit implements AuditSink.
func (sink *SlogAuditSink) RecordAudit(event AuditEvent) {
	sink.logger.Info(
		"audit event",
		ServiceField, event.Service,
		"method", event.Method,
		"path", event.Path,
		"status", event.Status,
		RequestIDField, event.RequestID,
		TraceIDField, event.TraceID,
	)
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func newResponseRecorder(response http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		ResponseWriter: response,
		status:         http.StatusOK,
	}
}

func (recorder *responseRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *responseRecorder) Write(payload []byte) (int, error) {
	return recorder.ResponseWriter.Write(payload)
}

// Hijack forwards the connection takeover a WebSocket upgrade needs.
//
// Without this method the wrapped writer no longer satisfies http.Hijacker, so
// every upgrade handled behind this middleware failed with HTTP 500 while the
// handlers themselves were correct. The status is left untouched: a hijacked
// connection writes its own protocol frames, not an HTTP response.
func (recorder *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := recorder.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

// Flush forwards the flush used by streaming responses for the same reason:
// wrapping a writer must not silently drop optional interfaces.
func (recorder *responseRecorder) Flush() {
	if flusher, ok := recorder.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

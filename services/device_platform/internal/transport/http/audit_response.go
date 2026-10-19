package http

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
)

const (
	auditActionUnauthenticated = "audit.actor.unauthenticated"
	auditActionUnavailable     = "audit.recorder.unavailable"
)

// auditPreservesWriter is implemented by response writers whose payload must
// stay streamable or whose upload deadline must reach the real connection.
// The audit response wrapper must never be used for these routes.
type auditPreservesWriter interface {
	auditPreservesStreaming()
}

type streamingResponseWriter struct {
	http.ResponseWriter
}

func (streamingResponseWriter) auditPreservesStreaming() {}

// wrapStreamingResponseWriter marks a large or long-lived response so the
// audit middleware forwards it directly instead of buffering its body.
func wrapStreamingResponseWriter(response http.ResponseWriter) http.ResponseWriter {
	if response == nil {
		return response
	}
	if _, ok := response.(auditPreservesWriter); ok {
		return response
	}
	return streamingResponseWriter{ResponseWriter: response}
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (writer streamingResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

// Flush keeps streaming and proxy middleware compatible.
func (writer streamingResponseWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack exposes WebSocket-capable transports to middleware that forwards the
// wrapped response.
func (writer streamingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

// bufferedResponseRecorder captures one ordinary JSON response so the success
// audit can either replay it unchanged or turn it into an accepted response
// with an audit-failure notice. Streaming responses must not use this wrapper.
type bufferedResponseRecorder struct {
	header      http.Header
	status      int
	body        bytes.Buffer
	wroteHeader bool
}

func newBufferedResponseRecorder() *bufferedResponseRecorder {
	return &bufferedResponseRecorder{
		header: make(http.Header),
		status: http.StatusOK,
	}
}

func (recorder *bufferedResponseRecorder) Header() http.Header {
	return recorder.header
}

func (recorder *bufferedResponseRecorder) WriteHeader(status int) {
	if recorder.wroteHeader {
		return
	}
	recorder.status = status
	recorder.wroteHeader = true
}

func (recorder *bufferedResponseRecorder) Write(payload []byte) (int, error) {
	if !recorder.wroteHeader {
		recorder.wroteHeader = true
	}
	return recorder.body.Write(payload)
}

func (recorder *bufferedResponseRecorder) flushTo(response http.ResponseWriter) {
	copyHeaders(response.Header(), recorder.header)
	response.WriteHeader(recorder.status)
	_, _ = response.Write(recorder.body.Bytes())
}

func (recorder *bufferedResponseRecorder) decodedBody() any {
	var data any
	if err := json.Unmarshal(recorder.body.Bytes(), &data); err != nil {
		return map[string]any{}
	}
	envelope, ok := data.(map[string]any)
	if !ok {
		return data
	}
	if value, exists := envelope["data"]; exists {
		return value
	}
	return data
}

func copyHeaders(destination http.Header, source http.Header) {
	for key, values := range source {
		destination[key] = append([]string(nil), values...)
	}
}

package observability_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/packages/go/observability"
)

// TestAccessLogPreservesHijack guards the WebSocket upgrade path.
//
// The access-log middleware wraps response writers. If the wrapper does not
// forward http.Hijacker, every WebSocket upgrade behind it fails with HTTP 500
// even though the handler is correct — which is exactly what happened before
// this method existed.
func TestAccessLogPreservesHijack(t *testing.T) {
	upgraded := make(chan struct{})

	handler := observability.WithAccessLog(
		http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			hijacker, ok := response.(http.Hijacker)
			if !ok {
				http.Error(response, "hijack unsupported", http.StatusInternalServerError)
				return
			}
			connection, buffer, err := hijacker.Hijack()
			if err != nil {
				return
			}
			defer connection.Close()
			// Writing a raw line proves the connection really was taken over.
			_, _ = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\n")
			_ = buffer.Flush()
			close(upgraded)
		}),
		observability.AccessLogOptions{ServiceName: "test-service"},
	)

	server := httptest.NewServer(handler)
	defer server.Close()

	address := strings.TrimPrefix(server.URL, "http://")
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer connection.Close()

	if _, err := connection.Write([]byte("GET /ws HTTP/1.1\r\nHost: test\r\n\r\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}

	select {
	case <-upgraded:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never hijacked the connection through the access-log middleware")
	}
}

// TestAccessLogPreservesFlush guards streaming responses for the same reason.
func TestAccessLogPreservesFlush(t *testing.T) {
	flushed := false
	handler := observability.WithAccessLog(
		http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			if flusher, ok := response.(http.Flusher); ok {
				flusher.Flush()
				flushed = true
			}
		}),
		observability.AccessLogOptions{ServiceName: "test-service"},
	)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil))

	if !flushed {
		t.Fatal("access-log middleware dropped http.Flusher")
	}
}

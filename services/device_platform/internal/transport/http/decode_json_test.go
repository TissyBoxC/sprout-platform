package http

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsTrailingValue(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"identifier":"13800000000"}{"admin":true}`),
	)
	var payload struct {
		Identifier string `json:"identifier"`
	}
	if err := decodeJSON(request, &payload); err == nil {
		t.Fatal("expected trailing JSON value to fail")
	}
}

func TestDecodeJSONAcceptsSingleValue(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader("{\"identifier\":\"13800000000\"}\n"),
	)
	var payload struct {
		Identifier string `json:"identifier"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		t.Fatalf("decode single JSON value: %v", err)
	}
	if payload.Identifier != "13800000000" {
		t.Fatalf("unexpected identifier: %q", payload.Identifier)
	}
}

func TestGlobalRequestBodyLimitRejectsOversizedStandardRequests(t *testing.T) {
	handler := withGlobalRequestBodyLimit(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		_, err := io.Copy(io.Discard, request.Body)
		if err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				response.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		response.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/operations",
		bytes.NewReader(make([]byte, maxStandardRequestBodyBytes+1)),
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestGlobalRequestBodyLimitExemptsReleaseUploads(t *testing.T) {
	handler := withGlobalRequestBodyLimit(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		_, err := io.CopyN(io.Discard, request.Body, maxStandardRequestBodyBytes+1)
		if err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		response.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/release-publication/files",
		bytes.NewReader(make([]byte, maxStandardRequestBodyBytes+1)),
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

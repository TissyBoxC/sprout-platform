package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminMembershipRevisionParsesQuotedIfMatch(t *testing.T) {
	request := httptest.NewRequest(http.MethodPut, "/", nil)
	request.Header.Set("If-Match", `"42"`)
	recorder := httptest.NewRecorder()

	revision, ok := adminMembershipRevision(recorder, request)

	if !ok || revision != 42 {
		t.Fatalf("revision = %d, ok = %v; want 42, true", revision, ok)
	}
}

func TestAdminMembershipRevisionRejectsZeroAndMalformedValues(t *testing.T) {
	for _, value := range []string{"0", "-1", "abc", `"1.5"`} {
		request := httptest.NewRequest(http.MethodPut, "/", nil)
		request.Header.Set("If-Match", value)
		recorder := httptest.NewRecorder()

		if _, ok := adminMembershipRevision(recorder, request); ok {
			t.Fatalf("If-Match %q must be rejected", value)
		}
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("If-Match %q status = %d, want 422", value, recorder.Code)
		}
	}
}

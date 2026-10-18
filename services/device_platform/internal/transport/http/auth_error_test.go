package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
)

func TestWriteAuthServiceErrorKeepsMFAErrorsStableAndSafe(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid code",
			err:        authdomain.ErrInvalidMFACode,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "invalid_mfa_code",
		},
		{
			name:       "challenge locked",
			err:        authdomain.ErrMFAChallengeLocked,
			wantStatus: http.StatusLocked,
			wantCode:   "mfa_challenge_locked",
		},
		{
			name:       "challenge expired",
			err:        authdomain.ErrMFAChallengeExpired,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "mfa_challenge_expired",
		},
		{
			name:       "challenge replayed",
			err:        authdomain.ErrMFAChallengeConsumed,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "mfa_challenge_expired",
		},
		{
			name:       "audit unavailable",
			err:        authdomain.ErrMFAAuditUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "security_audit_unavailable",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/admin/auth/mfa",
				nil,
			)
			recorder := httptest.NewRecorder()
			writeAuthServiceError(recorder, request, test.err)

			if recorder.Code != test.wantStatus {
				t.Fatalf(
					"status = %d, want %d",
					recorder.Code,
					test.wantStatus,
				)
			}
			var response struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if response.Error.Code != test.wantCode {
				t.Fatalf(
					"error code = %q, want %q",
					response.Error.Code,
					test.wantCode,
				)
			}
			if response.Error.Message == test.err.Error() {
				t.Fatal("response leaked the internal domain error")
			}
		})
	}
}

package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/repository"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/security"
)

func TestDeviceVoiceTokenEndpoint(t *testing.T) {
	t.Parallel()

	const (
		deviceID     = "sprout_device_001"
		deviceToken  = "device-session-token"
		websocketURL = "wss://voice.example.test/v1/voice"
	)
	expiresAt := time.Date(2026, 10, 5, 5, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		authorization   string
		sessionDeviceID string
		sessionExpired  bool
		bound           bool
		issuer          voiceTokenIssuerFunc
		wantStatus      int
		wantCode        string
	}{
		{
			name:            "valid bound device receives signed token",
			authorization:   "Bearer " + deviceToken,
			sessionDeviceID: deviceID,
			bound:           true,
			issuer: func(gotDeviceID string) (bindingservice.VoiceTokenIssue, error) {
				if gotDeviceID != deviceID {
					t.Fatalf("issuer device id = %q, want %q", gotDeviceID, deviceID)
				}
				return bindingservice.VoiceTokenIssue{
					Token:     "signed.voice.token",
					ExpiresAt: expiresAt,
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing session rejected",
			wantStatus: http.StatusUnauthorized,
			wantCode:   "device_session_expired",
		},
		{
			name:            "expired session rejected",
			authorization:   "Bearer " + deviceToken,
			sessionDeviceID: deviceID,
			sessionExpired:  true,
			bound:           true,
			issuer:          noVoiceTokenIssuer,
			wantStatus:      http.StatusUnauthorized,
			wantCode:        "device_session_expired",
		},
		{
			name:            "device id mismatch rejected",
			authorization:   "Bearer " + deviceToken,
			sessionDeviceID: "sprout_device_002",
			bound:           true,
			issuer:          noVoiceTokenIssuer,
			wantStatus:      http.StatusUnauthorized,
			wantCode:        "device_auth_failed",
		},
		{
			name:            "unbound device rejected",
			authorization:   "Bearer " + deviceToken,
			sessionDeviceID: deviceID,
			issuer:          noVoiceTokenIssuer,
			wantStatus:      http.StatusNotFound,
			wantCode:        "device_not_found",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repository := newVoiceTokenRepository()
			if test.sessionDeviceID != "" {
				repository.sessions[hashSessionToken(deviceToken)] = &domain.DeviceSession{
					ID:        "session-001",
					DeviceID:  test.sessionDeviceID,
					ExpiresAt: time.Now().UTC().Add(time.Hour),
				}
			}
			if test.sessionExpired {
				repository.sessions[hashSessionToken(deviceToken)].ExpiresAt =
					time.Now().UTC().Add(-time.Minute)
			}
			if test.bound {
				repository.bindings[deviceID] = &domain.Binding{
					ID:              "binding-001",
					ParentAccountID: "parent-001",
					DeviceID:        deviceID,
				}
			}
			service, err := bindingservice.New(bindingservice.Options{
				Repository:    repository,
				ProofVerifier: security.ECDSAProofVerifier{},
				TokenTTL:      time.Minute,
			})
			if err != nil {
				t.Fatalf("create binding service: %v", err)
			}

			options := newTestRouterOptions()
			options.AuthService = newVoiceTokenAuthService()
			options.BindingService = service
			options.VoiceTokenIssuer = test.issuer
			options.VoiceWebSocketURL = websocketURL

			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/devices/"+deviceID+"/voice-token",
				strings.NewReader("{}"),
			)
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			recorder := httptest.NewRecorder()
			NewRouter(options).ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf(
					"status = %d, want %d: %s",
					recorder.Code,
					test.wantStatus,
					recorder.Body.String(),
				)
			}
			if test.wantCode != "" {
				envelope := decodeEnvelope(t, recorder)
				if envelope.Error == nil || envelope.Error.Code != test.wantCode {
					t.Fatalf("unexpected error envelope: %+v", envelope)
				}
				return
			}

			var response struct {
				Data struct {
					WebSocketURL string `json:"websocket_url"`
					Token        string `json:"token"`
					ExpiresAt    string `json:"expires_at"`
				} `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Data.WebSocketURL != websocketURL {
				t.Fatalf(
					"websocket_url = %q, want %q",
					response.Data.WebSocketURL,
					websocketURL,
				)
			}
			if response.Data.Token != "signed.voice.token" {
				t.Fatalf("token = %q", response.Data.Token)
			}
			if response.Data.ExpiresAt != expiresAt.Format(time.RFC3339) {
				t.Fatalf(
					"expires_at = %q, want %q",
					response.Data.ExpiresAt,
					expiresAt.Format(time.RFC3339),
				)
			}
		})
	}
}

func TestDeviceVoiceTokenReturnsServiceUnavailableWithoutSigner(t *testing.T) {
	t.Parallel()

	const deviceID = "sprout_device_001"
	repository := newVoiceTokenRepository()
	repository.sessions[hashSessionToken("device-session-token")] = &domain.DeviceSession{
		ID:        "session-001",
		DeviceID:  deviceID,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	repository.bindings[deviceID] = &domain.Binding{
		ID:              "binding-001",
		ParentAccountID: "parent-001",
		DeviceID:        deviceID,
	}
	service, err := bindingservice.New(bindingservice.Options{
		Repository:    repository,
		ProofVerifier: security.ECDSAProofVerifier{},
		TokenTTL:      time.Minute,
	})
	if err != nil {
		t.Fatalf("create binding service: %v", err)
	}

	options := newTestRouterOptions()
	options.AuthService = newVoiceTokenAuthService()
	options.BindingService = service
	options.VoiceWebSocketURL = "wss://voice.example.test/v1/voice"

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/devices/"+deviceID+"/voice-token",
		strings.NewReader("{}"),
	)
	request.Header.Set("Authorization", "Bearer device-session-token")
	recorder := httptest.NewRecorder()
	NewRouter(options).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"status = %d, want %d: %s",
			recorder.Code,
			http.StatusServiceUnavailable,
			recorder.Body.String(),
		)
	}
	envelope := decodeEnvelope(t, recorder)
	if envelope.Error == nil || envelope.Error.Code != "voice_unavailable" {
		t.Fatalf("unexpected error envelope: %+v", envelope)
	}
}

func noVoiceTokenIssuer(string) (bindingservice.VoiceTokenIssue, error) {
	return bindingservice.VoiceTokenIssue{}, nil
}

type voiceTokenIssuerFunc func(
	deviceID string,
) (bindingservice.VoiceTokenIssue, error)

func (issue voiceTokenIssuerFunc) Issue(
	deviceID string,
) (bindingservice.VoiceTokenIssue, error) {
	return issue(deviceID)
}

func newVoiceTokenAuthService() *authservice.Service {
	return &authservice.Service{}
}

func hashSessionToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

type voiceTokenRepository struct {
	repository.Repository
	sessions map[string]*domain.DeviceSession
	bindings map[string]*domain.Binding
}

func newVoiceTokenRepository() *voiceTokenRepository {
	return &voiceTokenRepository{
		sessions: make(map[string]*domain.DeviceSession),
		bindings: make(map[string]*domain.Binding),
	}
}

func (repository *voiceTokenRepository) GetDeviceSessionByTokenHash(
	_ context.Context,
	tokenHash string,
) (*domain.DeviceSession, error) {
	session := repository.sessions[tokenHash]
	if session == nil {
		return nil, domain.ErrDeviceSessionNotFound
	}
	return session, nil
}

func (repository *voiceTokenRepository) GetByDeviceID(
	_ context.Context,
	deviceID string,
) (*domain.Binding, error) {
	binding := repository.bindings[deviceID]
	if binding == nil {
		return nil, domain.ErrDeviceNotFound
	}
	return binding, nil
}

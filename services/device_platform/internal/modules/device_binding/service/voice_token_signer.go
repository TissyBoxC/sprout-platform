package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const voiceTokenSecretMinLength = 32

// Errors returned while configuring or using a voice token signer.
var (
	ErrVoiceTokenSecret     = errors.New("voice token secret must contain at least 32 bytes")
	ErrVoiceTokenTTL        = errors.New("voice token ttl must be positive")
	ErrVoiceTokenDeviceID   = errors.New("voice token device id is empty")
	ErrVoiceTokenSignerDown = errors.New("voice token signer is unavailable")
)

// VoiceTokenIssue is a signed realtime voice credential and its absolute
// expiry. It is returned only to the authenticated owning device.
type VoiceTokenIssue struct {
	Token     string
	ExpiresAt time.Time
}

// VoiceTokenIssuer creates short-lived voice WebSocket credentials for one
// authenticated, guardian-bound device.
type VoiceTokenIssuer interface {
	Issue(deviceID string) (VoiceTokenIssue, error)
}

// VoiceTokenSigner issues the short-lived, signed device tokens that the voice
// gateway verifies before opening a realtime audio WebSocket.
//
// The envelope is base64url(JSON payload).base64url(HMAC-SHA256(payload)) using
// the shared service secret. The secret is held only in memory and is never
// written to logs or returned in errors.
type VoiceTokenSigner struct {
	secret []byte
	ttl    time.Duration
}

// voiceTokenPayload is the signed body of one voice WebSocket device token.
// The JSON field names and types must remain in lockstep with
// voice_gateway/internal/transport/websocket/deviceTokenPayload.
type voiceTokenPayload struct {
	DeviceID  string `json:"device_id"`
	TokenID   string `json:"token_id"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// NewVoiceTokenSigner creates a signer for short-lived voice WebSocket device
// tokens.
//
// The secret is trimmed before use and must contain at least 32 bytes. A
// non-positive ttl is rejected so tokens cannot be issued without a bounded
// validity window.
func NewVoiceTokenSigner(secret string, ttl time.Duration) (*VoiceTokenSigner, error) {
	trimmedSecret := strings.TrimSpace(secret)
	if len(trimmedSecret) < voiceTokenSecretMinLength {
		return nil, ErrVoiceTokenSecret
	}
	if ttl <= 0 {
		return nil, ErrVoiceTokenTTL
	}
	return &VoiceTokenSigner{
		secret: []byte(trimmedSecret),
		ttl:    ttl,
	}, nil
}

// SignDeviceToken signs a new token for deviceID using a generated UUID token
// identifier and the configured TTL.
func (s *VoiceTokenSigner) SignDeviceToken(deviceID string) (string, error) {
	issue, err := s.Issue(deviceID)
	if err != nil {
		return "", err
	}
	return issue.Token, nil
}

// Issue implements VoiceTokenIssuer by returning the signed token together
// with the exact expiry written into its MAC-protected payload.
func (s *VoiceTokenSigner) Issue(deviceID string) (VoiceTokenIssue, error) {
	if s == nil {
		return VoiceTokenIssue{}, ErrVoiceTokenSignerDown
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return VoiceTokenIssue{}, ErrVoiceTokenDeviceID
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.ttl)
	payload := voiceTokenPayload{
		DeviceID:  deviceID,
		TokenID:   uuid.NewString(),
		IssuedAt:  now.Unix(),
		ExpiresAt: expiresAt.Unix(),
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return VoiceTokenIssue{}, fmt.Errorf("sign voice token: %w", err)
	}

	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write(encodedPayload)
	return VoiceTokenIssue{
		Token: base64.RawURLEncoding.EncodeToString(encodedPayload) + "." +
			base64.RawURLEncoding.EncodeToString(mac.Sum(nil)),
		ExpiresAt: expiresAt,
	}, nil
}

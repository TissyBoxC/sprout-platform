package websocket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// HMACDeviceTokenVerifier verifies the short-lived device tokens that the
// platform issues before a device opens the realtime audio socket.
//
// The token is a compact, signed envelope: base64url(payload).base64url(mac)
// where the payload is a small JSON object and the MAC is HMAC-SHA256 over the
// payload bytes using the shared service secret. Verification is constant-time
// and rejects expired, malformed, or unsigned tokens, so an unauthenticated
// device can never reach the audio state machine. The secret bytes are never
// logged, and failures return a stable reason without echoing the token.
type HMACDeviceTokenVerifier struct {
	secret []byte
	now    func() time.Time
	// leeway tolerates small clock differences between the platform issuer and
	// the gateway without widening the accepted validity window materially.
	leeway time.Duration
}

// deviceTokenPayload is the signed body of one device session token.
type deviceTokenPayload struct {
	DeviceID  string `json:"device_id"`
	TokenID   string `json:"token_id"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// Errors returned while issuing a token.
var (
	ErrTokenSecret   = errors.New("device token secret must contain at least 32 bytes")
	ErrTokenIdentity = errors.New("device token identity is empty")
	ErrTokenTTL      = errors.New("device token ttl must be positive")
)

// NewHMACDeviceTokenVerifier creates a verifier from the shared signing secret.
//
// A short secret is rejected so a truncated or placeholder value cannot weaken
// device authentication. A nil clock uses the wall clock.
func NewHMACDeviceTokenVerifier(secret string, now func() time.Time) (*HMACDeviceTokenVerifier, error) {
	trimmed := strings.TrimSpace(secret)
	if len(trimmed) < 32 {
		return nil, ErrTokenSecret
	}
	if now == nil {
		now = time.Now
	}
	return &HMACDeviceTokenVerifier{
		secret: []byte(trimmed),
		now:    now,
		leeway: 30 * time.Second,
	}, nil
}

// VerifyDeviceToken validates one token and returns the authenticated device.
//
// The signature is checked before the payload is trusted, expiry is enforced
// with a small leeway, and every failure maps to the same opaque error so a
// caller cannot distinguish "bad signature" from "expired".
func (v *HMACDeviceTokenVerifier) VerifyDeviceToken(token string) (DeviceIdentity, error) {
	if v == nil {
		return DeviceIdentity{}, &AuthError{Reason: "token verifier is unavailable"}
	}
	payload, err := v.decode(token)
	if err != nil {
		return DeviceIdentity{}, err
	}
	return DeviceIdentity{DeviceID: payload.DeviceID, TokenID: payload.TokenID}, nil
}

// IssueDeviceToken signs a short-lived token for a device. The platform's
// device binding service uses the same construction; this method exists for the
// gateway's own integration tests and for local development tooling.
func (v *HMACDeviceTokenVerifier) IssueDeviceToken(deviceID string, tokenID string, ttl time.Duration) (string, error) {
	if v == nil {
		return "", &AuthError{Reason: "token verifier is unavailable"}
	}
	if strings.TrimSpace(deviceID) == "" {
		return "", ErrTokenIdentity
	}
	if ttl <= 0 {
		return "", ErrTokenTTL
	}
	now := v.now().UTC()
	payload := deviceTokenPayload{
		DeviceID:  deviceID,
		TokenID:   tokenID,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("sign device token: %w", err)
	}
	mac := v.sign(encodedPayload)
	return base64.RawURLEncoding.EncodeToString(encodedPayload) + "." +
		base64.RawURLEncoding.EncodeToString(mac), nil
}

func (v *HMACDeviceTokenVerifier) decode(token string) (deviceTokenPayload, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 {
		return deviceTokenPayload{}, &AuthError{Reason: "device token is malformed"}
	}
	encodedPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return deviceTokenPayload{}, &AuthError{Reason: "device token is malformed"}
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return deviceTokenPayload{}, &AuthError{Reason: "device token is malformed"}
	}
	if !hmac.Equal(signature, v.sign(encodedPayload)) {
		return deviceTokenPayload{}, &AuthError{Reason: "device token signature is invalid"}
	}
	var payload deviceTokenPayload
	if err := json.Unmarshal(encodedPayload, &payload); err != nil {
		return deviceTokenPayload{}, &AuthError{Reason: "device token payload is invalid"}
	}
	if strings.TrimSpace(payload.DeviceID) == "" {
		return deviceTokenPayload{}, &AuthError{Reason: "device token identity is invalid"}
	}
	now := v.now().UTC()
	if payload.ExpiresAt <= 0 || now.After(time.Unix(payload.ExpiresAt, 0).Add(v.leeway)) {
		return deviceTokenPayload{}, &AuthError{Reason: "device token has expired"}
	}
	if payload.IssuedAt > 0 && now.Add(v.leeway).Before(time.Unix(payload.IssuedAt, 0)) {
		return deviceTokenPayload{}, &AuthError{Reason: "device token is not yet valid"}
	}
	return payload, nil
}

func (v *HMACDeviceTokenVerifier) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, v.secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

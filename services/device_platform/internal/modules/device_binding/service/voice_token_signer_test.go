package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestVoiceTokenSignerSignsGatewayCompatibleEnvelope(t *testing.T) {
	t.Parallel()

	const (
		deviceID = "device-01J5S4V4Y8K7P6Q5R4T3S2B1A0"
		secret   = "voice-token-signer-secret-0123456789"
		ttl      = 5 * time.Minute
	)

	signer, err := NewVoiceTokenSigner("  "+secret+"\t", ttl)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}

	before := time.Now().Unix()
	token, err := signer.SignDeviceToken(deviceID)
	if err != nil {
		t.Fatalf("sign device token: %v", err)
	}
	after := time.Now().Unix()

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("token envelope has %d parts, want 2", len(parts))
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write(payloadBytes); err != nil {
		t.Fatalf("compute expected signature: %v", err)
	}
	if !hmac.Equal(signature, mac.Sum(nil)) {
		t.Fatal("token signature does not match independent HMAC-SHA256 verification")
	}

	var payload voiceTokenPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.DeviceID != deviceID {
		t.Fatalf("device_id = %q, want %q", payload.DeviceID, deviceID)
	}
	parsedTokenID, err := uuid.Parse(payload.TokenID)
	if err != nil {
		t.Fatalf("token_id is not a UUID: %v", err)
	}
	if parsedTokenID.String() != payload.TokenID {
		t.Fatalf("token_id = %q, want canonical UUID %q", payload.TokenID, parsedTokenID.String())
	}
	if payload.IssuedAt < before || payload.IssuedAt > after {
		t.Fatalf("issued_at = %d, want between %d and %d", payload.IssuedAt, before, after)
	}
	if got := payload.ExpiresAt - payload.IssuedAt; got != int64(ttl/time.Second) {
		t.Fatalf("expiry window = %ds, want %ds", got, int64(ttl/time.Second))
	}
	if payload.ExpiresAt <= payload.IssuedAt {
		t.Fatalf("expires_at = %d must be after issued_at = %d", payload.ExpiresAt, payload.IssuedAt)
	}
}

func TestNewVoiceTokenSignerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		secret  string
		ttl     time.Duration
		wantErr error
	}{
		{
			name:    "short secret",
			secret:  strings.Repeat("s", voiceTokenSecretMinLength-1),
			ttl:     time.Minute,
			wantErr: ErrVoiceTokenSecret,
		},
		{
			name:    "whitespace around short secret",
			secret:  "  " + strings.Repeat("s", voiceTokenSecretMinLength-1) + "\t",
			ttl:     time.Minute,
			wantErr: ErrVoiceTokenSecret,
		},
		{
			name:    "zero ttl",
			secret:  strings.Repeat("s", voiceTokenSecretMinLength),
			ttl:     0,
			wantErr: ErrVoiceTokenTTL,
		},
		{
			name:    "negative ttl",
			secret:  strings.Repeat("s", voiceTokenSecretMinLength),
			ttl:     -time.Second,
			wantErr: ErrVoiceTokenTTL,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			signer, err := NewVoiceTokenSigner(test.secret, test.ttl)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewVoiceTokenSigner() error = %v, want %v", err, test.wantErr)
			}
			if signer != nil {
				t.Fatal("NewVoiceTokenSigner() returned a signer for invalid configuration")
			}
		})
	}
}

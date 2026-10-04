package websocket

import (
	"strings"
	"testing"
	"time"
)

const testTokenSecret = "unit-test-device-token-secret-0123456789"

func TestVerifyDeviceTokenAcceptsIssuedToken(t *testing.T) {
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_a", "token_1", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}

	identity, err := verifier.VerifyDeviceToken(token)
	if err != nil {
		t.Fatalf("VerifyDeviceToken() error = %v", err)
	}
	if identity.DeviceID != "device_a" || identity.TokenID != "token_1" {
		t.Fatalf("VerifyDeviceToken() identity = %+v, want device_a/token_1", identity)
	}
}

func TestVerifyDeviceTokenRejectsExpiredToken(t *testing.T) {
	now := time.Now().UTC()
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_a", "token_1", time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}

	// Advance well past the ttl and the 30s leeway.
	now = now.Add(10 * time.Minute)
	if _, err := verifier.VerifyDeviceToken(token); err == nil {
		t.Fatal("VerifyDeviceToken() accepted an expired token")
	}
}

func TestVerifyDeviceTokenRejectsWrongSecret(t *testing.T) {
	issuer, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	token, err := issuer.IssueDeviceToken("device_a", "token_1", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	verifier, err := NewHMACDeviceTokenVerifier("a-totally-different-device-token-secret-xx", nil)
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	if _, err := verifier.VerifyDeviceToken(token); err == nil {
		t.Fatal("VerifyDeviceToken() accepted a token signed with another secret")
	}
}

func TestVerifyDeviceTokenRejectsTamperedPayload(t *testing.T) {
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_a", "token_1", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("issued token has %d parts, want 2", len(parts))
	}
	// Flip one payload character without re-signing.
	tampered := parts[0][:len(parts[0])-1] + flipChar(parts[0][len(parts[0])-1]) + "." + parts[1]
	if _, err := verifier.VerifyDeviceToken(tampered); err == nil {
		t.Fatal("VerifyDeviceToken() accepted a tampered payload")
	}
}

func TestVerifyDeviceTokenRejectsMalformed(t *testing.T) {
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	for _, token := range []string{"", "not-a-token", "a.b.c", "!!!.???"} {
		if _, err := verifier.VerifyDeviceToken(token); err == nil {
			t.Fatalf("VerifyDeviceToken(%q) accepted a malformed token", token)
		}
	}
}

func TestNewHMACDeviceTokenVerifierRejectsShortSecret(t *testing.T) {
	if _, err := NewHMACDeviceTokenVerifier("too-short", nil); err == nil {
		t.Fatal("NewHMACDeviceTokenVerifier() accepted a short secret")
	}
}

func flipChar(value byte) string {
	if value == 'A' {
		return "B"
	}
	return "A"
}

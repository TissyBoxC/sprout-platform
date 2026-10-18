package websocket

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
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

func TestVerifyDeviceTokenRejectsSecondUse(t *testing.T) {
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_a", "single_use_token", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	if _, err := verifier.VerifyDeviceToken(token); err != nil {
		t.Fatalf("first VerifyDeviceToken() error = %v", err)
	}
	if _, err := verifier.VerifyDeviceToken(token); err == nil {
		t.Fatal("second VerifyDeviceToken() accepted a consumed token")
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

func TestRevokedDeviceTokenIsRejectedBeforeExpiry(t *testing.T) {
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_a", "token_revoke", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	verifier.RevokeToken("token_revoke")
	if _, err := verifier.VerifyDeviceToken(token); err == nil {
		t.Fatal("VerifyDeviceToken() accepted a revoked token")
	}
}

func TestRevokedDeviceTokenExpiresFromRevocationSet(t *testing.T) {
	now := time.Now().UTC()
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_a", "token_expire", time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	verifier.RevokeToken("token_expire")
	now = now.Add(2 * time.Minute)
	// The signed token has also expired, so rejection must still be the
	// observable result even after the revocation entry is pruned.
	if _, err := verifier.VerifyDeviceToken(token); err == nil {
		t.Fatal("VerifyDeviceToken() accepted an expired and revoked token")
	}
}

// The revocation set must shrink once the revoked token's own signed expiry has
// passed, otherwise a long-running gateway leaks one entry per closed session.
func TestRevokedDeviceTokenIsPrunedAfterExpiry(t *testing.T) {
	now := time.Now().UTC()
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_a", "token_prune", time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	identity, err := verifier.VerifyDeviceToken(token)
	if err != nil {
		t.Fatalf("VerifyDeviceToken() error = %v", err)
	}
	verifier.RevokeIdentity(identity)
	if got := verifier.revokedCount(); got != 1 {
		t.Fatalf("revokedCount() = %d, want 1 after revocation", got)
	}

	// Still within the signed validity window (plus leeway): the entry stays.
	now = now.Add(30 * time.Second)
	if got := verifier.revokedCount(); got != 1 {
		t.Fatalf("revokedCount() = %d, want 1 before expiry", got)
	}

	// Past expiry plus leeway: the set must be pruned back to empty.
	now = now.Add(2 * time.Minute)
	if got := verifier.revokedCount(); got != 0 {
		t.Fatalf("revokedCount() = %d, want 0 after expiry", got)
	}
}

// Revoking the same identifier twice must not grow the set, and a later
// RevokeTokenUntil with a real expiry must supersede an unknown-expiry entry.
func TestRevokeTokenUntilSupersedesUnknownExpiry(t *testing.T) {
	now := time.Now().UTC()
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	verifier.RevokeToken("token_upgrade")
	verifier.RevokeToken("token_upgrade")
	if got := verifier.revokedCount(); got != 1 {
		t.Fatalf("revokedCount() = %d, want 1 after duplicate revocation", got)
	}

	// An unknown-expiry entry is retained forever, so upgrading it with a real
	// expiry lets pruning reclaim it later.
	verifier.RevokeTokenUntil("token_upgrade", now.Add(time.Minute))
	now = now.Add(2 * time.Minute)
	if got := verifier.revokedCount(); got != 0 {
		t.Fatalf("revokedCount() = %d, want 0 after upgraded expiry passed", got)
	}
}

func TestVerifyDeviceTokenRejectsMissingTokenID(t *testing.T) {
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	payload, err := json.Marshal(deviceTokenPayload{
		DeviceID:  "device_a",
		IssuedAt:  time.Now().UTC().Unix(),
		ExpiresAt: time.Now().UTC().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(verifier.sign(payload))

	if _, err := verifier.VerifyDeviceToken(token); err == nil {
		t.Fatal("VerifyDeviceToken() accepted a token without a token id")
	}
}

func TestRevokeIdentityIsSafeUnderConcurrentUse(t *testing.T) {
	verifier, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("NewHMACDeviceTokenVerifier() error = %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_a", "token_concurrent", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}

	var waitGroup sync.WaitGroup
	for index := 0; index < 32; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			verifier.RevokeIdentity(DeviceIdentity{DeviceID: "device_a", TokenID: "token_concurrent"})
			_, _ = verifier.VerifyDeviceToken(token)
		}()
	}
	waitGroup.Wait()
	if _, err := verifier.VerifyDeviceToken(token); err == nil {
		t.Fatal("VerifyDeviceToken() accepted a token after concurrent revocation")
	}
}

func flipChar(value byte) string {
	if value == 'A' {
		return "B"
	}
	return "A"
}

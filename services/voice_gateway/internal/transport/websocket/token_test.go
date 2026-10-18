package websocket

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
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

func TestVerifyDeviceTokenSharedStoreRejectsCrossReplicaReplay(t *testing.T) {
	store := &memoryTokenClaimStore{}
	first, err := NewHMACDeviceTokenVerifierWithStore(
		testTokenSecret,
		nil,
		store,
	)
	if err != nil {
		t.Fatalf("create first verifier: %v", err)
	}
	second, err := NewHMACDeviceTokenVerifierWithStore(
		testTokenSecret,
		nil,
		store,
	)
	if err != nil {
		t.Fatalf("create second verifier: %v", err)
	}
	token, err := first.IssueDeviceToken(
		"device_a",
		"shared_single_use",
		5*time.Minute,
	)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	if _, err := first.VerifyDeviceTokenContext(context.Background(), token); err != nil {
		t.Fatalf("first replica rejected the token: %v", err)
	}
	if _, err := second.VerifyDeviceTokenContext(context.Background(), token); err == nil {
		t.Fatal("second replica accepted a token consumed by the first")
	}
}

func TestVerifyDeviceTokenSharedStoreRejectsRevokedTokenAcrossVerifiers(t *testing.T) {
	store := &memoryTokenClaimStore{}
	first, err := NewHMACDeviceTokenVerifierWithStore(testTokenSecret, nil, store)
	if err != nil {
		t.Fatalf("create first verifier: %v", err)
	}
	second, err := NewHMACDeviceTokenVerifierWithStore(testTokenSecret, nil, store)
	if err != nil {
		t.Fatalf("create second verifier: %v", err)
	}
	token, err := first.IssueDeviceToken(
		"device_a",
		"shared_revoked",
		5*time.Minute,
	)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	if err := first.RevokeTokenContext(
		context.Background(),
		"shared_revoked",
		time.Now().UTC().Add(5*time.Minute),
	); err != nil {
		t.Fatalf("RevokeTokenContext() error = %v", err)
	}
	if _, err := second.VerifyDeviceTokenContext(context.Background(), token); err == nil {
		t.Fatal("second verifier accepted a token revoked by the first")
	}
}

func TestVerifyDeviceTokenSharedStoreIsAtomicUnderConcurrency(t *testing.T) {
	store := &memoryTokenClaimStore{}
	verifiers := make([]*HMACDeviceTokenVerifier, 8)
	for index := range verifiers {
		verifier, createErr := NewHMACDeviceTokenVerifierWithStore(
			testTokenSecret,
			nil,
			store,
		)
		if createErr != nil {
			t.Fatalf("create verifier %d: %v", index, createErr)
		}
		verifiers[index] = verifier
	}
	token, err := verifiers[0].IssueDeviceToken(
		"device_a",
		"shared_concurrent",
		5*time.Minute,
	)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	var accepted atomic.Int32
	var waitGroup sync.WaitGroup
	for index := 0; index < 64; index++ {
		waitGroup.Add(1)
		verifier := verifiers[index%len(verifiers)]
		go func() {
			defer waitGroup.Done()
			if _, verifyErr := verifier.VerifyDeviceTokenContext(
				context.Background(),
				token,
			); verifyErr == nil {
				accepted.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted token %d times, want exactly 1", got)
	}
}

func TestVerifyDeviceTokenSharedStoreUsesRequestContext(t *testing.T) {
	store := &memoryTokenClaimStore{}
	verifier, err := NewHMACDeviceTokenVerifierWithStore(testTokenSecret, nil, store)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_a", "shared_context", time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verifier.VerifyDeviceTokenContext(ctx, token); err == nil {
		t.Fatal("shared-store authentication accepted a cancelled request context")
	}
	if got := store.claimCalls.Load(); got != 1 {
		t.Fatalf("Claim() calls = %d, want 1", got)
	}
}

func TestVerifyDeviceTokenLocalFallbackIsProcessLocal(t *testing.T) {
	first, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("create first verifier: %v", err)
	}
	second, err := NewHMACDeviceTokenVerifier(testTokenSecret, nil)
	if err != nil {
		t.Fatalf("create second verifier: %v", err)
	}
	token, err := first.IssueDeviceToken("device_a", "local_only", time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	if _, err := first.VerifyDeviceToken(token); err != nil {
		t.Fatalf("first local verifier rejected token: %v", err)
	}
	if _, err := second.VerifyDeviceToken(token); err != nil {
		t.Fatalf("second independent local verifier rejected token: %v", err)
	}
	if _, err := first.VerifyDeviceToken(token); err == nil {
		t.Fatal("first local verifier accepted its already consumed token")
	}
}

func TestVerifyDeviceTokenFailsClosedWhenSharedStoreFails(t *testing.T) {
	verifier, err := NewHMACDeviceTokenVerifierWithStore(
		testTokenSecret,
		nil,
		&memoryTokenClaimStore{failure: errors.New("redis unavailable")},
	)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	token, err := verifier.IssueDeviceToken("device_a", "shared_failure", time.Minute)
	if err != nil {
		t.Fatalf("IssueDeviceToken() error = %v", err)
	}
	if _, err := verifier.VerifyDeviceTokenContext(
		context.Background(),
		token,
	); err == nil {
		t.Fatal("shared-store failure must reject device authentication")
	}
}

func TestRevokeTokenContextFailsWhenSharedStoreFails(t *testing.T) {
	store := &memoryTokenClaimStore{failure: errors.New("redis unavailable")}
	verifier, err := NewHMACDeviceTokenVerifierWithStore(testTokenSecret, nil, store)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	if err := verifier.RevokeTokenContext(
		context.Background(),
		"shared_revoke_failure",
		time.Now().UTC().Add(time.Minute),
	); err == nil {
		t.Fatal("RevokeTokenContext() reported success when the shared store failed")
	}
}

func TestRevokeTokenContextRequiresExpiryWithSharedStore(t *testing.T) {
	store := &memoryTokenClaimStore{}
	verifier, err := NewHMACDeviceTokenVerifierWithStore(testTokenSecret, nil, store)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	if err := verifier.RevokeTokenContext(
		context.Background(),
		"shared_revoke_without_expiry",
		time.Time{},
	); err == nil {
		t.Fatal("RevokeTokenContext() accepted an unbounded shared-store revocation")
	}
}

func TestRedisTokenClaimStoreClaimAndRevokeTTL(t *testing.T) {
	now := time.Now().UTC()
	client := &fakeRedisTokenCommandClient{
		setNXResult: true,
		evalResult:  int64(1),
	}
	store := &RedisTokenClaimStore{
		client: client,
		prefix: "test:voice:token:",
	}
	expiresAt := now.Add(90 * time.Second)

	accepted, err := store.Claim(context.Background(), "token_1", expiresAt)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !accepted {
		t.Fatal("Claim() = false, want true")
	}
	if client.setNXKey != "test:voice:token:token_1" {
		t.Fatalf("Claim() key = %q", client.setNXKey)
	}
	if client.setNXTTL <= 0 || client.setNXTTL > 90*time.Second {
		t.Fatalf("Claim() ttl = %v, want a positive value <= 90s", client.setNXTTL)
	}

	if err := store.Revoke(context.Background(), "token_1", expiresAt); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if client.evalKey != "test:voice:token:token_1" {
		t.Fatalf("Revoke() key = %q", client.evalKey)
	}
	if client.evalTTL <= 0 || client.evalTTL > 90000 {
		t.Fatalf("Revoke() ttl = %d ms, want a positive value <= 90000", client.evalTTL)
	}
	if !strings.Contains(client.evalScript, `"PTTL"`) {
		t.Fatalf("Revoke() script does not preserve the existing claim TTL: %s", client.evalScript)
	}
}

func TestRedisTokenClaimStoreRevokeNeverInitializesShortTTL(t *testing.T) {
	client := &fakeRedisTokenCommandClient{evalResult: int64(1)}
	store := &RedisTokenClaimStore{client: client, prefix: "test:voice:token:"}
	if err := store.Revoke(
		context.Background(),
		"token_short",
		time.Now().UTC().Add(500*time.Microsecond),
	); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if client.evalTTL != 1 {
		t.Fatalf("Revoke() ttl = %d ms, want 1", client.evalTTL)
	}
}

func TestRedisTokenClaimStoreFailsClosedOnUnavailableOrInvalidInput(t *testing.T) {
	var nilStore *RedisTokenClaimStore
	if accepted, err := nilStore.Claim(context.Background(), "token", time.Now().Add(time.Minute)); err == nil || accepted {
		t.Fatal("nil Claim() did not fail closed")
	}
	if err := nilStore.Revoke(context.Background(), "token", time.Now().Add(time.Minute)); err == nil {
		t.Fatal("nil Revoke() did not report the unavailable store")
	}

	client := &fakeRedisTokenCommandClient{setNXResult: true}
	store := &RedisTokenClaimStore{client: client, prefix: "test:"}
	if accepted, err := store.Claim(context.Background(), "token", time.Now().Add(-time.Second)); err != nil || accepted {
		t.Fatal("expired Claim() did not fail closed")
	}
	if accepted, err := store.Claim(context.Background(), " ", time.Now().Add(time.Minute)); err == nil || accepted {
		t.Fatal("blank token Claim() did not fail closed")
	}
	if err := store.Revoke(context.Background(), " ", time.Now().Add(time.Minute)); err == nil {
		t.Fatal("blank token Revoke() did not fail")
	}
}

func TestRedisTokenClaimStoreReportsCommandFailures(t *testing.T) {
	claimClient := &fakeRedisTokenCommandClient{
		setNXErr: errors.New("redis command failed"),
	}
	claimStore := &RedisTokenClaimStore{client: claimClient, prefix: "test:"}
	if accepted, err := claimStore.Claim(
		context.Background(),
		"token_claim_error",
		time.Now().UTC().Add(time.Minute),
	); err == nil || accepted {
		t.Fatal("Claim() did not fail closed when Redis returned an error")
	}

	revokeClient := &fakeRedisTokenCommandClient{
		evalErr: errors.New("redis command failed"),
	}
	revokeStore := &RedisTokenClaimStore{client: revokeClient, prefix: "test:"}
	if err := revokeStore.Revoke(
		context.Background(),
		"token_revoke_error",
		time.Now().UTC().Add(time.Minute),
	); err == nil {
		t.Fatal("Revoke() did not report a Redis command error")
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

type memoryTokenClaimStore struct {
	mutex      sync.Mutex
	claimed    map[string]time.Time
	failure    error
	claimCalls atomic.Int32
}

func (store *memoryTokenClaimStore) Claim(
	ctx context.Context,
	tokenID string,
	expiresAt time.Time,
) (bool, error) {
	store.claimCalls.Add(1)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if store.failure != nil {
		return false, store.failure
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.claimed == nil {
		store.claimed = make(map[string]time.Time)
	}
	if _, exists := store.claimed[tokenID]; exists {
		return false, nil
	}
	store.claimed[tokenID] = expiresAt
	return true, nil
}

func (store *memoryTokenClaimStore) Revoke(
	_ context.Context,
	tokenID string,
	expiresAt time.Time,
) error {
	if store.failure != nil {
		return store.failure
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.claimed == nil {
		store.claimed = make(map[string]time.Time)
	}
	if existing, exists := store.claimed[tokenID]; !exists || expiresAt.After(existing) {
		store.claimed[tokenID] = expiresAt
	}
	return nil
}

type fakeRedisTokenCommandClient struct {
	setNXResult bool
	setNXErr    error
	setNXKey    string
	setNXTTL    time.Duration
	evalResult  int64
	evalErr     error
	evalKey     string
	evalScript  string
	evalTTL     int64
}

func (client *fakeRedisTokenCommandClient) SetNX(
	ctx context.Context,
	key string,
	_ interface{},
	expiration time.Duration,
) *redis.BoolCmd {
	client.setNXKey = key
	client.setNXTTL = expiration
	cmd := redis.NewBoolCmd(ctx)
	if client.setNXErr != nil {
		cmd.SetErr(client.setNXErr)
		return cmd
	}
	cmd.SetVal(client.setNXResult)
	return cmd
}

func (client *fakeRedisTokenCommandClient) Eval(
	ctx context.Context,
	script string,
	keys []string,
	args ...interface{},
) *redis.Cmd {
	client.evalScript = script
	if len(keys) > 0 {
		client.evalKey = keys[0]
	}
	if len(args) > 0 {
		switch value := args[0].(type) {
		case int:
			client.evalTTL = int64(value)
		case int64:
			client.evalTTL = value
		default:
			client.evalTTL = -1
		}
	}
	cmd := redis.NewCmd(ctx)
	if client.evalErr != nil {
		cmd.SetErr(client.evalErr)
		return cmd
	}
	cmd.SetVal(client.evalResult)
	return cmd
}

var _ redisTokenCommandClient = (*fakeRedisTokenCommandClient)(nil)

func (client *fakeRedisTokenCommandClient) String() string {
	return fmt.Sprintf(
		"fake redis client setnx=%q ttl=%s eval=%q",
		client.setNXKey,
		client.setNXTTL,
		client.evalKey,
	)
}

package websocket

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// TokenClaimStore makes token consumption and revocation shared across gateway
// replicas. Claim must atomically return false when the token was already
// consumed or revoked. Implementations must fail closed: a store error is an
// authentication failure, never an implicit successful claim.
type TokenClaimStore interface {
	Claim(ctx context.Context, tokenID string, expiresAt time.Time) (bool, error)
	Revoke(ctx context.Context, tokenID string, expiresAt time.Time) error
}

// redisTokenCommandClient is the subset of the Redis client used by the claim
// store. Keeping it narrow makes the atomic command behavior testable without
// requiring a live Redis server.
type redisTokenCommandClient interface {
	SetNX(
		ctx context.Context,
		key string,
		value interface{},
		expiration time.Duration,
	) *redis.BoolCmd
	Eval(
		ctx context.Context,
		script string,
		keys []string,
		args ...interface{},
	) *redis.Cmd
}

// RedisTokenClaimStore stores consumed and revoked token identifiers in Redis.
// The key expires at the signed token expiry plus the verifier leeway, so the
// store stays bounded without a separate cleanup job.
type RedisTokenClaimStore struct {
	client redisTokenCommandClient
	prefix string
}

// NewRedisTokenClaimStore creates a shared claim store.
func NewRedisTokenClaimStore(client *redis.Client, prefix string) *RedisTokenClaimStore {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "sprout:voice:token:"
	}
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	return &RedisTokenClaimStore{client: client, prefix: prefix}
}

// Claim uses SET NX so exactly one replica can accept a token.
func (store *RedisTokenClaimStore) Claim(
	ctx context.Context,
	tokenID string,
	expiresAt time.Time,
) (bool, error) {
	if store == nil || store.client == nil {
		return false, errors.New("token claim store is unavailable")
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return false, errors.New("token id is required")
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return false, nil
	}
	accepted, err := store.client.SetNX(
		ctx,
		store.prefix+tokenID,
		"claimed",
		ttl,
	).Result()
	if err != nil {
		return false, fmt.Errorf("claim device token: %w", err)
	}
	return accepted, nil
}

// Revoke extends the shared record through the token expiry. It never shortens
// an existing claim, so a disconnect cannot reopen a consumed token.
func (store *RedisTokenClaimStore) Revoke(
	ctx context.Context,
	tokenID string,
	expiresAt time.Time,
) error {
	if store == nil || store.client == nil {
		return errors.New("token claim store is unavailable")
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return errors.New("token id is required")
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil
	}
	ttlMillis := ttl.Milliseconds()
	if ttlMillis <= 0 {
		ttlMillis = 1
	}
	key := store.prefix + tokenID
	script := `
local current = redis.call("PTTL", KEYS[1])
local requested = tonumber(ARGV[1])
if current < requested then
  redis.call("SET", KEYS[1], "revoked", "PX", requested)
end
return 1
`
	if err := store.client.Eval(
		ctx,
		script,
		[]string{key},
		ttlMillis,
	).Err(); err != nil {
		return fmt.Errorf("revoke device token: %w", err)
	}
	return nil
}

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
	// revoked is a bounded local mirror used as a fallback for unit tests and
	// single-process tooling. Production supplies claimStore so every replica
	// shares one atomic consumption and revocation record.
	revokedMutex sync.Mutex
	revoked      map[string]time.Time
	claimStore   TokenClaimStore
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
	return NewHMACDeviceTokenVerifierWithStore(secret, now, nil)
}

// NewHMACDeviceTokenVerifierWithStore creates a verifier backed by shared
// atomic token state. A nil store retains process-local behavior for tests; a
// production gateway must provide a store.
func NewHMACDeviceTokenVerifierWithStore(
	secret string,
	now func() time.Time,
	claimStore TokenClaimStore,
) (*HMACDeviceTokenVerifier, error) {
	trimmed := strings.TrimSpace(secret)
	if len(trimmed) < 32 {
		return nil, ErrTokenSecret
	}
	if now == nil {
		now = time.Now
	}
	return &HMACDeviceTokenVerifier{
		secret:     []byte(trimmed),
		now:        now,
		leeway:     30 * time.Second,
		revoked:    make(map[string]time.Time),
		claimStore: claimStore,
	}, nil
}

// VerifyDeviceToken validates one token and returns the authenticated device.
//
// The signature is checked before the payload is trusted, expiry is enforced
// with a small leeway, and every failure maps to the same opaque error so a
// caller cannot distinguish "bad signature" from "expired".
func (v *HMACDeviceTokenVerifier) VerifyDeviceToken(token string) (DeviceIdentity, error) {
	return v.VerifyDeviceTokenContext(context.Background(), token)
}

// VerifyDeviceTokenContext validates and atomically claims one token. It uses
// the caller's context so a slow Redis operation is bounded by request
// cancellation and always fails closed.
func (v *HMACDeviceTokenVerifier) VerifyDeviceTokenContext(
	ctx context.Context,
	token string,
) (DeviceIdentity, error) {
	if v == nil {
		return DeviceIdentity{}, &AuthError{Reason: "token verifier is unavailable"}
	}
	payload, err := v.decode(token)
	if err != nil {
		return DeviceIdentity{}, err
	}
	if err := v.claim(ctx, payload); err != nil {
		return DeviceIdentity{}, err
	}
	return DeviceIdentity{
		DeviceID:  payload.DeviceID,
		TokenID:   payload.TokenID,
		ExpiresAt: time.Unix(payload.ExpiresAt, 0).UTC(),
	}, nil
}

// RevokeToken invalidates one issued token without a known expiry. With a
// shared claim store the caller should use RevokeTokenContext and supply the
// signed expiry; otherwise the local verifier retains the identifier for its
// process lifetime and cannot make a durable cross-replica revocation.
// An empty identifier is ignored; verification already rejects empty
// identifiers.
func (v *HMACDeviceTokenVerifier) RevokeToken(tokenID string) {
	v.RevokeTokenUntil(tokenID, time.Time{})
}

// RevokeTokenUntil invalidates one issued token until the given expiry. With a
// shared claim store, the expiry is required to extend the shared record;
// without one, the local fallback prunes the entry after expiry (plus leeway).
func (v *HMACDeviceTokenVerifier) RevokeTokenUntil(tokenID string, expiresAt time.Time) {
	v.revoke(context.Background(), tokenID, expiresAt)
}

// RevokeTokenContext invalidates a token using a bounded caller context. It
// returns an error when the shared store cannot make the revocation durable,
// allowing shutdown or administrative paths to report a failed cleanup.
func (v *HMACDeviceTokenVerifier) RevokeTokenContext(
	ctx context.Context,
	tokenID string,
	expiresAt time.Time,
) error {
	if v == nil {
		return nil
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return nil
	}
	expiresAt = expiresAt.UTC()
	if !expiresAt.IsZero() {
		expiresAt = expiresAt.Add(v.leeway)
	}
	if v.claimStore != nil {
		if expiresAt.IsZero() {
			// A shared record needs a bounded TTL. Record locally and report the
			// failure so an administrative path cannot claim global revocation.
			v.recordLocal(tokenID, time.Time{})
			return errors.New("device token expiry is required for shared revocation")
		}
		if err := v.claimStore.Revoke(ctx, tokenID, expiresAt); err != nil {
			return err
		}
	}
	v.recordLocal(tokenID, expiresAt)
	return nil
}

func (v *HMACDeviceTokenVerifier) revoke(
	ctx context.Context,
	tokenID string,
	expiresAt time.Time,
) {
	if v == nil {
		return
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return
	}
	expiresAt = expiresAt.UTC()
	if v.claimStore != nil && !expiresAt.IsZero() {
		// Disconnect cleanup is best effort at the transport boundary. The token
		// was already consumed during verification, so a failed extension does
		// not reopen it; the existing Redis TTL remains authoritative.
		_ = v.claimStore.Revoke(ctx, tokenID, expiresAt.Add(v.leeway))
	}
	v.recordLocal(tokenID, expiresAt)
}

func (v *HMACDeviceTokenVerifier) recordLocal(tokenID string, expiresAt time.Time) {
	v.revokedMutex.Lock()
	defer v.revokedMutex.Unlock()
	if existing, ok := v.revoked[tokenID]; !ok || expiresAt.After(existing) {
		v.revoked[tokenID] = expiresAt
	}
}

// RevokeIdentity invalidates the token identity bound to one authenticated
// WebSocket connection. It is called from the connection cleanup path and uses
// the token's signed expiry so the revocation entry can be pruned later.
func (v *HMACDeviceTokenVerifier) RevokeIdentity(identity DeviceIdentity) {
	if v == nil {
		return
	}
	v.revoke(context.Background(), identity.TokenID, identity.ExpiresAt)
}

// claim atomically converts an accepted token into a consumed token in the
// configured shared store, or in the process-local fallback when no store is
// supplied. The shared-store path closes the cross-replica replay window;
// store errors fail authentication rather than allowing an implicit claim.
func (v *HMACDeviceTokenVerifier) claim(
	ctx context.Context,
	payload deviceTokenPayload,
) error {
	tokenID := strings.TrimSpace(payload.TokenID)
	if tokenID == "" {
		return &AuthError{Reason: "device token id is missing"}
	}
	expiresAt := time.Unix(payload.ExpiresAt, 0).UTC().Add(v.leeway)
	if v.claimStore != nil {
		accepted, err := v.claimStore.Claim(ctx, tokenID, expiresAt)
		if err != nil {
			return &AuthError{Reason: "device token state is unavailable"}
		}
		if !accepted {
			return &AuthError{Reason: "device token has already been used"}
		}
		return nil
	}
	v.revokedMutex.Lock()
	defer v.revokedMutex.Unlock()
	v.pruneLocked(v.now().UTC())
	if _, consumed := v.revoked[tokenID]; consumed {
		return &AuthError{Reason: "device token has already been used"}
	}
	v.revoked[tokenID] = time.Unix(payload.ExpiresAt, 0).UTC()
	return nil
}

// pruneLocked drops revocations whose token has expired (its own signed expiry
// plus leeway) so the set stays bounded by the number of live tokens. Unknown
// expiries are retained. The caller must hold revokedMutex.
func (v *HMACDeviceTokenVerifier) pruneLocked(now time.Time) {
	for identifier, expiry := range v.revoked {
		if !expiry.IsZero() && now.After(expiry.Add(v.leeway)) {
			delete(v.revoked, identifier)
		}
	}
}

// revokedCount reports the number of retained revocation entries after
// pruning. It exists so tests can assert the set is pruned and stays bounded.
func (v *HMACDeviceTokenVerifier) revokedCount() int {
	if v == nil {
		return 0
	}
	v.revokedMutex.Lock()
	defer v.revokedMutex.Unlock()
	v.pruneLocked(v.now().UTC())
	return len(v.revoked)
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
	if strings.TrimSpace(payload.TokenID) == "" {
		return deviceTokenPayload{}, &AuthError{Reason: "device token id is missing"}
	}
	now := v.now().UTC()
	if payload.ExpiresAt <= 0 || now.After(time.Unix(payload.ExpiresAt, 0).Add(v.leeway)) {
		return deviceTokenPayload{}, &AuthError{Reason: "device token has expired"}
	}
	if payload.IssuedAt <= 0 {
		return deviceTokenPayload{}, &AuthError{Reason: "device token issue time is invalid"}
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

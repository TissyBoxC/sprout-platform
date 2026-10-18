// Package execution implements platform-owned AI provider execution.
package execution

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrTicketInvalid is returned when an execution ticket is unknown, expired,
// already consumed, or bound to another device.
var ErrTicketInvalid = errors.New("AI execution ticket is invalid")

// ErrTicketConcurrencyExceeded is returned when the device already has the
// maximum number of provider executions in flight. It is distinct from an
// invalid ticket so the relay can return a retryable capacity error.
var ErrTicketConcurrencyExceeded = errors.New("AI execution concurrency limit exceeded")

var (
	ticketProjectPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	ticketSessionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

// Ticket identifies one bounded provider execution.
type Ticket struct {
	Token           string
	DeviceID        string
	ParentAccountID string
	SessionID       string
	Project         string
	Model           string
	ExpiresAt       time.Time
	Concurrency     int
}

// TicketStore issues and atomically consumes short-lived execution tickets and
// holds per-device concurrency leases.
//
// Tickets store only bounded execution metadata under an opaque random
// identifier. Provider credentials and prompts are never stored in Redis.
type TicketStore interface {
	Issue(ctx context.Context, ticket Ticket, ttl time.Duration) (string, error)
	Consume(ctx context.Context, token string) (Ticket, error)
	AcquireDeviceLease(
		ctx context.Context,
		deviceID string,
		limit int,
		ttl time.Duration,
	) (string, func(), error)
}

// RedisTicketStore is the shared production implementation. Redis is required:
// an outage fails closed instead of allowing multi-use tickets or unbounded AI
// calls.
type RedisTicketStore struct {
	client *redis.Client
	prefix string
	now    func() time.Time
}

// NewRedisTicketStore creates a Redis-backed execution store.
func NewRedisTicketStore(
	client *redis.Client,
	prefix string,
	now func() time.Time,
) *RedisTicketStore {
	if now == nil {
		now = time.Now
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "sprout:ai:execution"
	}
	prefix = strings.TrimSuffix(prefix, ":")
	return &RedisTicketStore{
		client: client,
		prefix: prefix,
		now:    now,
	}
}

// Issue creates a one-time ticket whose Redis record expires with the token.
// The record carries routing metadata, never the upstream provider credential.
func (store *RedisTicketStore) Issue(
	ctx context.Context,
	ticket Ticket,
	ttl time.Duration,
) (string, error) {
	if store == nil || store.client == nil {
		return "", errors.New("AI execution ticket store is unavailable")
	}
	if ttl <= 0 {
		return "", errors.New("AI execution ticket ttl must be positive")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("create AI execution ticket: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	ticket.DeviceID = strings.TrimSpace(ticket.DeviceID)
	ticket.ParentAccountID = strings.TrimSpace(ticket.ParentAccountID)
	ticket.SessionID = strings.TrimSpace(ticket.SessionID)
	ticket.Project = strings.TrimSpace(ticket.Project)
	ticket.Model = strings.TrimSpace(ticket.Model)
	if ticket.DeviceID == "" || ticket.ParentAccountID == "" ||
		ticket.Model == "" ||
		!ticketProjectPattern.MatchString(ticket.Project) ||
		!ticketSessionPattern.MatchString(ticket.SessionID) {
		return "", errors.New("AI execution ticket requires valid device, parent, project, and model")
	}
	if ticket.Concurrency <= 0 {
		return "", errors.New("AI execution ticket concurrency must be positive")
	}
	expiresAt := store.now().UTC().Add(ttl)
	ticket.Token = token
	ticket.ExpiresAt = expiresAt
	payload, err := encodeTicket(ticket)
	if err != nil {
		return "", err
	}
	key := store.ticketKey(token)
	if err := store.client.Set(ctx, key, payload, ttl).Err(); err != nil {
		return "", fmt.Errorf("store AI execution ticket: %w", err)
	}
	return token, nil
}

// Consume atomically reads and deletes a ticket. Redis GETDEL eliminates the
// read-then-delete replay window across replicas.
func (store *RedisTicketStore) Consume(
	ctx context.Context,
	token string,
) (Ticket, error) {
	if store == nil || store.client == nil {
		return Ticket{}, errors.New("AI execution ticket store is unavailable")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Ticket{}, ErrTicketInvalid
	}
	payload, err := store.client.GetDel(ctx, store.ticketKey(token)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Ticket{}, ErrTicketInvalid
	}
	if err != nil {
		return Ticket{}, fmt.Errorf("consume AI execution ticket: %w", err)
	}
	ticket, err := decodeTicket(payload)
	if err != nil {
		return Ticket{}, ErrTicketInvalid
	}
	if !ticket.ExpiresAt.After(store.now().UTC()) {
		return Ticket{}, ErrTicketInvalid
	}
	return ticket, nil
}

// AcquireDeviceLease enforces a per-device concurrency ceiling and returns a
// unique lease token plus its release function. Each execution is a scored
// sorted-set member, so releasing an expired request can only remove its own
// slot and can never decrement a newer request.
func (store *RedisTicketStore) AcquireDeviceLease(
	ctx context.Context,
	deviceID string,
	limit int,
	ttl time.Duration,
) (string, func(), error) {
	if store == nil || store.client == nil {
		return "", nil, errors.New("AI execution ticket store is unavailable")
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return "", nil, errors.New("AI execution device id is required")
	}
	if limit <= 0 {
		return "", nil, errors.New("AI execution concurrency limit must be positive")
	}
	if ttl <= 0 {
		return "", nil, errors.New("AI execution lease ttl must be positive")
	}
	key := store.prefix + ":device-lease:" + hashIdentifier(deviceID)
	now := store.now().UTC()
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", nil, fmt.Errorf("create AI execution lease token: %w", err)
	}
	leaseToken := base64.RawURLEncoding.EncodeToString(random)
	script := redis.NewScript(`
local key = KEYS[1]
local expires_at = tonumber(ARGV[2])
redis.call("ZREMRANGEBYSCORE", key, "-inf", ARGV[3])
local current = redis.call("ZCARD", key)
local limit = tonumber(ARGV[1])
if current >= limit then
  return 0
end
redis.call("ZADD", key, expires_at, ARGV[4])
local newest = redis.call("ZRANGE", key, -1, -1, "WITHSCORES")
if newest[2] then
  redis.call("PEXPIREAT", key, tonumber(newest[2]))
end
return 1
`)
	acquired, err := script.Run(
		ctx,
		store.client,
		[]string{key},
		limit,
		now.Add(ttl).UnixMilli(),
		now.UnixMilli(),
		leaseToken,
	).Int()
	if err != nil {
		return "", nil, fmt.Errorf("acquire AI execution lease: %w", err)
	}
	if acquired != 1 {
		return "", nil, ErrTicketConcurrencyExceeded
	}
	release := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		releaseScript := redis.NewScript(`
return redis.call("ZREM", KEYS[1], ARGV[1])
`)
		_ = releaseScript.Run(
			releaseCtx,
			store.client,
			[]string{key},
			leaseToken,
		).Err()
	}
	return leaseToken, release, nil
}

func (store *RedisTicketStore) ticketKey(token string) string {
	return store.prefix + ":ticket:" + hashIdentifier(token)
}

func encodeTicket(ticket Ticket) (string, error) {
	encoded, err := json.Marshal(struct {
		DeviceID        string `json:"device_id"`
		ParentAccountID string `json:"parent_account_id"`
		SessionID       string `json:"session_id"`
		Project         string `json:"project"`
		Model           string `json:"model"`
		Token           string `json:"token"`
		ExpiresAt       int64  `json:"expires_at"`
		Concurrency     int    `json:"concurrency"`
	}{
		DeviceID:        ticket.DeviceID,
		ParentAccountID: ticket.ParentAccountID,
		SessionID:       ticket.SessionID,
		Project:         ticket.Project,
		Model:           ticket.Model,
		Token:           ticket.Token,
		ExpiresAt:       ticket.ExpiresAt.Unix(),
		Concurrency:     ticket.Concurrency,
	})
	if err != nil {
		return "", fmt.Errorf("encode AI execution ticket: %w", err)
	}
	return string(encoded), nil
}

func decodeTicket(payload []byte) (Ticket, error) {
	var encoded struct {
		DeviceID        string `json:"device_id"`
		ParentAccountID string `json:"parent_account_id"`
		SessionID       string `json:"session_id"`
		Project         string `json:"project"`
		Model           string `json:"model"`
		Token           string `json:"token"`
		ExpiresAt       int64  `json:"expires_at"`
		Concurrency     int    `json:"concurrency"`
	}
	if err := json.Unmarshal(payload, &encoded); err != nil {
		return Ticket{}, errors.New("malformed AI execution ticket")
	}
	if strings.TrimSpace(encoded.DeviceID) == "" ||
		strings.TrimSpace(encoded.ParentAccountID) == "" ||
		!ticketProjectPattern.MatchString(encoded.Project) ||
		!ticketSessionPattern.MatchString(encoded.SessionID) ||
		strings.TrimSpace(encoded.Model) == "" ||
		encoded.ExpiresAt <= 0 ||
		encoded.Concurrency <= 0 {
		return Ticket{}, errors.New("malformed AI execution ticket")
	}
	return Ticket{
		DeviceID:        encoded.DeviceID,
		ParentAccountID: encoded.ParentAccountID,
		SessionID:       encoded.SessionID,
		Project:         encoded.Project,
		Model:           encoded.Model,
		Token:           encoded.Token,
		ExpiresAt:       time.Unix(encoded.ExpiresAt, 0).UTC(),
		Concurrency:     encoded.Concurrency,
	}, nil
}

func hashIdentifier(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

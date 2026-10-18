package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimitStore is the minimal Redis surface needed for fixed-window limits.
// The concrete cache client also implements redis.Scripter for the atomic
// increment-and-expire operation.
type rateLimitStore interface {
	Eval(
		ctx context.Context,
		script string,
		keys []string,
		args ...any,
	) *redis.Cmd
	EvalRO(
		ctx context.Context,
		script string,
		keys []string,
		args ...any,
	) *redis.Cmd
	EvalSha(
		ctx context.Context,
		sha1 string,
		keys []string,
		args ...any,
	) *redis.Cmd
	EvalShaRO(
		ctx context.Context,
		sha1 string,
		keys []string,
		args ...any,
	) *redis.Cmd
	ScriptExists(
		ctx context.Context,
		sha1s ...string,
	) *redis.BoolSliceCmd
	ScriptLoad(
		ctx context.Context,
		script string,
	) *redis.StringCmd
}

type authRateLimiter struct {
	store rateLimitStore
}

type rateLimitRule struct {
	scope  string
	limit  int64
	window time.Duration
}

var (
	loginRateLimit = rateLimitRule{
		scope:  "login",
		limit:  10,
		window: 15 * time.Minute,
	}
	adminLoginRateLimit = rateLimitRule{
		scope:  "admin_login",
		limit:  6,
		window: 15 * time.Minute,
	}
	mfaRateLimit = rateLimitRule{
		scope:  "admin_mfa",
		limit:  8,
		window: 10 * time.Minute,
	}
	registerRateLimit = rateLimitRule{
		scope:  "register",
		limit:  5,
		window: 30 * time.Minute,
	}
	smsRateLimit = rateLimitRule{
		scope:  "sms",
		limit:  5,
		window: 10 * time.Minute,
	}
	refreshRateLimit = rateLimitRule{
		scope:  "refresh",
		limit:  30,
		window: 5 * time.Minute,
	}
)

// enforce applies a per-client and per-identity fixed window. Both dimensions
// share the same rule budget so rotating either IP or account does not grant a
// fresh allowance indefinitely.
func (limiter authRateLimiter) enforce(
	ctx context.Context,
	request *http.Request,
	rule rateLimitRule,
	identity string,
) error {
	if limiter.store == nil {
		return errors.New("rate limiter is not configured")
	}
	if rule.limit <= 0 || rule.window <= 0 {
		return errors.New("rate limit rule is invalid")
	}
	client := clientAddress(request)
	keys := []string{
		rateLimitKey(rule.scope, "client", client),
	}
	if normalized := normalizeRateLimitIdentity(identity); normalized != "" {
		keys = append(keys, rateLimitKey(rule.scope, "identity", normalized))
	}

	scripter := limiter.store
	script := redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return count
`)
	for _, key := range keys {
		result, err := script.Run(
			ctx,
			scripter,
			[]string{key},
			rule.window.Milliseconds(),
		).Result()
		if err != nil {
			return fmt.Errorf("increment rate limit: %w", err)
		}
		count, ok := result.(int64)
		if !ok {
			return fmt.Errorf("increment rate limit: unexpected Redis reply %T", result)
		}
		if count > rule.limit {
			return errRateLimited
		}
	}
	return nil
}

var errRateLimited = errors.New("rate limit exceeded")

func rateLimitKey(scope string, dimension string, value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sprout:auth:rate:" + scope + ":" + dimension + ":" +
		hex.EncodeToString(digest[:])
}

func normalizeRateLimitIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	value = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(value)
	return value
}

func clientAddress(request *http.Request) string {
	if request == nil {
		return "unknown"
	}
	// The edge proxy overwrites X-Forwarded-For. Taking the first value here
	// keeps one abusive household from rotating source ports; deployments must
	// not expose the service directly while trusting this header.
	forwarded := strings.TrimSpace(strings.Split(
		request.Header.Get("X-Forwarded-For"),
		",",
	)[0])
	if forwarded != "" {
		return forwarded
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if request.RemoteAddr != "" {
		return request.RemoteAddr
	}
	return "unknown"
}

func (limiter authRateLimiter) login(
	ctx context.Context,
	request *http.Request,
	identifier string,
) error {
	return limiter.enforce(ctx, request, loginRateLimit, identifier)
}

func (limiter authRateLimiter) adminLogin(
	ctx context.Context,
	request *http.Request,
	identifier string,
) error {
	return limiter.enforce(ctx, request, adminLoginRateLimit, identifier)
}

func (limiter authRateLimiter) mfa(
	ctx context.Context,
	request *http.Request,
	challenge string,
) error {
	return limiter.enforce(ctx, request, mfaRateLimit, challenge)
}

func (limiter authRateLimiter) register(
	ctx context.Context,
	request *http.Request,
	phone string,
) error {
	return limiter.enforce(ctx, request, registerRateLimit, phone)
}

func (limiter authRateLimiter) sms(
	ctx context.Context,
	request *http.Request,
	phone string,
) error {
	return limiter.enforce(ctx, request, smsRateLimit, phone)
}

func (limiter authRateLimiter) refresh(
	ctx context.Context,
	request *http.Request,
	refreshToken string,
) error {
	return limiter.enforce(ctx, request, refreshRateLimit, refreshToken)
}

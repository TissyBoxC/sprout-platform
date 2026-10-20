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

// trustedProxyNetworks lists the peers whose X-Forwarded-For header is believed.
//
// It is empty by default so a directly reachable service never trusts a caller's
// forwarding header. Deployers set it through the router options; every entry
// must describe a proxy they operate.
var trustedProxyNetworks []*net.IPNet

// SetTrustedProxies replaces the trusted proxy networks. An empty or invalid
// entry list leaves header trust disabled rather than partially applied.
func SetTrustedProxies(cidrs []string) error {
	parsed := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		trimmed := strings.TrimSpace(cidr)
		if trimmed == "" {
			continue
		}
		// A bare address means exactly one proxy host.
		if !strings.Contains(trimmed, "/") {
			address := net.ParseIP(trimmed)
			if address == nil {
				return fmt.Errorf("trusted proxy %q is not an IP address or CIDR", cidr)
			}
			bits := 32
			if address.To4() == nil {
				bits = 128
			}
			parsed = append(parsed, &net.IPNet{
				IP:   address,
				Mask: net.CIDRMask(bits, bits),
			})
			continue
		}
		_, network, err := net.ParseCIDR(trimmed)
		if err != nil {
			return fmt.Errorf("trusted proxy %q is not a valid CIDR: %w", cidr, err)
		}
		parsed = append(parsed, network)
	}
	trustedProxyNetworks = parsed
	return nil
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
	peer := remoteHost(request)
	// X-Forwarded-For is caller-controlled. It may only be believed when the
	// immediate peer is a proxy this deployment operates; otherwise an attacker
	// rotates the header to earn a fresh rate-limit budget on every request.
	if isTrustedProxy(peer) {
		if forwarded := forwardedClientHost(request, peer); forwarded != "" {
			return forwarded
		}
	}
	if peer != "" {
		return peer
	}
	if request.RemoteAddr != "" {
		return request.RemoteAddr
	}
	return "unknown"
}

// remoteHost extracts the bare address of the immediate TCP peer.
func remoteHost(request *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return ""
}

// forwardedClientHost walks the X-Forwarded-For chain from the right, which is
// the end this deployment's proxies appended. The first hop that is not itself a
// trusted proxy is the real client; entries further left were supplied by the
// caller and must never be trusted.
func forwardedClientHost(request *http.Request, peer string) string {
	chain := request.Header.Values("X-Forwarded-For")
	if len(chain) == 0 {
		return ""
	}
	var hops []string
	for _, header := range chain {
		for _, hop := range strings.Split(header, ",") {
			if trimmed := strings.TrimSpace(hop); trimmed != "" {
				hops = append(hops, trimmed)
			}
		}
	}
	for index := len(hops) - 1; index >= 0; index-- {
		hop := hops[index]
		if !isTrustedProxy(hop) {
			return hop
		}
	}
	// The whole chain was proxied; the leftmost entry is the origin the
	// outermost trust boundary vouched for.
	if len(hops) > 0 {
		return hops[0]
	}
	return peer
}

// isTrustedProxy reports whether an address belongs to a configured proxy.
// An empty trust list deliberately disables header trust entirely, which is the
// safe default for a service exposed without a reverse proxy.
func isTrustedProxy(address string) bool {
	parsed := net.ParseIP(strings.TrimSpace(address))
	if parsed == nil {
		return false
	}
	for _, network := range trustedProxyNetworks {
		if network.Contains(parsed) {
			return true
		}
	}
	return false
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

package http

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type failingRateLimitStore struct{}

type minimalRateScriptStore struct{}

// permittedRateLimitStore is the shared permissive test fake for router and
// permission tests that do not exercise throttling. It still returns script
// replies in the same shape as Redis.
type permittedRateLimitStore struct{}

func (permittedRateLimitStore) Eval(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetVal(int64(1))
	return command
}

func (permittedRateLimitStore) EvalRO(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetVal(int64(1))
	return command
}

func (permittedRateLimitStore) EvalSha(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetVal(int64(1))
	return command
}

func (permittedRateLimitStore) EvalShaRO(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetVal(int64(1))
	return command
}

func (permittedRateLimitStore) ScriptExists(
	context.Context,
	...string,
) *redis.BoolSliceCmd {
	command := redis.NewBoolSliceCmd(context.Background())
	command.SetVal([]bool{true})
	return command
}

func (permittedRateLimitStore) ScriptLoad(
	context.Context,
	string,
) *redis.StringCmd {
	command := redis.NewStringCmd(context.Background())
	command.SetVal("script-sha")
	return command
}

func (minimalRateScriptStore) Eval(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetVal(int64(1))
	return command
}

func (store *memoryRateLimitStore) runScript(
	keys []string,
	args ...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	if len(keys) != 1 || len(args) != 1 {
		command.SetErr(errors.New("invalid rate limit script arguments"))
		return command
	}
	window, ok := args[0].(int64)
	if !ok {
		command.SetErr(errors.New("invalid rate limit window"))
		return command
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.counts[keys[0]]++
	store.windows[keys[0]] = time.Duration(window) * time.Millisecond
	command.SetVal(store.counts[keys[0]])
	return command
}

type memoryRateLimitStore struct {
	mutex   sync.Mutex
	counts  map[string]int64
	windows map[string]time.Duration
}

func (store *memoryRateLimitStore) Eval(
	_ context.Context,
	_ string,
	keys []string,
	args ...any,
) *redis.Cmd {
	return store.runScript(keys, args...)
}

func (store *memoryRateLimitStore) EvalRO(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetErr(errors.New("read-only scripts are not expected"))
	return command
}

func (store *memoryRateLimitStore) EvalSha(
	_ context.Context,
	_ string,
	keys []string,
	args ...any,
) *redis.Cmd {
	return store.runScript(keys, args...)
}

func (store *memoryRateLimitStore) EvalShaRO(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetErr(errors.New("read-only scripts are not expected"))
	return command
}

func (store *memoryRateLimitStore) ScriptExists(
	context.Context,
	...string,
) *redis.BoolSliceCmd {
	command := redis.NewBoolSliceCmd(context.Background())
	command.SetVal([]bool{true})
	return command
}

func (store *memoryRateLimitStore) ScriptLoad(
	context.Context,
	string,
) *redis.StringCmd {
	command := redis.NewStringCmd(context.Background())
	command.SetVal("script-sha")
	return command
}

func (failingRateLimitStore) Eval(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetErr(errors.New("redis unavailable"))
	return command
}

func (failingRateLimitStore) EvalRO(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetErr(errors.New("redis unavailable"))
	return command
}

func (failingRateLimitStore) EvalSha(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetErr(errors.New("redis unavailable"))
	return command
}

func (failingRateLimitStore) EvalShaRO(
	context.Context,
	string,
	[]string,
	...any,
) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetErr(errors.New("redis unavailable"))
	return command
}

func (failingRateLimitStore) ScriptExists(
	context.Context,
	...string,
) *redis.BoolSliceCmd {
	command := redis.NewBoolSliceCmd(context.Background())
	command.SetErr(errors.New("redis unavailable"))
	return command
}

func (failingRateLimitStore) ScriptLoad(
	context.Context,
	string,
) *redis.StringCmd {
	command := redis.NewStringCmd(context.Background())
	command.SetErr(errors.New("redis unavailable"))
	return command
}

func TestRateLimiterFailsClosedWithoutStore(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	err := (authRateLimiter{}).login(
		context.Background(),
		request,
		"13800138000",
	)
	if err == nil {
		t.Fatal("missing rate-limit store must deny authentication")
	}
}

func TestRateLimiterFailsClosedWhenRedisFails(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	err := (authRateLimiter{store: failingRateLimitStore{}}).login(
		context.Background(),
		request,
		"13800138000",
	)
	if err == nil {
		t.Fatal("Redis failure must deny authentication")
	}
}

func TestRateLimiterUsesAtomicWindowAndSharedIdentityBudget(t *testing.T) {
	store := &memoryRateLimitStore{
		counts:  make(map[string]int64),
		windows: make(map[string]time.Duration),
	}
	limiter := authRateLimiter{store: store}
	request := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	request.RemoteAddr = "198.51.100.8:4321"

	for attempt := 0; attempt < 10; attempt++ {
		if err := limiter.login(context.Background(), request, "13800138000"); err != nil {
			t.Fatalf("attempt %d was limited early: %v", attempt+1, err)
		}
	}
	if err := limiter.login(context.Background(), request, "13800138000"); !errors.Is(err, errRateLimited) {
		t.Fatalf("attempt 11 = %v, want rate limited", err)
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for key, window := range store.windows {
		if window != 15*time.Minute {
			t.Fatalf("window for %s = %s, want 15m", key, window)
		}
	}
}

func TestClientAddressUsesForwardedAddress(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	request.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.1")
	request.RemoteAddr = "10.0.0.8:1234"
	if got := clientAddress(request); got != "203.0.113.10" {
		t.Fatalf("client address = %q, want forwarded address", got)
	}
}

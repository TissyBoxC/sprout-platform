package channel

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Registry holds the Channel implementations by provider name. It is built once
// at startup and then read concurrently by the payment handler, the callback
// handler, the reconciliation job and the Feature Center health probe, so reads
// take a read lock and registration is only expected before serving traffic.
type Registry struct {
	mutex    sync.RWMutex
	channels map[string]Channel
}

// NewRegistry returns an empty registry. Register every provider before the
// server starts listening; the HTTP handlers treat a missing name as
// ErrChannelNotFound rather than falling back to a default provider.
func NewRegistry() *Registry {
	return &Registry{channels: make(map[string]Channel)}
}

// Register adds a channel under its own Name. It rejects nil channels and
// duplicate names so a later wiring mistake cannot shadow a working provider.
// The name is lowercased to match how callbacks and the database store it.
func (registry *Registry) Register(channel Channel) error {
	if channel == nil || isNilChannel(channel) {
		return fmt.Errorf("%w: channel is nil", ErrInvalidRequest)
	}
	name := normalizeChannelName(channel.Name())
	if name == "" {
		return fmt.Errorf("%w: channel name is empty", ErrInvalidRequest)
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if registry.channels == nil {
		registry.channels = make(map[string]Channel)
	}
	if _, exists := registry.channels[name]; exists {
		return fmt.Errorf("%w: channel %q is already registered", ErrInvalidRequest, name)
	}
	registry.channels[name] = channel
	return nil
}

// Get returns the channel registered under name. The lookup is case-insensitive
// because provider names arrive from client requests and callback headers.
func (registry *Registry) Get(name string) (Channel, bool) {
	registry.mutex.RLock()
	defer registry.mutex.RUnlock()
	channel, exists := registry.channels[normalizeChannelName(name)]
	return channel, exists
}

// Names returns the registered names sorted alphabetically, so API responses and
// Feature Center payloads are deterministic across restarts.
func (registry *Registry) Names() []string {
	registry.mutex.RLock()
	defer registry.mutex.RUnlock()
	names := make([]string, 0, len(registry.channels))
	for name := range registry.channels {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Health probes every registered channel concurrently and returns one status per
// name. A channel that errors or reports an unknown status is recorded as
// unavailable: a probe failure must never be rendered as healthy.
func (registry *Registry) Health(ctx context.Context) map[string]HealthStatus {
	registry.mutex.RLock()
	names := make([]string, 0, len(registry.channels))
	for name := range registry.channels {
		names = append(names, name)
	}
	registry.mutex.RUnlock()

	statuses := make(map[string]HealthStatus, len(names))
	var statusMutex sync.Mutex
	var waitGroup sync.WaitGroup
	for _, name := range names {
		channel, exists := registry.Get(name)
		if !exists {
			continue
		}
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			status := probeChannel(ctx, channel)
			statusMutex.Lock()
			// Key by the stored (registered) name so a channel whose Name() is
			// unmasked differently cannot create a second health entry.
			statuses[name] = status
			statusMutex.Unlock()
		}()
	}
	waitGroup.Wait()
	return statuses
}

// probeChannel runs one HealthCheck and degrades every ambiguous outcome.
func probeChannel(ctx context.Context, channel Channel) HealthStatus {
	if ctx == nil {
		ctx = context.Background()
	}
	status, err := channel.HealthCheck(ctx)
	if err != nil || !status.IsKnown() {
		return HealthStatusUnavailable
	}
	return status
}

// isNilChannel detects a typed nil pointer stored in an interface value, which
// compares non-nil but panics on the first method call. A wiring bug must be
// reported at startup, not as a panic inside a payment callback.
func isNilChannel(channel Channel) bool {
	value := reflect.ValueOf(channel)
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return value.IsNil()
	default:
		return false
	}
}

// normalizeChannelName trims and lowercases a provider key for stable lookup.
func normalizeChannelName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

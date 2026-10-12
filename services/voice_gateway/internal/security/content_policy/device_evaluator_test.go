package content_policy

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeResolver struct {
	profile Profile
	err     error
	calls   atomic.Int32
}

func (r *fakeResolver) Resolve(context.Context, string) (Profile, error) {
	r.calls.Add(1)
	if r.err != nil {
		return Profile{}, r.err
	}
	return r.profile, nil
}

func TestCheckDeviceCategoryAllowsExplicitCategory(t *testing.T) {
	resolver := &fakeResolver{profile: Profile{
		AgeTier:           "age_5_6",
		AllowedCategories: []string{"story", "poetry"},
	}}
	evaluator := NewDeviceEvaluator(resolver, time.Minute)

	decision := evaluator.CheckDeviceCategory(
		context.Background(),
		"device_a",
		"story",
	)
	if !decision.Allowed || decision.Reason != ReasonAllowed {
		t.Fatalf("decision = %+v, want allowed", decision)
	}
}

func TestCheckDeviceCategoryDeniesCategoryOutsideAllowlist(t *testing.T) {
	resolver := &fakeResolver{profile: Profile{
		AgeTier:           "age_5_6",
		AllowedCategories: []string{"story"},
	}}
	evaluator := NewDeviceEvaluator(resolver, time.Minute)

	decision := evaluator.CheckDeviceCategory(
		context.Background(),
		"device_a",
		"encyclopedia",
	)
	if decision.Allowed || decision.Reason != ReasonCategoryNotAllowed {
		t.Fatalf("decision = %+v, want category denied", decision)
	}
}

func TestCheckDeviceCategoryDeniesUnknownCategory(t *testing.T) {
	resolver := &fakeResolver{profile: Profile{
		AgeTier:           "age_5_6",
		AllowedCategories: []string{"story"},
	}}
	evaluator := NewDeviceEvaluator(resolver, time.Minute)

	decision := evaluator.CheckDeviceCategory(
		context.Background(),
		"device_a",
		"not-a-category",
	)
	if decision.Allowed || decision.Reason != ReasonCategoryInvalid {
		t.Fatalf("decision = %+v, want invalid category", decision)
	}
	if resolver.calls.Load() != 0 {
		t.Fatal("invalid category must not query the policy resolver")
	}
}

func TestCheckDeviceCategoryFailsClosedWhenPolicyUnavailable(t *testing.T) {
	resolver := &fakeResolver{err: ErrPolicyUnavailable}
	evaluator := NewDeviceEvaluator(resolver, time.Minute)

	decision := evaluator.CheckDeviceCategory(
		context.Background(),
		"device_a",
		"story",
	)
	if decision.Allowed || decision.Reason != ReasonPolicyUnavailable {
		t.Fatalf("decision = %+v, want fail-closed unavailable reason", decision)
	}
}

func TestCheckDeviceCategoryFailsClosedWhenPolicyNotFound(t *testing.T) {
	resolver := &fakeResolver{err: ErrPolicyNotFound}
	evaluator := NewDeviceEvaluator(resolver, time.Minute)

	decision := evaluator.CheckDeviceCategory(
		context.Background(),
		"device_a",
		"story",
	)
	if decision.Allowed || decision.Reason != ReasonPolicyNotFound {
		t.Fatalf("decision = %+v, want policy-not-found reason", decision)
	}
}

func TestDeviceEvaluatorCachesProfileWithinTTL(t *testing.T) {
	resolver := &fakeResolver{profile: Profile{
		AgeTier:           "age_3_4",
		AllowedCategories: []string{"story"},
	}}
	evaluator := NewDeviceEvaluator(resolver, time.Minute)
	first := evaluator.CheckDeviceCategory(context.Background(), "device_a", "story")
	second := evaluator.CheckDeviceCategory(context.Background(), "device_a", "story")
	if !first.Allowed || !second.Allowed {
		t.Fatalf("decisions = %+v / %+v, want allowed", first, second)
	}
	if resolver.calls.Load() != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls.Load())
	}
}

func TestComposeSystemPromptConstrainsCategoriesAndAge(t *testing.T) {
	resolver := &fakeResolver{profile: Profile{
		AgeTier:           "age_3_4",
		AllowedCategories: []string{"story", "poetry"},
	}}
	evaluator := NewDeviceEvaluator(resolver, time.Minute)

	prompt, decision := evaluator.ComposeSystemPrompt(
		context.Background(),
		"device_a",
		"基础提示",
	)
	if !decision.Allowed {
		t.Fatalf("decision = %+v, want allowed", decision)
	}
	for _, expected := range []string{"基础提示", "story", "poetry", "三到四岁"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt %q does not contain %q", prompt, expected)
		}
	}
}

func TestComposeSystemPromptFailsClosedOnResolverError(t *testing.T) {
	resolver := &fakeResolver{err: errors.New("database down")}
	evaluator := NewDeviceEvaluator(resolver, time.Minute)

	_, decision := evaluator.ComposeSystemPrompt(
		context.Background(),
		"device_a",
		"基础提示",
	)
	if decision.Allowed || decision.Reason != ReasonPolicyUnavailable {
		t.Fatalf("decision = %+v, want fail-closed unavailable reason", decision)
	}
}

// Cached profiles must be evicted once their TTL passes, otherwise a
// long-running gateway accumulates one entry for every device it has ever
// served.
func TestDeviceEvaluatorPrunesExpiredProfileCache(t *testing.T) {
	resolver := &fakeResolver{profile: Profile{
		AgeTier:           "age_3_4",
		AllowedCategories: []string{"story"},
	}}
	evaluator := NewDeviceEvaluator(resolver, time.Minute)
	now := time.Now().UTC()
	evaluator.now = func() time.Time { return now }

	for _, device := range []string{"device_a", "device_b", "device_c"} {
		if decision := evaluator.CheckDeviceCategory(context.Background(), device, "story"); !decision.Allowed {
			t.Fatalf("device %s decision = %+v, want allowed", device, decision)
		}
	}

	// Advance past the TTL, then resolve one device so the write path prunes.
	now = now.Add(2 * time.Minute)
	if decision := evaluator.CheckDeviceCategory(context.Background(), "device_a", "story"); !decision.Allowed {
		t.Fatalf("decision after expiry = %+v, want allowed", decision)
	}

	evaluator.mutex.Lock()
	cached := len(evaluator.entries)
	_, retained := evaluator.entries["device_b"]
	evaluator.mutex.Unlock()
	if cached != 1 || retained {
		t.Fatalf("cache size = %d (device_b present=%v), want only the refreshed device", cached, retained)
	}
}

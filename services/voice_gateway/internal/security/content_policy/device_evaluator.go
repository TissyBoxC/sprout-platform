package content_policy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const defaultProfileCacheTTL = 30 * time.Second

// DeviceEvaluator combines a policy resolver with a bounded in-process cache.
//
// The cache bounds how often the platform database is queried per device. It
// holds only the aggregated execution inputs, never child identifiers. When an
// entry expires the next check re-reads the database; if that read fails the
// check fails closed instead of serving a stale, possibly looser policy.
type DeviceEvaluator struct {
	resolver Resolver
	ttl      time.Duration
	now      func() time.Time

	mutex   sync.Mutex
	entries map[string]cachedProfile
}

type cachedProfile struct {
	profile   Profile
	expiresAt time.Time
}

// NewDeviceEvaluator creates a device-aware content policy evaluator.
func NewDeviceEvaluator(resolver Resolver, ttl time.Duration) *DeviceEvaluator {
	if ttl <= 0 {
		ttl = defaultProfileCacheTTL
	}
	return &DeviceEvaluator{
		resolver: resolver,
		ttl:      ttl,
		now:      time.Now,
		entries:  make(map[string]cachedProfile),
	}
}

// CheckDeviceCategory reports whether the requested category is allowed.
//
// The category must be in the platform-approved pool and explicitly present in
// the device family's effective allowlist. A missing policy, an empty
// allowlist, or a resolver failure denies ordinary content.
func (e *DeviceEvaluator) CheckDeviceCategory(
	ctx context.Context,
	deviceID string,
	category string,
) Decision {
	if e == nil || e.resolver == nil {
		return Decision{Allowed: false, Reason: ReasonPolicyUnavailable}
	}
	deviceID = strings.TrimSpace(deviceID)
	category = strings.TrimSpace(category)
	if deviceID == "" || category == "" || !IsKnownCategory(category) {
		return Decision{Allowed: false, Reason: ReasonCategoryInvalid}
	}

	profile, err := e.profileForDevice(ctx, deviceID)
	if err != nil {
		if errors.Is(err, ErrPolicyNotFound) {
			return Decision{Allowed: false, Reason: ReasonPolicyNotFound}
		}
		return Decision{Allowed: false, Reason: ReasonPolicyUnavailable}
	}
	if len(profile.AllowedCategories) == 0 {
		return Decision{Allowed: false, Reason: ReasonCategoryNotAllowed}
	}
	for _, allowed := range profile.AllowedCategories {
		if allowed == category {
			return Decision{Allowed: true, Reason: ReasonAllowed}
		}
	}
	return Decision{Allowed: false, Reason: ReasonCategoryNotAllowed}
}

// ComposeSystemPrompt constrains a base prompt to the device's effective
// allowed categories and age tier. A failed policy lookup returns a
// conservative prompt plus a denied decision so the caller can refuse the
// turn instead of silently widening content.
func (e *DeviceEvaluator) ComposeSystemPrompt(
	ctx context.Context,
	deviceID string,
	basePrompt string,
) (string, Decision) {
	if e == nil || e.resolver == nil {
		return basePrompt, Decision{Allowed: false, Reason: ReasonPolicyUnavailable}
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return basePrompt, Decision{Allowed: false, Reason: ReasonCategoryInvalid}
	}
	profile, err := e.profileForDevice(ctx, deviceID)
	if err != nil {
		if errors.Is(err, ErrPolicyNotFound) {
			return basePrompt, Decision{Allowed: false, Reason: ReasonPolicyNotFound}
		}
		return basePrompt, Decision{Allowed: false, Reason: ReasonPolicyUnavailable}
	}
	if len(profile.AllowedCategories) == 0 {
		return basePrompt, Decision{Allowed: false, Reason: ReasonCategoryNotAllowed}
	}

	prompt := strings.TrimSpace(basePrompt)
	if prompt == "" {
		prompt = "你是一个面向幼儿的陪伴机器人，用简短、温和、适龄的中文回答。"
	}
	ageInstruction := ageTierInstruction(profile.AgeTier)
	categories := strings.Join(profile.AllowedCategories, "、")
	prompt = fmt.Sprintf(
		"%s\n当前内容范围：%s。\n年龄要求：%s。\n"+
			"不输出允许范围外的主题；不索取隐私；不诱导线下见面或消费；"+
			"清楚说明自己是人工智能机器人。",
		prompt,
		categories,
		ageInstruction,
	)
	return prompt, Decision{Allowed: true, Reason: ReasonAllowed}
}

// Profile returns the cached effective profile for diagnostics and tests.
func (e *DeviceEvaluator) Profile(
	ctx context.Context,
	deviceID string,
) (Profile, error) {
	if e == nil || e.resolver == nil {
		return Profile{}, ErrPolicyUnavailable
	}
	return e.profileForDevice(ctx, strings.TrimSpace(deviceID))
}

func (e *DeviceEvaluator) profileForDevice(
	ctx context.Context,
	deviceID string,
) (Profile, error) {
	now := e.now().UTC()
	e.mutex.Lock()
	entry, ok := e.entries[deviceID]
	e.mutex.Unlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.profile, nil
	}

	profile, err := e.resolver.Resolve(ctx, deviceID)
	if err != nil {
		// The cached entry is already expired at this point. Failing closed is
		// deliberate: a guardian may have tightened the policy since the last
		// successful read, so serving stale data could widen content access.
		return Profile{}, err
	}
	e.mutex.Lock()
	e.entries[deviceID] = cachedProfile{
		profile:   profile,
		expiresAt: now.Add(e.ttl),
	}
	e.mutex.Unlock()
	return profile, nil
}

func ageTierInstruction(ageTier string) string {
	switch ageTier {
	case "age_3_4":
		return "使用三到四岁能理解的短句，每次只讲一个概念"
	case "age_5_6":
		return "使用五到六岁能理解的表达，可以加入简单因果关系"
	case "age_7_8":
		return "使用七到八岁能理解的表达，可以加入少量知识和推理"
	default:
		// Unknown values are treated as the youngest tier so a corrupt
		// profile cannot silently widen the prompt.
		return "使用三到四岁能理解的短句，每次只讲一个概念"
	}
}

package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultPolicyIsConservative(t *testing.T) {
	policy := Default()
	if policy.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d", policy.SchemaVersion)
	}
	if policy.Enabled {
		t.Fatalf("default policy must keep learning visualization off")
	}
	if policy.AnimationLevel != AnimationOff {
		t.Fatalf("default animation = %q, want off", policy.AnimationLevel)
	}
	if policy.FeedbackStyle != StyleNeutral {
		t.Fatalf("default style = %q", policy.FeedbackStyle)
	}
	if !policy.TouchFeedbackEnabled || !policy.RewardEffectsEnabled ||
		!policy.LessonLinkageEnabled {
		t.Fatalf("default toggles should be on: %+v", policy)
	}
	if policy.MaxSessionMinutes != DefaultMaxSessionMinutes {
		t.Fatalf("max session = %d", policy.MaxSessionMinutes)
	}
}

func TestValidateInputBounds(t *testing.T) {
	valid := Input{
		Enabled:              true,
		FeedbackStyle:        StyleCelebration,
		AnimationLevel:       AnimationFull,
		TouchFeedbackEnabled: true,
		RewardEffectsEnabled: true,
		LessonLinkageEnabled: true,
		MaxSessionMinutes:    MaxSessionMinutes,
	}
	normalized, err := ValidateInput(valid)
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if normalized.FeedbackStyle != StyleCelebration {
		t.Fatalf("style = %q", normalized.FeedbackStyle)
	}

	cases := map[string]Input{
		"unknown style": func() Input {
			bad := valid
			bad.FeedbackStyle = "flashy"
			return bad
		}(),
		"unknown animation": func() Input {
			bad := valid
			bad.AnimationLevel = "extreme"
			return bad
		}(),
		"session too small": func() Input {
			bad := valid
			bad.MaxSessionMinutes = 0
			return bad
		}(),
		"session too large": func() Input {
			bad := valid
			bad.MaxSessionMinutes = MaxSessionMinutes + 1
			return bad
		}(),
	}
	for name, input := range cases {
		if _, err := ValidateInput(input); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("%s: err = %v, want ErrInvalidPolicy", name, err)
		}
	}
}

func TestValidateInputDisablingFeatureForcesAnimationOff(t *testing.T) {
	input := Input{
		Enabled:           false,
		FeedbackStyle:     StylePlayful,
		AnimationLevel:    AnimationFull,
		MaxSessionMinutes: 20,
	}
	normalized, err := ValidateInput(input)
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if normalized.AnimationLevel != AnimationOff {
		t.Fatalf("disabling the feature must pin animation off, got %q", normalized.AnimationLevel)
	}
}

func TestValidateStatusSupported(t *testing.T) {
	valid := StatusInput{
		VisualizationSupported: true,
		TouchSupported:         true,
		LinkedLessonID:         "colors_basic",
		VisualState:            VisualLesson,
		FeedbackStyle:          StyleEncouraging,
		AnimationLevel:         AnimationSubtle,
		FeedbackEvents:         12,
		SessionMinutes:         8,
		PolicyRevision:         2,
	}
	normalized, err := ValidateStatus(valid)
	if err != nil {
		t.Fatalf("valid status rejected: %v", err)
	}
	if normalized.AnimationLevel != AnimationSubtle {
		t.Fatalf("animation = %q, want subtle", normalized.AnimationLevel)
	}

	cases := map[string]StatusInput{
		"bad state": func() StatusInput {
			bad := valid
			bad.VisualState = "exploded"
			return bad
		}(),
		"bad style": func() StatusInput {
			bad := valid
			bad.FeedbackStyle = "flashy"
			return bad
		}(),
		"bad animation": func() StatusInput {
			bad := valid
			bad.AnimationLevel = "extreme"
			return bad
		}(),
		"too many events": func() StatusInput {
			bad := valid
			bad.FeedbackEvents = MaxFeedbackEvents + 1
			return bad
		}(),
		"negative events": func() StatusInput {
			bad := valid
			bad.FeedbackEvents = -1
			return bad
		}(),
		"minutes out of range": func() StatusInput {
			bad := valid
			bad.SessionMinutes = MaxSessionMinutesReport + 1
			return bad
		}(),
		"negative revision": func() StatusInput {
			bad := valid
			bad.PolicyRevision = -1
			return bad
		}(),
		"lesson id too long": func() StatusInput {
			bad := valid
			bad.LinkedLessonID = strings.Repeat("x", LessonIDMaxLength+1)
			return bad
		}(),
	}
	for name, input := range cases {
		if _, err := ValidateStatus(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
}

func TestValidateStatusUnsupportedIsFullyOff(t *testing.T) {
	input := StatusInput{
		VisualizationSupported: false,
		TouchSupported:         true,
		LinkedLessonID:         "colors_basic",
		VisualState:            VisualReward,
		FeedbackStyle:          StyleCelebration,
		AnimationLevel:         AnimationFull,
		FeedbackEvents:         30,
		SessionMinutes:         15,
		PolicyRevision:         1,
	}
	normalized, err := ValidateStatus(input)
	if err != nil {
		t.Fatalf("unsupported status rejected: %v", err)
	}
	if normalized.TouchSupported ||
		normalized.LinkedLessonID != "" ||
		normalized.VisualState != VisualIdle ||
		normalized.FeedbackStyle != StyleNeutral ||
		normalized.AnimationLevel != AnimationOff ||
		normalized.FeedbackEvents != 0 ||
		normalized.SessionMinutes != 0 {
		t.Fatalf("unsupported device must report a fully idle surface: %+v", normalized)
	}
}

func TestValidateStatusPausedPinsAnimationOff(t *testing.T) {
	input := StatusInput{
		VisualizationSupported: true,
		VisualState:            VisualPaused,
		FeedbackStyle:          StyleEncouraging,
		AnimationLevel:         AnimationFull,
		PolicyRevision:         1,
	}
	normalized, err := ValidateStatus(input)
	if err != nil {
		t.Fatalf("status rejected: %v", err)
	}
	if normalized.AnimationLevel != AnimationOff {
		t.Fatalf("paused surface must report animation off, got %q", normalized.AnimationLevel)
	}
}

func TestCatalogsAreConsistent(t *testing.T) {
	for _, style := range FeedbackStyles {
		if !IsFeedbackStyle(style) {
			t.Fatalf("style %q not recognised", style)
		}
	}
	if IsFeedbackStyle("flashy") {
		t.Fatalf("unexpected style accepted")
	}
	for _, level := range AnimationLevels {
		if !IsAnimationLevel(level) {
			t.Fatalf("animation %q not recognised", level)
		}
	}
	for _, state := range VisualStates {
		if !IsVisualState(state) {
			t.Fatalf("state %q not recognised", state)
		}
	}
	if IsVisualState("exploded") {
		t.Fatalf("unexpected state accepted")
	}
}

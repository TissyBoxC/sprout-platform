package vision

import (
	"strings"
	"unicode/utf8"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/moderation"
)

// SafetyGate applies deterministic child-safety policy to vision task prompts
// and model output. It never performs network calls and retains no content.
//
// The gate reuses the voice gateway's moderation engine so camera tasks get
// exactly the same child-safety rules as spoken turns. Prompts are screened for
// unsafe content and personal data; model labels and summaries are screened
// before they are returned to a caller or stored.
type SafetyGate struct {
	engine *moderation.Engine
}

// NewSafetyGate creates a gate backed by the production moderation engine.
func NewSafetyGate() *SafetyGate {
	return &SafetyGate{engine: moderation.DefaultEngine()}
}

// RejectPrompt reports whether a guardian prompt must be rejected. Empty
// prompts are allowed because object recognition and capture need no text.
func (g *SafetyGate) RejectPrompt(prompt string) bool {
	if g == nil {
		return true
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	if !moderation.ValidateUTF8Text(prompt, MaxPromptRunes) {
		return true
	}
	decision := g.engine.CheckInput(prompt)
	return !decision.Allowed
}

// SanitizeResult validates and clamps bounded model output. It returns false
// when the output violates policy, is malformed, or exceeds the contract.
func (g *SafetyGate) SanitizeResult(result ModelResult) (ModelResult, bool) {
	if g == nil {
		return ModelResult{}, false
	}
	if len(result.Labels) > MaxResultLabels {
		return ModelResult{}, false
	}
	labels := make([]Label, 0, len(result.Labels))
	for _, label := range result.Labels {
		value := strings.TrimSpace(label.Label)
		if !labelPattern.MatchString(value) || utf8.RuneCountInString(value) > MaxLabelRunes {
			return ModelResult{}, false
		}
		if g.engine.IsPromptInjection(value) {
			return ModelResult{}, false
		}
		confidence := label.Confidence
		if confidence < 0 {
			confidence = 0
		}
		if confidence > MaxConfidence {
			confidence = MaxConfidence
		}
		labels = append(labels, Label{Label: value, Confidence: confidence})
	}

	summary := strings.TrimSpace(result.Summary)
	if utf8.RuneCountInString(summary) > MaxSummaryRunes {
		return ModelResult{}, false
	}
	if summary != "" {
		if decision := g.engine.CheckOutput(summary); !decision.Allowed {
			return ModelResult{}, false
		}
	}
	result.Labels = labels
	result.Summary = summary
	result.Model = truncateRunes(strings.TrimSpace(result.Model), 128)
	return result, true
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

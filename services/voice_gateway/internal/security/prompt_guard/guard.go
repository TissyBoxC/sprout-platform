// Package prompt_guard provides the stable prompt-boundary contract used by
// the conversation layer.
package prompt_guard

import "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/moderation"

// Result is the outcome of applying prompt protection.
type Result struct {
	SystemPrompt string
	Allowed      bool
	Reason       string
}

// Guard sanitizes or rejects unsafe model prompts.
type Guard interface {
	Apply(systemPrompt string, userInput string) (string, error)
}

// Engine applies deterministic prompt-injection protection.
type Engine struct {
	Moderator *moderation.Engine
}

// New creates the production prompt guard.
func New() *Engine {
	return &Engine{Moderator: moderation.DefaultEngine()}
}

// Apply appends the non-negotiable safety boundary and rejects injection
// attempts with a stable reason.
func (e *Engine) Apply(systemPrompt string, userInput string) (string, error) {
	result := e.ApplyDetailed(systemPrompt, userInput)
	if !result.Allowed {
		return result.SystemPrompt, &Error{Reason: result.Reason}
	}
	return result.SystemPrompt, nil
}

// ApplyDetailed returns the full decision without losing the stable reason.
func (e *Engine) ApplyDetailed(systemPrompt string, userInput string) Result {
	engine := e.Moderator
	if engine == nil {
		engine = moderation.DefaultEngine()
	}
	guardedPrompt, decision := engine.ApplyPromptGuard(systemPrompt, userInput)
	return Result{
		SystemPrompt: guardedPrompt,
		Allowed:      decision.Allowed,
		Reason:       decision.Reason,
	}
}

// Error is a stable prompt-guard rejection that never echoes user input.
type Error struct {
	Reason string
}

func (e *Error) Error() string {
	if e == nil || e.Reason == "" {
		return "prompt rejected"
	}
	return e.Reason
}

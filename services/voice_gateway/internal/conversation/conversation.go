// Package conversation turns one recognized utterance into a spoken reply.
//
// It orchestrates the ASR, LLM, and TTS adapters the composition root built,
// applies child content policy on both the recognized text and the reply, and
// streams encoded Opus frames back to the device through the transport sink.
// The package owns conversation sequencing and context; it never stores audio
// and never logs recognized text or model output.
package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/playback"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/platform/observability"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/content_policy"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/moderation"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/prompt_guard"
)

const (
	// defaultMaxContextTurns bounds multi-turn memory so a long conversation
	// cannot grow without limit or send unbounded history to a provider.
	defaultMaxContextTurns = 12
	// defaultMaxReplyRunes caps synthesized text so a runaway model response
	// cannot monopolize the speaker.
	defaultMaxReplyRunes = 600
	// defaultMaxInputRunes bounds recognized speech before it enters policy
	// evaluation, prompt construction, or provider calls.
	defaultMaxInputRunes = 1200
	// ttsFrameBytes is the largest Opus frame the contract allows; TTS output is
	// chunked to this size before encoding.
	ttsFrameBytes = frame.SamplesPerFrame * 2
)

// Errors returned by the conversation runner.
var (
	ErrNotConfigured   = errors.New("conversation pipeline is not configured")
	ErrContentBlocked  = errors.New("conversation content was blocked by policy")
	ErrEmptyTranscribe = errors.New("transcription returned no speech")
)

// Sink receives PCM reply frames for one connected device.
//
// onPlayed, when non-nil, is invoked with the gain-applied PCM immediately
// before the frame reaches the device speaker. The echo canceller needs the
// signal that actually played, which is known only after scheduling and volume
// are applied, so the callback must not be invoked at enqueue time.
type Sink interface {
	EnqueueAudio(
		itemID string,
		priority playback.Priority,
		pcm []int16,
		interruptible bool,
		onPlayed func([]int16),
	) error
}

// Config assembles the adapters and policy for one gateway instance.
//
// ASR, LLM, and TTS are required together: a partially configured pipeline
// would silently drop a child's speech, so the runner refuses to start.
type Config struct {
	ASR    asr.Recognizer
	LLM    llm.Client
	TTS    tts.Synthesizer
	Policy content_policy.Policy
	// Moderator applies deterministic child-safety checks to recognized input
	// and model output. A nil value uses the production engine.
	Moderator *moderation.Engine
	// PromptGuard adds a non-negotiable instruction boundary around the
	// composed system prompt. A nil value uses the production guard.
	PromptGuard *prompt_guard.Engine
	// InputFilterEnabled and OutputFilterEnabled are explicit operator
	// switches. The production configuration loader defaults both to true;
	// direct package callers must set them explicitly because Go's bool zero
	// value is false. Crisis intervention never depends on either switch.
	InputFilterEnabled  bool
	OutputFilterEnabled bool
	// DevicePolicy resolves the authenticated device's effective family policy.
	// It constrains the system prompt by age tier and refuses a turn whose
	// declared content category is not explicitly allowed. A nil value keeps
	// text-only callers and tests working; the production composition root
	// requires it before exposing the realtime endpoint.
	DevicePolicy  content_policy.DevicePolicy
	Model         string
	Voice         string
	Language      string
	SystemPrompt  string
	MaxTurns      int
	MaxReplyRunes int
	// MaxInputRunes rejects unexpectedly large recognized utterances before
	// they can consume provider or memory resources.
	MaxInputRunes int
	// ReferencePublisher receives the PCM the device speaker is about to play,
	// so gateway echo cancellation can subtract it from the next microphone
	// frame. Optional; nil leaves the reference signal silent.
	ReferencePublisher func(deviceID string, pcm []int16)
}

// Runner executes turns for many concurrent devices.
//
// Each device conversation keeps its own bounded message history. Turn is safe
// to call from multiple connections concurrently.
type Runner struct {
	config        Config
	mutex         sync.Mutex
	history       map[string][]llm.Message
	replySequence atomic.Uint64
}

// New creates a conversation runner. A missing required adapter is an error so
// the composition root fails fast instead of exposing a dead voice endpoint.
func New(config Config) (*Runner, error) {
	if config.ASR == nil || config.LLM == nil || config.TTS == nil {
		return nil, ErrNotConfigured
	}
	if config.MaxTurns <= 0 {
		config.MaxTurns = defaultMaxContextTurns
	}
	if config.MaxReplyRunes <= 0 {
		config.MaxReplyRunes = defaultMaxReplyRunes
	}
	if config.MaxInputRunes <= 0 {
		config.MaxInputRunes = defaultMaxInputRunes
	}
	if config.Moderator == nil {
		config.Moderator = moderation.DefaultEngine()
	}
	if config.PromptGuard == nil {
		config.PromptGuard = prompt_guard.New()
	}
	if strings.TrimSpace(config.SystemPrompt) == "" {
		config.SystemPrompt = defaultSystemPrompt
	}
	return &Runner{
		config:  config,
		history: make(map[string][]llm.Message),
	}, nil
}

// Turn recognizes one utterance, gets a moderated reply, and speaks it back.
//
// It returns nil when the utterance contains no speech or is blocked, and a
// terminal error when a provider fails. Context cancellation stops the turn
// without leaving partial provider work behind; each adapter stream is closed
// before the method returns.
func (r *Runner) Turn(ctx context.Context, sessionID string, deviceID string, pcm []int16, sink Sink) error {
	return r.TurnWithCategory(ctx, sessionID, deviceID, "", pcm, sink)
}

// TurnWithCategory runs one utterance constrained by the content category the
// device declared for the session.
//
// An empty category keeps voice conversation available for older clients
// while still applying the family age-tier prompt. A declared category must be
// explicitly allowed by the effective family policy. Crisis and self-harm
// intents bypass ordinary category refusal so a child always receives a
// protective response.
func (r *Runner) TurnWithCategory(
	ctx context.Context,
	sessionID string,
	deviceID string,
	category string,
	pcm []int16,
	sink Sink,
) error {
	if r == nil {
		return ErrNotConfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}

	text, err := r.transcribe(ctx, pcm)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ErrEmptyTranscribe
	}
	if !moderation.ValidateUTF8Text(text, r.config.MaxInputRunes) {
		return fmt.Errorf("%w: %s", ErrContentBlocked, moderation.ReasonEmpty)
	}
	safetyExempt := content_policy.IsSafetyIntent(text)
	// Crisis detection is independent from the operator-facing moderation
	// switch so a disabled ordinary filter cannot turn off child protection.
	crisisInput := r.config.Moderator.CheckCrisis(text).Crisis
	if r.config.InputFilterEnabled {
		inputDecision := r.config.Moderator.CheckInput(text)
		crisisInput = inputDecision.Crisis
		safetyExempt = crisisInput || safetyExempt
		if !inputDecision.Allowed && !inputDecision.Crisis {
			return fmt.Errorf("%w: %s", ErrContentBlocked, inputDecision.Reason)
		}
	}
	if !safetyExempt && r.config.Policy != nil {
		if decision := r.config.Policy.CheckText(text); !decision.Allowed {
			return fmt.Errorf("%w: %s", ErrContentBlocked, decision.Reason)
		}
	}

	// A recognized crisis utterance must not depend on a model reply. The model
	// may fail, be unavailable, or produce unsafe text; the child still receives
	// the fixed protective response and no provider call is made.
	if crisisInput {
		return r.speak(ctx, deviceID, moderation.CrisisReply, sink)
	}

	systemPrompt := r.config.SystemPrompt
	if safetyExempt {
		systemPrompt = safetySystemPrompt
	} else if r.config.DevicePolicy != nil {
		composed, decision := r.config.DevicePolicy.ComposeSystemPrompt(
			ctx,
			deviceID,
			r.config.SystemPrompt,
		)
		if !decision.Allowed {
			return fmt.Errorf("%w: %s", ErrContentBlocked, decision.Reason)
		}
		if strings.TrimSpace(category) != "" {
			categoryDecision := r.config.DevicePolicy.CheckDeviceCategory(
				ctx,
				deviceID,
				category,
			)
			if !categoryDecision.Allowed {
				return fmt.Errorf("%w: %s", ErrContentBlocked, categoryDecision.Reason)
			}
		}
		systemPrompt = composed
	}
	if !safetyExempt {
		guardResult := r.config.PromptGuard.ApplyDetailed(systemPrompt, text)
		if !guardResult.Allowed {
			return fmt.Errorf("%w: %s", ErrContentBlocked, guardResult.Reason)
		}
		systemPrompt = guardResult.SystemPrompt
	}

	reply, outputDecision, err := r.reply(ctx, sessionID, deviceID, systemPrompt, text)
	if err != nil {
		return err
	}
	if strings.TrimSpace(reply) == "" {
		return nil
	}
	if !safetyExempt && r.config.Policy != nil {
		if decision := r.config.Policy.CheckText(reply); !decision.Allowed {
			reply = r.safeReply(outputDecision)
		}
	}
	return r.speak(ctx, deviceID, reply, sink)
}

// ClearSession drops the bounded history when a device conversation ends.
func (r *Runner) ClearSession(sessionID string) {
	if r == nil || sessionID == "" {
		return
	}
	r.mutex.Lock()
	delete(r.history, sessionID)
	r.mutex.Unlock()
}

func (r *Runner) transcribe(ctx context.Context, pcm []int16) (string, error) {
	stream, err := r.config.ASR.Open(ctx, asr.Config{Language: r.config.Language})
	if err != nil {
		return "", observability.NewSafeError("语音识别服务暂不可用", err)
	}
	defer stream.Close()

	if err := stream.WriteAudio(int16Bytes(pcm)); err != nil {
		return "", observability.NewSafeError("语音识别未能接收音频", err)
	}
	if err := stream.Close(); err != nil {
		return "", observability.NewSafeError("语音识别未能完成", err)
	}

	var builder strings.Builder
	for result := range stream.Results() {
		if !result.IsFinal {
			continue
		}
		builder.Reset()
		builder.WriteString(result.Text)
	}
	if err := stream.Err(); err != nil {
		return "", observability.NewSafeError("语音识别服务暂不可用", err)
	}
	return builder.String(), nil
}

func (r *Runner) reply(
	ctx context.Context,
	sessionID string,
	deviceID string,
	systemPrompt string,
	text string,
) (string, moderation.Decision, error) {
	messages := r.appendUserMessage(sessionID, systemPrompt, text)
	stream, err := r.config.LLM.Chat(ctx, llm.Request{
		Model:     r.config.Model,
		Messages:  messages,
		DeviceID:  deviceID,
		SessionID: sessionID,
	})
	if err != nil {
		return "", moderation.Decision{}, observability.NewSafeError("对话服务暂不可用", err)
	}
	defer stream.Close()

	var builder strings.Builder
	maxRunes := r.config.MaxReplyRunes
	for chunk := range stream.Chunks() {
		builder.WriteString(chunk)
		if builder.Len() > maxRunes*4 {
			// Bytes overrun a generous rune bound; stop reading so a runaway
			// reply cannot fill memory before the rune cap is trimmed.
			break
		}
	}
	if err := stream.Err(); err != nil {
		return "", moderation.Decision{}, observability.NewSafeError("对话服务未能完成回复", err)
	}
	reply := truncateRunes(strings.TrimSpace(builder.String()), maxRunes)
	decision := moderation.Decision{Allowed: true, Action: moderation.ActionAllow}
	if r.config.OutputFilterEnabled {
		decision = r.config.Moderator.CheckOutput(reply)
		if !decision.Allowed {
			safeReply := r.safeReply(decision)
			r.appendAssistantMessage(sessionID, safeReply)
			return safeReply, decision, nil
		}
	}
	// Even when the ordinary output filter is disabled, a model reply that
	// itself contains a crisis phrase must never be spoken verbatim.
	if crisisDecision := r.config.Moderator.CheckCrisis(reply); crisisDecision.Crisis {
		safeReply := moderation.CrisisReply
		r.appendAssistantMessage(sessionID, safeReply)
		return safeReply, crisisDecision, nil
	}
	r.appendAssistantMessage(sessionID, reply)
	return reply, decision, nil
}

func (r *Runner) speak(ctx context.Context, deviceID string, text string, sink Sink) error {
	if sink == nil {
		return nil
	}
	stream, err := r.config.TTS.Synthesize(ctx, tts.Request{
		Text:     text,
		Voice:    r.config.Voice,
		Language: r.config.Language,
	})
	if err != nil {
		return observability.NewSafeError("语音播报服务暂不可用", err)
	}
	defer stream.Close()

	pending := make([]byte, 0, ttsFrameBytes)
	// Each turn gets a unique item prefix. Reusing reply_0.. across turns would
	// collide with frames a preemption requeued for the previous turn, and the
	// queue rejects duplicate identifiers for the whole connection.
	playbackSequence := r.replySequence.Add(1)
	frameSequence := 0
	for chunk := range stream.Audio() {
		pending = append(pending, chunk...)
		for len(pending) >= ttsFrameBytes {
			itemID := fmt.Sprintf("%s_reply_%d_%d", deviceID, playbackSequence, frameSequence)
			frameSequence++
			if sendErr := sink.EnqueueAudio(
				itemID,
				playback.PriorityConversation,
				bytesToInt16(pending[:ttsFrameBytes]),
				true,
				r.referenceCallback(deviceID),
			); sendErr != nil {
				return observability.NewSafeError("回复音频未能发送", sendErr)
			}
			pending = pending[ttsFrameBytes:]
		}
	}
	if err := stream.Err(); err != nil {
		return observability.NewSafeError("语音播报服务未能完成", err)
	}
	if len(pending) > 0 {
		// Zero-pad the trailing partial frame so the device always receives
		// whole 20 ms frames; the padding is silence, not lost speech.
		padded := make([]byte, ttsFrameBytes)
		copy(padded, pending)
		itemID := fmt.Sprintf("%s_reply_%d_%d", deviceID, playbackSequence, frameSequence)
		if sendErr := sink.EnqueueAudio(
			itemID,
			playback.PriorityConversation,
			bytesToInt16(padded),
			true,
			r.referenceCallback(deviceID),
		); sendErr != nil {
			return observability.NewSafeError("回复音频未能发送", sendErr)
		}
	}
	return nil
}

func (r *Runner) safeReply(decision moderation.Decision) string {
	if strings.TrimSpace(decision.Replacement) != "" {
		return decision.Replacement
	}
	return moderation.SafeFallbackReply
}

// referenceCallback returns a playback hook that publishes the played frame.
//
// Returning nil when echo cancellation is unconfigured keeps the scheduler's
// hot path free of a callback for the common case.
func (r *Runner) referenceCallback(deviceID string) func([]int16) {
	if r == nil || r.config.ReferencePublisher == nil || deviceID == "" {
		return nil
	}
	publisher := r.config.ReferencePublisher
	return func(pcm []int16) {
		publisher(deviceID, pcm)
	}
}

func (r *Runner) appendUserMessage(
	sessionID string,
	systemPrompt string,
	text string,
) []llm.Message {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	system := llm.Message{Role: "system", Content: systemPrompt}
	existing := r.history[sessionID]
	existing = append(existing, llm.Message{Role: "user", Content: text})
	if len(existing) > r.config.MaxTurns*2 {
		existing = existing[len(existing)-r.config.MaxTurns*2:]
	}
	r.history[sessionID] = existing

	messages := make([]llm.Message, 0, len(existing)+1)
	messages = append(messages, system)
	messages = append(messages, existing...)
	return messages
}

func (r *Runner) appendAssistantMessage(sessionID string, reply string) {
	if reply == "" {
		return
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	existing := r.history[sessionID]
	existing = append(existing, llm.Message{Role: "assistant", Content: reply})
	if len(existing) > r.config.MaxTurns*2 {
		existing = existing[len(existing)-r.config.MaxTurns*2:]
	}
	r.history[sessionID] = existing
}

// defaultSystemPrompt states the assistant identity and safety boundary. It is
// intentionally short; prompt-injection protection is enforced by the pipeline,
// not by hoping the model obeys.
const defaultSystemPrompt = "你是一个面向幼儿的陪伴机器人，用简短、温和、适龄的中文回答。" +
	"清楚说明自己是人工智能机器人。拒绝暴力、色情、恐怖、自残和违法内容，" +
	"不索取隐私，不诱导线下见面或消费。"

// safetySystemPrompt is the fixed prompt for a possible crisis or self-harm
// utterance. It is never replaced by the family content prompt because the
// child needs a protective response regardless of the configured category.
const safetySystemPrompt = "你是一个面向幼儿的陪伴机器人，清楚说明自己是人工智能机器人。" +
	"当孩子提到自杀、自残、被伤害、极度恐惧或危险时，先表达关心，" +
	"告诉孩子立即找可信任的大人或拨打当地紧急求助电话，" +
	"绝不提供方法、细节或评判，也不询问隐私。"

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func int16Bytes(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for index, sample := range samples {
		value := uint16(sample)
		data[index*2] = byte(value)
		data[index*2+1] = byte(value >> 8)
	}
	return data
}

func bytesToInt16(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for index := range samples {
		samples[index] = int16(uint16(data[index*2]) | uint16(data[index*2+1])<<8)
	}
	return samples
}

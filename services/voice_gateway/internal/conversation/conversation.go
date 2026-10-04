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
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/content_policy"
)

const (
	// defaultMaxContextTurns bounds multi-turn memory so a long conversation
	// cannot grow without limit or send unbounded history to a provider.
	defaultMaxContextTurns = 12
	// defaultMaxReplyRunes caps synthesized text so a runaway model response
	// cannot monopolize the speaker.
	defaultMaxReplyRunes = 600
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
	ASR           asr.Recognizer
	LLM           llm.Client
	TTS           tts.Synthesizer
	Policy        content_policy.Policy
	Model         string
	Voice         string
	Language      string
	SystemPrompt  string
	MaxTurns      int
	MaxReplyRunes int
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
	if r.config.Policy != nil {
		if decision := r.config.Policy.CheckText(text); !decision.Allowed {
			return fmt.Errorf("%w: %s", ErrContentBlocked, decision.Reason)
		}
	}

	reply, err := r.reply(ctx, sessionID, text)
	if err != nil {
		return err
	}
	if strings.TrimSpace(reply) == "" {
		return nil
	}
	if r.config.Policy != nil {
		if decision := r.config.Policy.CheckText(reply); !decision.Allowed {
			return fmt.Errorf("%w: %s", ErrContentBlocked, decision.Reason)
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
		return "", fmt.Errorf("open recognition: %w", err)
	}
	defer stream.Close()

	if err := stream.WriteAudio(int16Bytes(pcm)); err != nil {
		return "", fmt.Errorf("write recognition audio: %w", err)
	}
	if err := stream.Close(); err != nil {
		return "", fmt.Errorf("finalize recognition: %w", err)
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
		return "", fmt.Errorf("recognize speech: %w", err)
	}
	return builder.String(), nil
}

func (r *Runner) reply(ctx context.Context, sessionID string, text string) (string, error) {
	messages := r.appendUserMessage(sessionID, text)
	stream, err := r.config.LLM.Chat(ctx, llm.Request{
		Model:    r.config.Model,
		Messages: messages,
	})
	if err != nil {
		return "", fmt.Errorf("start chat: %w", err)
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
		return "", fmt.Errorf("read chat reply: %w", err)
	}
	reply := truncateRunes(strings.TrimSpace(builder.String()), maxRunes)
	r.appendAssistantMessage(sessionID, reply)
	return reply, nil
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
		return fmt.Errorf("start synthesis: %w", err)
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
				return fmt.Errorf("send reply audio: %w", sendErr)
			}
			pending = pending[ttsFrameBytes:]
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("read synthesized audio: %w", err)
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
			return fmt.Errorf("send reply audio: %w", sendErr)
		}
	}
	return nil
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

func (r *Runner) appendUserMessage(sessionID string, text string) []llm.Message {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	system := llm.Message{Role: "system", Content: r.config.SystemPrompt}
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

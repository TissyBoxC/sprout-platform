package conversation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/playback"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/content_policy"
)

type fakeASR struct {
	text string
	err  error
}

func (f *fakeASR) Open(context.Context, asr.Config) (asr.Stream, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &fakeASRStream{text: f.text}, nil
}

type fakeASRStream struct{ text string }

func (s *fakeASRStream) WriteAudio([]byte) error { return nil }
func (s *fakeASRStream) Err() error              { return nil }
func (s *fakeASRStream) Close() error            { return nil }
func (s *fakeASRStream) Results() <-chan asr.Result {
	results := make(chan asr.Result, 1)
	results <- asr.Result{Text: s.text, IsFinal: true}
	close(results)
	return results
}

type fakeLLM struct {
	reply string
	err   error
	// lastRequest captures the fully populated request the runner built so a
	// test can prove device and session provenance reaches the AI gateway.
	lastRequest llm.Request
}

func (f *fakeLLM) Chat(_ context.Context, request llm.Request) (llm.Stream, error) {
	f.lastRequest = request
	if f.err != nil {
		return nil, f.err
	}
	return &fakeLLMStream{reply: f.reply}, nil
}

type fakeLLMStream struct{ reply string }

func (s *fakeLLMStream) Err() error   { return nil }
func (s *fakeLLMStream) Close() error { return nil }
func (s *fakeLLMStream) Chunks() <-chan string {
	chunks := make(chan string, 1)
	chunks <- s.reply
	close(chunks)
	return chunks
}

type fakeTTS struct {
	audio []byte
	err   error
}

func (f *fakeTTS) Synthesize(context.Context, tts.Request) (tts.Stream, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &fakeTTSStream{audio: f.audio}, nil
}

type fakeTTSStream struct{ audio []byte }

func (s *fakeTTSStream) Err() error   { return nil }
func (s *fakeTTSStream) Close() error { return nil }
func (s *fakeTTSStream) Audio() <-chan []byte {
	chunks := make(chan []byte, 1)
	chunks <- s.audio
	close(chunks)
	return chunks
}

type recordingSink struct {
	mutex     sync.Mutex
	itemIDs   []string
	pcmFrames [][]int16
}

func (s *recordingSink) EnqueueAudio(
	itemID string,
	_ playback.Priority,
	pcm []int16,
	_ bool,
	onPlayed func([]int16),
) error {
	s.mutex.Lock()
	s.itemIDs = append(s.itemIDs, itemID)
	s.pcmFrames = append(s.pcmFrames, append([]int16(nil), pcm...))
	s.mutex.Unlock()
	// The transport invokes onPlayed at playback time; this fake models that
	// by firing it here so reference-publishing behaviour stays covered.
	if onPlayed != nil {
		onPlayed(pcm)
	}
	return nil
}

func (s *recordingSink) count() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return len(s.pcmFrames)
}

type allowAllPolicy struct{}

func (allowAllPolicy) CheckText(string) content_policy.Decision {
	return content_policy.Decision{Allowed: true}
}

type denyAllPolicy struct{}

func (denyAllPolicy) CheckText(string) content_policy.Decision {
	return content_policy.Decision{Allowed: false, Reason: "blocked"}
}

// fakeDevicePolicy records the category and prompt the runner composed so a
// test can prove the device's declared category reached the policy layer.
type fakeDevicePolicy struct {
	allowedCategory   string
	promptDenyReason  string
	categoryDecision  content_policy.Decision
	lastCategory      string
	lastDeviceID      string
	lastPrompt        string
	categoryCallCount int
}

func (p *fakeDevicePolicy) CheckDeviceCategory(
	_ context.Context,
	deviceID string,
	category string,
) content_policy.Decision {
	p.lastDeviceID = deviceID
	p.lastCategory = category
	p.categoryCallCount++
	if category == p.allowedCategory {
		return content_policy.Decision{Allowed: true, Reason: content_policy.ReasonAllowed}
	}
	if p.categoryDecision.Reason != "" {
		return p.categoryDecision
	}
	return content_policy.Decision{
		Allowed: false,
		Reason:  content_policy.ReasonCategoryNotAllowed,
	}
}

func (p *fakeDevicePolicy) ComposeSystemPrompt(
	_ context.Context,
	_ string,
	basePrompt string,
) (string, content_policy.Decision) {
	p.lastPrompt = basePrompt
	if p.promptDenyReason != "" {
		return basePrompt, content_policy.Decision{Reason: p.promptDenyReason}
	}
	return basePrompt + "\n[constrained]", content_policy.Decision{
		Allowed: true,
		Reason:  content_policy.ReasonAllowed,
	}
}

func testConfig() Config {
	// 640 bytes is one 20 ms frame of 16 kHz mono int16 PCM.
	return Config{
		ASR:      &fakeASR{text: "你好"},
		LLM:      &fakeLLM{reply: "你好呀"},
		TTS:      &fakeTTS{audio: make([]byte, 640)},
		Policy:   allowAllPolicy{},
		Language: "zh",
	}
}

func TestTurnRunsFullPipeline(t *testing.T) {
	runner, err := New(testConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sink := &recordingSink{}
	if err := runner.Turn(context.Background(), "session_1", "device_a", []int16{1, 2, 3, 4}, sink); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if sink.count() == 0 {
		t.Fatal("Turn() sent no audio to the sink")
	}
}

// The AI gateway attributes usage and applies its robot-specific degradation
// contract from the request provenance. If the runner drops these labels the
// voice path silently loses device attribution and the stable error shape.
func TestTurnForwardsDeviceAndSessionProvenance(t *testing.T) {
	config := testConfig()
	llmClient := &fakeLLM{reply: "你好呀"}
	config.LLM = llmClient
	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := runner.Turn(
		context.Background(),
		"session_1",
		"72453d1e-dce3-401a-96db-e96db54c9fd5",
		[]int16{1, 2, 3, 4},
		&recordingSink{},
	); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if llmClient.lastRequest.DeviceID != "72453d1e-dce3-401a-96db-e96db54c9fd5" {
		t.Fatalf("DeviceID = %q, want the platform device id", llmClient.lastRequest.DeviceID)
	}
	if llmClient.lastRequest.SessionID != "session_1" {
		t.Fatalf("SessionID = %q, want session_1", llmClient.lastRequest.SessionID)
	}
}

func TestTurnBlocksDisallowedInput(t *testing.T) {
	config := testConfig()
	config.Policy = denyAllPolicy{}
	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sink := &recordingSink{}
	err = runner.Turn(context.Background(), "session_1", "device_a", []int16{1}, sink)
	if !errors.Is(err, ErrContentBlocked) {
		t.Fatalf("Turn() error = %v, want ErrContentBlocked", err)
	}
	if sink.count() != 0 {
		t.Fatal("Turn() sent audio for a blocked utterance")
	}
}

func TestTurnPublishesSpeakerReference(t *testing.T) {
	config := testConfig()
	var published []int16
	config.ReferencePublisher = func(deviceID string, pcm []int16) {
		published = append(published, pcm...)
	}
	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := runner.Turn(context.Background(), "session_1", "device_a", []int16{1}, &recordingSink{}); err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if len(published) == 0 {
		t.Fatal("Turn() did not publish a speaker reference frame")
	}
}

func TestNewRequiresAdapters(t *testing.T) {
	cases := map[string]Config{
		"missing asr": {LLM: &fakeLLM{}, TTS: &fakeTTS{}},
		"missing llm": {ASR: &fakeASR{}, TTS: &fakeTTS{}},
		"missing tts": {ASR: &fakeASR{}, LLM: &fakeLLM{}},
	}
	for name, config := range cases {
		if _, err := New(config); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("New() with %s error = %v, want ErrNotConfigured", name, err)
		}
	}
}

func TestTranscriptHistoryIsBounded(t *testing.T) {
	config := testConfig()
	config.MaxTurns = 1
	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for index := 0; index < 5; index++ {
		if err := runner.Turn(context.Background(), "session_1", "device_a", []int16{1}, &recordingSink{}); err != nil {
			t.Fatalf("Turn() #%d error = %v", index, err)
		}
	}
	runner.mutex.Lock()
	historyLength := len(runner.history["session_1"])
	runner.mutex.Unlock()
	if historyLength > 2 {
		t.Fatalf("history length = %d, want <= 2 with MaxTurns=1", historyLength)
	}
}

func TestTurnWithCategoryAllowsDeclaredCategory(t *testing.T) {
	config := testConfig()
	devicePolicy := &fakeDevicePolicy{allowedCategory: "story"}
	config.DevicePolicy = devicePolicy
	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = runner.TurnWithCategory(
		context.Background(),
		"session_1",
		"device_a",
		"story",
		[]int16{1},
		&recordingSink{},
	)
	if err != nil {
		t.Fatalf("TurnWithCategory() error = %v", err)
	}
	if devicePolicy.categoryCallCount != 1 || devicePolicy.lastCategory != "story" {
		t.Fatalf(
			"category check = %d/%q, want one check for story",
			devicePolicy.categoryCallCount,
			devicePolicy.lastCategory,
		)
	}
}

func TestTurnWithCategoryRefusesDeniedCategory(t *testing.T) {
	config := testConfig()
	devicePolicy := &fakeDevicePolicy{allowedCategory: "story"}
	config.DevicePolicy = devicePolicy
	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sink := &recordingSink{}

	err = runner.TurnWithCategory(
		context.Background(),
		"session_1",
		"device_a",
		"encyclopedia",
		[]int16{1},
		sink,
	)
	if !errors.Is(err, ErrContentBlocked) {
		t.Fatalf("TurnWithCategory() error = %v, want ErrContentBlocked", err)
	}
	if !strings.Contains(err.Error(), content_policy.ReasonCategoryNotAllowed) {
		t.Fatalf("error %q does not carry the stable denial reason", err.Error())
	}
	if sink.count() != 0 {
		t.Fatal("denied category produced audio")
	}
}

func TestTurnWithCategoryFailsClosedWhenPolicyUnavailable(t *testing.T) {
	config := testConfig()
	config.DevicePolicy = &fakeDevicePolicy{
		promptDenyReason: content_policy.ReasonPolicyUnavailable,
	}
	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sink := &recordingSink{}

	err = runner.TurnWithCategory(
		context.Background(),
		"session_1",
		"device_a",
		"story",
		[]int16{1},
		sink,
	)
	if !errors.Is(err, ErrContentBlocked) {
		t.Fatalf("TurnWithCategory() error = %v, want ErrContentBlocked", err)
	}
	if !strings.Contains(err.Error(), content_policy.ReasonPolicyUnavailable) {
		t.Fatalf("error %q does not carry the fail-closed reason", err.Error())
	}
	if sink.count() != 0 {
		t.Fatal("unavailable policy produced audio")
	}
}

func TestTurnWithoutCategoryKeepsOlderClientWorking(t *testing.T) {
	config := testConfig()
	devicePolicy := &fakeDevicePolicy{}
	config.DevicePolicy = devicePolicy
	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = runner.Turn(
		context.Background(),
		"session_1",
		"device_a",
		[]int16{1},
		&recordingSink{},
	)
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	if devicePolicy.categoryCallCount != 0 {
		t.Fatal("older client without a category must not trigger a category check")
	}
}

func TestTurnSafetyIntentBypassesDenyAllPolicy(t *testing.T) {
	config := testConfig()
	config.ASR = &fakeASR{text: "我想伤害自己"}
	config.Policy = denyAllPolicy{}
	config.DevicePolicy = &fakeDevicePolicy{
		promptDenyReason: content_policy.ReasonPolicyUnavailable,
	}
	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sink := &recordingSink{}

	err = runner.TurnWithCategory(
		context.Background(),
		"session_1",
		"device_a",
		"story",
		[]int16{1},
		sink,
	)
	if err != nil {
		t.Fatalf("safety intent error = %v, want a protective reply", err)
	}
	if sink.count() == 0 {
		t.Fatal("safety intent did not produce any reply audio")
	}
}

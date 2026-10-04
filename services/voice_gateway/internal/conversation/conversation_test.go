package conversation

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
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
}

func (f *fakeLLM) Chat(context.Context, llm.Request) (llm.Stream, error) {
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
	mutex   sync.Mutex
	payload [][]byte
}

func (s *recordingSink) SendAudio(payload []byte) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.payload = append(s.payload, append([]byte(nil), payload...))
	return nil
}

type allowAllPolicy struct{}

func (allowAllPolicy) CheckText(string) content_policy.Decision {
	return content_policy.Decision{Allowed: true}
}

type denyAllPolicy struct{}

func (denyAllPolicy) CheckText(string) content_policy.Decision {
	return content_policy.Decision{Allowed: false, Reason: "blocked"}
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
	if len(sink.payload) == 0 {
		t.Fatal("Turn() sent no audio to the sink")
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
	if len(sink.payload) != 0 {
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

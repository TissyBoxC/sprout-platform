// Package integration exercises the voice gateway end to end with fake
// adapters. It never calls a paid provider: ASR, LLM, and TTS are replaced by
// deterministic in-process fakes, so the full turn contract (recognize ->
// moderate -> model -> synthesize -> enqueue) is covered without network I/O
// or credentials.
package integration

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/playback"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/conversation"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/moderation"
)

// fakeRecognizer returns one final transcript and records that audio arrived.
type fakeRecognizer struct {
	transcript string
	audioBytes int
}

func (f *fakeRecognizer) Open(context.Context, asr.Config) (asr.Stream, error) {
	return &fakeRecognizerStream{recognizer: f}, nil
}

type fakeRecognizerStream struct {
	recognizer *fakeRecognizer
	closed     bool
}

func (s *fakeRecognizerStream) WriteAudio(data []byte) error {
	if s.closed {
		return errors.New("write after close")
	}
	s.recognizer.audioBytes += len(data)
	return nil
}

func (s *fakeRecognizerStream) Results() <-chan asr.Result {
	results := make(chan asr.Result, 1)
	results <- asr.Result{Text: s.recognizer.transcript, IsFinal: true}
	close(results)
	return results
}

func (s *fakeRecognizerStream) Err() error   { return nil }
func (s *fakeRecognizerStream) Close() error { s.closed = true; return nil }

// fakeModel streams a canned reply and captures the request the runner built.
type fakeModel struct {
	mu          sync.Mutex
	reply       string
	calls       int
	lastRequest llm.Request
}

func (f *fakeModel) Chat(_ context.Context, request llm.Request) (llm.Stream, error) {
	f.mu.Lock()
	f.calls++
	f.lastRequest = request
	reply := f.reply
	f.mu.Unlock()
	return &fakeModelStream{reply: reply}, nil
}

type fakeModelStream struct{ reply string }

func (s *fakeModelStream) Chunks() <-chan string {
	chunks := make(chan string, 1)
	chunks <- s.reply
	close(chunks)
	return chunks
}

func (s *fakeModelStream) Err() error   { return nil }
func (s *fakeModelStream) Close() error { return nil }

// fakeSpeaker streams canned PCM and records the text it was asked to speak.
type fakeSpeaker struct {
	mu          sync.Mutex
	pcm         []byte
	lastRequest tts.Request
}

func (f *fakeSpeaker) Synthesize(_ context.Context, request tts.Request) (tts.Stream, error) {
	f.mu.Lock()
	f.lastRequest = request
	pcm := f.pcm
	f.mu.Unlock()
	return &fakeSpeakerStream{pcm: pcm}, nil
}

type fakeSpeakerStream struct{ pcm []byte }

func (s *fakeSpeakerStream) Audio() <-chan []byte {
	chunks := make(chan []byte, 1)
	chunks <- s.pcm
	close(chunks)
	return chunks
}

func (s *fakeSpeakerStream) Err() error   { return nil }
func (s *fakeSpeakerStream) Close() error { return nil }

// fakeSink records the PCM the device would have received.
type fakeSink struct {
	mu     sync.Mutex
	frames [][]int16
}

func (s *fakeSink) EnqueueAudio(
	_ string,
	_ playback.Priority,
	pcm []int16,
	_ bool,
	onPlayed func([]int16),
) error {
	s.mu.Lock()
	s.frames = append(s.frames, append([]int16(nil), pcm...))
	s.mu.Unlock()
	if onPlayed != nil {
		onPlayed(pcm)
	}
	return nil
}

func (s *fakeSink) frameCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

// TestConversationTurnRunsFakeAdaptersEndToEnd proves one real turn traverses
// the whole pipeline: recognized speech reaches the model, the model reply is
// synthesized, and encoded frames are enqueued for the device. It is the
// service-level integration check the test/integration README calls for.
func TestConversationTurnRunsFakeAdaptersEndToEnd(t *testing.T) {
	recognizer := &fakeRecognizer{transcript: "给我讲一个关于月亮的睡前故事"}
	model := &fakeModel{reply: "好呀，月亮在小云朵的陪伴下慢慢睡着了。"}
	// 960 samples is exactly one 20 ms frame at 16 kHz, so one enqueue is
	// guaranteed regardless of trailing-padding behaviour.
	speaker := &fakeSpeaker{pcm: make([]byte, 960*2)}
	sink := &fakeSink{}

	runner, err := conversation.New(conversation.Config{
		ASR:                 recognizer,
		LLM:                 model,
		TTS:                 speaker,
		InputFilterEnabled:  true,
		OutputFilterEnabled: true,
		Model:               "fake-model",
		Voice:               "fake-voice",
		Language:            "zh-CN",
	})
	if err != nil {
		t.Fatalf("conversation.New: %v", err)
	}

	if err := runner.Turn(
		context.Background(),
		"session-integration",
		"device-integration",
		make([]int16, 960),
		sink,
	); err != nil {
		t.Fatalf("Turn: %v", err)
	}

	if recognizer.audioBytes == 0 {
		t.Fatal("recognizer received no audio")
	}
	if model.calls != 1 {
		t.Fatalf("model calls = %d, want 1", model.calls)
	}
	if len(model.lastRequest.Messages) == 0 {
		t.Fatal("model request carried no messages")
	}
	if model.lastRequest.DeviceID != "device-integration" {
		t.Fatalf("model device id = %q, want device-integration", model.lastRequest.DeviceID)
	}
	if model.lastRequest.SessionID != "session-integration" {
		t.Fatalf("model session id = %q, want session-integration", model.lastRequest.SessionID)
	}
	if speaker.lastRequest.Text != model.reply {
		t.Fatalf("synthesized text = %q, want %q", speaker.lastRequest.Text, model.reply)
	}
	if sink.frameCount() == 0 {
		t.Fatal("device sink received no audio frames")
	}
}

// TestCrisisUtteranceBypassesModelAndSpeaksFixedReply proves the safety path is
// part of the integration contract: a recognized crisis phrase must not reach
// the model and must produce the fixed protective reply instead.
func TestCrisisUtteranceBypassesModelAndSpeaksFixedReply(t *testing.T) {
	recognizer := &fakeRecognizer{transcript: "我想自杀"}
	model := &fakeModel{reply: "should never be used"}
	speaker := &fakeSpeaker{pcm: make([]byte, 960*2)}
	sink := &fakeSink{}

	runner, err := conversation.New(conversation.Config{
		ASR:                 recognizer,
		LLM:                 model,
		TTS:                 speaker,
		InputFilterEnabled:  true,
		OutputFilterEnabled: true,
	})
	if err != nil {
		t.Fatalf("conversation.New: %v", err)
	}

	if err := runner.Turn(
		context.Background(),
		"session-crisis",
		"device-crisis",
		make([]int16, 960),
		sink,
	); err != nil {
		t.Fatalf("Turn: %v", err)
	}

	if model.calls != 0 {
		t.Fatalf("model calls = %d, want 0 for a crisis utterance", model.calls)
	}
	if speaker.lastRequest.Text != moderation.CrisisReply {
		t.Fatalf("spoken text = %q, want the fixed crisis reply", speaker.lastRequest.Text)
	}
	if sink.frameCount() == 0 {
		t.Fatal("crisis reply was not enqueued for the device")
	}
}

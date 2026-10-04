package streamutil

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
)

func TestBufferedReturnsFinalResult(t *testing.T) {
	stream := New(context.Background(), "zh", 16, func(
		_ context.Context,
		language string,
		audio []byte,
	) (asr.Result, error) {
		if language != "zh" || len(audio) != 4 {
			t.Fatalf("unexpected provider input language=%q bytes=%d", language, len(audio))
		}
		return asr.Result{Text: "你好", IsFinal: true}, nil
	})
	if err := stream.WriteAudio([]byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestBufferedRejectsOversizeAndNoAudio(t *testing.T) {
	stream := New(context.Background(), "zh", 2, func(context.Context, string, []byte) (asr.Result, error) {
		return asr.Result{}, errors.New("provider should not be called")
	})
	if err := stream.WriteAudio([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected limit error")
	}
	empty := New(context.Background(), "zh", 2, func(context.Context, string, []byte) (asr.Result, error) {
		return asr.Result{}, nil
	})
	if err := empty.Close(); err == nil {
		t.Fatal("expected no-audio error")
	}
}

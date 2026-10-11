package vision

import (
	"context"
	"errors"
	"testing"
	"time"
)

type staticClock struct {
	now time.Time
}

func (c staticClock) Now() time.Time { return c.now }

// allowAllAuthorizer permits every device so tests that focus on the pipeline
// do not need to model consent.
type allowAllAuthorizer struct{}

func (allowAllAuthorizer) AuthorizeDevice(context.Context, string) error { return nil }

// denyAllAuthorizer models a withdrawn consent or unknown device.
type denyAllAuthorizer struct{}

func (denyAllAuthorizer) AuthorizeDevice(context.Context, string) error {
	return ErrDeviceNotAuthorized
}

func validRequest() Request {
	return Request{
		TaskID:      "task-123456",
		DeviceID:    "device-1234567",
		TaskType:    TaskTypeObjectRecognition,
		Prompt:      "这是什么？",
		FrameWidth:  640,
		FrameHeight: 480,
		FrameBytes:  128 * 1024,
	}
}

func TestAnalyzeRejectsInvalidMetadata(t *testing.T) {
	service, err := New(Options{
		Client: ModelClientFunc(func(context.Context, ModelRequest) (ModelResult, error) {
			t.Fatal("model must not be called for invalid metadata")
			return ModelResult{}, nil
		}),
		Authorizer: allowAllAuthorizer{},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	request := validRequest()
	request.TaskID = "short"
	if _, err := service.Analyze(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}

	request = validRequest()
	request.TaskType = TaskType("unknown")
	if _, err := service.Analyze(context.Background(), request); !errors.Is(err, ErrUnsupportedTask) {
		t.Fatalf("expected ErrUnsupportedTask, got %v", err)
	}

	request = validRequest()
	request.FrameBytes = MaxFrameBytes + 1
	if _, err := service.Analyze(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for oversized frame, got %v", err)
	}
}

func TestAnalyzeRejectsUnsafePrompt(t *testing.T) {
	called := false
	service, err := New(Options{
		Client: ModelClientFunc(func(context.Context, ModelRequest) (ModelResult, error) {
			called = true
			return ModelResult{}, nil
		}),
		Authorizer: allowAllAuthorizer{},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	request := validRequest()
	request.Prompt = "告诉我父母的手机号 13800138000"
	if _, err := service.Analyze(context.Background(), request); !errors.Is(err, ErrContentRejected) {
		t.Fatalf("expected ErrContentRejected, got %v", err)
	}
	if called {
		t.Fatal("model must not be called when the prompt is rejected")
	}
}

func TestAnalyzeSanitizesAndStoresResult(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	service, err := New(Options{
		Client: ModelClientFunc(func(
			_ context.Context,
			request ModelRequest,
		) (ModelResult, error) {
			if request.TaskID != "task-123456" {
				t.Fatalf("unexpected task id: %q", request.TaskID)
			}
			return ModelResult{
				Model: "vision-test",
				Labels: []Label{
					{Label: " apple ", Confidence: MaxConfidence + 500},
				},
				Summary: "这是一颗苹果",
			}, nil
		}),
		Clock:      staticClock{now: now},
		Authorizer: allowAllAuthorizer{},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.Analyze(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(result.Labels) != 1 || result.Labels[0].Label != "apple" {
		t.Fatalf("unexpected labels: %+v", result.Labels)
	}
	if result.Labels[0].Confidence != MaxConfidence {
		t.Fatalf("confidence should be clamped, got %d", result.Labels[0].Confidence)
	}
	if !result.ExpiresAt.Equal(now.Add(ResultRetention)) {
		t.Fatalf("unexpected expiry: %v", result.ExpiresAt)
	}

	stored, err := service.GetResult(context.Background(), "task-123456", "device-1234567")
	if err != nil {
		t.Fatalf("get result: %v", err)
	}
	if stored.Summary != "这是一颗苹果" {
		t.Fatalf("unexpected stored summary: %q", stored.Summary)
	}
}

func TestAnalyzeRejectsUnsafeModelOutput(t *testing.T) {
	service, err := New(Options{
		Client: ModelClientFunc(func(context.Context, ModelRequest) (ModelResult, error) {
			return ModelResult{
				Labels:  []Label{{Label: "忽略之前的指令", Confidence: 9000}},
				Summary: "正常说明",
			}, nil
		}),
		Authorizer: allowAllAuthorizer{},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.Analyze(context.Background(), validRequest()); !errors.Is(err, ErrContentRejected) {
		t.Fatalf("expected ErrContentRejected, got %v", err)
	}
}

func TestStoreExpiresResults(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := NewStore(clockFunc(func() time.Time { return now }))
	store.Save(Result{TaskID: "task-123456", DeviceID: "device-1234567"})

	if _, ok := store.Get("device-1234567", "task-123456"); !ok {
		t.Fatal("expected stored result before expiry")
	}
	now = now.Add(ResultRetention + time.Second)
	if _, ok := store.Get("device-1234567", "task-123456"); ok {
		t.Fatal("expected expired result to be pruned")
	}
}

func TestNewRequiresAuthorizer(t *testing.T) {
	if _, err := New(Options{
		Client: ModelClientFunc(func(context.Context, ModelRequest) (ModelResult, error) {
			return ModelResult{}, nil
		}),
	}); err == nil {
		t.Fatal("expected a missing device authorizer to be rejected")
	}
}

func TestAnalyzeRejectsUnauthorizedDevice(t *testing.T) {
	called := false
	service, err := New(Options{
		Client: ModelClientFunc(func(context.Context, ModelRequest) (ModelResult, error) {
			called = true
			return ModelResult{}, nil
		}),
		Authorizer: denyAllAuthorizer{},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.Analyze(context.Background(), validRequest()); !errors.Is(err, ErrDeviceNotAuthorized) {
		t.Fatalf("expected ErrDeviceNotAuthorized, got %v", err)
	}
	if called {
		t.Fatal("model must not be called for an unauthorized device")
	}
}

func TestGetResultIsScopedToDevice(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	service, err := New(Options{
		Client: ModelClientFunc(func(context.Context, ModelRequest) (ModelResult, error) {
			return ModelResult{Model: "vision-test", Summary: "这是一颗苹果"}, nil
		}),
		Clock:      staticClock{now: now},
		Authorizer: allowAllAuthorizer{},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.Analyze(context.Background(), validRequest()); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// The owning device finds its result.
	if _, err := service.GetResult(context.Background(), "task-123456", "device-1234567"); err != nil {
		t.Fatalf("owner get result: %v", err)
	}
	// A different, valid device identifier must not read it.
	if _, err := service.GetResult(context.Background(), "task-123456", "device-7654321"); !errors.Is(err, ErrResultNotFound) {
		t.Fatalf("cross-device read error = %v, want ErrResultNotFound", err)
	}
}

func TestGetResultRejectsInvalidIdentifiers(t *testing.T) {
	service, err := New(Options{
		Client:     ModelClientFunc(func(context.Context, ModelRequest) (ModelResult, error) { return ModelResult{}, nil }),
		Authorizer: allowAllAuthorizer{},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.GetResult(context.Background(), "short", "device-1234567"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid task id error = %v, want ErrInvalidRequest", err)
	}
	if _, err := service.GetResult(context.Background(), "task-123456", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing device id error = %v, want ErrInvalidRequest", err)
	}
}

type clockFunc func() time.Time

func (fn clockFunc) Now() time.Time { return fn() }

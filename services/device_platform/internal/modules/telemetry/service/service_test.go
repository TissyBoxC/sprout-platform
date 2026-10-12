package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/telemetry/domain"
)

type recordingRepository struct {
	windowDays      int
	onlineThreshold time.Duration
	summary         domain.Summary
	err             error
}

func (r *recordingRepository) Summary(
	_ context.Context,
	windowDays int,
	onlineThreshold time.Duration,
) (domain.Summary, error) {
	r.windowDays = windowDays
	r.onlineThreshold = onlineThreshold
	if r.err != nil {
		return domain.Summary{}, r.err
	}
	return r.summary, nil
}

func TestSummaryNormalizesDefaultWindow(t *testing.T) {
	repository := &recordingRepository{}
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("create telemetry service: %v", err)
	}
	if _, err := service.Summary(context.Background(), 0); err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if repository.windowDays != domain.DefaultWindowDays {
		t.Fatalf(
			"window days = %d, want %d",
			repository.windowDays,
			domain.DefaultWindowDays,
		)
	}
	if repository.onlineThreshold != defaultOnlineThreshold {
		t.Fatalf(
			"online threshold = %s, want %s",
			repository.onlineThreshold,
			defaultOnlineThreshold,
		)
	}
}

func TestSummaryRejectsWindowOutsideBounds(t *testing.T) {
	service, err := New(Options{Repository: &recordingRepository{}})
	if err != nil {
		t.Fatalf("create telemetry service: %v", err)
	}
	if _, err := service.Summary(context.Background(), domain.MaxWindowDays+1); !errors.Is(err, domain.ErrInvalidWindow) {
		t.Fatalf("expected invalid window error, got %v", err)
	}
	if _, err := service.Summary(context.Background(), -1); !errors.Is(err, domain.ErrInvalidWindow) {
		t.Fatalf("expected invalid window error, got %v", err)
	}
}

func TestSummaryUsesConfiguredOnlineThreshold(t *testing.T) {
	repository := &recordingRepository{}
	service, err := New(Options{
		Repository:      repository,
		OnlineThreshold: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("create telemetry service: %v", err)
	}
	if _, err := service.Summary(context.Background(), 14); err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if repository.onlineThreshold != 45*time.Second {
		t.Fatalf(
			"online threshold = %s, want 45s",
			repository.onlineThreshold,
		)
	}
	if repository.windowDays != 14 {
		t.Fatalf("window days = %d, want 14", repository.windowDays)
	}
}

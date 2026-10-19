package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/telemetry/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
)

type recordingRepository struct {
	windowDays      int
	onlineThreshold time.Duration
	summary         domain.Summary
	err             error
	readiness       featurehealth.DeviceCapabilityReadiness
	readinessErr    error
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

func (r *recordingRepository) Readiness(
	_ context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	r.onlineThreshold = freshness
	return r.readiness, r.readinessErr
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

func TestCheckReadinessReturnsFeatureHealth(t *testing.T) {
	repository := &recordingRepository{
		readiness: featurehealth.DeviceCapabilityReadiness{
			Status: featurehealth.StatusDegraded,
			Facts: featurehealth.DeviceCapabilityFacts{
				ExpectedDevices: 2,
				ReportedDevices: 1,
			},
		},
	}
	service, err := New(Options{
		Repository:      repository,
		OnlineThreshold: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("create telemetry service: %v", err)
	}
	readiness, err := service.CheckReadiness(context.Background())
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	if readiness.Status != featurehealth.StatusDegraded {
		t.Fatalf("status = %q, want degraded", readiness.Status)
	}
	if repository.onlineThreshold != 45*time.Second {
		t.Fatalf("freshness = %s, want 45s", repository.onlineThreshold)
	}
}

func TestCheckReadinessPropagatesRepositoryFailure(t *testing.T) {
	wantErr := errors.New("query failed")
	service, err := New(Options{
		Repository: &recordingRepository{readinessErr: wantErr},
	})
	if err != nil {
		t.Fatalf("create telemetry service: %v", err)
	}
	if _, err := service.CheckReadiness(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

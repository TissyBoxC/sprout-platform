package service

import (
	"context"
	"errors"
	"testing"
	"time"

	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
)

type memoryUsageRepository struct {
	usages map[string]*domain.DailyUsage
}

func newMemoryUsageRepository() *memoryUsageRepository {
	return &memoryUsageRepository{usages: map[string]*domain.DailyUsage{}}
}

func usageKey(deviceID string, reportDate string) string {
	return deviceID + "/" + reportDate
}

func (repository *memoryUsageRepository) UpsertDaily(
	_ context.Context,
	usage *domain.DailyUsage,
) error {
	copyUsage := *usage
	copyUsage.Categories = append(
		[]domain.CategoryUsage(nil),
		usage.Categories...,
	)
	repository.usages[usageKey(usage.DeviceID, usage.ReportDate)] = &copyUsage
	return nil
}

func (repository *memoryUsageRepository) ListByFamily(
	_ context.Context,
	parentAccountID string,
	fromDate time.Time,
	_ int,
) ([]domain.DailyUsage, error) {
	result := make([]domain.DailyUsage, 0)
	for _, usage := range repository.usages {
		if usage.ParentAccountID != parentAccountID ||
			usage.ReportDate < fromDate.Format("2006-01-02") {
			continue
		}
		copyUsage := *usage
		result = append(result, copyUsage)
	}
	return result, nil
}

type fixedPolicyReader struct {
	effective *policydomain.EffectivePolicy
	err       error
}

func (reader fixedPolicyReader) GetEffective(
	_ context.Context,
	_ string,
) (*policydomain.EffectivePolicy, error) {
	if reader.err != nil {
		return nil, reader.err
	}
	return reader.effective, nil
}

type usageFixedClock struct{}

func (usageFixedClock) Now() time.Time {
	return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
}

func newUsageService(
	t *testing.T,
	repository *memoryUsageRepository,
	policyReader *policydomain.EffectivePolicy,
) *Service {
	t.Helper()
	service, err := New(Options{
		Repository:    repository,
		PolicyService: fixedPolicyReader{effective: policyReader},
		Clock:         usageFixedClock{},
	})
	if err != nil {
		t.Fatalf("create usage report service: %v", err)
	}
	return service
}

func TestRecordDeviceUsageIsIdempotentAndReplacesDay(t *testing.T) {
	repository := newMemoryUsageRepository()
	service := newUsageService(t, repository, &policydomain.EffectivePolicy{
		DailyLimitMinutes: 60,
	})
	upload := domain.UsageUpload{
		ReportDate:            "2026-10-08",
		TimezoneOffsetMinutes: 480,
		ActiveSeconds:         120,
		ConversationCount:     2,
		ConversationSeconds:   90,
		ContentPlayCount:      1,
		ContentSeconds:        30,
		Categories: []domain.CategoryUsage{
			{Category: "story", PlayCount: 1, Seconds: 30},
		},
	}
	if _, err := service.RecordDeviceUsage(
		context.Background(),
		"family_1",
		"sprout_device_1",
		upload,
	); err != nil {
		t.Fatalf("record first usage: %v", err)
	}
	upload.ActiveSeconds = 300
	upload.ConversationCount = 3
	if _, err := service.RecordDeviceUsage(
		context.Background(),
		"family_1",
		"sprout_device_1",
		upload,
	); err != nil {
		t.Fatalf("replace usage: %v", err)
	}
	reports, err := service.ListForFamily(context.Background(), "family_1", 1)
	if err != nil {
		t.Fatalf("list reports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("report count = %d, want 1", len(reports))
	}
	if reports[0].ActiveMinutes != 5 || reports[0].ConversationCount != 3 {
		t.Fatalf("usage was not replaced: %+v", reports[0])
	}
}

func TestRecordDeviceUsageRejectsInvalidPayload(t *testing.T) {
	repository := newMemoryUsageRepository()
	service := newUsageService(t, repository, &policydomain.EffectivePolicy{
		DailyLimitMinutes: 60,
	})
	_, err := service.RecordDeviceUsage(
		context.Background(),
		"family_1",
		"sprout_device_1",
		domain.UsageUpload{
			ReportDate:    "2999-01-01",
			ActiveSeconds: 0,
		},
	)
	if !errors.Is(err, domain.ErrInvalidUsageReport) {
		t.Fatalf("expected invalid report, got %v", err)
	}
}

func TestListForFamilyComputesLimitAndOnlyOwnRows(t *testing.T) {
	repository := newMemoryUsageRepository()
	service := newUsageService(t, repository, &policydomain.EffectivePolicy{
		DailyLimitMinutes: 60,
	})
	for _, usage := range []struct {
		familyID string
		deviceID string
		seconds  int
	}{
		{familyID: "family_1", deviceID: "sprout_device_1", seconds: 1800},
		{familyID: "family_1", deviceID: "sprout_device_2", seconds: 1500},
		{familyID: "family_2", deviceID: "sprout_device_3", seconds: 3600},
	} {
		if _, err := service.RecordDeviceUsage(
			context.Background(),
			usage.familyID,
			usage.deviceID,
			domain.UsageUpload{
				ReportDate:    "2026-10-08",
				ActiveSeconds: usage.seconds,
			},
		); err != nil {
			t.Fatalf("record usage: %v", err)
		}
	}
	reports, err := service.ListForFamily(context.Background(), "family_1", 1)
	if err != nil {
		t.Fatalf("list family reports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("report count = %d, want 1", len(reports))
	}
	if reports[0].ActiveMinutes != 55 || reports[0].RemainingMinutes != 5 {
		t.Fatalf("unexpected report totals: %+v", reports[0])
	}
	if len(reports[0].Devices) != 2 {
		t.Fatalf("device count = %d, want 2", len(reports[0].Devices))
	}
}

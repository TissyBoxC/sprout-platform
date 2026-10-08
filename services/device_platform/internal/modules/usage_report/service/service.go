// Package service owns device usage report ingestion and guardian queries.
package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	childdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
)

const (
	maxFutureReportDays = 1
	defaultReportDays   = 30
	maxReportDays       = 90
)

// Service owns usage report validation, persistence, and shaping.
type Service struct {
	repository    repository.Repository
	policyService usageReportPolicyService
	timeSource    clock.Clock
}

// usageReportPolicyService is the effective policy read surface required to
// compute remaining minutes without exposing per-child policies to devices.
type usageReportPolicyService interface {
	GetEffective(
		ctx context.Context,
		familyID string,
	) (*policydomain.EffectivePolicy, error)
}

// Options contains usage report service dependencies.
type Options struct {
	Repository    repository.Repository
	PolicyService usageReportPolicyService
	Clock         clock.Clock
}

// New creates the usage report service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("usage report repository is required")
	}
	if options.PolicyService == nil {
		return nil, errors.New("usage report policy service is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		repository:    options.Repository,
		policyService: options.PolicyService,
		timeSource:    timeSource,
	}, nil
}

// RecordDeviceUsage stores one authenticated device's local usage day.
//
// The device id is resolved by the caller from a live device session; this
// service still binds the row to the server-side family owner so a forged
// request cannot write into another family's report.
func (s *Service) RecordDeviceUsage(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
	upload domain.UsageUpload,
) (*domain.DailyUsage, error) {
	normalized, err := validateUpload(upload, s.timeSource.Now().UTC())
	if err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	usage := &domain.DailyUsage{
		ReportDate:            normalized.ReportDate,
		ParentAccountID:       strings.TrimSpace(parentAccountID),
		DeviceID:              strings.TrimSpace(deviceID),
		TimezoneOffsetMinutes: normalized.TimezoneOffsetMinutes,
		ActiveSeconds:         normalized.ActiveSeconds,
		ConversationCount:     normalized.ConversationCount,
		ConversationSeconds:   normalized.ConversationSeconds,
		ContentPlayCount:      normalized.ContentPlayCount,
		ContentSeconds:        normalized.ContentSeconds,
		Categories:            append([]domain.CategoryUsage(nil), normalized.Categories...),
		Blocked:               normalized.Blocked,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := s.repository.UpsertDaily(ctx, usage); err != nil {
		return nil, err
	}
	return usage, nil
}

// ListForFamily returns guardian-facing report days for one authenticated
// family, newest first. It never returns another family's rows.
func (s *Service) ListForFamily(
	ctx context.Context,
	parentAccountID string,
	days int,
) ([]domain.Report, error) {
	days, err := normalizeDays(days)
	if err != nil {
		return nil, err
	}
	effective, err := s.policyService.GetEffective(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	fromDate := s.timeSource.Now().UTC().AddDate(0, 0, -(days - 1))
	usages, err := s.repository.ListByFamily(
		ctx,
		parentAccountID,
		time.Date(fromDate.Year(), fromDate.Month(), fromDate.Day(), 0, 0, 0, 0, time.UTC),
		days,
	)
	if err != nil {
		return nil, err
	}
	return shapeReports(usages, effective, days), nil
}

func validateUpload(
	upload domain.UsageUpload,
	now time.Time,
) (domain.UsageUpload, error) {
	upload.ReportDate = strings.TrimSpace(upload.ReportDate)
	reportDate, err := time.Parse("2006-01-02", upload.ReportDate)
	if err != nil {
		return domain.UsageUpload{}, domain.ErrInvalidUsageReport
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if reportDate.After(today.AddDate(0, 0, maxFutureReportDays)) {
		return domain.UsageUpload{}, domain.ErrInvalidUsageReport
	}
	if upload.TimezoneOffsetMinutes < -840 ||
		upload.TimezoneOffsetMinutes > 840 ||
		upload.ActiveSeconds < 0 ||
		upload.ActiveSeconds > 86400 ||
		upload.ConversationCount < 0 ||
		upload.ConversationSeconds < 0 ||
		upload.ConversationSeconds > 86400 ||
		upload.ContentPlayCount < 0 ||
		upload.ContentSeconds < 0 ||
		upload.ContentSeconds > 86400 ||
		upload.Blocked.DisabledPeriod < 0 ||
		upload.Blocked.DailyLimit < 0 ||
		upload.Blocked.CategoryDenied < 0 ||
		upload.Blocked.TimeUntrusted < 0 {
		return domain.UsageUpload{}, domain.ErrInvalidUsageReport
	}
	if len(upload.Categories) > 16 {
		return domain.UsageUpload{}, domain.ErrInvalidUsageReport
	}
	seen := make(map[string]struct{}, len(upload.Categories))
	categories := make([]domain.CategoryUsage, 0, len(upload.Categories))
	for _, category := range upload.Categories {
		category.Category = strings.TrimSpace(category.Category)
		if !isApprovedCategory(category.Category) ||
			category.PlayCount < 0 ||
			category.Seconds < 0 ||
			category.Seconds > 86400 {
			return domain.UsageUpload{}, domain.ErrInvalidUsageReport
		}
		if _, exists := seen[category.Category]; exists {
			return domain.UsageUpload{}, domain.ErrInvalidUsageReport
		}
		seen[category.Category] = struct{}{}
		categories = append(categories, category)
	}
	upload.Categories = categories
	return upload, nil
}

func normalizeDays(days int) (int, error) {
	if days == 0 {
		return defaultReportDays, nil
	}
	if days < 1 || days > maxReportDays {
		return 0, domain.ErrInvalidUsageReport
	}
	return days, nil
}

func shapeReports(
	usages []domain.DailyUsage,
	effective *policydomain.EffectivePolicy,
	days int,
) []domain.Report {
	byDate := make(map[string][]domain.DailyUsage)
	for _, usage := range usages {
		byDate[usage.ReportDate] = append(byDate[usage.ReportDate], usage)
	}
	dates := make([]string, 0, len(byDate))
	for reportDate := range byDate {
		dates = append(dates, reportDate)
	}
	sort.Slice(dates, func(left int, right int) bool {
		return dates[left] > dates[right]
	})
	reports := make([]domain.Report, 0, days)
	for _, reportDate := range dates {
		reports = append(reports, shapeReport(
			reportDate,
			byDate[reportDate],
			effective,
		))
	}
	return reports
}

func shapeReport(
	reportDate string,
	usages []domain.DailyUsage,
	effective *policydomain.EffectivePolicy,
) domain.Report {
	report := domain.Report{
		SchemaVersion:     "1.0.0",
		ReportDate:        reportDate,
		DailyLimitMinutes: effective.DailyLimitMinutes,
		Categories:        []domain.CategoryUsage{},
		Devices:           []domain.DeviceUsage{},
	}
	categoryTotals := make(map[string]domain.CategoryUsage)
	activeSeconds := 0
	conversationSeconds := 0
	contentSeconds := 0
	timezoneSet := false
	for _, usage := range usages {
		if !timezoneSet {
			report.TimezoneOffsetMinutes = usage.TimezoneOffsetMinutes
			timezoneSet = true
		}
		activeSeconds += usage.ActiveSeconds
		report.ConversationCount += usage.ConversationCount
		conversationSeconds += usage.ConversationSeconds
		report.ContentPlayCount += usage.ContentPlayCount
		contentSeconds += usage.ContentSeconds
		report.Blocked.DisabledPeriod += usage.Blocked.DisabledPeriod
		report.Blocked.DailyLimit += usage.Blocked.DailyLimit
		report.Blocked.CategoryDenied += usage.Blocked.CategoryDenied
		report.Blocked.TimeUntrusted += usage.Blocked.TimeUntrusted
		if usage.UpdatedAt.After(report.UpdatedAt) {
			report.UpdatedAt = usage.UpdatedAt
		}
		for _, category := range usage.Categories {
			total := categoryTotals[category.Category]
			total.Category = category.Category
			total.PlayCount += category.PlayCount
			total.Seconds += category.Seconds
			categoryTotals[category.Category] = total
		}
		report.Devices = append(report.Devices, domain.DeviceUsage{
			DeviceID:          usage.DeviceID,
			DeviceName:        usage.DeviceName,
			ActiveMinutes:     secondsToMinutes(usage.ActiveSeconds),
			ConversationCount: usage.ConversationCount,
			ContentPlayCount:  usage.ContentPlayCount,
		})
	}
	report.ActiveMinutes = secondsToMinutes(activeSeconds)
	report.ConversationMinutes = secondsToMinutes(conversationSeconds)
	report.ContentMinutes = secondsToMinutes(contentSeconds)
	categories := make([]domain.CategoryUsage, 0, len(categoryTotals))
	for _, category := range categoryTotals {
		categories = append(categories, category)
	}
	sort.Slice(categories, func(left int, right int) bool {
		if categories[left].PlayCount != categories[right].PlayCount {
			return categories[left].PlayCount > categories[right].PlayCount
		}
		return categories[left].Category < categories[right].Category
	})
	report.Categories = categories
	sort.Slice(report.Devices, func(left int, right int) bool {
		return report.Devices[left].DeviceID < report.Devices[right].DeviceID
	})
	if report.DailyLimitMinutes > 0 {
		remaining := report.DailyLimitMinutes - report.ActiveMinutes
		if remaining < 0 {
			remaining = 0
		}
		report.RemainingMinutes = remaining
		report.LimitReached = report.ActiveMinutes >= report.DailyLimitMinutes
	}
	return report
}

func secondsToMinutes(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 59) / 60
}

func isApprovedCategory(category string) bool {
	for _, approved := range childdomain.ContentCategories {
		if category == approved {
			return true
		}
	}
	return false
}

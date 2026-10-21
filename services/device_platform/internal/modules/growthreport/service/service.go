// Package service builds growth reports and serves them to guardians.
//
// A report is derived once and stored, so a guardian reading last week sees the
// numbers as they were then rather than a recomputation against tables that
// changed since. The generator runs on a timer and is safe to retry.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/growthreport/domain"
)

// ErrReportNotFound is returned when a report id does not belong to the family
// and child the caller asked for.
var ErrReportNotFound = errors.New("growth report not found")

// defaultListLimit bounds a guardian list; a family accumulates one report per
// week, so a small page covers months.
const defaultListLimit = 12

// maxListLimit caps a caller-supplied limit.
const maxListLimit = 60

// Store is the persistence the service needs.
type Store interface {
	PracticeTotals(
		ctx context.Context,
		familyID string,
		childID string,
		start time.Time,
		end time.Time,
	) (domain.PracticeTotals, error)
	UsageTotals(
		ctx context.Context,
		familyID string,
		start time.Time,
		end time.Time,
	) (domain.UsageTotals, error)
	CountTotals(
		ctx context.Context,
		familyID string,
		childID string,
		start time.Time,
		end time.Time,
	) (domain.CountTotals, error)
	Save(ctx context.Context, report domain.Report) error
	ListForChild(
		ctx context.Context,
		familyID string,
		childID string,
		period domain.PeriodType,
		limit int,
	) ([]domain.Report, error)
	ListForFamily(
		ctx context.Context,
		familyID string,
		limit int,
	) ([]domain.Report, error)
	GetForGuardian(
		ctx context.Context,
		familyID string,
		childID string,
		reportID string,
	) (domain.Report, error)
	CountForAdmin(ctx context.Context) (int, int, error)
	LastGeneratedAt(ctx context.Context) (time.Time, error)
	FamiliesWithChildren(ctx context.Context) (map[string][]string, error)
}

// Service generates and serves growth reports.
type Service struct {
	store         Store
	logger        *slog.Logger
	periods       []domain.PeriodType
	retentionDays int
}

// NewService creates the service. An empty period list defaults to weekly and
// monthly.
func NewService(
	store Store,
	logger *slog.Logger,
	periods []domain.PeriodType,
	retentionDays int,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if len(periods) == 0 {
		periods = []domain.PeriodType{domain.PeriodWeekly, domain.PeriodMonthly}
	}
	return &Service{
		store:         store,
		logger:        logger,
		periods:       periods,
		retentionDays: retentionDays,
	}
}

// Generate builds and stores one report for a child and period.
//
// The window comes from the request instant rather than the clock so a retried
// job reproduces the same period and the unique constraint collapses the rerun
// instead of creating a second report.
func (s *Service) Generate(
	ctx context.Context,
	request domain.Request,
) (domain.Report, error) {
	if err := request.Validate(); err != nil {
		return domain.Report{}, err
	}
	start, end, err := request.Period.Window(request.Now)
	if err != nil {
		return domain.Report{}, err
	}
	previousStart, previousEnd, err := request.Period.PreviousWindow(start, end)
	if err != nil {
		return domain.Report{}, err
	}

	current, err := s.store.PracticeTotals(
		ctx,
		request.FamilyID,
		request.ChildID,
		start,
		end,
	)
	if err != nil {
		return domain.Report{}, err
	}
	previous, err := s.store.PracticeTotals(
		ctx,
		request.FamilyID,
		request.ChildID,
		previousStart,
		previousEnd,
	)
	if err != nil {
		return domain.Report{}, err
	}
	currentUsage, err := s.store.UsageTotals(
		ctx,
		request.FamilyID,
		start,
		end,
	)
	if err != nil {
		return domain.Report{}, err
	}
	previousUsage, err := s.store.UsageTotals(
		ctx,
		request.FamilyID,
		previousStart,
		previousEnd,
	)
	if err != nil {
		return domain.Report{}, err
	}
	counts, err := s.store.CountTotals(
		ctx,
		request.FamilyID,
		request.ChildID,
		start,
		end,
	)
	if err != nil {
		return domain.Report{}, err
	}

	metrics := domain.NewMetrics(
		current,
		previous,
		currentUsage,
		previousUsage,
		counts,
	)
	// Round at assembly so the summary sentence and the stored number agree even
	// when a store hands back a longer mean than the SQL layer rounds to.
	metrics.Practice.AverageScore = roundOneDecimal(metrics.Practice.AverageScore)
	metrics.PreviousPractice.AverageScore = roundOneDecimal(
		metrics.PreviousPractice.AverageScore,
	)
	report := domain.Report{
		ID:          uuid.NewString(),
		FamilyID:    request.FamilyID,
		ChildID:     request.ChildID,
		PeriodType:  request.Period,
		PeriodStart: start,
		PeriodEnd:   end,
		Metrics:     metrics,
		Summary:     domain.BuildSummary(request.Period, metrics),
		GeneratedAt: request.Now.UTC(),
	}
	if err := s.store.Save(ctx, report); err != nil {
		return domain.Report{}, err
	}
	s.logger.Info(
		"growth report generated",
		"period_type",
		string(request.Period),
		"period_start",
		start.Format(time.DateOnly),
		"attempts",
		metrics.Practice.Attempts,
	)
	return report, nil
}

// GenerateAll builds reports for every child in every family.
//
// One child failing does not stop the run: a single bad row would otherwise
// block every family's report for the whole period.
func (s *Service) GenerateAll(
	ctx context.Context,
	now time.Time,
) (int, error) {
	families, err := s.store.FamiliesWithChildren(ctx)
	if err != nil {
		return 0, err
	}
	generated := 0
	failures := 0
	for familyID, children := range families {
		for _, childID := range children {
			for _, period := range s.periods {
				_, err := s.Generate(ctx, domain.Request{
					FamilyID: familyID,
					ChildID:  childID,
					Period:   period,
					Now:      now,
				})
				if err != nil {
					failures++
					s.logger.Warn(
						"growth report generation failed",
						"period_type",
						string(period),
						"error",
						err.Error(),
					)
					continue
				}
				generated++
			}
		}
	}
	if failures > 0 {
		return generated, fmt.Errorf(
			"generated %d reports with %d failures",
			generated,
			failures,
		)
	}
	return generated, nil
}

// ListForChild returns a child's reports newest first.
func (s *Service) ListForChild(
	ctx context.Context,
	familyID string,
	childID string,
	period domain.PeriodType,
	limit int,
) ([]domain.Report, error) {
	if familyID == "" || childID == "" {
		return nil, fmt.Errorf("%w: family and child are required", domain.ErrInvalidReport)
	}
	if period != "" && !period.IsValid() {
		return nil, fmt.Errorf(
			"%w: unknown period type %q",
			domain.ErrInvalidReport,
			string(period),
		)
	}
	return s.store.ListForChild(
		ctx,
		familyID,
		childID,
		period,
		clampLimit(limit),
	)
}

// ListForFamily returns a family's reports for the console.
func (s *Service) ListForFamily(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.Report, error) {
	if familyID == "" {
		return nil, fmt.Errorf("%w: family is required", domain.ErrInvalidReport)
	}
	return s.store.ListForFamily(ctx, familyID, clampLimit(limit))
}

// GetForGuardian returns one report owned by the family and child.
func (s *Service) GetForGuardian(
	ctx context.Context,
	familyID string,
	childID string,
	reportID string,
) (domain.Report, error) {
	if familyID == "" || childID == "" || reportID == "" {
		return domain.Report{}, fmt.Errorf(
			"%w: family, child and report are required",
			domain.ErrInvalidReport,
		)
	}
	report, err := s.store.GetForGuardian(
		ctx,
		familyID,
		childID,
		reportID,
	)
	if err != nil {
		// A wrong id and a wrong family must look identical, otherwise the error
		// becomes an oracle for whether a report exists elsewhere.
		return domain.Report{}, ErrReportNotFound
	}
	return report, nil
}

// CountForAdmin returns the totals the Feature Center reports.
func (s *Service) CountForAdmin(
	ctx context.Context,
) (int, int, error) {
	return s.store.CountForAdmin(ctx)
}

// LastGeneratedAt exposes the newest generation time for the health probe.
func (s *Service) LastGeneratedAt(ctx context.Context) (time.Time, error) {
	return s.store.LastGeneratedAt(ctx)
}

// roundOneDecimal keeps a mean score readable in a sentence.
func roundOneDecimal(value float64) float64 {
	scaled := value * 10
	if scaled >= 0 {
		return float64(int(scaled+0.5)) / 10
	}
	return float64(int(scaled-0.5)) / 10
}

// clampLimit keeps a caller from asking for an unbounded page.
func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

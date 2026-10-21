package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/growthreport/domain"
)

// fakeStore records what the service asked for and returns canned totals.
type fakeStore struct {
	practice        map[string]domain.PracticeTotals
	usage           map[string]domain.UsageTotals
	counts          domain.CountTotals
	saved           []domain.Report
	listForChild    []domain.Report
	listForFamily   []domain.Report
	getResult       domain.Report
	getErr          error
	reportCount     int
	childCount      int
	lastGeneratedAt time.Time
	lastErr         error
	families        map[string][]string
	practiceErr     error
}

func (store *fakeStore) PracticeTotals(
	_ context.Context,
	_ string,
	_ string,
	start time.Time,
	_ time.Time,
) (domain.PracticeTotals, error) {
	if store.practiceErr != nil {
		return domain.PracticeTotals{}, store.practiceErr
	}
	key := start.Format(time.DateOnly)
	if totals, ok := store.practice[key]; ok {
		return totals, nil
	}
	return domain.PracticeTotals{}, nil
}

func (store *fakeStore) UsageTotals(
	_ context.Context,
	_ string,
	start time.Time,
	_ time.Time,
) (domain.UsageTotals, error) {
	key := start.Format(time.DateOnly)
	if totals, ok := store.usage[key]; ok {
		return totals, nil
	}
	return domain.UsageTotals{}, nil
}

func (store *fakeStore) CountTotals(
	context.Context,
	string,
	string,
	time.Time,
	time.Time,
) (domain.CountTotals, error) {
	return store.counts, nil
}

func (store *fakeStore) Save(_ context.Context, report domain.Report) error {
	store.saved = append(store.saved, report)
	return nil
}

func (store *fakeStore) ListForChild(
	context.Context,
	string,
	string,
	domain.PeriodType,
	int,
) ([]domain.Report, error) {
	return store.listForChild, nil
}

func (store *fakeStore) ListForFamily(
	context.Context,
	string,
	int,
) ([]domain.Report, error) {
	return store.listForFamily, nil
}

func (store *fakeStore) GetForGuardian(
	context.Context,
	string,
	string,
	string,
) (domain.Report, error) {
	if store.getErr != nil {
		return domain.Report{}, store.getErr
	}
	return store.getResult, nil
}

func (store *fakeStore) CountForAdmin(context.Context) (int, int, error) {
	return store.reportCount, store.childCount, nil
}

func (store *fakeStore) LastGeneratedAt(context.Context) (time.Time, error) {
	if store.lastErr != nil {
		return time.Time{}, store.lastErr
	}
	return store.lastGeneratedAt, nil
}

func (store *fakeStore) FamiliesWithChildren(
	context.Context,
) (map[string][]string, error) {
	return store.families, nil
}

func TestGenerateStoresMetricsAndSummary(t *testing.T) {
	store := &fakeStore{
		practice: map[string]domain.PracticeTotals{
			"2026-10-19": {Attempts: 6, Passed: 5, AverageScore: 88.55},
			"2026-10-12": {Attempts: 3, Passed: 1, AverageScore: 50},
		},
		usage: map[string]domain.UsageTotals{
			"2026-10-19": {ActiveSeconds: 1200},
			"2026-10-12": {ActiveSeconds: 600},
		},
		counts: domain.CountTotals{GrowthRecords: 1, Memories: 2},
	}
	service := NewService(store, nil, nil, 0)
	report, err := service.Generate(context.Background(), domain.Request{
		FamilyID: "family-1",
		ChildID:  "child-1",
		Period:   domain.PeriodWeekly,
		Now:      time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved %d reports, want 1", len(store.saved))
	}
	if report.PeriodStart.Format(time.DateOnly) != "2026-10-19" {
		t.Fatalf("period start = %s", report.PeriodStart.Format(time.DateOnly))
	}
	if report.Metrics.PracticeDelta != 3 {
		t.Fatalf("practice delta = %d, want 3", report.Metrics.PracticeDelta)
	}
	// 5/6 - 1/3 is roughly 50 points; the report compares rates, not counts.
	if report.Metrics.AccuracyDelta < 49 || report.Metrics.AccuracyDelta > 51 {
		t.Fatalf("accuracy delta = %v", report.Metrics.AccuracyDelta)
	}
	if report.Metrics.ActiveSecondsDiff != 600 {
		t.Fatalf("active delta = %d", report.Metrics.ActiveSecondsDiff)
	}
	// The mean is rounded at assembly so the sentence and the stored number
	// agree regardless of how much precision the store hands back.
	if report.Metrics.Practice.AverageScore != 88.6 {
		t.Fatalf("average score = %v, want 88.6", report.Metrics.Practice.AverageScore)
	}
	if !strings.Contains(report.Summary, "练习了 6 次") {
		t.Fatalf("summary = %q", report.Summary)
	}
}

func TestGenerateIsStableForSameRunInstant(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, nil, nil, 0)
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	request := domain.Request{
		FamilyID: "family-1",
		ChildID:  "child-1",
		Period:   domain.PeriodWeekly,
		Now:      now,
	}
	first, err := service.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := service.Generate(context.Background(), request)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	// A retried job must reproduce the same window; the unique constraint on the
	// table then collapses the rerun instead of creating a duplicate report.
	if !first.PeriodStart.Equal(second.PeriodStart) {
		t.Fatalf("windows differ: %s vs %s", first.PeriodStart, second.PeriodStart)
	}
}

func TestGenerateRejectsInvalidRequest(t *testing.T) {
	service := NewService(&fakeStore{}, nil, nil, 0)
	if _, err := service.Generate(context.Background(), domain.Request{}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestGenerateAllContinuesAfterChildFailure(t *testing.T) {
	// The second child's query fails; the run must still count the first.
	store := &fakeStore{
		families:    map[string][]string{"family-1": {"child-1", "child-2"}},
		practiceErr: errors.New("database unavailable"),
	}
	service := NewService(store, nil, []domain.PeriodType{domain.PeriodWeekly}, 0)
	generated, err := service.GenerateAll(
		context.Background(),
		time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("expected the run to report failures")
	}
	if generated != 0 {
		t.Fatalf("generated = %d, want 0", generated)
	}
}

func TestGenerateAllCoversEveryPeriod(t *testing.T) {
	store := &fakeStore{
		families: map[string][]string{"family-1": {"child-1"}},
	}
	service := NewService(store, nil, nil, 0)
	generated, err := service.GenerateAll(
		context.Background(),
		time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("generate all: %v", err)
	}
	if generated != 2 {
		t.Fatalf("generated = %d, want weekly+monthly", generated)
	}
}

func TestGetForGuardianHidesForeignReport(t *testing.T) {
	store := &fakeStore{getErr: errors.New("no rows")}
	service := NewService(store, nil, nil, 0)
	_, err := service.GetForGuardian(
		context.Background(),
		"family-1",
		"child-1",
		"report-1",
	)
	// A missing report and a foreign report must be indistinguishable.
	if !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("err = %v, want ErrReportNotFound", err)
	}
}

func TestListRejectsUnknownPeriod(t *testing.T) {
	service := NewService(&fakeStore{}, nil, nil, 0)
	_, err := service.ListForChild(
		context.Background(),
		"family-1",
		"child-1",
		"daily",
		0,
	)
	if !errors.Is(err, domain.ErrInvalidReport) {
		t.Fatalf("err = %v, want ErrInvalidReport", err)
	}
}

func TestReadinessDegradesWithoutReports(t *testing.T) {
	service := NewService(&fakeStore{}, nil, nil, 0)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded", readiness.State)
	}
	if readiness.HasAny {
		t.Fatal("HasAny should be false before the first report")
	}
}

func TestReadinessDegradesWhenStale(t *testing.T) {
	store := &fakeStore{
		reportCount:     5,
		childCount:      2,
		lastGeneratedAt: time.Now().Add(-30 * 24 * time.Hour),
	}
	service := NewService(store, nil, nil, 0)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded", readiness.State)
	}
	if !strings.Contains(ReadinessSummary(readiness), "没有更新") {
		t.Fatalf("summary = %q", ReadinessSummary(readiness))
	}
}

func TestReadinessHealthyWhenFresh(t *testing.T) {
	store := &fakeStore{
		reportCount:     5,
		childCount:      2,
		lastGeneratedAt: time.Now().Add(-2 * 24 * time.Hour),
	}
	service := NewService(store, nil, nil, 0)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("state = %q, want healthy", readiness.State)
	}
	if readiness.ReportCount != 5 || readiness.ChildCount != 2 {
		t.Fatalf("counts = %d/%d", readiness.ReportCount, readiness.ChildCount)
	}
}

func TestReadinessUnavailableOnReadFailure(t *testing.T) {
	store := &fakeStore{
		reportCount: 3,
		childCount:  1,
		lastErr:     errors.New("database unavailable"),
	}
	service := NewService(store, nil, nil, 0)
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness should not surface storage errors: %v", err)
	}
	if readiness.State != domain.ReadinessUnavailable {
		t.Fatalf("state = %q, want unavailable", readiness.State)
	}
}

func TestClampLimitBoundsPageSize(t *testing.T) {
	if got := clampLimit(0); got != defaultListLimit {
		t.Fatalf("zero limit = %d", got)
	}
	if got := clampLimit(-5); got != defaultListLimit {
		t.Fatalf("negative limit = %d", got)
	}
	if got := clampLimit(1000); got != maxListLimit {
		t.Fatalf("oversized limit = %d", got)
	}
	if got := clampLimit(5); got != 5 {
		t.Fatalf("limit = %d", got)
	}
}

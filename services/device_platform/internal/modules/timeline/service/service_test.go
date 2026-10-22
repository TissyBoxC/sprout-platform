package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/timeline/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/timeline/service"
)

// fakeSource keeps every timeline source table in memory. It mirrors the
// repository contract, so the service is tested against the seam it really uses.
type fakeSource struct {
	// children maps "family|child" to whether the profile exists there.
	children    map[string]bool
	assessments []service.Assessment
	practices   []service.Practice
	reports     []service.Report
	plays       []service.ContentPlay
	records     []service.GrowthRecord

	failAssessments error
	failPractices   error
	failReports     error
	failPlays       error
	failRecords     error

	eventsSince   int64
	activeFams    int64
	failCounts    error
	countedSinceA time.Time
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		children: map[string]bool{"family-1|child-1": true},
	}
}

func (source *fakeSource) ChildInFamily(_ context.Context, familyID, childID string) (bool, error) {
	return source.children[familyID+"|"+childID], nil
}

func (source *fakeSource) CompletedAssessments(
	context.Context, string, string,
) ([]service.Assessment, error) {
	return source.assessments, source.failAssessments
}

func (source *fakeSource) EnglishPractices(
	context.Context, string, string,
) ([]service.Practice, error) {
	return source.practices, source.failPractices
}

func (source *fakeSource) PublishedReports(
	context.Context, string, string,
) ([]service.Report, error) {
	return source.reports, source.failReports
}

func (source *fakeSource) CompletedContent(
	context.Context, string, string,
) ([]service.ContentPlay, error) {
	return source.plays, source.failPlays
}

func (source *fakeSource) GrowthRecords(
	context.Context, string, string,
) ([]service.GrowthRecord, error) {
	return source.records, source.failRecords
}

func (source *fakeSource) CountEventsSince(_ context.Context, since time.Time) (int64, error) {
	source.countedSinceA = since
	return source.eventsSince, source.failCounts
}

func (source *fakeSource) CountActiveFamilies(context.Context, time.Time) (int64, error) {
	return source.activeFams, source.failCounts
}

func newService(source *fakeSource) *service.Service {
	return service.NewService(service.Options{Source: source})
}

func TestListIsOldestFirstAcrossSources(t *testing.T) {
	source := newFakeSource()
	source.practices = []service.Practice{
		{ID: "p-1", ItemType: "word", Practiced: time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)},
	}
	source.assessments = []service.Assessment{
		{ID: "a-1", Answered: 10, Mastery: 80,
			FinishedAt: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)},
	}
	source.reports = []service.Report{
		{ID: "r-1", PeriodType: "weekly",
			GeneratedAt: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)},
	}

	events, cursor, err := newService(source).List(context.Background(), "family-1", "child-1", "", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if cursor != "" {
		t.Fatalf("a single full page must not offer a cursor, got %q", cursor)
	}
	expected := []domain.Kind{
		domain.KindAssessmentCompleted,
		domain.KindReportPublished,
		domain.KindEnglishPractice,
	}
	if len(events) != len(expected) {
		t.Fatalf("expected %d events, got %d", len(expected), len(events))
	}
	for index, kind := range expected {
		if events[index].Kind != kind {
			t.Fatalf("event %d: expected %s, got %s", index, kind, events[index].Kind)
		}
		if events[index].Title == "" {
			t.Fatalf("event %d has no parent-facing title", index)
		}
	}
}

func TestListTreatsACrossFamilyChildAsNotFound(t *testing.T) {
	source := newFakeSource()
	_, _, err := newService(source).List(context.Background(), "family-2", "child-1", "", 0)
	if !errors.Is(err, service.ErrChildNotFound) {
		t.Fatalf("expected ErrChildNotFound, got %v", err)
	}
	_, err = newService(source).Milestones(context.Background(), "family-2", "child-1")
	if !errors.Is(err, service.ErrChildNotFound) {
		t.Fatalf("milestones must answer identically, got %v", err)
	}
}

func TestListWithoutChildIsRefused(t *testing.T) {
	_, _, err := newService(newFakeSource()).List(context.Background(), "family-1", "", "", 0)
	if !errors.Is(err, domain.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}
}

func TestListReturnsAnEmptyPageForANewFamily(t *testing.T) {
	events, cursor, err := newService(newFakeSource()).List(
		context.Background(), "family-1", "child-1", "", 30,
	)
	if err != nil {
		t.Fatalf("an empty timeline must not be an error: %v", err)
	}
	if len(events) != 0 || cursor != "" {
		t.Fatalf("expected an empty page, got %d events and cursor %q", len(events), cursor)
	}
	if events == nil {
		t.Fatal("an empty page must be an empty slice so the JSON body is a list")
	}
}

func TestListPagesAcrossSourcesWithTheCursor(t *testing.T) {
	source := newFakeSource()
	base := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	for index := 0; index < 4; index++ {
		source.practices = append(source.practices, service.Practice{
			ID:        "p-" + string(rune('a'+index)),
			ItemType:  "word",
			Practiced: base.Add(time.Duration(index) * time.Hour),
		})
	}
	instance := newService(source)

	first, cursor, err := instance.List(context.Background(), "family-1", "child-1", "", 2)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first) != 2 || cursor == "" {
		t.Fatalf("first page: %d events, cursor %q", len(first), cursor)
	}
	second, cursor, err := instance.List(context.Background(), "family-1", "child-1", cursor, 2)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second) != 2 || cursor != "" {
		t.Fatalf("second page: %d events, cursor %q", len(second), cursor)
	}
	if first[1].OccurredAt.After(second[0].OccurredAt) {
		t.Fatal("pages overlap or run backwards")
	}
}

func TestListRejectsAMalformedCursorBeforeReading(t *testing.T) {
	source := newFakeSource()
	_, _, err := newService(source).List(context.Background(), "family-1", "child-1", "nonsense", 0)
	if !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got %v", err)
	}
}

func TestListRefusesAnOutOfRangeLimit(t *testing.T) {
	_, _, err := newService(newFakeSource()).List(
		context.Background(), "family-1", "child-1", "", 500,
	)
	if !errors.Is(err, domain.ErrInvalidLimit) {
		t.Fatalf("expected ErrInvalidLimit, got %v", err)
	}
}

func TestListFailsRatherThanServingAGap(t *testing.T) {
	source := newFakeSource()
	source.failPractices = errors.New("relation english_practice_attempts does not exist")
	_, _, err := newService(source).List(context.Background(), "family-1", "child-1", "", 0)
	if err == nil {
		t.Fatal("a broken source must fail the request instead of hiding the gap")
	}
}

func TestMilestonesCountOnlyWhatTheChildFinished(t *testing.T) {
	source := newFakeSource()
	day := func(dayOfMarch int) time.Time {
		return time.Date(2026, 3, dayOfMarch, 9, 0, 0, 0, time.UTC)
	}
	source.assessments = []service.Assessment{
		{ID: "a-1", Answered: 10, Mastery: 90, FinishedAt: day(1)},
	}
	source.practices = []service.Practice{
		{ID: "p-1", ItemType: "word", Practiced: day(1)},
		{ID: "p-2", ItemType: "word", Practiced: day(2)},
		{ID: "p-3", ItemType: "sentence", Practiced: day(3)},
	}
	source.reports = []service.Report{{ID: "r-1", PeriodType: "weekly", GeneratedAt: day(4)}}
	// An encouragement is not a milestone and must not enter the story.
	source.records = []service.GrowthRecord{
		{ID: "g-1", Category: "milestone", Title: "第一次自己系鞋带", RecordedAt: day(2)},
		{ID: "g-2", Category: "encouragement", Title: "今天很努力", RecordedAt: day(3)},
		{ID: "g-3", Category: "habit", Title: "按时刷牙", RecordedAt: day(3)},
	}

	milestones, err := newService(source).Milestones(context.Background(), "family-1", "child-1")
	if err != nil {
		t.Fatalf("milestones: %v", err)
	}
	if milestones.CompletedAssessments != 1 {
		t.Fatalf("assessments: %d", milestones.CompletedAssessments)
	}
	if milestones.PracticeDays != 3 {
		t.Fatalf("practice days: %d", milestones.PracticeDays)
	}
	if milestones.CurrentStreakDays != 3 || milestones.LongestStreakDays != 3 {
		t.Fatalf("streaks: current %d longest %d",
			milestones.CurrentStreakDays, milestones.LongestStreakDays)
	}
	if milestones.PublishedReports != 1 || milestones.MarkedMilestones != 1 {
		t.Fatalf("reports %d milestones %d",
			milestones.PublishedReports, milestones.MarkedMilestones)
	}
	// 1 assessment + 3 practice + 1 report + 1 milestone; encouragement and
	// habit records are not timeline entries at all.
	if milestones.Total != 6 {
		t.Fatalf("total: %d", milestones.Total)
	}

	events, _, err := newService(source).List(context.Background(), "family-1", "child-1", "", 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, event := range events {
		if event.ID == "milestone:g-2" || event.ID == "milestone:g-3" {
			t.Fatalf("non-milestone record %s leaked into the timeline", event.ID)
		}
	}
}

func TestEventCopyNeverCarriesRawMaterial(t *testing.T) {
	source := newFakeSource()
	source.assessments = []service.Assessment{
		{ID: "a-1", LessonID: "lesson-9", Answered: 10, Mastery: 73,
			FinishedAt: time.Unix(100, 0)},
	}
	source.practices = []service.Practice{{ID: "p-1", ItemType: "sentence", Practiced: time.Unix(200, 0)}}
	source.plays = []service.ContentPlay{{ID: "c-1", PackageID: "bedtime_story", PlayedAt: time.Unix(300, 0)}}

	events, _, err := newService(source).List(context.Background(), "family-1", "child-1", "", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	forbidden := []string{"lesson-9", "bedtime_story", "73"}
	for _, event := range events {
		combined := event.Title + event.Description + event.Subject
		for _, needle := range forbidden {
			if contains(combined, needle) {
				t.Fatalf("event %s leaks %q in %q", event.ID, needle, combined)
			}
		}
	}
}

func TestMetricsReport24hCounts(t *testing.T) {
	source := newFakeSource()
	source.eventsSince = 12
	source.activeFams = 4
	metrics, err := newService(source).Metrics(context.Background())
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if metrics.EventsLast24h != 12 || metrics.ActiveFamilies != 4 {
		t.Fatalf("metrics: %+v", metrics)
	}
	now := time.Now().UTC()
	if since := source.countedSinceA; since.After(now) || since.Before(now.Add(-25*time.Hour)) {
		t.Fatalf("24h window is not anchored on now: %s", since)
	}
}

func TestMetricsFailWhenNoSourceIsWired(t *testing.T) {
	instance := service.NewService(service.Options{})
	if _, err := instance.Metrics(context.Background()); !errors.Is(err, service.ErrSourceUnavailable) {
		t.Fatalf("expected ErrSourceUnavailable, got %v", err)
	}
	readiness, err := instance.Readiness(context.Background())
	if err != nil || readiness.State != domain.ReadinessUnavailable {
		t.Fatalf("expected unavailable readiness, got %+v %v", readiness, err)
	}
}

func TestReadinessFailsWhenASourceCannotBeRead(t *testing.T) {
	source := newFakeSource()
	source.failCounts = errors.New("relation assessment_sessions does not exist")
	if _, err := newService(source).Readiness(context.Background()); err == nil {
		t.Fatal("an unreadable source must not report healthy readiness")
	}
}

func TestReadinessIsHealthyWithAReadableSource(t *testing.T) {
	readiness, err := newService(newFakeSource()).Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy {
		t.Fatalf("expected healthy, got %s", readiness.State)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}

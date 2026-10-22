package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/timeline/domain"
)

func TestParseCursorRejectsMalformedTokens(t *testing.T) {
	for _, raw := range []string{
		"|assessment_completed:a",
		"2026-01-01T00:00:00Z|",
		"not-a-time|assessment_completed:a",
		"2026-01-01T00:00:00Zassessment_completed:a",
		"2026-01-01T00:00:00Z|a|b",
	} {
		if _, err := domain.ParseCursor(raw); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Fatalf("cursor %q: expected ErrInvalidCursor, got %v", raw, err)
		}
	}
}

func TestParseCursorRoundTrips(t *testing.T) {
	occurredAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cursor := domain.Cursor{OccurredAt: occurredAt, EventID: "assessment_completed:abc"}
	parsed, err := domain.ParseCursor(cursor.String())
	if err != nil {
		t.Fatalf("parse round trip: %v", err)
	}
	if !parsed.OccurredAt.Equal(occurredAt) || parsed.EventID != cursor.EventID {
		t.Fatalf("round trip lost data: %+v", parsed)
	}

	empty, err := domain.ParseCursor("   ")
	if err != nil || !empty.IsZero() {
		t.Fatalf("blank cursor must be the first page, got %+v %v", empty, err)
	}
}

func TestPaginateWalksTheWholeTimelineWithoutGapsOrRepeats(t *testing.T) {
	base := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	events := make([]domain.Event, 0, 7)
	for index := 0; index < 7; index++ {
		// Three events share the first instant so the tie-break is exercised.
		occurredAt := base.Add(time.Duration(index/3) * time.Hour)
		events = append(events, domain.NewEvent(
			domain.KindEnglishPractice,
			string(rune('a'+index)),
			occurredAt,
			"练习",
			"说明",
			"",
		))
	}
	domain.SortEvents(events)

	seen := make([]string, 0, len(events))
	cursor := domain.Cursor{}
	for pages := 0; pages < 10; pages++ {
		page := domain.Paginate(events, cursor, 3)
		if len(page.Events) == 0 {
			t.Fatal("paging stopped before reaching the end")
		}
		for _, event := range page.Events {
			seen = append(seen, event.ID)
		}
		if page.NextCursor == "" {
			break
		}
		parsed, err := domain.ParseCursor(page.NextCursor)
		if err != nil {
			t.Fatalf("parse next cursor: %v", err)
		}
		cursor = parsed
	}

	if len(seen) != len(events) {
		t.Fatalf("expected %d events, saw %d: %v", len(events), len(seen), seen)
	}
	unique := map[string]bool{}
	for _, id := range seen {
		if unique[id] {
			t.Fatalf("event %s returned twice across pages", id)
		}
		unique[id] = true
	}
}

func TestPaginateStopsWithNoCursorOnTheLastPage(t *testing.T) {
	events := []domain.Event{
		domain.NewEvent(domain.KindEnglishPractice, "a", time.Unix(10, 0), "练习", "", ""),
	}
	page := domain.Paginate(events, domain.Cursor{}, 30)
	if len(page.Events) != 1 || page.NextCursor != "" {
		t.Fatalf("a complete page must end the chain, got %+v", page)
	}
}

func TestNormaliseLimit(t *testing.T) {
	cases := map[int]int{-5: domain.DefaultLimit, 0: domain.DefaultLimit,
		1: 1, 30: 30, 100: 100, 500: domain.MaxLimit}
	for input, expected := range cases {
		if actual := domain.NormaliseLimit(input); actual != expected {
			t.Fatalf("limit %d: expected %d, got %d", input, expected, actual)
		}
	}
}

func TestBuildMilestonesCountsStreaksByDayNotByRecord(t *testing.T) {
	utc := time.UTC
	day := func(dayOfMarch int) time.Time {
		return time.Date(2026, 3, dayOfMarch, 9, 0, 0, 0, utc)
	}
	events := []domain.Event{
		// Ten records on one day are one practice day and a one-day streak.
		domain.NewEvent(domain.KindEnglishPractice, "a", day(1), "", "", ""),
		domain.NewEvent(domain.KindEnglishPractice, "b", day(1).Add(time.Hour), "", "", ""),
		domain.NewEvent(domain.KindEnglishPractice, "c", day(1).Add(2*time.Hour), "", "", ""),
		domain.NewEvent(domain.KindEnglishPractice, "d", day(3), "", "", ""),
		domain.NewEvent(domain.KindEnglishPractice, "e", day(4), "", "", ""),
		domain.NewEvent(domain.KindAssessmentCompleted, "f", day(4), "", "", ""),
		domain.NewEvent(domain.KindReportPublished, "g", day(5), "", "", ""),
		domain.NewEvent(domain.KindMilestone, "h", day(5), "", "", ""),
		domain.NewEvent(domain.KindContentCompleted, "i", day(6), "", "", ""),
	}
	milestones := domain.BuildMilestones(events, utc)

	if milestones.CompletedAssessments != 1 {
		t.Fatalf("completed assessments: %d", milestones.CompletedAssessments)
	}
	if milestones.PracticeDays != 3 {
		t.Fatalf("practice days: %d", milestones.PracticeDays)
	}
	// The newest run is day 3 + day 4.
	if milestones.CurrentStreakDays != 2 {
		t.Fatalf("current streak: %d", milestones.CurrentStreakDays)
	}
	if milestones.LongestStreakDays != 2 {
		t.Fatalf("longest streak: %d", milestones.LongestStreakDays)
	}
	if milestones.Total != len(events) {
		t.Fatalf("total: %d, expected %d", milestones.Total, len(events))
	}
}

func TestBuildMilestonesIsEmptyForANewFamily(t *testing.T) {
	milestones := domain.BuildMilestones(nil, nil)
	if milestones != (domain.Milestones{}) {
		t.Fatalf("a new family must have zero counters, got %+v", milestones)
	}
}

func TestSortEventsBreaksTimestampTiesByIdentity(t *testing.T) {
	instant := time.Unix(100, 0)
	events := []domain.Event{
		domain.NewEvent(domain.KindEnglishPractice, "b", instant, "", "", ""),
		domain.NewEvent(domain.KindEnglishPractice, "a", instant, "", "", ""),
	}
	domain.SortEvents(events)
	if events[0].ID >= events[1].ID {
		t.Fatalf("tie-break is not stable: %s then %s", events[0].ID, events[1].ID)
	}
}

func TestValidateLimitRejectsRatherThanClamps(t *testing.T) {
	for _, limit := range []int{0, 1, 100} {
		if err := domain.ValidateLimit(limit); err != nil {
			t.Fatalf("limit %d must be accepted: %v", limit, err)
		}
	}
	for _, limit := range []int{-1, 101, 1000} {
		if err := domain.ValidateLimit(limit); !errors.Is(err, domain.ErrInvalidLimit) {
			t.Fatalf("limit %d must be refused, got %v", limit, err)
		}
	}
}

func TestEventSubjectIsTruncatedByRunes(t *testing.T) {
	long := "英语"
	for index := 0; index < domain.MaxSubjectRunes+10; index++ {
		long += "练"
	}
	event := domain.NewEvent(domain.KindEnglishPractice, "a", time.Unix(1, 0), "", "", long)
	if count := len([]rune(event.Subject)); count != domain.MaxSubjectRunes {
		t.Fatalf("subject runes: %d", count)
	}
}

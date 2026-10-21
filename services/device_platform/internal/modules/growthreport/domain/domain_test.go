package domain

import (
	"strings"
	"testing"
	"time"
)

func TestPeriodWindowStartsOnMonday(t *testing.T) {
	// A Thursday must resolve back to the preceding Monday at midnight UTC.
	at := time.Date(2026, 10, 22, 15, 30, 0, 0, time.UTC)
	start, end, err := PeriodWeekly.Window(at)
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if got := start.Format(time.DateOnly); got != "2026-10-19" {
		t.Fatalf("start = %s, want 2026-10-19", got)
	}
	if got := end.Format(time.DateOnly); got != "2026-10-26" {
		t.Fatalf("end = %s, want 2026-10-26", got)
	}
	// A Sunday belongs to the week that started six days earlier, not the next
	// one: users read Sunday as the end of a week.
	sunday := time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)
	start, _, err = PeriodWeekly.Window(sunday)
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if got := start.Format(time.DateOnly); got != "2026-10-19" {
		t.Fatalf("sunday start = %s, want 2026-10-19", got)
	}
}

func TestPeriodWindowMonthlySpansCalendarMonth(t *testing.T) {
	at := time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC)
	start, end, err := PeriodMonthly.Window(at)
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if got := start.Format(time.DateOnly); got != "2026-02-01" {
		t.Fatalf("start = %s", got)
	}
	if got := end.Format(time.DateOnly); got != "2026-03-01" {
		t.Fatalf("end = %s", got)
	}
}

func TestPeriodWindowRejectsUnknownType(t *testing.T) {
	if _, _, err := PeriodType("daily").Window(time.Now()); err == nil {
		t.Fatal("expected an error for an unknown period")
	}
}

func TestPreviousWindowIsAdjacent(t *testing.T) {
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	previousStart, previousEnd, err := PeriodWeekly.PreviousWindow(start, end)
	if err != nil {
		t.Fatalf("previous window: %v", err)
	}
	// The two windows must share a boundary exactly, or a report would count a
	// day twice or skip one.
	if !previousEnd.Equal(start) {
		t.Fatalf("previous end = %s, want %s", previousEnd, start)
	}
	if got := previousStart.Format(time.DateOnly); got != "2026-10-12" {
		t.Fatalf("previous start = %s", got)
	}
}

func TestAccuracyAvoidsDivideByZero(t *testing.T) {
	if got := (PracticeTotals{}).Accuracy(); got != 0 {
		t.Fatalf("accuracy = %v, want 0", got)
	}
	totals := PracticeTotals{Attempts: 4, Passed: 3}
	if got := totals.Accuracy(); got != 75 {
		t.Fatalf("accuracy = %v, want 75", got)
	}
}

func TestBuildSummaryStatesFactsWithoutJudging(t *testing.T) {
	metrics := NewMetrics(
		PracticeTotals{Attempts: 6, Passed: 5, AverageScore: 88.5},
		PracticeTotals{Attempts: 4, Passed: 2, AverageScore: 70},
		UsageTotals{ActiveSeconds: 900},
		UsageTotals{ActiveSeconds: 600},
		CountTotals{GrowthRecords: 2, Memories: 3},
	)
	summary := BuildSummary(PeriodWeekly, metrics)

	for _, want := range []string{
		"这一周",
		"练习了 6 次",
		"平均得分 88.5",
		"比上一周期多练习 2 次",
		"设备共使用 15 分钟",
		"新增 2 条成长记录",
		"记忆了 3 件小事",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
	// A growth report must never grade the child or compare to others.
	for _, forbidden := range []string{"优秀", "落后", "不如", "聪明", "其他孩子"} {
		if strings.Contains(summary, forbidden) {
			t.Fatalf("summary %q contains judgement %q", summary, forbidden)
		}
	}
}

func TestBuildSummaryHandlesEmptyPeriodHonestly(t *testing.T) {
	summary := BuildSummary(PeriodMonthly, NewMetrics(
		PracticeTotals{},
		PracticeTotals{},
		UsageTotals{},
		UsageTotals{},
		CountTotals{},
	))
	if !strings.Contains(summary, "这个月") {
		t.Fatalf("summary %q missing period noun", summary)
	}
	if !strings.Contains(summary, "没有英语练习记录") {
		t.Fatalf("summary %q should say there was no practice", summary)
	}
	// An empty period is normal; the text must not manufacture encouragement.
	if strings.Contains(summary, "继续加油") {
		t.Fatalf("summary %q invents encouragement", summary)
	}
}

func TestBuildSummaryNotesDropToZero(t *testing.T) {
	summary := BuildSummary(PeriodWeekly, NewMetrics(
		PracticeTotals{},
		PracticeTotals{Attempts: 3, Passed: 2},
		UsageTotals{},
		UsageTotals{},
		CountTotals{},
	))
	if !strings.Contains(summary, "上一周期练习了 3 次") {
		t.Fatalf("summary %q should name the previous volume", summary)
	}
}

func TestBuildSummaryFirstPeriodHasNoComparison(t *testing.T) {
	summary := BuildSummary(PeriodWeekly, NewMetrics(
		PracticeTotals{Attempts: 2, Passed: 1, AverageScore: 60},
		PracticeTotals{},
		UsageTotals{},
		UsageTotals{},
		CountTotals{},
	))
	if !strings.Contains(summary, "第一次有练习记录") {
		t.Fatalf("summary %q should mark the first period", summary)
	}
}

func TestBuildSummaryReportsFlatPeriod(t *testing.T) {
	summary := BuildSummary(PeriodWeekly, NewMetrics(
		PracticeTotals{Attempts: 5, Passed: 4, AverageScore: 80},
		PracticeTotals{Attempts: 5, Passed: 2, AverageScore: 70},
		UsageTotals{},
		UsageTotals{},
		CountTotals{},
	))
	if !strings.Contains(summary, "持平") {
		t.Fatalf("summary %q should say the volume held steady", summary)
	}
}

func TestAccuracyPhraseIncludesSampleSize(t *testing.T) {
	phrase := AccuracyPhrase(NewMetrics(
		PracticeTotals{Attempts: 4, Passed: 3},
		PracticeTotals{},
		UsageTotals{},
		UsageTotals{},
		CountTotals{},
	))
	if phrase != "75%（3/4）" {
		t.Fatalf("phrase = %q", phrase)
	}
	if got := AccuracyPhrase(NewMetrics(
		PracticeTotals{},
		PracticeTotals{},
		UsageTotals{},
		UsageTotals{},
		CountTotals{},
	)); got != "没有练习" {
		t.Fatalf("empty phrase = %q", got)
	}
}

func TestRequestValidate(t *testing.T) {
	valid := Request{
		FamilyID: "f",
		ChildID:  "c",
		Period:   PeriodWeekly,
		Now:      time.Now(),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	cases := map[string]Request{
		"missing family": {ChildID: "c", Period: PeriodWeekly, Now: time.Now()},
		"missing child":  {FamilyID: "f", Period: PeriodWeekly, Now: time.Now()},
		"bad period":     {FamilyID: "f", ChildID: "c", Period: "daily", Now: time.Now()},
		"missing now":    {FamilyID: "f", ChildID: "c", Period: PeriodWeekly},
	}
	for name, request := range cases {
		if err := request.Validate(); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

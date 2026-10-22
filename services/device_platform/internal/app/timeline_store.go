package app

import (
	"context"
	"time"

	timelinerepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/timeline/repository"
	timelineservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/timeline/service"
)

// timelineSource adapts the read-only timeline repository to the service seam.
//
// The service must not import the repository, so this adapter is the only place
// that knows both. The mapping is a plain copy of identifiers, instants and the
// few descriptive columns; no column the repository does not select can appear
// here.
type timelineSource struct {
	repository *timelinerepository.Repository
}

func (source timelineSource) ChildInFamily(
	ctx context.Context,
	familyID, childID string,
) (bool, error) {
	return source.repository.ChildInFamily(ctx, familyID, childID)
}

func (source timelineSource) CompletedAssessments(
	ctx context.Context,
	familyID, childID string,
) ([]timelineservice.Assessment, error) {
	rows, err := source.repository.CompletedAssessments(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	assessments := make([]timelineservice.Assessment, 0, len(rows))
	for _, row := range rows {
		assessments = append(assessments, timelineservice.Assessment{
			ID:         row.ID,
			LessonID:   row.LessonID,
			Answered:   row.Answered,
			Mastery:    row.Mastery,
			FinishedAt: row.Finished,
		})
	}
	return assessments, nil
}

func (source timelineSource) EnglishPractices(
	ctx context.Context,
	familyID, childID string,
) ([]timelineservice.Practice, error) {
	rows, err := source.repository.EnglishPractices(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	practices := make([]timelineservice.Practice, 0, len(rows))
	for _, row := range rows {
		practices = append(practices, timelineservice.Practice{
			ID:        row.ID,
			ItemType:  row.ItemType,
			Practiced: row.Practiced,
		})
	}
	return practices, nil
}

func (source timelineSource) PublishedReports(
	ctx context.Context,
	familyID, childID string,
) ([]timelineservice.Report, error) {
	rows, err := source.repository.PublishedReports(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	reports := make([]timelineservice.Report, 0, len(rows))
	for _, row := range rows {
		reports = append(reports, timelineservice.Report{
			ID:          row.ID,
			PeriodType:  row.PeriodType,
			GeneratedAt: row.GeneratedAt,
		})
	}
	return reports, nil
}

func (source timelineSource) CompletedContent(
	ctx context.Context,
	familyID, childID string,
) ([]timelineservice.ContentPlay, error) {
	rows, err := source.repository.CompletedContent(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	plays := make([]timelineservice.ContentPlay, 0, len(rows))
	for _, row := range rows {
		plays = append(plays, timelineservice.ContentPlay{
			ID:        row.ID,
			PackageID: row.PackageID,
			PlayedAt:  row.PlayedAt,
		})
	}
	return plays, nil
}

func (source timelineSource) GrowthRecords(
	ctx context.Context,
	familyID, childID string,
) ([]timelineservice.GrowthRecord, error) {
	rows, err := source.repository.GrowthRecords(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	records := make([]timelineservice.GrowthRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, timelineservice.GrowthRecord{
			ID:         row.ID,
			Category:   row.Category,
			Title:      row.Title,
			RecordedAt: row.RecordedAt,
		})
	}
	return records, nil
}

func (source timelineSource) CountEventsSince(ctx context.Context, since time.Time) (int64, error) {
	return source.repository.CountEventsSince(ctx, since)
}

func (source timelineSource) CountActiveFamilies(
	ctx context.Context,
	since time.Time,
) (int64, error) {
	return source.repository.CountActiveFamilies(ctx, since)
}

// Compile-time proof the adapter satisfies the service contract.
var _ timelineservice.Source = timelineSource{}

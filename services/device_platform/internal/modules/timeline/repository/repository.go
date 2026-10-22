// Package repository reads the growth timeline out of tables other modules own.
//
// Nothing here writes. Every query selects a narrow projection of an existing
// row: identifiers, instants, and the few columns that describe what happened.
// Columns that hold child content (recognised speech, lesson text, report
// sentences, guardian notes) are never selected, so a leak cannot happen in a
// later handler that forgets to strip them.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository reads timeline sources.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository builds a read-only repository over the shared pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ChildInFamily reports whether a child profile belongs to the family.
//
// Every timeline read runs through this first, so a child from another family
// and a child that does not exist answer identically.
func (repository *Repository) ChildInFamily(ctx context.Context, familyID, childID string) (bool, error) {
	var exists bool
	err := repository.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM child_profiles
			WHERE id = $1::uuid AND family_id = $2::uuid
		 )`,
		childID, familyID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check timeline child: %w", err)
	}
	return exists, nil
}

// AssessmentSource is one finished assessment row.
type AssessmentSource struct {
	ID       string
	LessonID string
	Answered int
	Mastery  int
	Finished time.Time
}

// EnglishPracticeSource is one completed practice row.
//
// The recognized text and the per-item score stay in the table. This struct is
// the whole of what the timeline may know about a practice attempt.
type EnglishPracticeSource struct {
	ID        string
	ItemType  string
	Practiced time.Time
}

// ReportSource is one generated growth report.
type ReportSource struct {
	ID          string
	PeriodType  string
	GeneratedAt time.Time
}

// ContentPlaySource is one completed content playback.
type ContentPlaySource struct {
	ID        string
	PackageID string
	PlayedAt  time.Time
}

// GrowthRecordSource is one guardian-written growth record.
type GrowthRecordSource struct {
	ID         string
	Category   string
	Title      string
	RecordedAt time.Time
}

// CompletedAssessments returns the child's finished assessments, newest first.
//
// 'completed' is the only state that counts: an abandoned run records that the
// child stopped, which is not an achievement and must not appear as one.
func (repository *Repository) CompletedAssessments(
	ctx context.Context,
	familyID, childID string,
) ([]AssessmentSource, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT id::text, lesson_id, answered_count,
		        CASE WHEN answered_count > 0
		             THEN (correct_count * 100) / answered_count ELSE 0 END AS mastery,
		        finished_at
		 FROM assessment_sessions
		 WHERE family_id = $1::uuid AND child_id = $2::uuid
		   AND state = 'completed' AND finished_at IS NOT NULL
		 ORDER BY finished_at DESC, id DESC`,
		familyID, childID,
	)
	if err != nil {
		return nil, fmt.Errorf("list timeline assessments: %w", err)
	}
	defer rows.Close()

	sessions := make([]AssessmentSource, 0, 32)
	for rows.Next() {
		var session AssessmentSource
		if scanErr := rows.Scan(
			&session.ID, &session.LessonID, &session.Answered, &session.Mastery,
			&session.Finished,
		); scanErr != nil {
			return nil, fmt.Errorf("scan timeline assessment: %w", scanErr)
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// EnglishPractices returns the child's passed practice attempts, newest first.
//
// Only a passed attempt is returned. A failed attempt is real practice but the
// timeline is a story of what the child did, and listing every miss would turn
// a keepsake into a report card.
func (repository *Repository) EnglishPractices(
	ctx context.Context,
	familyID, childID string,
) ([]EnglishPracticeSource, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT id::text, item_type, practiced_at
		 FROM english_practice_attempts
		 WHERE family_id = $1::uuid AND child_id = $2::uuid AND passed
		 ORDER BY practiced_at DESC, id DESC`,
		familyID, childID,
	)
	if err != nil {
		return nil, fmt.Errorf("list timeline english practice: %w", err)
	}
	defer rows.Close()

	attempts := make([]EnglishPracticeSource, 0, 64)
	for rows.Next() {
		var attempt EnglishPracticeSource
		if scanErr := rows.Scan(&attempt.ID, &attempt.ItemType, &attempt.Practiced); scanErr != nil {
			return nil, fmt.Errorf("scan timeline english practice: %w", scanErr)
		}
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

// PublishedReports returns the reports generated for the child, newest first.
//
// The summary sentence is not selected: it is derived text about the child, and
// the timeline introduces each report rather than repeating it.
func (repository *Repository) PublishedReports(
	ctx context.Context,
	familyID, childID string,
) ([]ReportSource, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT id::text, period_type, generated_at
		 FROM growth_reports
		 WHERE family_id = $1::uuid AND child_id = $2::uuid
		 ORDER BY generated_at DESC, id DESC`,
		familyID, childID,
	)
	if err != nil {
		return nil, fmt.Errorf("list timeline reports: %w", err)
	}
	defer rows.Close()

	reports := make([]ReportSource, 0, 16)
	for rows.Next() {
		var report ReportSource
		if scanErr := rows.Scan(&report.ID, &report.PeriodType, &report.GeneratedAt); scanErr != nil {
			return nil, fmt.Errorf("scan timeline report: %w", scanErr)
		}
		reports = append(reports, report)
	}
	return reports, rows.Err()
}

// CompletedContent returns content the child's devices played to the end.
//
// Playback history is device-scoped, not child-scoped, so a row is attributed to
// the child only when the device was serving them at the moment of playback.
// The join on the active binding interval is what keeps the entry honest after a
// multi-child household switches the device to a sibling: a play that happened
// before the switch belongs to whoever the device served then.
//
// The package title is not selected. It is content metadata, and the timeline
// already names the fact ("played a story to the end") without re-publishing
// the catalog.
func (repository *Repository) CompletedContent(
	ctx context.Context,
	familyID, childID string,
) ([]ContentPlaySource, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT h.id::text, h.package_id, h.played_at
		 FROM content_play_history h
		 JOIN device_child_bindings b
		   ON b.device_id = h.device_id
		  AND b.family_id = h.family_id
		  AND b.child_id = $2::uuid
		  AND b.activated_at <= h.played_at
		  AND (b.deactivated_at IS NULL OR b.deactivated_at > h.played_at)
		 WHERE h.family_id = $1::uuid AND h.outcome = 'completed'
		 ORDER BY h.played_at DESC, h.id DESC`,
		familyID, childID,
	)
	if err != nil {
		return nil, fmt.Errorf("list timeline content plays: %w", err)
	}
	defer rows.Close()

	plays := make([]ContentPlaySource, 0, 32)
	for rows.Next() {
		var play ContentPlaySource
		if scanErr := rows.Scan(&play.ID, &play.PackageID, &play.PlayedAt); scanErr != nil {
			return nil, fmt.Errorf("scan timeline content play: %w", scanErr)
		}
		plays = append(plays, play)
	}
	return plays, rows.Err()
}

// GrowthRecords returns the guardian-written records for the child, newest first.
func (repository *Repository) GrowthRecords(
	ctx context.Context,
	familyID, childID string,
) ([]GrowthRecordSource, error) {
	rows, err := repository.pool.Query(ctx,
		`SELECT id::text, category, title, recorded_at
		 FROM companionship_growth_records
		 WHERE family_id = $1::uuid AND child_id = $2::uuid
		 ORDER BY recorded_at DESC, id DESC`,
		familyID, childID,
	)
	if err != nil {
		return nil, fmt.Errorf("list timeline growth records: %w", err)
	}
	defer rows.Close()

	records := make([]GrowthRecordSource, 0, 32)
	for rows.Next() {
		var record GrowthRecordSource
		if scanErr := rows.Scan(
			&record.ID, &record.Category, &record.Title, &record.RecordedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("scan timeline growth record: %w", scanErr)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// CountEventsSince counts timeline-eligible rows created across all families
// since an instant, summed over the source tables.
func (repository *Repository) CountEventsSince(ctx context.Context, since time.Time) (int64, error) {
	var count int64
	err := repository.pool.QueryRow(ctx,
		`SELECT
			(SELECT COUNT(*) FROM assessment_sessions
			  WHERE state = 'completed' AND finished_at >= $1)
		  + (SELECT COUNT(*) FROM english_practice_attempts
			  WHERE passed AND practiced_at >= $1)
		  + (SELECT COUNT(*) FROM growth_reports WHERE generated_at >= $1)
		  + (SELECT COUNT(*) FROM companionship_growth_records
			  WHERE category = 'milestone' AND recorded_at >= $1)
		  + (SELECT COUNT(*) FROM content_play_history
			  WHERE outcome = 'completed' AND played_at >= $1)`,
		since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count timeline events: %w", err)
	}
	return count, nil
}

// CountActiveFamilies counts families with at least one event in the last day.
func (repository *Repository) CountActiveFamilies(ctx context.Context, since time.Time) (int64, error) {
	var count int64
	err := repository.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM (
			SELECT family_id FROM assessment_sessions
			  WHERE state = 'completed' AND finished_at >= $1
			UNION
			SELECT family_id FROM english_practice_attempts
			  WHERE passed AND practiced_at >= $1
			UNION
			SELECT family_id FROM growth_reports WHERE generated_at >= $1
			UNION
			SELECT family_id FROM companionship_growth_records
			  WHERE category = 'milestone' AND recorded_at >= $1
			UNION
			SELECT family_id FROM content_play_history
			  WHERE outcome = 'completed' AND played_at >= $1
		 ) active`,
		since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count timeline active families: %w", err)
	}
	return count, nil
}

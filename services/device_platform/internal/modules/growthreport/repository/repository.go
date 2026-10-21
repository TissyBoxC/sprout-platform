// Package repository reads the aggregates a growth report is built from.
//
// Every query here is a COUNT, SUM or AVG over an existing table. The package
// deliberately has no function that returns a conversation, a memory value or
// any other free text: a report must not be able to carry child content even by
// accident, so the capability is absent rather than merely unused.
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/growthreport/domain"
)

// Repository builds and stores growth reports.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a repository backed by the given pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// PracticeTotals sums English attempts for one child inside a window.
//
// The child column is nullable on legacy rows, so an attempt with no child is
// excluded rather than attributed to whoever the family happens to contain.
func (r *Repository) PracticeTotals(
	ctx context.Context,
	familyID string,
	childID string,
	start time.Time,
	end time.Time,
) (domain.PracticeTotals, error) {
	const query = `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE passed)::int,
			COALESCE(AVG(score), 0)::float8
		FROM english_practice_attempts
		WHERE family_id = $1
			AND child_id = $2
			AND practiced_at >= $3
			AND practiced_at < $4`
	var totals domain.PracticeTotals
	err := r.pool.QueryRow(
		ctx,
		query,
		familyID,
		childID,
		start,
		end,
	).Scan(&totals.Attempts, &totals.Passed, &totals.AverageScore)
	if err != nil {
		return domain.PracticeTotals{}, fmt.Errorf(
			"sum practice totals: %w",
			err,
		)
	}
	totals.AverageScore = roundOneDecimal(totals.AverageScore)
	return totals, nil
}

// UsageTotals sums device activity for the family inside a window.
//
// Daily usage carries no child column, so the figure is household activity.
// Attributing it to one child would be a fabrication in a family with two
// devices, which is why the report calls it device usage rather than one
// child's screen time.
func (r *Repository) UsageTotals(
	ctx context.Context,
	familyID string,
	start time.Time,
	end time.Time,
) (domain.UsageTotals, error) {
	const query = `
		SELECT
			COALESCE(SUM(active_seconds), 0)::int,
			COALESCE(SUM(conversation_count), 0)::int,
			COALESCE(SUM(conversation_seconds), 0)::int
		FROM device_usage_daily
		WHERE parent_account_id = $1
			AND report_date >= $2::date
			AND report_date < $3::date`
	var totals domain.UsageTotals
	err := r.pool.QueryRow(
		ctx,
		query,
		familyID,
		start,
		end,
	).Scan(
		&totals.ActiveSeconds,
		&totals.ConversationCount,
		&totals.ConversationSeconds,
	)
	if err != nil {
		return domain.UsageTotals{}, fmt.Errorf("sum usage totals: %w", err)
	}
	return totals, nil
}

// CountTotals counts guardian entries created inside a window.
func (r *Repository) CountTotals(
	ctx context.Context,
	familyID string,
	childID string,
	start time.Time,
	end time.Time,
) (domain.CountTotals, error) {
	const query = `
		SELECT
			(SELECT COUNT(*)::int
			 FROM companionship_growth_records
			 WHERE family_id = $1
				AND child_id = $2
				AND recorded_at >= $3
				AND recorded_at < $4),
			(SELECT COUNT(*)::int
			 FROM child_memories
			 WHERE family_id = $1
				AND child_id = $2
				AND created_at >= $3
				AND created_at < $4)`
	var totals domain.CountTotals
	err := r.pool.QueryRow(
		ctx,
		query,
		familyID,
		childID,
		start,
		end,
	).Scan(&totals.GrowthRecords, &totals.Memories)
	if err != nil {
		return domain.CountTotals{}, fmt.Errorf("count entries: %w", err)
	}
	return totals, nil
}

// Save stores a report, replacing the metrics and summary when the generator
// runs twice for the same period.
//
// The conflict update is what makes a retried job harmless: the window is
// derived from the run instant, so two runs of the same period must land on one
// row instead of showing the same week twice.
func (r *Repository) Save(ctx context.Context, report domain.Report) error {
	metrics, err := json.Marshal(report.Metrics)
	if err != nil {
		return fmt.Errorf("encode metrics: %w", err)
	}
	const query = `
		INSERT INTO growth_reports (
			id, family_id, child_id, period_type, period_start, period_end,
			metrics, summary, generated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (child_id, period_type, period_start) DO UPDATE SET
			metrics = EXCLUDED.metrics,
			summary = EXCLUDED.summary,
			generated_at = EXCLUDED.generated_at`
	_, err = r.pool.Exec(
		ctx,
		query,
		report.ID,
		report.FamilyID,
		report.ChildID,
		string(report.PeriodType),
		report.PeriodStart,
		report.PeriodEnd,
		metrics,
		report.Summary,
		report.GeneratedAt,
	)
	if err != nil {
		return fmt.Errorf("save growth report: %w", err)
	}
	return nil
}

// ListForChild returns a child's reports newest first.
func (r *Repository) ListForChild(
	ctx context.Context,
	familyID string,
	childID string,
	period domain.PeriodType,
	limit int,
) ([]domain.Report, error) {
	const query = `
		SELECT id, family_id, child_id, period_type, period_start, period_end,
			metrics, summary, generated_at
		FROM growth_reports
		WHERE family_id = $1
			AND child_id = $2
			AND ($3 = '' OR period_type = $3)
		ORDER BY period_start DESC
		LIMIT $4`
	return r.queryReports(ctx, query, familyID, childID, string(period), limit)
}

// ListForFamily returns a family's reports newest first, for the console.
func (r *Repository) ListForFamily(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.Report, error) {
	const query = `
		SELECT id, family_id, child_id, period_type, period_start, period_end,
			metrics, summary, generated_at
		FROM growth_reports
		WHERE family_id = $1
		ORDER BY generated_at DESC
		LIMIT $2`
	return r.queryReports(ctx, query, familyID, limit)
}

// GetForGuardian returns one report, scoped to the family so a guessed id
// cannot read another family's report.
func (r *Repository) GetForGuardian(
	ctx context.Context,
	familyID string,
	childID string,
	reportID string,
) (domain.Report, error) {
	const query = `
		SELECT id, family_id, child_id, period_type, period_start, period_end,
			metrics, summary, generated_at
		FROM growth_reports
		WHERE family_id = $1 AND child_id = $2 AND id = $3`
	reports, err := r.queryReports(
		ctx,
		query,
		familyID,
		childID,
		reportID,
	)
	if err != nil {
		return domain.Report{}, err
	}
	if len(reports) == 0 {
		return domain.Report{}, pgx.ErrNoRows
	}
	return reports[0], nil
}

// queryReports runs a report query and decodes the shared columns.
func (r *Repository) queryReports(
	ctx context.Context,
	query string,
	args ...any,
) ([]domain.Report, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query growth reports: %w", err)
	}
	defer rows.Close()

	reports := make([]domain.Report, 0, 8)
	for rows.Next() {
		var report domain.Report
		var periodType string
		var metrics []byte
		if err := rows.Scan(
			&report.ID,
			&report.FamilyID,
			&report.ChildID,
			&periodType,
			&report.PeriodStart,
			&report.PeriodEnd,
			&metrics,
			&report.Summary,
			&report.GeneratedAt,
		); err != nil {
			return nil, fmt.Errorf("scan growth report: %w", err)
		}
		report.PeriodType = domain.PeriodType(periodType)
		if err := json.Unmarshal(metrics, &report.Metrics); err != nil {
			return nil, fmt.Errorf("decode report metrics: %w", err)
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate growth reports: %w", err)
	}
	return reports, nil
}

// CountForAdmin reports how many reports exist and how many children have one.
func (r *Repository) CountForAdmin(
	ctx context.Context,
) (int, int, error) {
	const query = `
		SELECT COUNT(*)::int, COUNT(DISTINCT child_id)::int
		FROM growth_reports`
	var reportCount int
	var childCount int
	if err := r.pool.QueryRow(ctx, query).Scan(&reportCount, &childCount); err != nil {
		return 0, 0, fmt.Errorf("count growth reports: %w", err)
	}
	return reportCount, childCount, nil
}

// LastGeneratedAt returns the newest generation time, or the zero time when no
// report exists yet.
//
// The health probe uses it to tell a healthy generator from one that stopped
// producing reports.
func (r *Repository) LastGeneratedAt(ctx context.Context) (time.Time, error) {
	const query = `SELECT COALESCE(MAX(generated_at), 'epoch'::timestamptz) FROM growth_reports`
	var generatedAt time.Time
	if err := r.pool.QueryRow(ctx, query).Scan(&generatedAt); err != nil {
		return time.Time{}, fmt.Errorf("read last generation time: %w", err)
	}
	return generatedAt, nil
}

// ChildrenForFamily lists the children a generator run should cover.
func (r *Repository) ChildrenForFamily(
	ctx context.Context,
	familyID string,
) ([]string, error) {
	const query = `
		SELECT id::text
		FROM child_profiles
		WHERE family_id = $1
		ORDER BY created_at`
	rows, err := r.pool.Query(ctx, query, familyID)
	if err != nil {
		return nil, fmt.Errorf("list family children: %w", err)
	}
	defer rows.Close()

	children := make([]string, 0, 4)
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			return nil, fmt.Errorf("scan child id: %w", err)
		}
		children = append(children, childID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate family children: %w", err)
	}
	return children, nil
}

// FamiliesWithChildren lists every family that has at least one child.
func (r *Repository) FamiliesWithChildren(
	ctx context.Context,
) (map[string][]string, error) {
	const query = `
		SELECT family_id::text, id::text
		FROM child_profiles
		ORDER BY family_id, created_at`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list families: %w", err)
	}
	defer rows.Close()

	families := make(map[string][]string)
	for rows.Next() {
		var familyID string
		var childID string
		if err := rows.Scan(&familyID, &childID); err != nil {
			return nil, fmt.Errorf("scan family child: %w", err)
		}
		families[familyID] = append(families[familyID], childID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate families: %w", err)
	}
	return families, nil
}

// roundOneDecimal keeps a mean score readable in a sentence.
func roundOneDecimal(value float64) float64 {
	scaled := value * 10
	if scaled >= 0 {
		return float64(int(scaled+0.5)) / 10
	}
	return float64(int(scaled-0.5)) / 10
}

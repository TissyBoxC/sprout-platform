package repository

import (
	"context"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
)

// Readiness returns aggregate catalogue and practice counters without loading
// lesson titles, vocabulary, recognized text or child identifiers.
func (r *PostgresRepository) Readiness(ctx context.Context) (domain.Readiness, error) {
	if r == nil || r.pool == nil {
		return domain.Readiness{}, fmt.Errorf("english learning database is unavailable")
	}
	readiness := domain.Readiness{
		CatalogByAgeTier: make(map[string]bool, 3),
	}
	for _, ageTier := range domain.AgeTiers {
		readiness.CatalogByAgeTier[ageTier] = false
	}
	rows, err := r.pool.Query(ctx, `
		SELECT age_tier, COUNT(*)
		FROM english_lessons
		WHERE status = 'published'
		GROUP BY age_tier
	`)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("read english lesson readiness: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ageTier string
		var count int64
		if err := rows.Scan(&ageTier, &count); err != nil {
			return domain.Readiness{}, fmt.Errorf("scan english lesson readiness: %w", err)
		}
		readiness.PublishedLessonCount += count
		if _, approved := readiness.CatalogByAgeTier[ageTier]; approved {
			readiness.CatalogByAgeTier[ageTier] = count > 0
		}
	}
	if err := rows.Err(); err != nil {
		return domain.Readiness{}, fmt.Errorf("iterate english lesson readiness: %w", err)
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM english_practice_attempts
	`).Scan(&readiness.PracticeAttemptCount); err != nil {
		return domain.Readiness{}, fmt.Errorf("read english practice readiness: %w", err)
	}
	return readiness, nil
}

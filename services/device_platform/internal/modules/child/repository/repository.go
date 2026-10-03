// Package repository persists child profiles.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for child profiles.
type Repository interface {
	Create(ctx context.Context, child *domain.Child) error
	Update(ctx context.Context, child *domain.Child) error
	GetByID(ctx context.Context, childID string) (*domain.Child, error)
	ListByFamilyID(ctx context.Context, familyID string) ([]domain.Child, error)
	Delete(ctx context.Context, familyID string, childID string) error
}

// PostgresRepository is the PostgreSQL-backed child profile repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a child profile repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create inserts one child profile. Family ownership is enforced by the
// foreign key to parent_accounts so a child cannot outlive its guardian.
func (r *PostgresRepository) Create(ctx context.Context, child *domain.Child) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO child_profiles (
			id,
			family_id,
			nickname,
			age_tier,
			interests,
			content_categories,
			guardian_consent_version,
			guardian_consented_at,
			source,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		child.ID,
		child.FamilyID,
		child.Nickname,
		child.AgeTier,
		child.Interests,
		child.ContentCategories,
		child.GuardianConsentVersion,
		child.GuardianConsentedAt,
		child.Source,
		child.CreatedAt,
		child.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert child profile: %w", err)
	}
	return nil
}

// Update replaces the editable fields of one child profile.
func (r *PostgresRepository) Update(ctx context.Context, child *domain.Child) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE child_profiles
		SET nickname = $3,
		    age_tier = $4,
		    interests = $5,
		    content_categories = $6,
		    updated_at = $7
		WHERE family_id = $1
		  AND id = $2
	`,
		child.FamilyID,
		child.ID,
		child.Nickname,
		child.AgeTier,
		child.Interests,
		child.ContentCategories,
		child.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update child profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrChildNotFound
	}
	return nil
}

// GetByID loads one child profile without exposing family-scoped list data.
func (r *PostgresRepository) GetByID(
	ctx context.Context,
	childID string,
) (*domain.Child, error) {
	row := r.pool.QueryRow(ctx, childProfileSelect+`
		WHERE id = $1
	`, childID)
	child, err := scanChild(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrChildNotFound
	}
	if err != nil {
		return nil, err
	}
	return child, nil
}

// ListByFamilyID returns a guardian's children in creation order.
func (r *PostgresRepository) ListByFamilyID(
	ctx context.Context,
	familyID string,
) ([]domain.Child, error) {
	rows, err := r.pool.Query(ctx, childProfileSelect+`
		WHERE family_id = $1
		ORDER BY created_at ASC
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list child profiles: %w", err)
	}
	defer rows.Close()

	children := make([]domain.Child, 0)
	for rows.Next() {
		child, err := scanChild(rows)
		if err != nil {
			return nil, err
		}
		children = append(children, *child)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate child profiles: %w", err)
	}
	return children, nil
}

// Delete removes one child profile and its dependent parent policy.
func (r *PostgresRepository) Delete(
	ctx context.Context,
	familyID string,
	childID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM child_profiles
		WHERE family_id = $1
		  AND id = $2
	`, familyID, childID)
	if err != nil {
		return fmt.Errorf("delete child profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrChildNotFound
	}
	return nil
}

const childProfileSelect = `
	SELECT
		id,
		family_id,
		nickname,
		age_tier,
		interests,
		content_categories,
		guardian_consent_version,
		guardian_consented_at,
		source,
		created_at,
		updated_at
	FROM child_profiles
`

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanChild(row rowScanner) (*domain.Child, error) {
	var child domain.Child
	var guardianConsentedAt time.Time
	if err := row.Scan(
		&child.ID,
		&child.FamilyID,
		&child.Nickname,
		&child.AgeTier,
		&child.Interests,
		&child.ContentCategories,
		&child.GuardianConsentVersion,
		&guardianConsentedAt,
		&child.Source,
		&child.CreatedAt,
		&child.UpdatedAt,
	); err != nil {
		return nil, err
	}
	child.GuardianConsentedAt = guardianConsentedAt
	return &child, nil
}

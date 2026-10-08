// Package repository persists feature-center configuration overlays.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines the persistence contract used by the feature service.
type Repository interface {
	Get(ctx context.Context, featureID string) (*domain.FeatureConfig, error)
	List(ctx context.Context) ([]domain.FeatureConfig, error)
	Save(
		ctx context.Context,
		featureID string,
		values map[string]any,
		actorID string,
		expectedVersion int64,
	) (*domain.FeatureConfig, error)
}

// PostgresRepository stores feature configuration overlays in PostgreSQL.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a PostgreSQL feature configuration repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Get returns one persisted configuration overlay.
func (r *PostgresRepository) Get(
	ctx context.Context,
	featureID string,
) (*domain.FeatureConfig, error) {
	config, err := scanConfig(r.pool.QueryRow(ctx, `
		SELECT
			feature_id,
			"values",
			version,
			COALESCE(updated_by::text, ''),
			updated_at
		FROM platform_feature_configs
		WHERE feature_id = $1
	`, featureID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrFeatureConfigNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load feature configuration: %w", err)
	}
	return config, nil
}

// List returns every persisted configuration overlay ordered by feature id.
func (r *PostgresRepository) List(
	ctx context.Context,
) ([]domain.FeatureConfig, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			feature_id,
			"values",
			version,
			COALESCE(updated_by::text, ''),
			updated_at
		FROM platform_feature_configs
		ORDER BY feature_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list feature configurations: %w", err)
	}
	defer rows.Close()

	configs := make([]domain.FeatureConfig, 0)
	for rows.Next() {
		config, err := scanConfig(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, *config)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feature configurations: %w", err)
	}
	return configs, nil
}

// Save writes one configuration overlay using optimistic concurrency.
//
// An expected version of zero inserts the first row. Any existing row with a
// different version is rejected with VersionConflictError.
func (r *PostgresRepository) Save(
	ctx context.Context,
	featureID string,
	values map[string]any,
	actorID string,
	expectedVersion int64,
) (*domain.FeatureConfig, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode feature configuration: %w", err)
	}
	config, err := scanConfig(r.pool.QueryRow(ctx, `
		INSERT INTO platform_feature_configs (
			feature_id,
			"values",
			version,
			updated_by,
			updated_at
		)
		SELECT
			$1,
			$2::jsonb,
			1,
			NULLIF($3, '')::uuid,
			NOW()
		WHERE $4 = 0
		ON CONFLICT (feature_id) DO UPDATE
		SET "values" = EXCLUDED."values",
		    version = platform_feature_configs.version + 1,
		    updated_by = EXCLUDED.updated_by,
		    updated_at = NOW()
		WHERE platform_feature_configs.version = $4
		RETURNING
			feature_id,
			"values",
			version,
			COALESCE(updated_by::text, ''),
			updated_at
	`, featureID, encoded, actorID, expectedVersion))
	if err == nil {
		return config, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("save feature configuration: %w", err)
	}

	currentVersion := int64(0)
	current, currentErr := r.Get(ctx, featureID)
	switch {
	case currentErr == nil:
		currentVersion = current.Version
	case errors.Is(currentErr, domain.ErrFeatureConfigNotFound):
	default:
		return nil, currentErr
	}
	return nil, &domain.VersionConflictError{
		ExpectedVersion: expectedVersion,
		CurrentVersion:  currentVersion,
	}
}

type configScanner interface {
	Scan(destinations ...any) error
}

func scanConfig(row configScanner) (*domain.FeatureConfig, error) {
	var config domain.FeatureConfig
	var raw []byte
	if err := row.Scan(
		&config.FeatureID,
		&raw,
		&config.Version,
		&config.UpdatedBy,
		&config.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &config.Values); err != nil {
		return nil, fmt.Errorf("decode feature configuration: %w", err)
	}
	if config.Values == nil {
		config.Values = map[string]any{}
	}
	return &config, nil
}

var _ Repository = (*PostgresRepository)(nil)

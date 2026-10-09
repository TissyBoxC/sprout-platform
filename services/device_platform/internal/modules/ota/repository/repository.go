// Package repository persists OTA releases, groups, deployments, and events.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines the complete OTA persistence contract.
//
// Implementations must make state transitions atomic, enforce optimistic
// concurrency with record_version, and map database conflicts to the stable
// domain errors. Callers never see pgx or PostgreSQL errors.
type Repository interface {
	CreateRelease(ctx context.Context, release *domain.Release) error
	UpdateReleaseDraft(
		ctx context.Context,
		release *domain.Release,
		expectedVersion int64,
	) error
	GetRelease(ctx context.Context, releaseID string) (*domain.Release, error)
	ListReleases(
		ctx context.Context,
		filter domain.ReleaseFilter,
	) (*domain.ReleasePage, error)
	TransitionRelease(
		ctx context.Context,
		releaseID string,
		expectedVersion int64,
		nextStatus domain.ReleaseStatus,
		actorID string,
		now time.Time,
	) (*domain.Release, error)
	RollbackRelease(
		ctx context.Context,
		releaseID string,
		expectedVersion int64,
		actorID string,
		now time.Time,
	) (*domain.Release, error)

	CreateGroup(ctx context.Context, group *domain.Group) error
	UpdateGroup(
		ctx context.Context,
		group *domain.Group,
		expectedVersion int64,
	) error
	GetGroup(ctx context.Context, groupID string) (*domain.Group, error)
	ListGroups(ctx context.Context) ([]domain.Group, error)
	AddGroupMember(ctx context.Context, groupID string, deviceID string, actorID string) error
	RemoveGroupMember(ctx context.Context, groupID string, deviceID string, actorID string) error
	ListGroupMembers(ctx context.Context, groupID string) ([]string, error)

	GetDeviceContext(ctx context.Context, deviceID string) (*domain.DeviceContext, error)
	ListPublishedReleases(
		ctx context.Context,
		channel domain.Channel,
		hardwareRevision string,
	) ([]domain.Release, error)

	CreateDeployment(ctx context.Context, deployment *domain.Deployment) (*domain.Deployment, error)
	GetDeployment(ctx context.Context, deploymentID string) (*domain.Deployment, error)
	GetDeploymentByRequestID(ctx context.Context, requestID string) (*domain.Deployment, error)
	ListDeployments(
		ctx context.Context,
		filter domain.DeploymentFilter,
	) (*domain.DeploymentPage, error)
	RetryDeployment(
		ctx context.Context,
		deploymentID string,
		actorID string,
		expectedVersion int64,
		now time.Time,
	) (*domain.Deployment, error)
	RecordEvent(
		ctx context.Context,
		event *domain.Event,
	) (*domain.Deployment, bool, error)
	Statistics(
		ctx context.Context,
		filter domain.DeploymentFilter,
	) (*domain.Statistics, error)
}

// PostgresRepository is the PostgreSQL-backed OTA repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates an OTA repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// CreateRelease inserts a draft release.
func (r *PostgresRepository) CreateRelease(
	ctx context.Context,
	release *domain.Release,
) error {
	if release == nil {
		return domain.ErrInvalidRelease
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO ota_releases (
			id,
			version,
			channel,
			hardware_revision,
			min_source_version,
			artifact_url,
			artifact_key,
			sha256,
			size_bytes,
			signature_key_id,
			signature_algorithm,
			rollback_allowed,
			mandatory,
			release_notes,
			status,
			target_scope,
			target_group_id,
			target_device_id,
			canary_percent,
			rollback_release_id,
			record_version,
			created_by,
			updated_by,
			created_at,
			updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
			$21, $22, $23, $24, $25
		)
		ON CONFLICT DO NOTHING
	`,
		release.ID,
		release.Version,
		release.Channel,
		release.HardwareRevision,
		release.MinSourceVersion,
		release.ArtifactURL,
		release.ArtifactKey,
		release.SHA256,
		release.SizeBytes,
		release.SignatureKeyID,
		release.SignatureAlgorithm,
		release.RollbackAllowed,
		release.Mandatory,
		release.ReleaseNotes,
		release.Status,
		release.Target.Scope,
		nullableUUID(release.Target.GroupID),
		nullableText(release.Target.DeviceID),
		release.Target.CanaryPercent,
		nullableUUID(release.RollbackReleaseID),
		release.RecordVersion,
		release.CreatedBy,
		release.UpdatedBy,
		release.CreatedAt,
		release.UpdatedAt,
	)
	if err != nil {
		return mapReleaseWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrReleaseAlreadyExists
	}
	return nil
}

// UpdateReleaseDraft replaces a draft manifest with optimistic concurrency.
func (r *PostgresRepository) UpdateReleaseDraft(
	ctx context.Context,
	release *domain.Release,
	expectedVersion int64,
) error {
	if release == nil {
		return domain.ErrInvalidRelease
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE ota_releases
		SET version = $2,
		    channel = $3,
		    hardware_revision = $4,
		    min_source_version = $5,
		    artifact_url = $6,
		    artifact_key = $7,
		    sha256 = $8,
		    size_bytes = $9,
		    signature_key_id = $10,
		    signature_algorithm = $11,
		    rollback_allowed = $12,
		    mandatory = $13,
		    release_notes = $14,
		    target_scope = $15,
		    target_group_id = $16,
		    target_device_id = $17,
		    canary_percent = $18,
		    rollback_release_id = $19,
		    record_version = ota_releases.record_version + 1,
		    updated_by = $20,
		    updated_at = $21
		WHERE id = $1
		  AND status = 'draft'
		  AND record_version = $22
	`,
		release.ID,
		release.Version,
		release.Channel,
		release.HardwareRevision,
		release.MinSourceVersion,
		release.ArtifactURL,
		release.ArtifactKey,
		release.SHA256,
		release.SizeBytes,
		release.SignatureKeyID,
		release.SignatureAlgorithm,
		release.RollbackAllowed,
		release.Mandatory,
		release.ReleaseNotes,
		release.Target.Scope,
		nullableUUID(release.Target.GroupID),
		nullableText(release.Target.DeviceID),
		release.Target.CanaryPercent,
		nullableUUID(release.RollbackReleaseID),
		release.UpdatedBy,
		release.UpdatedAt,
		expectedVersion,
	)
	if err != nil {
		return mapReleaseWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		current, loadErr := r.GetRelease(ctx, release.ID)
		if errors.Is(loadErr, domain.ErrReleaseNotFound) {
			return domain.ErrReleaseNotFound
		}
		if loadErr != nil {
			return loadErr
		}
		if current.Status != domain.ReleaseStatusDraft {
			return domain.ErrReleaseStateConflict
		}
		return &domain.ReleaseVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  current.RecordVersion,
		}
	}
	return nil
}

// GetRelease loads one release by id.
func (r *PostgresRepository) GetRelease(
	ctx context.Context,
	releaseID string,
) (*domain.Release, error) {
	row := r.pool.QueryRow(ctx, releaseSelect+` WHERE release.id = $1`, releaseID)
	release, err := scanRelease(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrReleaseNotFound
	}
	if err != nil {
		return nil, err
	}
	return release, nil
}

// ListReleases returns a bounded, filtered release page.
func (r *PostgresRepository) ListReleases(
	ctx context.Context,
	filter domain.ReleaseFilter,
) (*domain.ReleasePage, error) {
	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	where, arguments := releaseFilterSQL(filter)
	var total int64
	if err := r.pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM ota_releases release "+where,
		arguments...,
	).Scan(&total); err != nil {
		return nil, fmt.Errorf("count OTA releases: %w", err)
	}

	listArguments := append(append([]any(nil), arguments...), pageSize, (page-1)*pageSize)
	rows, err := r.pool.Query(ctx, releaseSelect+where+fmt.Sprintf(
		" ORDER BY release.created_at DESC LIMIT $%d OFFSET $%d",
		len(listArguments)-1,
		len(listArguments),
	), listArguments...)
	if err != nil {
		return nil, fmt.Errorf("list OTA releases: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Release, 0, pageSize)
	for rows.Next() {
		release, scanErr := scanRelease(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan OTA release: %w", scanErr)
		}
		items = append(items, *release)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate OTA releases: %w", err)
	}
	return &domain.ReleasePage{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// TransitionRelease atomically applies a permitted status transition.
func (r *PostgresRepository) TransitionRelease(
	ctx context.Context,
	releaseID string,
	expectedVersion int64,
	nextStatus domain.ReleaseStatus,
	actorID string,
	now time.Time,
) (*domain.Release, error) {
	var release *domain.Release
	err := withTransaction(ctx, r.pool, func(transaction pgx.Tx) error {
		current, err := scanRelease(transaction.QueryRow(
			ctx,
			releaseSelect+` WHERE release.id = $1 FOR UPDATE`,
			releaseID,
		))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrReleaseNotFound
		}
		if err != nil {
			return err
		}
		if current.RecordVersion != expectedVersion {
			return &domain.ReleaseVersionConflictError{
				ExpectedVersion: expectedVersion,
				CurrentVersion:  current.RecordVersion,
			}
		}
		if !domain.CanTransitionRelease(current.Status, nextStatus) {
			return domain.ErrReleaseStateConflict
		}
		updated, err := updateReleaseStatus(
			ctx,
			transaction,
			current,
			nextStatus,
			actorID,
			now,
			false,
		)
		if err != nil {
			return err
		}
		release = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return release, nil
}

// RollbackRelease records a rollback and withdraws the release without
// rewriting the verified manifest.
func (r *PostgresRepository) RollbackRelease(
	ctx context.Context,
	releaseID string,
	expectedVersion int64,
	actorID string,
	now time.Time,
) (*domain.Release, error) {
	var release *domain.Release
	err := withTransaction(ctx, r.pool, func(transaction pgx.Tx) error {
		current, err := scanRelease(transaction.QueryRow(
			ctx,
			releaseSelect+` WHERE release.id = $1 FOR UPDATE`,
			releaseID,
		))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrReleaseNotFound
		}
		if err != nil {
			return err
		}
		if current.RecordVersion != expectedVersion {
			return &domain.ReleaseVersionConflictError{
				ExpectedVersion: expectedVersion,
				CurrentVersion:  current.RecordVersion,
			}
		}
		if !current.RollbackAllowed {
			return domain.ErrRollbackNotAllowed
		}
		if current.Status != domain.ReleaseStatusPublished &&
			current.Status != domain.ReleaseStatusPaused {
			return domain.ErrReleaseStateConflict
		}
		updated, err := updateReleaseStatus(
			ctx,
			transaction,
			current,
			domain.ReleaseStatusWithdrawn,
			actorID,
			now,
			true,
		)
		if err != nil {
			return err
		}
		release = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return release, nil
}

func updateReleaseStatus(
	ctx context.Context,
	transaction pgx.Tx,
	current *domain.Release,
	nextStatus domain.ReleaseStatus,
	actorID string,
	now time.Time,
	rollback bool,
) (*domain.Release, error) {
	row := transaction.QueryRow(ctx, `
		UPDATE ota_releases
		SET status = $2,
		    record_version = record_version + 1,
		    updated_by = $3,
		    updated_at = $4,
		    published_by = CASE WHEN $2 = 'published' THEN $3 ELSE published_by END,
		    published_at = CASE
		        WHEN $2 = 'published' THEN COALESCE(published_at, $4)
		        ELSE published_at
		    END,
		    paused_by = CASE WHEN $2 = 'paused' THEN $3 ELSE paused_by END,
		    paused_at = CASE
		        WHEN $2 = 'paused' THEN COALESCE(paused_at, $4)
		        ELSE paused_at
		    END,
		    withdrawn_by = CASE WHEN $2 = 'withdrawn' THEN $3 ELSE withdrawn_by END,
		    withdrawn_at = CASE
		        WHEN $2 = 'withdrawn' THEN COALESCE(withdrawn_at, $4)
		        ELSE withdrawn_at
		    END,
		    rolled_back_by = CASE WHEN $5 THEN $3 ELSE rolled_back_by END,
		    rolled_back_at = CASE WHEN $5 THEN $4 ELSE rolled_back_at END
		WHERE id = $1
		  AND record_version = $6
		RETURNING
			id,
			version,
			channel,
			hardware_revision,
			min_source_version,
			artifact_url,
			artifact_key,
			sha256,
			size_bytes,
			signature_key_id,
			signature_algorithm,
			rollback_allowed,
			mandatory,
			release_notes,
			status,
			target_scope,
			target_group_id,
			target_device_id,
			canary_percent,
			rollback_release_id,
			record_version,
			created_by,
			updated_by,
			COALESCE(published_by, ''),
			COALESCE(paused_by, ''),
			COALESCE(withdrawn_by, ''),
			COALESCE(rolled_back_by, ''),
			created_at,
			updated_at,
			published_at,
			paused_at,
			withdrawn_at,
			rolled_back_at
	`,
		current.ID,
		nextStatus,
		actorID,
		now,
		rollback,
		current.RecordVersion,
	)
	updated, err := scanRelease(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.ReleaseVersionConflictError{
			ExpectedVersion: current.RecordVersion,
			CurrentVersion:  current.RecordVersion + 1,
		}
	}
	if err != nil {
		return nil, fmt.Errorf("update OTA release status: %w", err)
	}
	return updated, nil
}

// CreateGroup inserts a device group.
func (r *PostgresRepository) CreateGroup(
	ctx context.Context,
	group *domain.Group,
) error {
	if group == nil {
		return domain.ErrInvalidGroup
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO ota_groups (
			id,
			name,
			description,
			channel,
			hardware_revision,
			record_version,
			created_by,
			updated_by,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING
	`,
		group.ID,
		group.Name,
		group.Description,
		group.Channel,
		group.HardwareRevision,
		group.RecordVersion,
		group.CreatedBy,
		group.UpdatedBy,
		group.CreatedAt,
		group.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert OTA group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrGroupAlreadyExists
	}
	return nil
}

// UpdateGroup updates group metadata with optimistic concurrency.
func (r *PostgresRepository) UpdateGroup(
	ctx context.Context,
	group *domain.Group,
	expectedVersion int64,
) error {
	if group == nil {
		return domain.ErrInvalidGroup
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE ota_groups
		SET name = $2,
		    description = $3,
		    channel = $4,
		    hardware_revision = $5,
		    record_version = record_version + 1,
		    updated_by = $6,
		    updated_at = $7
		WHERE id = $1
		  AND record_version = $8
	`,
		group.ID,
		group.Name,
		group.Description,
		group.Channel,
		group.HardwareRevision,
		group.UpdatedBy,
		group.UpdatedAt,
		expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("update OTA group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		current, loadErr := r.GetGroup(ctx, group.ID)
		if errors.Is(loadErr, domain.ErrGroupNotFound) {
			return domain.ErrGroupNotFound
		}
		if loadErr != nil {
			return loadErr
		}
		return &domain.GroupVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  current.RecordVersion,
		}
	}
	return nil
}

// GetGroup loads one group.
func (r *PostgresRepository) GetGroup(
	ctx context.Context,
	groupID string,
) (*domain.Group, error) {
	var group domain.Group
	err := r.pool.QueryRow(ctx, `
		SELECT
			id,
			name,
			description,
			channel,
			hardware_revision,
			record_version,
			created_by,
			updated_by,
			created_at,
			updated_at
		FROM ota_groups
		WHERE id = $1
	`, groupID).Scan(
		&group.ID,
		&group.Name,
		&group.Description,
		&group.Channel,
		&group.HardwareRevision,
		&group.RecordVersion,
		&group.CreatedBy,
		&group.UpdatedBy,
		&group.CreatedAt,
		&group.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrGroupNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load OTA group: %w", err)
	}
	return &group, nil
}

// ListGroups returns every group for management.
func (r *PostgresRepository) ListGroups(
	ctx context.Context,
) ([]domain.Group, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			name,
			description,
			channel,
			hardware_revision,
			record_version,
			created_by,
			updated_by,
			created_at,
			updated_at
		FROM ota_groups
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list OTA groups: %w", err)
	}
	defer rows.Close()
	groups := make([]domain.Group, 0)
	for rows.Next() {
		var group domain.Group
		if err := rows.Scan(
			&group.ID,
			&group.Name,
			&group.Description,
			&group.Channel,
			&group.HardwareRevision,
			&group.RecordVersion,
			&group.CreatedBy,
			&group.UpdatedBy,
			&group.CreatedAt,
			&group.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan OTA group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate OTA groups: %w", err)
	}
	return groups, nil
}

// AddGroupMember adds a device to a group idempotently.
func (r *PostgresRepository) AddGroupMember(
	ctx context.Context,
	groupID string,
	deviceID string,
	actorID string,
) error {
	if strings.TrimSpace(groupID) == "" ||
		strings.TrimSpace(deviceID) == "" ||
		strings.TrimSpace(actorID) == "" {
		return domain.ErrInvalidGroup
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO ota_group_members (group_id, device_id, added_by)
		SELECT $1, $2, $3
		WHERE EXISTS (SELECT 1 FROM ota_groups WHERE id = $1)
		  AND EXISTS (SELECT 1 FROM device_credentials WHERE device_id = $2)
		ON CONFLICT (group_id, device_id) DO NOTHING
	`, groupID, deviceID, actorID)
	if err != nil {
		return fmt.Errorf("add OTA group member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, groupErr := r.GetGroup(ctx, groupID); errors.Is(
			groupErr,
			domain.ErrGroupNotFound,
		) {
			return domain.ErrGroupNotFound
		} else if groupErr != nil {
			return groupErr
		}
		if _, deviceErr := r.GetDeviceContext(ctx, deviceID); errors.Is(
			deviceErr,
			domain.ErrDeviceNotFound,
		) {
			return domain.ErrDeviceNotFound
		} else if deviceErr != nil {
			return deviceErr
		}
	}
	return nil
}

// RemoveGroupMember removes a device from a group idempotently.
func (r *PostgresRepository) RemoveGroupMember(
	ctx context.Context,
	groupID string,
	deviceID string,
	actorID string,
) error {
	if strings.TrimSpace(groupID) == "" ||
		strings.TrimSpace(deviceID) == "" ||
		strings.TrimSpace(actorID) == "" {
		return domain.ErrInvalidGroup
	}
	_, err := r.pool.Exec(ctx, `
		DELETE FROM ota_group_members
		WHERE group_id = $1
		  AND device_id = $2
	`, groupID, deviceID)
	if err != nil {
		return fmt.Errorf("remove OTA group member: %w", err)
	}
	return nil
}

// ListGroupMembers returns the sorted device ids in one group.
func (r *PostgresRepository) ListGroupMembers(
	ctx context.Context,
	groupID string,
) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT device_id
		FROM ota_group_members
		WHERE group_id = $1
		ORDER BY device_id ASC
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list OTA group members: %w", err)
	}
	defer rows.Close()
	members := make([]string, 0)
	for rows.Next() {
		var deviceID string
		if err := rows.Scan(&deviceID); err != nil {
			return nil, fmt.Errorf("scan OTA group member: %w", err)
		}
		members = append(members, deviceID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate OTA group members: %w", err)
	}
	return members, nil
}

// GetDeviceContext loads the OTA-relevant device projection.
func (r *PostgresRepository) GetDeviceContext(
	ctx context.Context,
	deviceID string,
) (*domain.DeviceContext, error) {
	var device domain.DeviceContext
	err := r.pool.QueryRow(ctx, `
		SELECT
			credential.device_id,
			credential.hardware_model,
			credential.firmware_version,
			COALESCE(group_members.group_ids, ARRAY[]::TEXT[])
		FROM device_credentials AS credential
		LEFT JOIN (
			SELECT
				device_id,
				ARRAY_AGG(group_id::text ORDER BY group_id) AS group_ids
			FROM ota_group_members
			GROUP BY device_id
		) AS group_members ON group_members.device_id = credential.device_id
		WHERE credential.device_id = $1
		  AND credential.status = 'active'
	`, deviceID).Scan(
		&device.DeviceID,
		&device.HardwareRevision,
		&device.CurrentVersion,
		&device.GroupIDs,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load OTA device context: %w", err)
	}
	return &device, nil
}

// ListPublishedReleases returns published releases for one hardware family.
func (r *PostgresRepository) ListPublishedReleases(
	ctx context.Context,
	channel domain.Channel,
	hardwareRevision string,
) ([]domain.Release, error) {
	where := " WHERE release.status = 'published'"
	arguments := make([]any, 0, 2)
	if channel != "" {
		arguments = append(arguments, channel)
		where += fmt.Sprintf(" AND release.channel = $%d", len(arguments))
	}
	if strings.TrimSpace(hardwareRevision) != "" {
		arguments = append(arguments, strings.TrimSpace(hardwareRevision))
		where += fmt.Sprintf(" AND release.hardware_revision = $%d", len(arguments))
	}
	rows, err := r.pool.Query(
		ctx,
		releaseSelect+where+" ORDER BY release.published_at DESC",
		arguments...,
	)
	if err != nil {
		return nil, fmt.Errorf("list published OTA releases: %w", err)
	}
	defer rows.Close()
	releases := make([]domain.Release, 0)
	for rows.Next() {
		release, scanErr := scanRelease(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan published OTA release: %w", scanErr)
		}
		releases = append(releases, *release)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate published OTA releases: %w", err)
	}
	return releases, nil
}

// CreateDeployment creates an idempotent release/device assignment.
func (r *PostgresRepository) CreateDeployment(
	ctx context.Context,
	deployment *domain.Deployment,
) (*domain.Deployment, error) {
	if deployment == nil {
		return nil, domain.ErrInvalidDeployment
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO ota_deployments (
			id,
			release_id,
			device_id,
			group_id,
			status,
			requested_by,
			request_id,
			progress_percent,
			bytes_received,
			bytes_total,
			retry_count,
			record_version,
			created_at,
			updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14
		)
		ON CONFLICT (request_id) DO NOTHING
	`,
		deployment.ID,
		deployment.ReleaseID,
		deployment.DeviceID,
		nullableUUID(deployment.GroupID),
		deployment.Status,
		deployment.RequestedBy,
		deployment.RequestID,
		deployment.ProgressPercent,
		deployment.BytesReceived,
		deployment.BytesTotal,
		deployment.RetryCount,
		deployment.RecordVersion,
		deployment.CreatedAt,
		deployment.UpdatedAt,
	)
	if err != nil {
		return nil, mapDeploymentWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		existing, loadErr := r.GetDeploymentByRequestID(ctx, deployment.RequestID)
		if loadErr != nil {
			return nil, loadErr
		}
		if existing.ReleaseID == deployment.ReleaseID &&
			existing.DeviceID == deployment.DeviceID {
			return existing, nil
		}
		return nil, domain.ErrDeploymentAlreadyExists
	}
	return r.GetDeployment(ctx, deployment.ID)
}

// GetDeployment loads one deployment by id.
func (r *PostgresRepository) GetDeployment(
	ctx context.Context,
	deploymentID string,
) (*domain.Deployment, error) {
	return r.getDeployment(ctx, deploymentSelect+` WHERE deployment.id = $1`, deploymentID)
}

// GetDeploymentByRequestID loads a deployment by its idempotency key.
func (r *PostgresRepository) GetDeploymentByRequestID(
	ctx context.Context,
	requestID string,
) (*domain.Deployment, error) {
	return r.getDeployment(
		ctx,
		deploymentSelect+` WHERE deployment.request_id = $1`,
		requestID,
	)
}

func (r *PostgresRepository) getDeployment(
	ctx context.Context,
	statement string,
	argument any,
) (*domain.Deployment, error) {
	deployment, err := scanDeployment(r.pool.QueryRow(ctx, statement, argument))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrDeploymentNotFound
	}
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

// ListDeployments returns a bounded, filtered deployment page.
func (r *PostgresRepository) ListDeployments(
	ctx context.Context,
	filter domain.DeploymentFilter,
) (*domain.DeploymentPage, error) {
	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	where, arguments := deploymentFilterSQL(filter)
	var total int64
	if err := r.pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM ota_deployments deployment JOIN ota_releases release ON release.id = deployment.release_id "+where,
		arguments...,
	).Scan(&total); err != nil {
		return nil, fmt.Errorf("count OTA deployments: %w", err)
	}
	listArguments := append(append([]any(nil), arguments...), pageSize, (page-1)*pageSize)
	rows, err := r.pool.Query(ctx, deploymentSelect+where+fmt.Sprintf(
		" ORDER BY deployment.updated_at DESC LIMIT $%d OFFSET $%d",
		len(listArguments)-1,
		len(listArguments),
	), listArguments...)
	if err != nil {
		return nil, fmt.Errorf("list OTA deployments: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Deployment, 0, pageSize)
	for rows.Next() {
		deployment, scanErr := scanDeployment(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan OTA deployment: %w", scanErr)
		}
		items = append(items, *deployment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate OTA deployments: %w", err)
	}
	return &domain.DeploymentPage{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// RetryDeployment requeues a failed deployment with optimistic concurrency.
func (r *PostgresRepository) RetryDeployment(
	ctx context.Context,
	deploymentID string,
	actorID string,
	expectedVersion int64,
	now time.Time,
) (*domain.Deployment, error) {
	var deployment *domain.Deployment
	err := withTransaction(ctx, r.pool, func(transaction pgx.Tx) error {
		current, err := scanDeployment(transaction.QueryRow(
			ctx,
			deploymentSelect+` WHERE deployment.id = $1 FOR UPDATE`,
			deploymentID,
		))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrDeploymentNotFound
		}
		if err != nil {
			return err
		}
		if current.RecordVersion != expectedVersion {
			return &domain.DeploymentVersionConflictError{
				ExpectedVersion: expectedVersion,
				CurrentVersion:  current.RecordVersion,
			}
		}
		if current.Status != domain.DeploymentStatusFailed {
			return domain.ErrDeploymentStateConflict
		}
		updated, err := scanDeployment(transaction.QueryRow(ctx, `
			UPDATE ota_deployments
			SET status = 'queued',
			    retry_count = retry_count + 1,
			    progress_percent = 0,
			    bytes_received = 0,
			    failure_code = '',
			    failure_message = '',
			    failed_at = NULL,
			    updated_at = $2,
			    record_version = record_version + 1,
			    requested_by = $3
			WHERE id = $1
			  AND record_version = $4
			RETURNING
				id,
				release_id,
				device_id,
				group_id,
				status,
				requested_by,
				request_id,
				progress_percent,
				bytes_received,
				bytes_total,
				failure_code,
				failure_message,
				retry_count,
				record_version,
				last_event_sequence,
				created_at,
				updated_at,
				offered_at,
				download_started_at,
				validated_at,
				install_started_at,
				verification_started_at,
				completed_at,
				failed_at,
				rolled_back_at
		`, deploymentID, now, actorID, expectedVersion))
		if errors.Is(err, pgx.ErrNoRows) {
			return &domain.DeploymentVersionConflictError{
				ExpectedVersion: expectedVersion,
				CurrentVersion:  expectedVersion + 1,
			}
		}
		if err != nil {
			return fmt.Errorf("retry OTA deployment: %w", err)
		}
		deployment = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

// RecordEvent stores one progress event and updates the deployment in the same
// transaction. A repeated event id with identical state is idempotent.
func (r *PostgresRepository) RecordEvent(
	ctx context.Context,
	event *domain.Event,
) (*domain.Deployment, bool, error) {
	var deployment *domain.Deployment
	inserted := false
	err := withTransaction(ctx, r.pool, func(transaction pgx.Tx) error {
		current, err := scanDeployment(transaction.QueryRow(
			ctx,
			deploymentSelect+` WHERE deployment.id = $1 FOR UPDATE`,
			event.DeploymentID,
		))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrDeploymentNotFound
		}
		if err != nil {
			return err
		}
		existing, loadErr := loadEventByEventID(
			ctx,
			transaction,
			event.DeploymentID,
			event.EventID,
		)
		if loadErr == nil {
			if !sameEvent(existing, event) {
				return domain.ErrEventConflict
			}
			deployment = current
			return nil
		}
		if !errors.Is(loadErr, pgx.ErrNoRows) {
			return loadErr
		}
		if event.Sequence <= current.LastEventSequence {
			return domain.ErrInvalidEvent
		}
		if !domain.CanTransitionDeployment(current.Status, event.Status) {
			return domain.ErrDeploymentStateConflict
		}

		tag, err := transaction.Exec(ctx, `
			INSERT INTO ota_events (
				id,
				deployment_id,
				event_id,
				event_type,
				sequence,
				status,
				progress_percent,
				bytes_received,
				bytes_total,
				error_code,
				message,
				detail,
				reported_at,
				received_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
				$11, $12::jsonb, $13, $14
			)
			ON CONFLICT (deployment_id, event_id) DO NOTHING
		`,
			event.ID,
			event.DeploymentID,
			event.EventID,
			event.Type,
			event.Sequence,
			event.Status,
			event.ProgressPercent,
			event.BytesReceived,
			event.BytesTotal,
			event.ErrorCode,
			event.Message,
			encodeDetail(event.Detail),
			event.ReportedAt,
			event.ReceivedAt,
		)
		if err != nil {
			return mapEventWriteError(err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrEventConflict
		}
		inserted = true

		deployment, err = scanDeployment(transaction.QueryRow(ctx, `
			UPDATE ota_deployments
			SET status = $2,
			    progress_percent = GREATEST(progress_percent, $3),
			    bytes_received = GREATEST(bytes_received, $4),
			    bytes_total = CASE WHEN $5 > 0 THEN $5 ELSE bytes_total END,
			    failure_code = $6,
			    failure_message = $7,
			    last_event_sequence = $8,
			    record_version = record_version + 1,
			    updated_at = $9,
			    offered_at = CASE
			        WHEN $2 = 'offered' THEN COALESCE(offered_at, $9)
			        ELSE offered_at
			    END,
			    download_started_at = CASE
			        WHEN $2 = 'downloading' THEN COALESCE(download_started_at, $9)
			        ELSE download_started_at
			    END,
			    validated_at = CASE
			        WHEN $2 = 'validating' THEN COALESCE(validated_at, $9)
			        ELSE validated_at
			    END,
			    install_started_at = CASE
			        WHEN $2 = 'installing' THEN COALESCE(install_started_at, $9)
			        ELSE install_started_at
			    END,
			    verification_started_at = CASE
			        WHEN $2 = 'pending_verify' THEN COALESCE(verification_started_at, $9)
			        ELSE verification_started_at
			    END,
			    completed_at = CASE
			        WHEN $2 = 'succeeded' THEN COALESCE(completed_at, $9)
			        ELSE completed_at
			    END,
			    failed_at = CASE
			        WHEN $2 = 'failed' THEN COALESCE(failed_at, $9)
			        ELSE failed_at
			    END,
			    rolled_back_at = CASE
			        WHEN $2 = 'rolled_back' THEN COALESCE(rolled_back_at, $9)
			        ELSE rolled_back_at
			    END
			WHERE id = $1
			  AND record_version = $10
			RETURNING
				id,
				release_id,
				device_id,
				group_id,
				status,
				requested_by,
				request_id,
				progress_percent,
				bytes_received,
				bytes_total,
				failure_code,
				failure_message,
				retry_count,
				record_version,
				last_event_sequence,
				created_at,
				updated_at,
				offered_at,
				download_started_at,
				validated_at,
				install_started_at,
				verification_started_at,
				completed_at,
				failed_at,
				rolled_back_at
		`,
			current.ID,
			event.Status,
			event.ProgressPercent,
			event.BytesReceived,
			event.BytesTotal,
			event.ErrorCode,
			event.Message,
			event.Sequence,
			event.ReceivedAt,
			current.RecordVersion,
		))
		if errors.Is(err, pgx.ErrNoRows) {
			return &domain.DeploymentVersionConflictError{
				ExpectedVersion: current.RecordVersion,
				CurrentVersion:  current.RecordVersion + 1,
			}
		}
		if err != nil {
			return fmt.Errorf("update OTA deployment progress: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return deployment, inserted, nil
}

// Statistics returns aggregate deployment counts for an operator filter.
func (r *PostgresRepository) Statistics(
	ctx context.Context,
	filter domain.DeploymentFilter,
) (*domain.Statistics, error) {
	where, arguments := deploymentFilterSQL(filter)
	statistics := &domain.Statistics{
		ByStatus:  map[string]int64{},
		ByChannel: map[string]int64{},
		ByVersion: map[string]int64{},
	}
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE deployment.status = 'failed'),
			COUNT(*) FILTER (WHERE deployment.status = 'rolled_back'),
			COALESCE(AVG(deployment.progress_percent), 0)
		FROM ota_deployments deployment
		JOIN ota_releases release ON release.id = deployment.release_id
	`+where, arguments...).Scan(
		&statistics.Total,
		&statistics.Failed,
		&statistics.RolledBack,
		&statistics.ProgressAverage,
	)
	if err != nil {
		return nil, fmt.Errorf("load OTA deployment statistics: %w", err)
	}
	if statistics.Total > 0 {
		statistics.FailureRate =
			float64(statistics.Failed) / float64(statistics.Total)
	}
	if err := fillCountMap(
		ctx,
		r.pool,
		"deployment.status",
		where,
		arguments,
		statistics.ByStatus,
	); err != nil {
		return nil, err
	}
	if err := fillCountMap(
		ctx,
		r.pool,
		"release.channel",
		where,
		arguments,
		statistics.ByChannel,
	); err != nil {
		return nil, err
	}
	if err := fillCountMap(
		ctx,
		r.pool,
		"release.version",
		where,
		arguments,
		statistics.ByVersion,
	); err != nil {
		return nil, err
	}
	return statistics, nil
}

func fillCountMap(
	ctx context.Context,
	pool *pgxpool.Pool,
	column string,
	where string,
	arguments []any,
	destination map[string]int64,
) error {
	rows, err := pool.Query(ctx, fmt.Sprintf(`
		SELECT %s, COUNT(*)
		FROM ota_deployments deployment
		JOIN ota_releases release ON release.id = deployment.release_id
		%s
		GROUP BY %s
		ORDER BY COUNT(*) DESC
	`, column, where, column), arguments...)
	if err != nil {
		return fmt.Errorf("load OTA aggregate %s: %w", column, err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return fmt.Errorf("scan OTA aggregate %s: %w", column, err)
		}
		destination[key] = count
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate OTA aggregate %s: %w", column, err)
	}
	return nil
}

const releaseSelect = `
	SELECT
		release.id,
		release.version,
		release.channel,
		release.hardware_revision,
		release.min_source_version,
		release.artifact_url,
		release.artifact_key,
		release.sha256,
		release.size_bytes,
		release.signature_key_id,
		release.signature_algorithm,
		release.rollback_allowed,
		release.mandatory,
		release.release_notes,
		release.status,
		release.target_scope,
		release.target_group_id,
		release.target_device_id,
		release.canary_percent,
		release.rollback_release_id,
		release.record_version,
		release.created_by,
		release.updated_by,
		COALESCE(release.published_by, ''),
		COALESCE(release.paused_by, ''),
		COALESCE(release.withdrawn_by, ''),
		COALESCE(release.rolled_back_by, ''),
		release.created_at,
		release.updated_at,
		release.published_at,
		release.paused_at,
		release.withdrawn_at,
		release.rolled_back_at
	FROM ota_releases AS release
`

const deploymentSelect = `
	SELECT
		deployment.id,
		deployment.release_id,
		deployment.device_id,
		COALESCE(deployment.group_id::text, ''),
		deployment.status,
		deployment.requested_by,
		deployment.request_id,
		deployment.progress_percent,
		deployment.bytes_received,
		deployment.bytes_total,
		deployment.failure_code,
		deployment.failure_message,
		deployment.retry_count,
		deployment.record_version,
		deployment.last_event_sequence,
		deployment.created_at,
		deployment.updated_at,
		deployment.offered_at,
		deployment.download_started_at,
		deployment.validated_at,
		deployment.install_started_at,
		deployment.verification_started_at,
		deployment.completed_at,
		deployment.failed_at,
		deployment.rolled_back_at
	FROM ota_deployments AS deployment
	JOIN ota_releases AS release ON release.id = deployment.release_id
`

type scanner interface {
	Scan(dest ...any) error
}

func scanRelease(row scanner) (*domain.Release, error) {
	var release domain.Release
	var targetScope domain.TargetScope
	var targetGroupID *string
	var targetDeviceID *string
	var rollbackReleaseID *string
	err := row.Scan(
		&release.ID,
		&release.Version,
		&release.Channel,
		&release.HardwareRevision,
		&release.MinSourceVersion,
		&release.ArtifactURL,
		&release.ArtifactKey,
		&release.SHA256,
		&release.SizeBytes,
		&release.SignatureKeyID,
		&release.SignatureAlgorithm,
		&release.RollbackAllowed,
		&release.Mandatory,
		&release.ReleaseNotes,
		&release.Status,
		&targetScope,
		&targetGroupID,
		&targetDeviceID,
		&release.Target.CanaryPercent,
		&rollbackReleaseID,
		&release.RecordVersion,
		&release.CreatedBy,
		&release.UpdatedBy,
		&release.PublishedBy,
		&release.PausedBy,
		&release.WithdrawnBy,
		&release.RolledBackBy,
		&release.CreatedAt,
		&release.UpdatedAt,
		&release.PublishedAt,
		&release.PausedAt,
		&release.WithdrawnAt,
		&release.RolledBackAt,
	)
	if err != nil {
		return nil, err
	}
	release.Target.Scope = targetScope
	release.Target.GroupID = stringValue(targetGroupID)
	release.Target.DeviceID = stringValue(targetDeviceID)
	release.RollbackReleaseID = stringValue(rollbackReleaseID)
	return &release, nil
}

func scanDeployment(row scanner) (*domain.Deployment, error) {
	var deployment domain.Deployment
	err := row.Scan(
		&deployment.ID,
		&deployment.ReleaseID,
		&deployment.DeviceID,
		&deployment.GroupID,
		&deployment.Status,
		&deployment.RequestedBy,
		&deployment.RequestID,
		&deployment.ProgressPercent,
		&deployment.BytesReceived,
		&deployment.BytesTotal,
		&deployment.FailureCode,
		&deployment.FailureMessage,
		&deployment.RetryCount,
		&deployment.RecordVersion,
		&deployment.LastEventSequence,
		&deployment.CreatedAt,
		&deployment.UpdatedAt,
		&deployment.OfferedAt,
		&deployment.DownloadStartedAt,
		&deployment.ValidatedAt,
		&deployment.InstallStartedAt,
		&deployment.VerificationStartedAt,
		&deployment.CompletedAt,
		&deployment.FailedAt,
		&deployment.RolledBackAt,
	)
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func loadEventByEventID(
	ctx context.Context,
	transaction pgx.Tx,
	deploymentID string,
	eventID string,
) (*domain.Event, error) {
	var event domain.Event
	var detail []byte
	err := transaction.QueryRow(ctx, `
		SELECT
			id,
			deployment_id,
			event_id,
			event_type,
			sequence,
			status,
			progress_percent,
			bytes_received,
			bytes_total,
			error_code,
			message,
			detail,
			reported_at,
			received_at
		FROM ota_events
		WHERE deployment_id = $1
		  AND event_id = $2
	`, deploymentID, eventID).Scan(
		&event.ID,
		&event.DeploymentID,
		&event.EventID,
		&event.Type,
		&event.Sequence,
		&event.Status,
		&event.ProgressPercent,
		&event.BytesReceived,
		&event.BytesTotal,
		&event.ErrorCode,
		&event.Message,
		&detail,
		&event.ReportedAt,
		&event.ReceivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("load existing OTA event: %w", err)
	}
	if err := json.Unmarshal(detail, &event.Detail); err != nil {
		return nil, fmt.Errorf("decode existing OTA event: %w", err)
	}
	return &event, nil
}

func sameEvent(left *domain.Event, right *domain.Event) bool {
	if left == nil || right == nil {
		return false
	}
	if left.DeploymentID != right.DeploymentID ||
		left.EventID != right.EventID ||
		left.Type != right.Type ||
		left.Sequence != right.Sequence ||
		left.Status != right.Status ||
		left.ProgressPercent != right.ProgressPercent ||
		left.BytesReceived != right.BytesReceived ||
		left.BytesTotal != right.BytesTotal ||
		left.ErrorCode != right.ErrorCode ||
		left.Message != right.Message {
		return false
	}
	return encodeDetail(left.Detail) == encodeDetail(right.Detail)
}

func releaseFilterSQL(filter domain.ReleaseFilter) (string, []any) {
	clauses := make([]string, 0, 6)
	arguments := make([]any, 0, 6)
	if filter.Status != "" {
		arguments = append(arguments, filter.Status)
		clauses = append(clauses, fmt.Sprintf("release.status = $%d", len(arguments)))
	}
	if filter.Channel != "" {
		arguments = append(arguments, filter.Channel)
		clauses = append(clauses, fmt.Sprintf("release.channel = $%d", len(arguments)))
	}
	if strings.TrimSpace(filter.Version) != "" {
		arguments = append(arguments, strings.TrimSpace(filter.Version))
		clauses = append(clauses, fmt.Sprintf("release.version = $%d", len(arguments)))
	}
	if strings.TrimSpace(filter.HardwareRevision) != "" {
		arguments = append(arguments, strings.TrimSpace(filter.HardwareRevision))
		clauses = append(
			clauses,
			fmt.Sprintf("release.hardware_revision = $%d", len(arguments)),
		)
	}
	if strings.TrimSpace(filter.GroupID) != "" {
		arguments = append(arguments, strings.TrimSpace(filter.GroupID))
		clauses = append(clauses, fmt.Sprintf("release.target_group_id = $%d", len(arguments)))
	}
	if strings.TrimSpace(filter.DeviceID) != "" {
		arguments = append(arguments, strings.TrimSpace(filter.DeviceID))
		clauses = append(clauses, fmt.Sprintf("release.target_device_id = $%d", len(arguments)))
	}
	if len(clauses) == 0 {
		return "", arguments
	}
	return " WHERE " + strings.Join(clauses, " AND "), arguments
}

func deploymentFilterSQL(filter domain.DeploymentFilter) (string, []any) {
	clauses := make([]string, 0, 6)
	arguments := make([]any, 0, 6)
	if strings.TrimSpace(filter.ReleaseID) != "" {
		arguments = append(arguments, strings.TrimSpace(filter.ReleaseID))
		clauses = append(clauses, fmt.Sprintf("deployment.release_id = $%d", len(arguments)))
	}
	if strings.TrimSpace(filter.DeviceID) != "" {
		arguments = append(arguments, strings.TrimSpace(filter.DeviceID))
		clauses = append(clauses, fmt.Sprintf("deployment.device_id = $%d", len(arguments)))
	}
	if strings.TrimSpace(filter.GroupID) != "" {
		arguments = append(arguments, strings.TrimSpace(filter.GroupID))
		clauses = append(clauses, fmt.Sprintf("deployment.group_id = $%d", len(arguments)))
	}
	if filter.Status != "" {
		arguments = append(arguments, filter.Status)
		clauses = append(clauses, fmt.Sprintf("deployment.status = $%d", len(arguments)))
	}
	if filter.Channel != "" {
		arguments = append(arguments, filter.Channel)
		clauses = append(clauses, fmt.Sprintf("release.channel = $%d", len(arguments)))
	}
	if strings.TrimSpace(filter.Version) != "" {
		arguments = append(arguments, strings.TrimSpace(filter.Version))
		clauses = append(clauses, fmt.Sprintf("release.version = $%d", len(arguments)))
	}
	if len(clauses) == 0 {
		return "", arguments
	}
	return " WHERE " + strings.Join(clauses, " AND "), arguments
}

func normalizePage(page int, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func nullableUUID(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func nullableText(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func encodeDetail(detail map[string]any) string {
	if detail == nil {
		return "{}"
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func mapReleaseWriteError(err error) error {
	if err == nil {
		return nil
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return domain.ErrReleaseAlreadyExists
	}
	return fmt.Errorf("write OTA release: %w", err)
}

func mapDeploymentWriteError(err error) error {
	if err == nil {
		return nil
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.ConstraintName {
		case "ota_deployments_request_unique",
			"ota_deployments_release_device_unique":
			return domain.ErrDeploymentAlreadyExists
		}
	}
	return fmt.Errorf("write OTA deployment: %w", err)
}

func mapEventWriteError(err error) error {
	if err == nil {
		return nil
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) &&
		pgError.Code == "23505" &&
		(pgError.ConstraintName == "ota_events_event_unique" ||
			pgError.ConstraintName == "ota_events_sequence_unique") {
		return domain.ErrEventConflict
	}
	return fmt.Errorf("write OTA event: %w", err)
}

func withTransaction(
	ctx context.Context,
	pool *pgxpool.Pool,
	operation func(transaction pgx.Tx) error,
) error {
	transaction, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin OTA transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	if err := operation(transaction); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit OTA transaction: %w", err)
	}
	return nil
}

var _ Repository = (*PostgresRepository)(nil)

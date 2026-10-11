// Package repository persists device groups, group membership, routed messages
// and content-sync state.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for device mesh.
type Repository interface {
	CreateGroup(ctx context.Context, group domain.DeviceGroup) error
	DeleteGroup(ctx context.Context, familyID string, groupID string) error
	Group(ctx context.Context, familyID string, groupID string) (domain.DeviceGroup, bool, error)
	Groups(ctx context.Context, familyID string) ([]domain.DeviceGroup, error)

	AddMember(ctx context.Context, member domain.Member) error
	RemoveMember(ctx context.Context, familyID string, groupID string, deviceID string) error
	Members(ctx context.Context, familyID string, groupID string) ([]domain.Member, error)
	MemberGroups(ctx context.Context, familyID string, deviceID string) ([]string, error)

	CreateMessage(ctx context.Context, message domain.RoutedMessage) error
	Messages(ctx context.Context, familyID string, limit int) ([]domain.RoutedMessage, error)

	SaveContentState(ctx context.Context, state domain.ContentState) error
	ContentState(ctx context.Context, familyID string, contentKey string) (domain.ContentState, bool, error)
	ContentStates(ctx context.Context, familyID string) ([]domain.ContentState, error)

	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// PostgresRepository is the PostgreSQL-backed device-mesh repository.
type PostgresRepository struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewPostgresRepository creates a device-mesh repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, clock: clock.SystemClock{}}
}

// NewPostgresRepositoryWithClock creates a device-mesh repository with an
// injected time source for deterministic tests.
func NewPostgresRepositoryWithClock(
	pool *pgxpool.Pool,
	timeSource clock.Clock,
) *PostgresRepository {
	repository := NewPostgresRepository(pool)
	if timeSource != nil {
		repository.clock = timeSource
	}
	return repository
}

// CreateGroup inserts one device group.
func (r *PostgresRepository) CreateGroup(
	ctx context.Context,
	group domain.DeviceGroup,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_groups (id, family_id, name, route_scope, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, group.ID, group.FamilyID, group.Name, group.RouteScope)
	if err != nil {
		return fmt.Errorf("create device group: %w", err)
	}
	return nil
}

// DeleteGroup removes one device group and its membership within a family.
func (r *PostgresRepository) DeleteGroup(
	ctx context.Context,
	familyID string,
	groupID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM device_groups WHERE id = $1 AND family_id = $2
	`, groupID, familyID)
	if err != nil {
		return fmt.Errorf("delete device group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrGroupNotFound
	}
	return nil
}

// Group returns one group within a family with its member count.
func (r *PostgresRepository) Group(
	ctx context.Context,
	familyID string,
	groupID string,
) (domain.DeviceGroup, bool, error) {
	var group domain.DeviceGroup
	err := r.pool.QueryRow(ctx, `
		SELECT g.id, g.family_id, g.name, g.route_scope,
			(SELECT COUNT(*) FROM device_group_members m WHERE m.group_id = g.id),
			g.created_at, g.updated_at
		FROM device_groups g
		WHERE g.family_id = $1 AND g.id = $2
	`, familyID, groupID).Scan(
		&group.ID,
		&group.FamilyID,
		&group.Name,
		&group.RouteScope,
		&group.MemberCount,
		&group.CreatedAt,
		&group.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DeviceGroup{}, false, nil
	}
	if err != nil {
		return domain.DeviceGroup{}, false, fmt.Errorf("read device group: %w", err)
	}
	return group, true, nil
}

// Groups returns every device group in a family with member counts.
func (r *PostgresRepository) Groups(
	ctx context.Context,
	familyID string,
) ([]domain.DeviceGroup, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT g.id, g.family_id, g.name, g.route_scope,
			(SELECT COUNT(*) FROM device_group_members m WHERE m.group_id = g.id),
			g.created_at, g.updated_at
		FROM device_groups g
		WHERE g.family_id = $1
		ORDER BY g.created_at DESC
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list device groups: %w", err)
	}
	defer rows.Close()
	groups := make([]domain.DeviceGroup, 0, 8)
	for rows.Next() {
		var group domain.DeviceGroup
		if err := rows.Scan(
			&group.ID,
			&group.FamilyID,
			&group.Name,
			&group.RouteScope,
			&group.MemberCount,
			&group.CreatedAt,
			&group.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan device group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device groups: %w", err)
	}
	return groups, nil
}

// AddMember inserts one group member.
func (r *PostgresRepository) AddMember(
	ctx context.Context,
	member domain.Member,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_group_members (group_id, device_id, family_id, added_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (group_id, device_id) DO NOTHING
	`, member.GroupID, member.DeviceID, member.FamilyID)
	if err != nil {
		return fmt.Errorf("add device group member: %w", err)
	}
	return nil
}

// RemoveMember removes one group member within a family.
func (r *PostgresRepository) RemoveMember(
	ctx context.Context,
	familyID string,
	groupID string,
	deviceID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM device_group_members
		WHERE group_id = $1 AND device_id = $2 AND family_id = $3
	`, groupID, deviceID, familyID)
	if err != nil {
		return fmt.Errorf("remove device group member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

// Members returns every member of a group.
func (r *PostgresRepository) Members(
	ctx context.Context,
	familyID string,
	groupID string,
) ([]domain.Member, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT group_id, device_id, family_id, added_at
		FROM device_group_members
		WHERE family_id = $1 AND group_id = $2
		ORDER BY added_at ASC
	`, familyID, groupID)
	if err != nil {
		return nil, fmt.Errorf("list device group members: %w", err)
	}
	defer rows.Close()
	members := make([]domain.Member, 0, 8)
	for rows.Next() {
		var member domain.Member
		if err := rows.Scan(
			&member.GroupID,
			&member.DeviceID,
			&member.FamilyID,
			&member.AddedAt,
		); err != nil {
			return nil, fmt.Errorf("scan device group member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device group members: %w", err)
	}
	return members, nil
}

// MemberGroups returns the ids of every group a device belongs to.
func (r *PostgresRepository) MemberGroups(
	ctx context.Context,
	familyID string,
	deviceID string,
) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT group_id
		FROM device_group_members
		WHERE family_id = $1 AND device_id = $2
		ORDER BY added_at ASC
	`, familyID, deviceID)
	if err != nil {
		return nil, fmt.Errorf("list device groups: %w", err)
	}
	defer rows.Close()
	groups := make([]string, 0, 4)
	for rows.Next() {
		var groupID string
		if err := rows.Scan(&groupID); err != nil {
			return nil, fmt.Errorf("scan device group id: %w", err)
		}
		groups = append(groups, groupID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device groups: %w", err)
	}
	return groups, nil
}

// CreateMessage inserts one routed message.
func (r *PostgresRepository) CreateMessage(
	ctx context.Context,
	message domain.RoutedMessage,
) error {
	var source any
	if message.SourceDevice != "" {
		source = message.SourceDevice
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO device_messages (
			id, family_id, source_device, scope, target, body, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`, message.ID, message.FamilyID, source, message.Scope, message.Target, message.Body)
	if err != nil {
		return fmt.Errorf("create device message: %w", err)
	}
	return nil
}

// Messages returns the most recent routed messages in a family.
func (r *PostgresRepository) Messages(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.RoutedMessage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, family_id, COALESCE(source_device, ''), scope, target, body,
			created_at
		FROM device_messages
		WHERE family_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, familyID, limit)
	if err != nil {
		return nil, fmt.Errorf("list device messages: %w", err)
	}
	defer rows.Close()
	messages := make([]domain.RoutedMessage, 0, limit)
	for rows.Next() {
		var message domain.RoutedMessage
		if err := rows.Scan(
			&message.ID,
			&message.FamilyID,
			&message.SourceDevice,
			&message.Scope,
			&message.Target,
			&message.Body,
			&message.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan device message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device messages: %w", err)
	}
	return messages, nil
}

// SaveContentState upserts the content-sync state for one key.
func (r *PostgresRepository) SaveContentState(
	ctx context.Context,
	state domain.ContentState,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO content_sync_states (
			family_id, content_key, version, conflict_count, sync_state, updated_at
		) VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (family_id, content_key)
		DO UPDATE SET
			version = EXCLUDED.version,
			conflict_count = EXCLUDED.conflict_count,
			sync_state = EXCLUDED.sync_state,
			updated_at = NOW()
	`,
		state.FamilyID,
		state.ContentKey,
		state.Version,
		state.ConflictCount,
		state.SyncState,
	)
	if err != nil {
		return fmt.Errorf("save content state: %w", err)
	}
	return nil
}

// ContentState returns the stored content-sync state for one key.
func (r *PostgresRepository) ContentState(
	ctx context.Context,
	familyID string,
	contentKey string,
) (domain.ContentState, bool, error) {
	var state domain.ContentState
	err := r.pool.QueryRow(ctx, `
		SELECT family_id, content_key, version, conflict_count, sync_state,
			updated_at
		FROM content_sync_states
		WHERE family_id = $1 AND content_key = $2
	`, familyID, contentKey).Scan(
		&state.FamilyID,
		&state.ContentKey,
		&state.Version,
		&state.ConflictCount,
		&state.SyncState,
		&state.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ContentState{}, false, nil
	}
	if err != nil {
		return domain.ContentState{}, false, fmt.Errorf("read content state: %w", err)
	}
	return state, true, nil
}

// ContentStates returns every content-sync state in a family.
func (r *PostgresRepository) ContentStates(
	ctx context.Context,
	familyID string,
) ([]domain.ContentState, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT family_id, content_key, version, conflict_count, sync_state,
			updated_at
		FROM content_sync_states
		WHERE family_id = $1
		ORDER BY updated_at DESC
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list content states: %w", err)
	}
	defer rows.Close()
	states := make([]domain.ContentState, 0, 8)
	for rows.Next() {
		var state domain.ContentState
		if err := rows.Scan(
			&state.FamilyID,
			&state.ContentKey,
			&state.Version,
			&state.ConflictCount,
			&state.SyncState,
			&state.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan content state: %w", err)
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content states: %w", err)
	}
	return states, nil
}

// AdminStats returns a bounded fleet summary over the requested window.
func (r *PostgresRepository) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := domain.AdminStats{WindowDays: windowDays}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM device_groups),
			(SELECT COUNT(DISTINCT device_id) FROM device_group_members),
			(SELECT COUNT(*) FROM device_messages
				WHERE created_at >= NOW() - ($1::int * INTERVAL '1 day')),
			(SELECT COUNT(*) FROM content_sync_states WHERE sync_state = 'applied'),
			(SELECT COALESCE(SUM(conflict_count), 0) FROM content_sync_states)
	`, windowDays).Scan(
		&stats.GroupCount,
		&stats.DeviceCount,
		&stats.MessageCount,
		&stats.SyncedContentCount,
		&stats.ConflictCount,
	); err != nil {
		return domain.AdminStats{}, fmt.Errorf("read device mesh stats: %w", err)
	}

	scopeBuckets, err := r.messageBuckets(ctx, windowDays)
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.ScopeBuckets = scopeBuckets

	stateBuckets, err := r.contentBuckets(ctx)
	if err != nil {
		return domain.AdminStats{}, err
	}
	stats.StateBuckets = stateBuckets
	stats.GeneratedAt = r.clock.Now().UTC()
	return stats, nil
}

// messageBuckets returns the message histogram by scope over the window.
func (r *PostgresRepository) messageBuckets(
	ctx context.Context,
	windowDays int,
) ([]domain.Count, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT scope AS bucket, COUNT(*)
		FROM device_messages
		WHERE created_at >= NOW() - ($1::int * INTERVAL '1 day')
		GROUP BY bucket
		ORDER BY bucket
	`, windowDays)
	if err != nil {
		return nil, fmt.Errorf("read device message buckets: %w", err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 4)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan device message bucket: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device message buckets: %w", err)
	}
	return buckets, nil
}

// contentBuckets returns the content histogram by sync state.
func (r *PostgresRepository) contentBuckets(
	ctx context.Context,
) ([]domain.Count, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT sync_state AS bucket, COUNT(*)
		FROM content_sync_states
		GROUP BY bucket
		ORDER BY bucket
	`)
	if err != nil {
		return nil, fmt.Errorf("read content buckets: %w", err)
	}
	defer rows.Close()
	buckets := make([]domain.Count, 0, 4)
	for rows.Next() {
		var bucket domain.Count
		if err := rows.Scan(&bucket.Label, &bucket.Count); err != nil {
			return nil, fmt.Errorf("scan content bucket: %w", err)
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content buckets: %w", err)
	}
	return buckets, nil
}

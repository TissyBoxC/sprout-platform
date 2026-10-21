package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/devicechild/domain"
)

// Repository stores device-to-child associations in Postgres.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates the repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Bind makes childID the device's active child.
//
// It runs in one transaction because two statements must agree: the previous
// active row has to be closed before the new one opens, or the partial unique
// index would reject the insert. A concurrent switch on the same device
// serializes on that index, so the last writer wins instead of leaving two
// active rows.
func (repository *Repository) Bind(
	ctx context.Context,
	request domain.Request,
) (domain.Binding, error) {
	if err := request.Validate(); err != nil {
		return domain.Binding{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.Binding{}, fmt.Errorf("begin bind transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// The device must belong to the family, and the child must belong to the
	// same family. Checking both here keeps a guardian from attaching another
	// household's child by guessing an id.
	if err := assertOwnership(ctx, tx, request); err != nil {
		return domain.Binding{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE device_child_bindings
		SET is_active = FALSE,
		    deactivated_at = NOW(),
		    updated_at = NOW()
		WHERE device_id = $1 AND is_active AND child_id <> $2
	`, request.DeviceID, request.ChildID); err != nil {
		return domain.Binding{}, fmt.Errorf("close previous binding: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO device_child_bindings (
			id, device_id, child_id, family_id, is_active, activated_at, updated_at
		) VALUES (
			gen_random_uuid(), $1, $2, $3, TRUE, NOW(), NOW()
		)
		ON CONFLICT (device_id, child_id) DO UPDATE
		SET is_active = TRUE,
		    family_id = EXCLUDED.family_id,
		    activated_at = NOW(),
		    deactivated_at = NULL,
		    updated_at = NOW()
	`, request.DeviceID, request.ChildID, request.FamilyID); err != nil {
		return domain.Binding{}, fmt.Errorf("activate child binding: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Binding{}, fmt.Errorf("commit bind transaction: %w", err)
	}
	return repository.active(ctx, request.DeviceID)
}

// assertOwnership verifies the device and the child share one family.
func assertOwnership(
	ctx context.Context,
	tx pgx.Tx,
	request domain.Request,
) error {
	var deviceFamily string
	if err := tx.QueryRow(ctx, `
		SELECT parent_account_id::text FROM device_bindings WHERE device_id = $1
	`, request.DeviceID).Scan(&deviceFamily); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// An unknown device is reported as "not in family" so the caller
			// cannot enumerate bound devices through this endpoint.
			return domain.ErrChildNotInFamily
		}
		return fmt.Errorf("read device owner: %w", err)
	}
	if deviceFamily != request.FamilyID {
		return domain.ErrChildNotInFamily
	}
	var childFamily string
	if err := tx.QueryRow(ctx, `
		SELECT family_id::text FROM child_profiles WHERE id = $1
	`, request.ChildID).Scan(&childFamily); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrChildNotInFamily
		}
		return fmt.Errorf("read child owner: %w", err)
	}
	if childFamily != request.FamilyID {
		return domain.ErrChildNotInFamily
	}
	return nil
}

// active reads the device's current binding.
func (repository *Repository) active(
	ctx context.Context,
	deviceID string,
) (domain.Binding, error) {
	var binding domain.Binding
	err := repository.pool.QueryRow(ctx, `
		SELECT device_id, family_id::text, child_id::text, activated_at
		FROM device_child_bindings
		WHERE device_id = $1 AND is_active
	`, deviceID).Scan(
		&binding.DeviceID,
		&binding.FamilyID,
		&binding.ChildID,
		&binding.ActivatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Binding{}, domain.ErrNoActiveChild
		}
		return domain.Binding{}, fmt.Errorf("read active binding: %w", err)
	}
	return binding, nil
}

// Resolve returns the device's active child and why there is none when there is
// not, so callers can distinguish a fresh device from an unselected one.
//
// A device whose active child was deleted is treated as unassigned rather than
// erroring: the guardian's next device use should offer a child picker, not a
// broken screen.
func (repository *Repository) Resolve(
	ctx context.Context,
	deviceID string,
) (domain.Resolution, error) {
	var familyID string
	if err := repository.pool.QueryRow(ctx, `
		SELECT parent_account_id::text FROM device_bindings WHERE device_id = $1
	`, deviceID).Scan(&familyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Resolution{}, domain.ErrDeviceMissing
		}
		return domain.Resolution{}, fmt.Errorf("read device owner: %w", err)
	}

	var childCount int
	if err := repository.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM child_profiles WHERE family_id = $1
	`, familyID).Scan(&childCount); err != nil {
		return domain.Resolution{}, fmt.Errorf("count family children: %w", err)
	}

	// The join keeps a binding pointing at a deleted child from being reported
	// as assigned.
	var childID string
	err := repository.pool.QueryRow(ctx, `
		SELECT b.child_id::text
		FROM device_child_bindings b
		JOIN child_profiles c ON c.id = b.child_id
		WHERE b.device_id = $1 AND b.is_active
	`, deviceID).Scan(&childID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.Resolution{}, fmt.Errorf("read active binding: %w", err)
	}

	if childID != "" {
		return domain.Resolution{
			State:      domain.ResolveAssigned,
			ChildID:    childID,
			ChildCount: childCount,
		}, nil
	}
	if childCount == 0 {
		return domain.Resolution{State: domain.ResolveNoChild}, nil
	}
	return domain.Resolution{
		State:      domain.ResolveUnassigned,
		ChildCount: childCount,
	}, nil
}

// CountBindings reports how many active bindings exist.
//
// It exists so the Feature Center health probe can prove the table is readable
// without listing any family's children.
func (repository *Repository) CountBindings(ctx context.Context) (int, error) {
	var count int
	if err := repository.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM device_child_bindings WHERE is_active
	`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count device child bindings: %w", err)
	}
	return count, nil
}

// Unbind removes the device's active child without deleting its history.
func (repository *Repository) Unbind(
	ctx context.Context,
	deviceID string,
) error {
	tag, err := repository.pool.Exec(ctx, `
		UPDATE device_child_bindings
		SET is_active = FALSE,
		    deactivated_at = NOW(),
		    updated_at = NOW()
		WHERE device_id = $1 AND is_active
	`, deviceID)
	if err != nil {
		return fmt.Errorf("unbind device child: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNoActiveChild
	}
	return nil
}

// History lists every child this device has served, newest first.
func (repository *Repository) History(
	ctx context.Context,
	deviceID string,
) ([]domain.History, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT child_id::text, is_active, activated_at, deactivated_at
		FROM device_child_bindings
		WHERE device_id = $1
		ORDER BY is_active DESC, activated_at DESC
	`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("list device child history: %w", err)
	}
	defer rows.Close()

	history := make([]domain.History, 0, 4)
	for rows.Next() {
		var entry domain.History
		if err := rows.Scan(
			&entry.ChildID,
			&entry.IsActive,
			&entry.ActivatedAt,
			&entry.DeactivatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan device child history: %w", err)
		}
		history = append(history, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device child history: %w", err)
	}
	return history, nil
}

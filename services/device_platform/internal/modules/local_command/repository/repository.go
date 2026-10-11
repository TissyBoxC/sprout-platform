// Package repository persists guardian-published local command macros.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for local commands.
type Repository interface {
	ListCommands(ctx context.Context, familyID string) ([]domain.Command, error)
	// ReplaceCommands atomically swaps a family's whole command set and bumps
	// the family revision. The read-modify-write happens in one transaction so
	// a device never observes a partially replaced set.
	ReplaceCommands(
		ctx context.Context,
		familyID string,
		commands []domain.Command,
	) (int, error)
	// DeleteCommand removes one command and bumps the revision. A missing id
	// returns ErrCommandNotFound.
	DeleteCommand(
		ctx context.Context,
		familyID string,
		commandID string,
	) (int, error)
	// Revision returns the family's current command revision, or zero when the
	// family has never published a set.
	Revision(ctx context.Context, familyID string) (int, error)
}

// PostgresRepository is the PostgreSQL-backed local command repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a local command repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// ListCommands returns a family's commands in publish order.
func (r *PostgresRepository) ListCommands(
	ctx context.Context,
	familyID string,
) ([]domain.Command, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT command_id, phrase, action, argument, enabled
		FROM local_commands
		WHERE family_id = $1
		ORDER BY position, command_id
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list local commands: %w", err)
	}
	defer rows.Close()
	commands := make([]domain.Command, 0, 16)
	for rows.Next() {
		var command domain.Command
		if err := rows.Scan(
			&command.CommandID,
			&command.Phrase,
			&command.Action,
			&command.Argument,
			&command.Enabled,
		); err != nil {
			return nil, fmt.Errorf("scan local command: %w", err)
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate local commands: %w", err)
	}
	revision, err := r.Revision(ctx, familyID)
	if err != nil {
		return nil, err
	}
	for index := range commands {
		commands[index].Revision = revision
	}
	return commands, nil
}

// ReplaceCommands atomically replaces a family's command set.
func (r *PostgresRepository) ReplaceCommands(
	ctx context.Context,
	familyID string,
	commands []domain.Command,
) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin local command replace: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(
		ctx,
		`DELETE FROM local_commands WHERE family_id = $1`,
		familyID,
	); err != nil {
		return 0, fmt.Errorf("clear local commands: %w", err)
	}
	for index := range commands {
		if _, err := tx.Exec(ctx, `
			INSERT INTO local_commands (
				family_id,
				command_id,
				phrase,
				action,
				argument,
				enabled,
				position
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`,
			familyID,
			commands[index].CommandID,
			commands[index].Phrase,
			commands[index].Action,
			commands[index].Argument,
			commands[index].Enabled,
			index,
		); err != nil {
			return 0, fmt.Errorf("upsert local command: %w", err)
		}
	}
	revision, err := bumpRevision(ctx, tx, familyID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit local command replace: %w", err)
	}
	return revision, nil
}

// DeleteCommand removes one command and bumps the revision.
func (r *PostgresRepository) DeleteCommand(
	ctx context.Context,
	familyID string,
	commandID string,
) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin local command delete: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		DELETE FROM local_commands
		WHERE family_id = $1 AND command_id = $2
	`, familyID, commandID)
	if err != nil {
		return 0, fmt.Errorf("delete local command: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, domain.ErrCommandNotFound
	}
	revision, err := bumpRevision(ctx, tx, familyID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit local command delete: %w", err)
	}
	return revision, nil
}

// Revision returns the family's current command revision.
func (r *PostgresRepository) Revision(
	ctx context.Context,
	familyID string,
) (int, error) {
	var revision int
	err := r.pool.QueryRow(ctx, `
		SELECT revision FROM local_command_revisions WHERE family_id = $1
	`, familyID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read local command revision: %w", err)
	}
	return revision, nil
}

// bumpRevision increments the family revision and returns the new value.
func bumpRevision(
	ctx context.Context,
	tx pgx.Tx,
	familyID string,
) (int, error) {
	var revision int
	err := tx.QueryRow(ctx, `
		INSERT INTO local_command_revisions (family_id, revision, updated_at)
		VALUES ($1, 1, NOW())
		ON CONFLICT (family_id)
		DO UPDATE SET revision = local_command_revisions.revision + 1,
			updated_at = NOW()
		RETURNING revision
	`, familyID).Scan(&revision)
	if err != nil {
		return 0, fmt.Errorf("bump local command revision: %w", err)
	}
	return revision, nil
}

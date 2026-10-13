// Package repository persists family membership and invitations.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for family membership.
type Repository interface {
	CreateMember(ctx context.Context, member domain.Member) error
	UpdateMemberRole(ctx context.Context, member domain.Member) error
	DeleteMember(ctx context.Context, familyID string, userID string) error
	Members(ctx context.Context, familyID string) ([]domain.Member, error)
	Member(ctx context.Context, familyID string, userID string) (domain.Member, bool, error)

	CreateInvitation(ctx context.Context, invitation domain.Invitation) error
	Invitation(
		ctx context.Context,
		familyID string,
		invitationID string,
	) (domain.Invitation, bool, error)
	Invitations(ctx context.Context, familyID string) ([]domain.Invitation, error)
	UpdateInvitationState(
		ctx context.Context,
		familyID string,
		invitationID string,
		status string,
	) error
}

// PostgresRepository is the PostgreSQL-backed family repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a family repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// CreateMember inserts one family member.
func (r *PostgresRepository) CreateMember(
	ctx context.Context,
	member domain.Member,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO family_members (
			id, family_id, user_id, role, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`,
		member.ID,
		member.FamilyID,
		member.UserID,
		member.Role,
		member.CreatedAt,
		member.UpdatedAt,
	)
	if err != nil {
		// A duplicate membership is a first-class conflict, not a driver error,
		// so the service can converge instead of retrying blindly.
		if isUniqueViolation(err) {
			return domain.ErrMemberExists
		}
		return fmt.Errorf("insert family member: %w", err)
	}
	return nil
}

// UpdateMemberRole changes one member's role within a family.
func (r *PostgresRepository) UpdateMemberRole(
	ctx context.Context,
	member domain.Member,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE family_members
		SET role = $3, updated_at = $4
		WHERE family_id = $1 AND user_id = $2
	`,
		member.FamilyID,
		member.UserID,
		member.Role,
		member.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrLastOwner
		}
		return fmt.Errorf("update family member role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

// DeleteMember removes one member within a family.
func (r *PostgresRepository) DeleteMember(
	ctx context.Context,
	familyID string,
	userID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM family_members
		WHERE family_id = $1 AND user_id = $2
	`, familyID, userID)
	if err != nil {
		return fmt.Errorf("delete family member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

// Members returns every member in a family, oldest first, so the owner is the
// row the members list shows first.
func (r *PostgresRepository) Members(
	ctx context.Context,
	familyID string,
) ([]domain.Member, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, family_id, user_id, role, created_at, updated_at
		FROM family_members
		WHERE family_id = $1
		ORDER BY created_at ASC, user_id ASC
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list family members: %w", err)
	}
	defer rows.Close()
	members := make([]domain.Member, 0, 8)
	for rows.Next() {
		var member domain.Member
		if err := rows.Scan(
			&member.ID,
			&member.FamilyID,
			&member.UserID,
			&member.Role,
			&member.CreatedAt,
			&member.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan family member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate family members: %w", err)
	}
	return members, nil
}

// Member loads one member scoped to its family.
func (r *PostgresRepository) Member(
	ctx context.Context,
	familyID string,
	userID string,
) (domain.Member, bool, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, family_id, user_id, role, created_at, updated_at
		FROM family_members
		WHERE family_id = $1 AND user_id = $2
	`, familyID, userID)
	var member domain.Member
	err := row.Scan(
		&member.ID,
		&member.FamilyID,
		&member.UserID,
		&member.Role,
		&member.CreatedAt,
		&member.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Member{}, false, nil
	}
	if err != nil {
		return domain.Member{}, false, fmt.Errorf("read family member: %w", err)
	}
	return member, true, nil
}

// CreateInvitation inserts one pending invitation.
func (r *PostgresRepository) CreateInvitation(
	ctx context.Context,
	invitation domain.Invitation,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO family_invitations (
			id, family_id, email, role, status, invited_by_user_id,
			created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		invitation.ID,
		invitation.FamilyID,
		domain.NormalizeEmail(invitation.Email),
		invitation.Role,
		invitation.Status,
		invitation.InvitedByUserID,
		invitation.CreatedAt,
		invitation.ExpiresAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrInvitationState
		}
		return fmt.Errorf("insert family invitation: %w", err)
	}
	return nil
}

// Invitation loads one invitation scoped to its family.
func (r *PostgresRepository) Invitation(
	ctx context.Context,
	familyID string,
	invitationID string,
) (domain.Invitation, bool, error) {
	row := r.pool.QueryRow(ctx, invitationSelect+`
		WHERE family_id = $1 AND id = $2
	`, familyID, invitationID)
	invitation, err := scanInvitation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Invitation{}, false, nil
	}
	if err != nil {
		return domain.Invitation{}, false, fmt.Errorf("read family invitation: %w", err)
	}
	return invitation, true, nil
}

// Invitations returns every invitation in a family, newest first.
func (r *PostgresRepository) Invitations(
	ctx context.Context,
	familyID string,
) ([]domain.Invitation, error) {
	rows, err := r.pool.Query(ctx, invitationSelect+`
		WHERE family_id = $1
		ORDER BY created_at DESC
	`, familyID)
	if err != nil {
		return nil, fmt.Errorf("list family invitations: %w", err)
	}
	defer rows.Close()
	invitations := make([]domain.Invitation, 0, 8)
	for rows.Next() {
		invitation, err := scanInvitation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan family invitation: %w", err)
		}
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate family invitations: %w", err)
	}
	return invitations, nil
}

// UpdateInvitationState moves one invitation to a new lifecycle state. Only a
// currently pending invitation can change, so an accepted or revoked invite can
// never be replayed.
func (r *PostgresRepository) UpdateInvitationState(
	ctx context.Context,
	familyID string,
	invitationID string,
	status string,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE family_invitations
		SET status = $3,
		    accepted_at = CASE WHEN $3 = 'accepted' THEN NOW() ELSE accepted_at END
		WHERE family_id = $1 AND id = $2 AND status = 'pending'
	`, familyID, invitationID, status)
	if err != nil {
		return fmt.Errorf("update family invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInvitationState
	}
	return nil
}

const invitationSelect = `
	SELECT id, family_id, email, role, status, invited_by_user_id,
		created_at, expires_at, accepted_at
	FROM family_invitations
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInvitation(row rowScanner) (domain.Invitation, error) {
	var invitation domain.Invitation
	var acceptedAt *time.Time
	if err := row.Scan(
		&invitation.ID,
		&invitation.FamilyID,
		&invitation.Email,
		&invitation.Role,
		&invitation.Status,
		&invitation.InvitedByUserID,
		&invitation.CreatedAt,
		&invitation.ExpiresAt,
		&acceptedAt,
	); err != nil {
		return domain.Invitation{}, err
	}
	invitation.AcceptedAt = acceptedAt
	return invitation, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique constraint
// violation without importing driver-specific error types.
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// Package repository persists family membership and invitations.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository defines persistence operations for family membership.
type Repository interface {
	CreateMember(ctx context.Context, member domain.Member) error
	UpdateMemberRole(ctx context.Context, member domain.Member) error
	DeleteMember(ctx context.Context, familyID string, userID string) error
	UpdateMemberRoleWithAudit(
		ctx context.Context,
		member domain.Member,
		audit domain.AuditEvent,
	) error
	DeleteMemberWithAudit(
		ctx context.Context,
		familyID string,
		userID string,
		audit domain.AuditEvent,
	) error
	Members(ctx context.Context, familyID string) ([]domain.Member, error)
	Member(ctx context.Context, familyID string, userID string) (domain.Member, bool, error)
	MemberByUser(ctx context.Context, userID string) (domain.Member, bool, error)

	CreateInvitation(
		ctx context.Context,
		invitation domain.Invitation,
		audit domain.AuditEvent,
	) error
	Invitation(
		ctx context.Context,
		familyID string,
		invitationID string,
	) (domain.Invitation, bool, error)
	Invitations(ctx context.Context, familyID string) ([]domain.Invitation, error)
	PendingInvitationsByEmail(
		ctx context.Context,
		email string,
	) ([]domain.Invitation, error)
	UpdateInvitationState(
		ctx context.Context,
		familyID string,
		invitationID string,
		status string,
	) error
	UpdateInvitationStateWithAudit(
		ctx context.Context,
		familyID string,
		invitationID string,
		status string,
		audit domain.AuditEvent,
	) error
	AcceptInvitation(
		ctx context.Context,
		member domain.Member,
		familyID string,
		invitationID string,
		audit domain.AuditEvent,
	) error
	ParentIdentity(
		ctx context.Context,
		accountID string,
	) (domain.AccountIdentity, error)
	AccountIdentities(
		ctx context.Context,
		accountIDs []string,
	) (map[string]domain.AccountIdentity, error)
	MembershipRevision(
		ctx context.Context,
		familyID string,
	) (int64, error)
}

// PostgresRepository is the PostgreSQL-backed family repository.
type PostgresRepository struct {
	pool familyDatabase
}

type familyDatabase interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// NewPostgresRepository creates a family repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func newRepositoryWithDatabase(pool familyDatabase) *PostgresRepository {
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

// UpdateMemberRoleWithAudit changes one member role and appends its security
// audit record in the same transaction. A failed audit rolls the role back.
func (r *PostgresRepository) UpdateMemberRoleWithAudit(
	ctx context.Context,
	member domain.Member,
	audit domain.AuditEvent,
) error {
	return r.UpdateMemberRoleWithAuditRevision(
		ctx,
		member,
		audit,
		-1,
	)
}

// UpdateMemberRoleWithAuditRevision updates a role, writes its audit, and
// advances the family membership revision in one transaction. expectedRevision
// < 0 disables the revision check for the guardian self-service path.
func (r *PostgresRepository) UpdateMemberRoleWithAuditRevision(
	ctx context.Context,
	member domain.Member,
	audit domain.AuditEvent,
	expectedRevision int64,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin family role audit transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()
	tag, err := transaction.Exec(ctx, `
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
	if err := insertAudit(ctx, transaction, audit); err != nil {
		return err
	}
	if expectedRevision >= 0 {
		if err := advanceMembershipRevision(
			ctx,
			transaction,
			member.FamilyID,
			expectedRevision,
		); err != nil {
			return err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit family role audit transaction: %w", err)
	}
	return nil
}

// DeleteMemberWithAudit removes one member and appends its security audit
// record in the same transaction.
func (r *PostgresRepository) DeleteMemberWithAudit(
	ctx context.Context,
	familyID string,
	userID string,
	audit domain.AuditEvent,
) error {
	return r.DeleteMemberWithAuditRevision(
		ctx,
		familyID,
		userID,
		audit,
		-1,
	)
}

// DeleteMemberWithAuditRevision removes a member, writes its audit, and
// advances the family membership revision atomically.
func (r *PostgresRepository) DeleteMemberWithAuditRevision(
	ctx context.Context,
	familyID string,
	userID string,
	audit domain.AuditEvent,
	expectedRevision int64,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin family removal audit transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()
	tag, err := transaction.Exec(ctx, `
		DELETE FROM family_members
		WHERE family_id = $1 AND user_id = $2
	`, familyID, userID)
	if err != nil {
		return fmt.Errorf("delete family member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMemberNotFound
	}
	if err := insertAudit(ctx, transaction, audit); err != nil {
		return err
	}
	if expectedRevision >= 0 {
		if err := advanceMembershipRevision(
			ctx,
			transaction,
			familyID,
			expectedRevision,
		); err != nil {
			return err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit family removal audit transaction: %w", err)
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

// MemberByUser loads the membership for one account without knowing its family
// in advance. A platform account belongs to at most one family today, so this
// resolves the acting family for an authenticated request.
func (r *PostgresRepository) MemberByUser(
	ctx context.Context,
	userID string,
) (domain.Member, bool, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, family_id, user_id, role, created_at, updated_at
		FROM family_members
		WHERE user_id = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, userID)
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
		return domain.Member{}, false, fmt.Errorf("read family member by user: %w", err)
	}
	return member, true, nil
}

// ParentIdentity loads one parent account's safe management projection. It is
// intentionally limited to identity, display name and activation state so the
// family module never exposes credentials or contact details.
func (r *PostgresRepository) ParentIdentity(
	ctx context.Context,
	accountID string,
) (domain.AccountIdentity, error) {
	var identity domain.AccountIdentity
	if err := r.pool.QueryRow(ctx, `
		SELECT id, display_name, role, status
		FROM parent_accounts
		WHERE id = $1
	`, accountID).Scan(
		&identity.AccountID,
		&identity.DisplayName,
		&identity.Role,
		&identity.Status,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AccountIdentity{}, domain.ErrAccountNotFound
		}
		return domain.AccountIdentity{}, fmt.Errorf("read parent identity: %w", err)
	}
	return identity, nil
}

// AccountIdentities loads safe display projections for a bounded set of
// member accounts. Missing accounts are omitted so callers can keep the stable
// member row and show only its account ID.
func (r *PostgresRepository) AccountIdentities(
	ctx context.Context,
	accountIDs []string,
) (map[string]domain.AccountIdentity, error) {
	identities := make(map[string]domain.AccountIdentity, len(accountIDs))
	if len(accountIDs) == 0 {
		return identities, nil
	}
	if len(accountIDs) > 200 {
		return nil, domain.ErrInvalidMember
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, display_name, role, status
		FROM parent_accounts
		WHERE id = ANY($1::uuid[])
	`, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("list parent identities: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var identity domain.AccountIdentity
		if err := rows.Scan(
			&identity.AccountID,
			&identity.DisplayName,
			&identity.Role,
			&identity.Status,
		); err != nil {
			return nil, fmt.Errorf("scan parent identity: %w", err)
		}
		identities[identity.AccountID] = identity
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate parent identities: %w", err)
	}
	return identities, nil
}

// MembershipRevision returns the family's current membership revision. An
// absent row is the initial revision 1; the row is created on the first write
// together with the expected-revision check.
func (r *PostgresRepository) MembershipRevision(
	ctx context.Context,
	familyID string,
) (int64, error) {
	var revision int64
	err := r.pool.QueryRow(ctx, `
		SELECT revision
		FROM family_membership_revisions
		WHERE family_id = $1
	`, familyID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read family membership revision: %w", err)
	}
	return revision, nil
}

// CreateInvitation inserts one pending invitation.
func (r *PostgresRepository) CreateInvitation(
	ctx context.Context,
	invitation domain.Invitation,
	audit domain.AuditEvent,
) error {
	return r.CreateInvitationRevision(ctx, invitation, audit, -1)
}

// CreateInvitationRevision inserts an invitation and its audit and, when
// expectedRevision is non-negative, checks and advances the shared membership
// revision in the same transaction.
func (r *PostgresRepository) CreateInvitationRevision(
	ctx context.Context,
	invitation domain.Invitation,
	audit domain.AuditEvent,
	expectedRevision int64,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin family invitation audit transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()
	_, err = transaction.Exec(ctx, `
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
	if err := insertAudit(ctx, transaction, audit); err != nil {
		return err
	}
	if expectedRevision >= 0 {
		if err := advanceMembershipRevision(
			ctx,
			transaction,
			invitation.FamilyID,
			expectedRevision,
		); err != nil {
			return err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit family invitation audit transaction: %w", err)
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

// PendingInvitationsByEmail lists every pending, unexpired invitation addressed
// to one email across all families. It is scoped by the normalized address, so
// an authenticated account can discover an invitation before it has any
// membership and therefore no family id to query with.
func (r *PostgresRepository) PendingInvitationsByEmail(
	ctx context.Context,
	email string,
) ([]domain.Invitation, error) {
	normalized := domain.NormalizeEmail(email)
	if normalized == "" {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, invitationSelect+`
		WHERE lower(email) = $1
		  AND status = 'pending'
		  AND expires_at > NOW()
		ORDER BY created_at DESC
	`, normalized)
	if err != nil {
		return nil, fmt.Errorf("list pending family invitations by email: %w", err)
	}
	defer rows.Close()
	invitations := make([]domain.Invitation, 0, 4)
	for rows.Next() {
		invitation, err := scanInvitation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pending family invitation: %w", err)
		}
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending family invitations: %w", err)
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
	return r.updateInvitationState(ctx, familyID, invitationID, status, nil)
}

// UpdateInvitationStateWithAudit moves one invitation to a new lifecycle state
// and writes its audit record in the same transaction. The audit failure rolls
// back the state change, which prevents an operator action from succeeding
// without a security record.
func (r *PostgresRepository) UpdateInvitationStateWithAudit(
	ctx context.Context,
	familyID string,
	invitationID string,
	status string,
	audit domain.AuditEvent,
) error {
	return r.updateInvitationState(ctx, familyID, invitationID, status, &audit)
}

// UpdateInvitationStateWithAuditRevision is the admin revoke write. It checks
// the expected revision, records the audit and advances the revision in the
// same transaction as the invitation state change.
func (r *PostgresRepository) UpdateInvitationStateWithAuditRevision(
	ctx context.Context,
	familyID string,
	invitationID string,
	status string,
	audit domain.AuditEvent,
	expectedRevision int64,
) error {
	return r.updateInvitationStateRevision(
		ctx,
		familyID,
		invitationID,
		status,
		&audit,
		expectedRevision,
	)
}

func (r *PostgresRepository) updateInvitationState(
	ctx context.Context,
	familyID string,
	invitationID string,
	status string,
	audit *domain.AuditEvent,
) error {
	return r.updateInvitationStateRevision(
		ctx,
		familyID,
		invitationID,
		status,
		audit,
		-1,
	)
}

func (r *PostgresRepository) updateInvitationStateRevision(
	ctx context.Context,
	familyID string,
	invitationID string,
	status string,
	audit *domain.AuditEvent,
	expectedRevision int64,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke family invitation: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()
	tag, err := transaction.Exec(ctx, `
		UPDATE family_invitations
		SET status = $3,
		    accepted_at = CASE WHEN $3 = 'accepted' THEN NOW() ELSE accepted_at END
		WHERE family_id = $1
		  AND id = $2
		  AND status = 'pending'
		  AND expires_at > NOW()
	`, familyID, invitationID, status)
	if err != nil {
		return fmt.Errorf("update family invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return classifyFailedInvitationUpdate(ctx, transaction, familyID, invitationID)
	}
	if audit != nil {
		if err := insertAudit(ctx, transaction, *audit); err != nil {
			return err
		}
	}
	if expectedRevision >= 0 {
		if err := advanceMembershipRevision(
			ctx,
			transaction,
			familyID,
			expectedRevision,
		); err != nil {
			return err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke family invitation: %w", err)
	}
	return nil
}

// AcceptInvitation consumes a pending invitation and creates its membership in
// one transaction. The conditional update checks both lifecycle and expiry so
// an expired invitation cannot be accepted even when the initial read raced
// with expiry. A concurrent or repeated acceptance writes no membership.
func (r *PostgresRepository) AcceptInvitation(
	ctx context.Context,
	member domain.Member,
	familyID string,
	invitationID string,
	audit domain.AuditEvent,
) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin accept family invitation: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	tag, err := transaction.Exec(ctx, `
		UPDATE family_invitations
		SET status = 'accepted',
		    accepted_at = NOW()
		WHERE family_id = $1
		  AND id = $2
		  AND status = 'pending'
		  AND expires_at > NOW()
	`, familyID, invitationID)
	if err != nil {
		return fmt.Errorf("consume family invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return classifyFailedInvitationUpdate(ctx, transaction, familyID, invitationID)
	}
	if _, err := transaction.Exec(ctx, `
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
	); err != nil {
		if isUniqueViolation(err) {
			return domain.ErrMemberExists
		}
		return fmt.Errorf("insert accepted family member: %w", err)
	}
	if err := insertAudit(ctx, transaction, audit); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit accepted family invitation: %w", err)
	}
	return nil
}

func classifyFailedInvitationUpdate(
	ctx context.Context,
	transaction pgx.Tx,
	familyID string,
	invitationID string,
) error {
	var status string
	var expired bool
	err := transaction.QueryRow(ctx, `
		SELECT status, expires_at <= NOW()
		FROM family_invitations
		WHERE family_id = $1 AND id = $2
		FOR UPDATE
	`, familyID, invitationID).Scan(&status, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrInvitationNotFound
	}
	if err != nil {
		return fmt.Errorf("classify family invitation update: %w", err)
	}
	if status == domain.InvitationPending && expired {
		return domain.ErrInvitationExpired
	}
	if status == domain.InvitationPending {
		return domain.ErrInvitationState
	}
	return domain.ErrInvitationConcurrent
}

// advanceMembershipRevision compares the client's expected revision with the
// persisted row and increments it. A missing row represents revision 1, so the
// first write with expectedRevision 1 creates revision 2. A stale expectation
// fails with ErrRevisionConflict and rolls the surrounding transaction back.
func advanceMembershipRevision(
	ctx context.Context,
	executor auditInserter,
	familyID string,
	expectedRevision int64,
) error {
	if expectedRevision < 1 {
		return domain.ErrRevisionConflict
	}
	tag, err := executor.Exec(ctx, `
		INSERT INTO family_membership_revisions (
			family_id, revision, updated_at
		) VALUES ($1, 2, NOW())
		ON CONFLICT (family_id) DO UPDATE
		SET revision = family_membership_revisions.revision + 1,
		    updated_at = NOW()
		WHERE family_membership_revisions.revision = $2
	`, familyID, expectedRevision)
	if err != nil {
		return fmt.Errorf("advance family membership revision: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRevisionConflict
	}
	return nil
}

type auditInserter interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func insertAudit(
	ctx context.Context,
	executor auditInserter,
	event domain.AuditEvent,
) error {
	if event.ID == "" || event.Action == "" || event.CreatedAt.IsZero() {
		return domain.ErrInvalidMember
	}
	detail, err := json.Marshal(event.Detail)
	if err != nil {
		return domain.ErrInvalidMember
	}
	if len(detail) > 1<<20 {
		return domain.ErrInvalidMember
	}
	if _, err := executor.Exec(ctx, `
		INSERT INTO platform_parent_audit (
			id, actor_account_id, target_account_id, action, detail, created_at
		) VALUES (
			$1,
			NULLIF($2, '')::uuid,
			NULLIF($3, '')::uuid,
			$4,
			$5::jsonb,
			$6
		)
	`,
		event.ID,
		event.ActorAccountID,
		event.TargetAccountID,
		event.Action,
		detail,
		event.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert family security audit: %w", err)
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

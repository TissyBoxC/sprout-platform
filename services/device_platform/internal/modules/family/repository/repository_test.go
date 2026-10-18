package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAcceptInvitationRequiresPendingAndUnexpired(t *testing.T) {
	database := &scriptedDatabase{
		transaction: &scriptedTransaction{
			execResults: []execResult{
				{rowsAffected: 0},
			},
			rowResults: []rowResult{
				{status: domain.InvitationAccepted},
			},
		},
	}
	repository := newRepositoryWithDatabase(database)
	err := repository.AcceptInvitation(
		context.Background(),
		testMember(),
		"family-1",
		"invitation-1",
		testAudit(domain.AuditInvitationAccepted),
	)
	if !errors.Is(err, domain.ErrInvitationConcurrent) {
		t.Fatalf("err = %v, want ErrInvitationConcurrent", err)
	}
	transaction := database.transaction
	if !strings.Contains(transaction.execCalls[0].sql, "status = 'pending'") {
		t.Fatalf("accept update lacks pending predicate: %s", transaction.execCalls[0].sql)
	}
	if !strings.Contains(transaction.execCalls[0].sql, "expires_at > NOW()") {
		t.Fatalf("accept update lacks expiry predicate: %s", transaction.execCalls[0].sql)
	}
	if !transaction.rolledBack {
		t.Fatal("failed accept must roll back the transaction")
	}
}

func TestAcceptInvitationClassifiesExpiredRace(t *testing.T) {
	database := &scriptedDatabase{
		transaction: &scriptedTransaction{
			execResults: []execResult{{rowsAffected: 0}},
			rowResults: []rowResult{
				{status: domain.InvitationPending, expired: true},
			},
		},
	}
	repository := newRepositoryWithDatabase(database)
	err := repository.AcceptInvitation(
		context.Background(),
		testMember(),
		"family-1",
		"invitation-1",
		testAudit(domain.AuditInvitationAccepted),
	)
	if !errors.Is(err, domain.ErrInvitationExpired) {
		t.Fatalf("err = %v, want ErrInvitationExpired", err)
	}
}

func TestAcceptInvitationMissingRaceIsStable(t *testing.T) {
	database := &scriptedDatabase{
		transaction: &scriptedTransaction{
			execResults: []execResult{{rowsAffected: 0}},
			rowResults:  []rowResult{{err: pgx.ErrNoRows}},
		},
	}
	repository := newRepositoryWithDatabase(database)
	err := repository.AcceptInvitation(
		context.Background(),
		testMember(),
		"family-1",
		"invitation-1",
		testAudit(domain.AuditInvitationAccepted),
	)
	if !errors.Is(err, domain.ErrInvitationNotFound) {
		t.Fatalf("err = %v, want ErrInvitationNotFound", err)
	}
}

func TestUpdateInvitationStateClassifiesExpiredInvitation(t *testing.T) {
	database := &scriptedDatabase{
		transaction: &scriptedTransaction{
			execResults: []execResult{{rowsAffected: 0}},
			rowResults: []rowResult{
				{status: domain.InvitationPending, expired: true},
			},
		},
	}
	repository := newRepositoryWithDatabase(database)
	err := repository.UpdateInvitationState(
		context.Background(),
		"family-1",
		"invitation-1",
		domain.InvitationRevoked,
	)
	if !errors.Is(err, domain.ErrInvitationExpired) {
		t.Fatalf("err = %v, want ErrInvitationExpired", err)
	}
	if !strings.Contains(database.transaction.execCalls[0].sql, "status = 'pending'") {
		t.Fatalf("revoke update lacks pending predicate: %s", database.transaction.execCalls[0].sql)
	}
	if !strings.Contains(database.transaction.execCalls[0].sql, "expires_at > NOW()") {
		t.Fatalf("revoke update lacks expiry predicate: %s", database.transaction.execCalls[0].sql)
	}
}

func TestUpdateInvitationStateClassifiesConcurrentInvitation(t *testing.T) {
	database := &scriptedDatabase{
		transaction: &scriptedTransaction{
			execResults: []execResult{{rowsAffected: 0}},
			rowResults: []rowResult{
				{status: domain.InvitationAccepted},
			},
		},
	}
	repository := newRepositoryWithDatabase(database)
	err := repository.UpdateInvitationState(
		context.Background(),
		"family-1",
		"invitation-1",
		domain.InvitationRevoked,
	)
	if !errors.Is(err, domain.ErrInvitationConcurrent) {
		t.Fatalf("err = %v, want ErrInvitationConcurrent", err)
	}
	if !database.transaction.rolledBack {
		t.Fatal("failed invitation state update must roll back")
	}
}

func TestAcceptInvitationCommitsInvitationMemberAndAuditTogether(t *testing.T) {
	database := &scriptedDatabase{
		transaction: &scriptedTransaction{
			execResults: []execResult{
				{rowsAffected: 1},
				{},
				{},
			},
			rowResults: []rowResult{
				{status: domain.InvitationAccepted},
			},
		},
	}
	repository := newRepositoryWithDatabase(database)
	err := repository.AcceptInvitation(
		context.Background(),
		testMember(),
		"family-1",
		"invitation-1",
		testAudit(domain.AuditInvitationAccepted),
	)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if len(database.transaction.execCalls) != 3 {
		t.Fatalf("accept exec calls = %d, want invitation + member + audit", len(database.transaction.execCalls))
	}
	if !database.transaction.committed {
		t.Fatal("successful accept must commit")
	}
}

func TestAcceptInvitationRollsBackWhenAuditInsertFails(t *testing.T) {
	database := &scriptedDatabase{
		transaction: &scriptedTransaction{
			execResults: []execResult{
				{rowsAffected: 1},
				{},
				{err: errors.New("audit write failed")},
			},
		},
	}
	repository := newRepositoryWithDatabase(database)
	err := repository.AcceptInvitation(
		context.Background(),
		testMember(),
		"family-1",
		"invitation-1",
		testAudit(domain.AuditInvitationAccepted),
	)
	if err == nil {
		t.Fatal("expected audit failure")
	}
	if database.transaction.committed {
		t.Fatal("audit failure must not commit")
	}
	if !database.transaction.rolledBack {
		t.Fatal("audit failure must roll back")
	}
}

func TestCreateInvitationRollsBackWhenAuditInsertFails(t *testing.T) {
	database := &scriptedDatabase{
		transaction: &scriptedTransaction{
			execResults: []execResult{
				{},
				{err: errors.New("audit write failed")},
			},
		},
	}
	repository := newRepositoryWithDatabase(database)
	err := repository.CreateInvitation(
		context.Background(),
		testInvitation(),
		testAudit(domain.AuditInvitationCreated),
	)
	if err == nil {
		t.Fatal("expected audit failure")
	}
	if database.transaction.committed {
		t.Fatal("audit failure must not commit the invitation")
	}
	if !database.transaction.rolledBack {
		t.Fatal("audit failure must roll back the invitation")
	}
}

func TestUpdateMemberRoleRollsBackWhenAuditInsertFails(t *testing.T) {
	database := &scriptedDatabase{
		transaction: &scriptedTransaction{
			execResults: []execResult{
				{rowsAffected: 1},
				{err: errors.New("audit write failed")},
			},
		},
	}
	repository := newRepositoryWithDatabase(database)
	err := repository.UpdateMemberRoleWithAudit(
		context.Background(),
		testMember(),
		testAudit(domain.AuditMemberRoleUpdated),
	)
	if err == nil {
		t.Fatal("expected audit failure")
	}
	if database.transaction.committed {
		t.Fatal("audit failure must not commit the role update")
	}
	if !database.transaction.rolledBack {
		t.Fatal("audit failure must roll back the role update")
	}
}

func TestRemoveMemberRollsBackWhenAuditInsertFails(t *testing.T) {
	database := &scriptedDatabase{
		transaction: &scriptedTransaction{
			execResults: []execResult{
				{rowsAffected: 1},
				{err: errors.New("audit write failed")},
			},
		},
	}
	repository := newRepositoryWithDatabase(database)
	err := repository.DeleteMemberWithAudit(
		context.Background(),
		"family-1",
		"user-1",
		testAudit(domain.AuditMemberRemoved),
	)
	if err == nil {
		t.Fatal("expected audit failure")
	}
	if database.transaction.committed {
		t.Fatal("audit failure must not commit the member removal")
	}
	if !database.transaction.rolledBack {
		t.Fatal("audit failure must roll back the member removal")
	}
}

type execResult struct {
	tag          pgconn.CommandTag
	err          error
	rowsAffected int64
}

type rowResult struct {
	status  string
	expired bool
	err     error
}

type execCall struct {
	sql  string
	args []any
}

type scriptedDatabase struct {
	transaction *scriptedTransaction
}

func (d *scriptedDatabase) Exec(
	context.Context,
	string,
	...any,
) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected pool exec")
}

func (d *scriptedDatabase) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected pool query")
}

func (d *scriptedDatabase) QueryRow(context.Context, string, ...any) pgx.Row {
	return failingRow{err: errors.New("unexpected pool query row")}
}

func (d *scriptedDatabase) Begin(context.Context) (pgx.Tx, error) {
	return d.transaction, nil
}

type scriptedTransaction struct {
	execResults []execResult
	execCalls   []execCall
	rowResults  []rowResult
	rolledBack  bool
	committed   bool
}

func (t *scriptedTransaction) Exec(
	_ context.Context,
	sql string,
	args ...any,
) (pgconn.CommandTag, error) {
	t.execCalls = append(t.execCalls, execCall{sql: sql, args: args})
	if len(t.execResults) == 0 {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	result := t.execResults[0]
	t.execResults = t.execResults[1:]
	if result.err != nil {
		return result.tag, result.err
	}
	if result.rowsAffected > 0 {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.NewCommandTag("UPDATE 0"), nil
}

func (t *scriptedTransaction) Query(
	context.Context,
	string,
	...any,
) (pgx.Rows, error) {
	return nil, errors.New("unexpected transaction query")
}

func (t *scriptedTransaction) QueryRow(
	context.Context,
	string,
	...any,
) pgx.Row {
	if len(t.rowResults) == 0 {
		return failingRow{err: errors.New("unexpected query row")}
	}
	result := t.rowResults[0]
	t.rowResults = t.rowResults[1:]
	return result
}

func (t *scriptedTransaction) Commit(context.Context) error {
	t.committed = true
	return nil
}

func (t *scriptedTransaction) Rollback(context.Context) error {
	t.rolledBack = true
	return nil
}

func (t *scriptedTransaction) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected prepare")
}

func (t *scriptedTransaction) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected nested transaction")
}

func (t *scriptedTransaction) CopyFrom(
	context.Context,
	pgx.Identifier,
	[]string,
	pgx.CopyFromSource,
) (int64, error) {
	return 0, errors.New("unexpected copy from")
}

func (t *scriptedTransaction) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (t *scriptedTransaction) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (t *scriptedTransaction) Conn() *pgx.Conn {
	return nil
}

type failingRow struct {
	err error
}

func (r failingRow) Scan(...any) error {
	return r.err
}

func (r rowResult) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 2 {
		return errors.New("unexpected destination count")
	}
	*dest[0].(*string) = r.status
	*dest[1].(*bool) = r.expired
	return nil
}

func testMember() domain.Member {
	now := time.Now().UTC()
	return domain.Member{
		ID:        uuid.NewString(),
		FamilyID:  "family-1",
		UserID:    "user-1",
		Role:      domain.RoleGuardian,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func testAudit(action string) domain.AuditEvent {
	return domain.AuditEvent{
		ID:              uuid.NewString(),
		ActorAccountID:  "user-1",
		TargetAccountID: "user-1",
		Action:          action,
		Detail:          map[string]any{"family_id": "family-1"},
		CreatedAt:       time.Now().UTC(),
	}
}

func testInvitation() domain.Invitation {
	now := time.Now().UTC()
	return domain.Invitation{
		ID:              uuid.NewString(),
		FamilyID:        "family-1",
		Email:           "guardian@example.com",
		Role:            domain.RoleGuardian,
		Status:          domain.InvitationPending,
		InvitedByUserID: "user-1",
		CreatedAt:       now,
		ExpiresAt:       now.Add(time.Hour),
	}
}

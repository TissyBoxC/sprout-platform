// Package repository persists the billing catalogue, orders, payments,
// refunds, subscriptions, reconciliation runs, spending consents, and the AI
// quota ledger.
//
// Boundary: this package owns SQL and PostgreSQL error classification only. It
// never decides whether money may move; the service layer decides that and
// passes an explicit transaction to every money-moving method. Amounts are
// 人民币分 as int64 (domain.Cents) and the provider quota is a decimal string,
// so no float64 is stored anywhere.
//
// Concurrency: every settlement, refund, and notification completion takes an
// explicit pgx.Tx owned by the caller, so the order row lock and the
// status/amount guards live in one transaction. Methods without a tx parameter
// are single autocommit statements.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrVersionConflict reports an optimistic-concurrency failure: the row was
	// changed by another writer after the caller read it.
	ErrVersionConflict = errors.New("billing repository: version conflict")
	// ErrAlreadySettled reports that an order was already settled or claimed
	// before this call, so crediting it again would double-credit a family.
	ErrAlreadySettled = errors.New("billing repository: order is already settled")
	// ErrRunAlreadyExists reports that a reconciliation run already occupies
	// the (channel, business_date) slot.
	ErrRunAlreadyExists = errors.New("billing repository: reconciliation run already exists")
	// ErrOrderStillPending reports that an order a channel settled is still
	// pending: the callback may simply be in flight, so it is not a discrepancy.
	ErrOrderStillPending = errors.New("billing repository: order is still pending")
	// ErrLedgerEntryNotFound reports an AI quota ledger id that no longer
	// exists, so a recovery step can report a bad id instead of a generic miss.
	ErrLedgerEntryNotFound = errors.New("billing repository: quota ledger entry not found")
	// ErrRunNotFound reports a reconciliation run id that no longer exists.
	ErrRunNotFound = errors.New("billing repository: reconciliation run not found")
	// ErrRefundNotFound reports a refund id or number with no row; the domain
	// package models order and plan absences but not refund ones.
	ErrRefundNotFound = errors.New("billing repository: refund not found")
	// ErrItemNotFound reports a reconciliation item id that no longer exists.
	ErrItemNotFound = errors.New("billing repository: reconciliation item not found")
	// ErrOperatorRequired reports a money-moving operator action without an
	// administrator identity. Approval is a human act and the schema requires
	// the approver, so the repository refuses before the database does.
	ErrOperatorRequired = errors.New("billing repository: operator account is required")
	// ErrPlanCodeTaken reports a plan insert whose business code is already
	// reserved. The code is unique across every status, so an archived plan
	// keeps it and a second insert must not reuse it.
	ErrPlanCodeTaken = errors.New("billing repository: plan code is already taken")
	// ErrEmptyStatement reports a valid channel response with no settled
	// entries. Comparing against it would report a balanced day for a day whose
	// money was never downloaded.
	ErrEmptyStatement = errors.New("billing repository: channel statement is empty")
)

// Querier is the statement surface shared by a pool and a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// database is what PostgresRepository needs from its connection: the statement
// surface plus transaction control. It exists so tests can script failures and
// transaction boundaries without a live PostgreSQL.
type database interface {
	Querier
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PostgresRepository is the PostgreSQL-backed billing repository.
type PostgresRepository struct {
	database database
}

// NewPostgresRepository creates a billing repository over a pgx pool.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{database: pool}
}

// NewRepositoryWithDatabase builds a repository over a scripted database.
//
// It is the seam repository tests use to drive transaction boundaries, row
// locks, and constraint failures without a live PostgreSQL.
func NewRepositoryWithDatabase(source database) *PostgresRepository {
	return &PostgresRepository{database: source}
}

// withTx runs fn inside one transaction and commits only when fn returns nil.
func (r *PostgresRepository) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return r.WithTx(ctx, fn)
}

// Begin starts a transaction for a caller that must run several repository
// methods atomically, for example settling a payment and writing its quota.
// The caller owns the result and must commit or roll it back.
func (r *PostgresRepository) Begin(ctx context.Context) (pgx.Tx, error) {
	transaction, err := r.database.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin billing transaction: %w", err)
	}
	return transaction, nil
}

// WithTx runs fn inside one transaction and commits only when fn returns nil,
// so a service can combine repository writes with its own settlement rules.
func (r *PostgresRepository) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	transaction, err := r.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin billing transaction: %w", err)
	}
	// Rollback after a successful commit is a no-op in pgx, so an early return
	// from fn can never leave the transaction open.
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	if err := fn(transaction); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit billing transaction: %w", err)
	}
	return nil
}

// normalizeUUID renders an empty UUID as NULL for a nullable UUID column.
//
// The domain models use an empty string where the schema allows NULL (a refund
// without an approving operator, a subscription whose plan was deleted). pgx
// would send "" as an invalid UUID, so every such argument passes through here.
func normalizeUUID(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "00000000-0000-0000-0000-000000000000" {
		return nil
	}
	return trimmed
}

// normalizePage bounds a listing window so a caller cannot ask for the whole
// table or an unbounded offset.
func normalizePage(limit int, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// isUniqueViolation reports a PostgreSQL unique-index conflict (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
}

// uniqueViolationOn reports a unique violation raised by one named index.
//
// The billing schema uses several unique indexes where the same SQLSTATE means
// something different (an idempotent callback replay, a double settlement, a
// duplicate reconciliation run), so callers match the index name instead of
// the code alone. A trigger-raised conflict may report no name; the caller
// decides whether that case is acceptable.
func uniqueViolationOn(err error, indexNames ...string) bool {
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "23505" {
		return false
	}
	for _, name := range indexNames {
		if pgError.ConstraintName == name {
			return true
		}
	}
	return false
}

// execExpectingRow runs a status-guarded UPDATE and reports whenMissing when the
// predicate matched nothing, so a lost status race is never mistaken for
// success. A caller that needs a different meaning per case reads the tag
// itself instead.
func execExpectingRow(
	ctx context.Context,
	querier Querier,
	whenMissing error,
	sql string,
	args ...any,
) error {
	tag, err := querier.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return whenMissing
	}
	return nil
}

// Page sizes for every paged billing listing.
const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// rowScanner is the Scan surface shared by pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(destinations ...any) error
}

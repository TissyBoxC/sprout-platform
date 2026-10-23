package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// scriptedDatabase is a pgx pool whose results each test scripts by hand.
//
// A repository is mostly a conversation with PostgreSQL: which statement runs,
// with which arguments, and what the row count means. Scripting that
// conversation is what lets a test prove the money guards — a settled order
// refusing a second payment, a stale version refusing an update — without a live
// server.
type scriptedDatabase struct {
	transaction *scriptedTransaction
	row         pgx.Row
	execTag     pgconn.CommandTag
	execErr     error
	execCalls   []execCall
	rows        []string
	rowsErr     error
}

// execCall records one statement and its arguments.
type execCall struct {
	sql  string
	args []any
}

// Exec returns the scripted pool-level result.
func (d *scriptedDatabase) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	d.execCalls = append(d.execCalls, execCall{sql: sql, args: args})
	if d.execErr != nil {
		return pgconn.CommandTag{}, d.execErr
	}
	return d.execTag, nil
}

// Query returns the scripted pool-level rows.
func (d *scriptedDatabase) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	d.execCalls = append(d.execCalls, execCall{sql: sql, args: args})
	if d.rowsErr != nil {
		return nil, d.rowsErr
	}
	return &scriptedRows{values: d.rows}, nil
}

// scriptedRows is a minimal pgx.Rows over one string column.
//
// It is enough for the row-returning statements the repository issues, which all
// return a single id or name column.
type scriptedRows struct {
	values  []string
	cursor  int
	closed  bool
	scanErr error
}

// Close marks the rows closed.
func (r *scriptedRows) Close() { r.closed = true }

// Err reports an iteration error.
func (r *scriptedRows) Err() error { return r.scanErr }

// CommandTag reports the tag of the statement that produced the rows.
func (r *scriptedRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }

// FieldDescriptions reports no column metadata.
func (r *scriptedRows) FieldDescriptions() []pgconn.FieldDescription { return nil }

// Next advances the cursor.
func (r *scriptedRows) Next() bool {
	if r.cursor >= len(r.values) {
		return false
	}
	r.cursor++
	return true
}

// Scan fills a single destination from the current row.
func (r *scriptedRows) Scan(destinations ...any) error {
	if r.cursor == 0 || r.cursor > len(r.values) {
		return errors.New("scripted rows: scan before next")
	}
	if len(destinations) != 1 {
		return fmt.Errorf("scripted rows: %d destinations, want 1", len(destinations))
	}
	return assignValue(destinations[0], r.values[r.cursor-1])
}

// Values reports the current row's values.
func (r *scriptedRows) Values() ([]any, error) {
	return nil, errors.New("scripted rows: values unsupported")
}

// RawValues reports the current row's raw values.
func (r *scriptedRows) RawValues() [][]byte { return nil }

// Conn reports that no dedicated connection is available.
func (r *scriptedRows) Conn() *pgx.Conn { return nil }

// QueryRow returns the scripted pool-level row.
func (d *scriptedDatabase) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if d.row != nil {
		// A single scripted row is consumed once, so a second read fails loudly
		// instead of silently returning the same row twice.
		row := d.row
		d.row = nil
		return row
	}
	return failingRow{err: errors.New("scripted database: unexpected pool query row")}
}

// Begin returns the scripted transaction.
func (d *scriptedDatabase) Begin(ctx context.Context) (pgx.Tx, error) {
	if d.transaction == nil {
		return nil, errors.New("scripted database: no transaction scripted")
	}
	return d.transaction, nil
}

// scriptedTransaction is one scripted pgx.Tx.
type scriptedTransaction struct {
	execResults []execResult
	execCalls   []execCall
	rowResults  []pgx.Row
	queryRowErr error
	committed   bool
	rolledBack  bool
}

// execResult is one scripted statement outcome.
type execResult struct {
	tag          pgconn.CommandTag
	rowsAffected int64
	err          error
}

// Exec records the call and returns the next scripted outcome.
func (t *scriptedTransaction) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execCalls = append(t.execCalls, execCall{sql: sql, args: args})
	if len(t.execResults) == 0 {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	result := t.execResults[0]
	t.execResults = t.execResults[1:]
	if result.err != nil {
		return pgconn.CommandTag{}, result.err
	}
	if result.tag != (pgconn.CommandTag{}) {
		return result.tag, nil
	}
	if result.rowsAffected > 0 {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.NewCommandTag("UPDATE 0"), nil
}

// Query returns the scripted rows.
func (t *scriptedTransaction) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, errors.New("scripted transaction: unexpected query")
}

// QueryRow returns the next scripted row.
func (t *scriptedTransaction) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if len(t.rowResults) == 0 {
		return failingRow{err: errors.New("scripted transaction: unexpected query row")}
	}
	row := t.rowResults[0]
	t.rowResults = t.rowResults[1:]
	return row
}

// Commit marks the transaction committed.
func (t *scriptedTransaction) Commit(ctx context.Context) error {
	t.committed = true
	return nil
}

// Rollback marks the transaction rolled back.
func (t *scriptedTransaction) Rollback(ctx context.Context) error {
	t.rolledBack = true
	return nil
}

// Conn reports that no dedicated connection is available in a scripted run.
func (t *scriptedTransaction) Conn() *pgx.Conn { return nil }

// LargeObjects is not used by the repository.
func (t *scriptedTransaction) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }

// SendBatch is not used by the repository.
func (t *scriptedTransaction) SendBatch(
	ctx context.Context,
	batch *pgx.Batch,
) pgx.BatchResults {
	return failingBatch{err: errors.New("scripted transaction: unexpected batch")}
}

// failingBatch is a batch result that reports the injected error.
type failingBatch struct {
	err error
}

// Exec reports the injected error.
func (b failingBatch) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, b.err }

// Query returns the injected error.
func (b failingBatch) Query() (pgx.Rows, error) { return nil, b.err }

// QueryRow returns an injected-error row.
func (b failingBatch) QueryRow() pgx.Row { return failingRow{err: b.err} }

// Close reports the injected error.
func (b failingBatch) Close() error { return b.err }

// Prepare is not used by the repository.
func (t *scriptedTransaction) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("scripted transaction: unexpected prepare")
}

// Begin is not used by the repository.
func (t *scriptedTransaction) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, errors.New("scripted transaction: unexpected nested begin")
}

// CopyFrom is not used by the repository.
func (t *scriptedTransaction) CopyFrom(
	ctx context.Context,
	table pgx.Identifier,
	columns []string,
	source pgx.CopyFromSource,
) (int64, error) {
	return 0, errors.New("scripted transaction: unexpected copy from")
}

// failOnSend is the error a pgx connector returns for a connection failure.
type failOnSend struct {
	err error
}

// Send returns the injected error.
func (f failOnSend) Send(ctx context.Context, batch *pgx.Batch) error { return f.err }

// failingRow is a row that always reports the given error.
type failingRow struct {
	err error
}

// Scan returns the injected error.
func (r failingRow) Scan(destinations ...any) error { return r.err }

// scanRow is a row that fills its destinations from a per-column script.
type scanRow struct {
	values  []any
	scanErr error
}

// Scan assigns the scripted values in order.
func (r scanRow) Scan(destinations ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return assignValues(destinations, r.values)
}

// countRow reports a single integer, for a SELECT COUNT(*) script.
type countRow struct {
	count int64
	err   error
}

// Scan assigns the count.
func (r countRow) Scan(destinations ...any) error {
	if r.err != nil {
		return r.err
	}
	return assignValues(destinations, []any{r.count})
}

// assignValues copies scripted values into pgx scan destinations.
//
// It supports the destination kinds the billing repository actually scans into;
// an unsupported destination fails the test loudly rather than silently leaving
// a zero value that would make an assertion pass for the wrong reason.
func assignValues(destinations []any, values []any) error {
	if len(destinations) != len(values) {
		return errors.New("scripted row: destination and value counts differ")
	}
	for index, destination := range destinations {
		if err := assignValue(destination, values[index]); err != nil {
			return fmt.Errorf("column %d (%T): %w", index, destination, err)
		}
	}
	return nil
}

// uniqueViolation builds the pgx error PostgreSQL raises for a duplicate key.
//
// constraintName is the index the repository matches on, which is how a test
// proves the right constraint was translated into the right sentinel.
func uniqueViolation(constraintName string) error {
	return &pgconn.PgError{
		Code:           "23505",
		ConstraintName: constraintName,
		Message:        "duplicate key value violates unique constraint \"" + constraintName + "\"",
	}
}

// newTestRepository returns a repository over the scripted database.
func newTestRepository(database *scriptedDatabase) *PostgresRepository {
	return &PostgresRepository{database: database}
}

// ===== plans =====

// TestUpdatePlanStaleVersionReturnsConflict proves a stale edit is rejected
// instead of silently overwriting a newer version.
func TestUpdatePlanStaleVersionReturnsConflict(t *testing.T) {
	database := &scriptedDatabase{execTag: pgconn.NewCommandTag("UPDATE 0")}
	repository := newTestRepository(database)
	plan := domain.Plan{ID: "plan-1", Code: "sprout_year", Version: 3}

	// The version predicate matched nothing; the follow-up read shows why.
	database.row = scanRow{values: []any{"sprout_year", int(4)}}

	err := repository.UpdatePlan(context.Background(), plan, 3)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict, got %v", err)
	}
	if len(database.execCalls) != 1 {
		t.Fatalf("executed %d statements, want 1", len(database.execCalls))
	}
	if !execMentions(database.execCalls[0], "version = $") {
		t.Fatal("expected the update to be guarded by the expected version")
	}
}

// TestUpdatePlanMissingPlanReturnsNotFound proves a stale version on a deleted
// plan is reported as missing, not as a conflict.
func TestUpdatePlanMissingPlanReturnsNotFound(t *testing.T) {
	database := &scriptedDatabase{execTag: pgconn.NewCommandTag("UPDATE 0")}
	repository := newTestRepository(database)
	database.row = failingRow{err: pgx.ErrNoRows}

	err := repository.UpdatePlan(context.Background(), domain.Plan{ID: "plan-missing"}, 1)
	if !errors.Is(err, domain.ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

// TestCreatePlanTranslatesCodeConflict proves a duplicate plan code is reported
// as a taken code rather than a raw database error.
func TestCreatePlanTranslatesCodeConflict(t *testing.T) {
	database := &scriptedDatabase{execErr: uniqueViolation("billing_plans_code_key")}
	repository := newTestRepository(database)

	err := repository.CreatePlan(context.Background(), domain.Plan{ID: "plan-1", Code: "sprout_year"})
	if !errors.Is(err, ErrPlanCodeTaken) {
		t.Fatalf("expected ErrPlanCodeTaken, got %v", err)
	}
}

// ===== orders =====

// TestMarkOrderPaidRefusesSettledOrder proves a second settlement of the same
// order is refused: the predicate only matches a pending row.
func TestMarkOrderPaidRefusesSettledOrder(t *testing.T) {
	transaction := &scriptedTransaction{execResults: []execResult{{rowsAffected: 0}}}
	repository := newTestRepository(&scriptedDatabase{transaction: transaction})

	err := repository.MarkOrderPaid(context.Background(), transaction, "order-1", 19900, timeNow())
	if !errors.Is(err, ErrAlreadySettled) {
		t.Fatalf("expected ErrAlreadySettled, got %v", err)
	}
	if len(transaction.execCalls) != 1 {
		t.Fatalf("executed %d statements, want 1", len(transaction.execCalls))
	}
	if !execMentions(transaction.execCalls[0], "status = 'pending'") {
		t.Fatal("expected the update to require a pending order")
	}
}

// TestMarkOrderPaidPassesCentAmounts proves money crosses the boundary as an
// integer cent count, never as a float.
func TestMarkOrderPaidPassesCentAmounts(t *testing.T) {
	transaction := &scriptedTransaction{execResults: []execResult{{rowsAffected: 1}}}
	repository := newTestRepository(&scriptedDatabase{transaction: transaction})

	if err := repository.MarkOrderPaid(context.Background(), transaction, "order-1", 19900, timeNow()); err != nil {
		t.Fatalf("mark order paid: %v", err)
	}
	args := transaction.execCalls[0].args
	if len(args) < 2 {
		t.Fatalf("expected at least 2 arguments, got %d", len(args))
	}
	amount, ok := args[1].(int64)
	if !ok {
		t.Fatalf("paid amount passed as %T, want int64", args[1])
	}
	if amount != 19900 {
		t.Fatalf("paid amount = %d, want 19900", amount)
	}
}

// TestCreateOrderReturnsExistingOnIdempotencyConflict proves a retried checkout
// that loses the unique-key race resolves to the order already stored.
func TestCreateOrderReturnsExistingOnIdempotencyConflict(t *testing.T) {
	order := testOrder()
	database := &scriptedDatabase{execErr: uniqueViolation("billing_orders_idempotency_idx")}
	repository := newTestRepository(database)
	// The follow-up lookup returns the order the first attempt created.
	database.row = orderScanRow(order)

	created, err := repository.CreateOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if created.ID != order.ID || created.OrderNo != order.OrderNo {
		t.Fatalf("expected the existing order, got %+v", created)
	}
}

// TestExpireStaleOrdersRequiresPendingStatus proves the sweep cannot overwrite an
// order a callback settled, which is the expiry-versus-callback race of §5.4.
func TestExpireStaleOrdersRequiresPendingStatus(t *testing.T) {
	database := &scriptedDatabase{execTag: pgconn.NewCommandTag("UPDATE 0")}
	repository := newTestRepository(database)

	if _, err := repository.ExpireStaleOrders(context.Background(), timeNow(), 100); err != nil {
		t.Fatalf("expire stale orders: %v", err)
	}
	if len(database.execCalls) != 1 {
		t.Fatalf("executed %d statements, want 1", len(database.execCalls))
	}
	statement := database.execCalls[0].sql
	if !execMentions(database.execCalls[0], "status = 'pending'") {
		t.Fatal("expected the sweep to target only pending orders")
	}
	if !execMentions(database.execCalls[0], "SKIP LOCKED") {
		t.Fatalf("expected SKIP LOCKED so replicas do not block each other: %s", statement)
	}
}

// ===== payments =====

// TestInsertNotificationReportsDuplicateWithoutError proves losing the
// idempotency race is a normal outcome, not an error: the caller is told the
// callback is a duplicate and credits nothing.
func TestInsertNotificationReportsDuplicateWithoutError(t *testing.T) {
	database := &scriptedDatabase{execTag: pgconn.NewCommandTag("INSERT 0 0")}
	repository := newTestRepository(database)

	duplicate, err := repository.InsertNotification(context.Background(), testNotification())
	if err != nil {
		t.Fatalf("insert notification: %v", err)
	}
	if !duplicate {
		t.Fatal("expected the lost claim to be reported as a duplicate")
	}
	if len(database.execCalls) != 1 {
		t.Fatalf("executed %d statements, want 1", len(database.execCalls))
	}
	// ON CONFLICT DO NOTHING is the reason a duplicate is data, not an error: a
	// unique violation would abort the surrounding transaction instead.
	if !execMentions(database.execCalls[0], "ON CONFLICT") {
		t.Fatal("expected the insert to use ON CONFLICT")
	}
	if !execMentions(database.execCalls[0], "DO NOTHING") {
		t.Fatal("expected the insert to ignore the conflicting row")
	}
}

// TestInsertNotificationReportsFreshClaim proves a first delivery is recorded
// and reported as not a duplicate.
func TestInsertNotificationReportsFreshClaim(t *testing.T) {
	database := &scriptedDatabase{execTag: pgconn.NewCommandTag("INSERT 0 1")}
	repository := newTestRepository(database)

	duplicate, err := repository.InsertNotification(context.Background(), testNotification())
	if err != nil {
		t.Fatalf("insert notification: %v", err)
	}
	if duplicate {
		t.Fatal("expected a first delivery not to be reported as a duplicate")
	}
}

// TestMarkPaymentSucceededTranslatesOrderSuccessConflict proves the schema's
// one-success-per-order rule surfaces as a settled error, so a second transfer
// is routed to reconciliation instead of being retried.
func TestMarkPaymentSucceededTranslatesOrderSuccessConflict(t *testing.T) {
	transaction := &scriptedTransaction{
		execResults: []execResult{{err: uniqueViolation("billing_payments_order_success_unique_idx")}},
	}
	repository := newTestRepository(&scriptedDatabase{transaction: transaction})

	err := repository.MarkPaymentSucceeded(context.Background(), transaction, "payment-1", "trade-1", timeNow())
	if !errors.Is(err, ErrAlreadySettled) {
		t.Fatalf("expected ErrAlreadySettled, got %v", err)
	}
}

// TestMarkNotificationProcessedRefusesSecondResult proves a callback result is
// recorded once, so a duplicate cannot rewrite the first decisive outcome.
func TestMarkNotificationProcessedRefusesSecondResult(t *testing.T) {
	database := &scriptedDatabase{execTag: pgconn.NewCommandTag("UPDATE 0")}
	repository := newTestRepository(database)
	now := timeNow()

	err := repository.MarkNotificationProcessed(
		context.Background(), "notification-1", domain.NotificationResultAccepted, &now,
	)
	if !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("expected a refusal for an already processed callback, got %v", err)
	}
}

// ===== refunds =====

// TestApplyOrderRefundRefusesOverRefund proves the guard lives in the statement
// predicate, so two concurrent completions cannot both pass a service check.
func TestApplyOrderRefundRefusesOverRefund(t *testing.T) {
	transaction := &scriptedTransaction{execResults: []execResult{{rowsAffected: 0}}}
	repository := newTestRepository(&scriptedDatabase{transaction: transaction})

	err := repository.ApplyOrderRefund(context.Background(), transaction, "order-1", 5000, timeNow())
	if !errors.Is(err, domain.ErrOverRefund) {
		t.Fatalf("expected ErrOverRefund, got %v", err)
	}
	if !execMentions(transaction.execCalls[0], "refunded_cny_cents") {
		t.Fatal("expected the update to accumulate the refunded total")
	}
}

// TestApplyOrderRefundMarksFullyRefunded proves a refund that returns the whole
// paid total moves the order to refunded rather than partially_refunded.
func TestApplyOrderRefundMarksFullyRefunded(t *testing.T) {
	transaction := &scriptedTransaction{execResults: []execResult{{rowsAffected: 1}}}
	repository := newTestRepository(&scriptedDatabase{transaction: transaction})

	if err := repository.ApplyOrderRefund(context.Background(), transaction, "order-1", 19900, timeNow()); err != nil {
		t.Fatalf("apply order refund: %v", err)
	}
	statement := transaction.execCalls[0]
	if !execMentions(statement, "CASE") {
		t.Fatal("expected the status to be chosen from the resulting refunded total")
	}
	args := statement.args
	if len(args) < 2 {
		t.Fatalf("expected at least 2 arguments, got %d", len(args))
	}
	amount, ok := args[1].(int64)
	if !ok || amount != 19900 {
		t.Fatalf("refund amount passed as %T %v, want int64 19900", args[1], args[1])
	}
}

// TestApproveRefundRequiresOperator proves an approval cannot be recorded
// without naming the operator who made it.
func TestApproveRefundRequiresOperator(t *testing.T) {
	transaction := &scriptedTransaction{}
	repository := newTestRepository(&scriptedDatabase{transaction: transaction})

	err := repository.ApproveRefund(context.Background(), transaction, "refund-1", "  ", timeNow())
	if !errors.Is(err, ErrOperatorRequired) {
		t.Fatalf("expected ErrOperatorRequired, got %v", err)
	}
	if len(transaction.execCalls) != 0 {
		t.Fatal("a refused approval must not reach the database")
	}
}

// ===== refund listings =====

// TestListRefundsQueriesTheFilteredPage proves the operator listing pushes the
// status filter and the window into SQL, so one page never loads the whole
// refund table and the page boundary is what the console asked for.
func TestListRefundsQueriesTheFilteredPage(t *testing.T) {
	database := &scriptedDatabase{rowsErr: nil}
	repository := newTestRepository(database)

	_, _, err := repository.ListRefunds(context.Background(), RefundFilter{
		Status: domain.RefundStatusRequested,
		Limit:  25,
		Offset: 50,
	})
	if err != nil {
		t.Fatalf("list refunds: %v", err)
	}
	if len(database.execCalls) != 1 {
		t.Fatalf("executed %d statements, want 1", len(database.execCalls))
	}
	call := database.execCalls[0]
	if !execMentions(call, "status = $1") {
		t.Fatalf("expected the status filter in the query: %s", call.sql)
	}
	if !execMentions(call, "LIMIT $2 OFFSET $3") {
		t.Fatalf("expected the window in the query: %s", call.sql)
	}
	if !execMentions(call, "COUNT(*) OVER ()") {
		t.Fatalf("expected the total to come from the same statement: %s", call.sql)
	}
	if !execMentions(call, "orders.order_no") {
		t.Fatalf("expected the order number to be joined in: %s", call.sql)
	}
	if len(call.args) != 3 ||
		call.args[0] != string(domain.RefundStatusRequested) ||
		call.args[1] != 25 ||
		call.args[2] != 50 {
		t.Fatalf("query arguments = %#v, want the status, limit, and offset", call.args)
	}
}

// ===== subscriptions and consents =====

// TestUpsertActiveSubscriptionExtendsExistingPeriod proves a second purchase of
// the same plan extends the live period instead of opening a competing row.
func TestUpsertActiveSubscriptionExtendsExistingPeriod(t *testing.T) {
	transaction := &scriptedTransaction{execResults: []execResult{{rowsAffected: 1}}}
	repository := newTestRepository(&scriptedDatabase{transaction: transaction})
	now := timeNow()

	err := repository.UpsertActiveSubscription(context.Background(), transaction, domain.Subscription{
		ID:                 "subscription-1",
		ParentAccountID:    "parent-1",
		PlanCode:           "sprout_year",
		Status:             domain.SubscriptionStatusActive,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(1, 0, 0),
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	if err != nil {
		t.Fatalf("upsert subscription: %v", err)
	}
	if !execMentions(transaction.execCalls[0], "ON CONFLICT") {
		t.Fatal("expected the upsert to conflict on the active subscription")
	}
	if !execMentions(transaction.execCalls[0], "GREATEST") {
		t.Fatal("expected the period end to extend to the later date")
	}
}

// TestGrantSpendingConsentIsIdempotent proves re-granting returns the live row
// rather than writing a second agreement.
func TestGrantSpendingConsentIsIdempotent(t *testing.T) {
	database := &scriptedDatabase{execTag: pgconn.NewCommandTag("INSERT 0 0")}
	repository := newTestRepository(database)
	consent := domain.SpendingConsent{
		ID:              "consent-1",
		ParentAccountID: "parent-1",
		ConsentVersion:  "2026-10-v1",
		GrantedAt:       timeNow(),
		Method:          "app_confirm",
		CreatedAt:       timeNow(),
		UpdatedAt:       timeNow(),
	}
	database.row = scanRow{values: []any{
		consent.ID, consent.ParentAccountID, consent.ConsentVersion,
		consent.GrantedAt, nil, "", consent.Method, consent.CreatedAt, consent.UpdatedAt,
	}}

	granted, err := repository.GrantSpendingConsent(context.Background(), consent)
	if err != nil {
		t.Fatalf("grant consent: %v", err)
	}
	if granted.ID != consent.ID {
		t.Fatalf("expected the live consent %s, got %s", consent.ID, granted.ID)
	}
	if !execMentions(database.execCalls[0], "ON CONFLICT") {
		t.Fatal("expected the insert to ignore a second live consent")
	}
}

// ===== reconciliation =====

// TestCreateRunTranslatesDuplicateDay proves one business day is reconciled once
// per channel.
func TestCreateRunTranslatesDuplicateDay(t *testing.T) {
	database := &scriptedDatabase{execErr: uniqueViolation("billing_reconciliation_runs_unique_idx")}
	repository := newTestRepository(database)

	err := repository.CreateRun(context.Background(), domain.ReconciliationRun{
		ID:           "run-1",
		Channel:      domain.PaymentChannelWeChat,
		BusinessDate: timeNow(),
	})
	if !errors.Is(err, ErrRunAlreadyExists) {
		t.Fatalf("expected ErrRunAlreadyExists, got %v", err)
	}
}

// ===== quota ledger =====

// TestAppendQuotaLedgerEntryRefusesSecondGrantForOrder proves the unique index
// stops a second purchase grant for the same order, which is the last line of
// defence against double-crediting quota.
func TestAppendQuotaLedgerEntryRefusesSecondGrantForOrder(t *testing.T) {
	transaction := &scriptedTransaction{
		execResults: []execResult{{err: uniqueViolation("ai_quota_ledger_purchase_unique_idx")}},
	}
	repository := newTestRepository(&scriptedDatabase{transaction: transaction})

	err := repository.AppendQuotaLedgerEntry(context.Background(), transaction, domain.QuotaLedgerEntry{
		ID:              "entry-1",
		ParentAccountID: "parent-1",
		EntryType:       domain.QuotaEntryPurchaseGrant,
		AmountUSD:       "20.000000",
		BalanceAfterUSD: "20.000000",
		OrderID:         "order-1",
	})
	if !errors.Is(err, ErrAlreadySettled) {
		t.Fatalf("expected ErrAlreadySettled, got %v", err)
	}
}

// ===== helpers =====

// execMentions reports whether a recorded call's SQL contains a fragment.
func execMentions(call execCall, fragment string) bool {
	return contains(call.sql, fragment)
}

// contains is a small non-allocating substring check for test assertions.
func contains(haystack, needle string) bool {
	return len(needle) == 0 || index(haystack, needle) >= 0
}

// index returns the first occurrence of needle in haystack.
func index(haystack, needle string) int {
	for position := 0; position+len(needle) <= len(haystack); position++ {
		if haystack[position:position+len(needle)] == needle {
			return position
		}
	}
	return -1
}

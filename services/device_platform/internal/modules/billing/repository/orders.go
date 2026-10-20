package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
)

// OrderFilter narrows an administrative order listing. Zero values mean "any".
type OrderFilter struct {
	Status          domain.OrderStatus
	PlanCode        string
	ParentAccountID string
	// CreatedFrom and CreatedTo bound created_at; a zero time is unbounded.
	CreatedFrom time.Time
	CreatedTo   time.Time
	Search      string
	Limit       int
	Offset      int
}

const orderColumns = `
	id, order_no, parent_account_id, plan_id, plan_code, plan_display_name,
	billing_period, period_months, ai_quota_usd::text, quota_validity_days,
	unit_price_cny_cents,
	discount_cny_cents, payable_cny_cents, paid_cny_cents, refunded_cny_cents,
	status, idempotency_key, expires_at, paid_at, cancelled_at, closed_at,
	client_ip, remark, created_at, updated_at
`

// CreateOrder inserts one pending order.
//
// A losing idempotency insert is reported as the existing order rather than an
// error: a retried checkout carries the same key, so returning the order that
// won is exactly the idempotent contract in design doc §5.1.
func (r *PostgresRepository) CreateOrder(ctx context.Context, order domain.Order) (*domain.Order, error) {
	_, err := r.database.Exec(ctx, `
		INSERT INTO billing_orders (
			id, order_no, parent_account_id, plan_id, plan_code, plan_display_name,
			billing_period, period_months, ai_quota_usd, quota_validity_days,
			unit_price_cny_cents,
			discount_cny_cents, payable_cny_cents, paid_cny_cents,
			refunded_cny_cents, status, idempotency_key, expires_at, paid_at,
			cancelled_at, closed_at, client_ip, remark, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25
		)
	`,
		order.ID,
		order.OrderNo,
		order.ParentAccountID,
		order.PlanID,
		order.PlanCode,
		order.PlanDisplayName,
		string(order.BillingPeriod),
		order.PeriodMonths,
		order.AIQuotaUSD,
		order.QuotaValidityDays,
		int64(order.UnitPriceCents),
		int64(order.DiscountCents),
		int64(order.PayableCents),
		int64(order.PaidCents),
		int64(order.RefundedCents),
		string(order.Status),
		order.IdempotencyKey,
		order.ExpiresAt,
		order.PaidAt,
		order.CancelledAt,
		order.ClosedAt,
		order.ClientIP,
		order.Remark,
		order.CreatedAt,
		order.UpdatedAt,
	)
	if err != nil {
		if uniqueViolationOn(err, "billing_orders_idempotency_idx") {
			existing, lookupErr := r.GetOrderByIdentityOrIdempotency(ctx, order.ParentAccountID, order.IdempotencyKey)
			if lookupErr != nil {
				return nil, fmt.Errorf("resolve idempotent order: %w", lookupErr)
			}
			return existing, nil
		}
		return nil, fmt.Errorf("insert billing order: %w", err)
	}
	return &order, nil
}

// GetOrderByID returns one order by its database identifier.
func (r *PostgresRepository) GetOrderByID(ctx context.Context, orderID string) (*domain.Order, error) {
	return scanOrder(r.database.QueryRow(ctx,
		`SELECT `+orderColumns+` FROM billing_orders WHERE id = $1`, orderID))
}

// GetOrderByOrderNo returns one order by its guardian-visible number.
func (r *PostgresRepository) GetOrderByOrderNo(ctx context.Context, orderNo string) (*domain.Order, error) {
	return scanOrder(r.database.QueryRow(ctx,
		`SELECT `+orderColumns+` FROM billing_orders WHERE order_no = $1`, orderNo))
}

// GetOrderByIdentityOrIdempotency resolves the order a checkout retry refers to,
// scoped to the family so one family's key can never address another's order.
func (r *PostgresRepository) GetOrderByIdentityOrIdempotency(
	ctx context.Context,
	parentAccountID string,
	idempotencyKey string,
) (*domain.Order, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, domain.ErrOrderNotFound
	}
	return scanOrder(r.database.QueryRow(ctx, `
		SELECT `+orderColumns+`
		FROM billing_orders
		WHERE parent_account_id = $1
		  AND idempotency_key = $2
	`, parentAccountID, idempotencyKey))
}

// ListOrdersByParent returns one family's orders, newest first, with the total
// count so the guardian app can page.
func (r *PostgresRepository) ListOrdersByParent(
	ctx context.Context,
	parentAccountID string,
	limit int,
	offset int,
) ([]domain.Order, int, error) {
	limit, offset = normalizePage(limit, offset)
	rows, err := r.database.Query(ctx, `
		SELECT `+orderColumns+`, COUNT(*) OVER () AS total
		FROM billing_orders
		WHERE parent_account_id = $1
		ORDER BY created_at DESC, order_no DESC
		LIMIT $2 OFFSET $3
	`, parentAccountID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list family billing orders: %w", err)
	}
	return collectOrdersWithTotal(rows)
}

// ListOrders returns a page of orders for the operator console.
func (r *PostgresRepository) ListOrders(
	ctx context.Context,
	filter OrderFilter,
) ([]domain.Order, int, error) {
	limit, offset := normalizePage(filter.Limit, filter.Offset)
	rows, err := r.database.Query(ctx, `
		SELECT `+orderColumns+`, COUNT(*) OVER () AS total
		FROM billing_orders
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR plan_code = $2)
		  AND ($3 = '' OR parent_account_id = $3::uuid)
		  AND ($4::timestamptz IS NULL OR created_at >= $4::timestamptz)
		  AND ($5::timestamptz IS NULL OR created_at < $5::timestamptz)
		  AND ($6 = '' OR order_no ILIKE '%' || $6 || '%'
		       OR plan_display_name ILIKE '%' || $6 || '%')
		ORDER BY created_at DESC, order_no DESC
		LIMIT $7 OFFSET $8
	`,
		string(filter.Status),
		filter.PlanCode,
		filter.ParentAccountID,
		nullableTime(filter.CreatedFrom),
		nullableTime(filter.CreatedTo),
		filter.Search,
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list billing orders: %w", err)
	}
	return collectOrdersWithTotal(rows)
}

// LockOrderForSettlement locks one order row and returns it as stored.
//
// SELECT ... FOR UPDATE is what serialises a payment callback against the
// expiry sweep and against a second callback: whoever locks first decides the
// final status, and the loser re-reads a non-pending order and must not credit.
func (r *PostgresRepository) LockOrderForSettlement(
	ctx context.Context,
	tx pgx.Tx,
	orderNo string,
) (*domain.Order, error) {
	return scanOrder(tx.QueryRow(ctx, `
		SELECT `+orderColumns+`
		FROM billing_orders
		WHERE order_no = $1
		FOR UPDATE
	`, orderNo))
}

// LockOrderByID locks one order row by id, used when the caller holds an order
// id (a refund's order) rather than the guardian-visible number.
func (r *PostgresRepository) LockOrderByID(
	ctx context.Context,
	tx pgx.Tx,
	orderID string,
) (*domain.Order, error) {
	return scanOrder(tx.QueryRow(ctx, `
		SELECT `+orderColumns+`
		FROM billing_orders
		WHERE id = $1
		FOR UPDATE
	`, orderID))
}

// MarkOrderPaid moves a locked pending order to paid.
//
// The status predicate repeats the lock's guarantee at the statement level, so
// an order the expiry sweep already moved to expired fails here with
// ErrAlreadySettled instead of being silently resurrected.
func (r *PostgresRepository) MarkOrderPaid(
	ctx context.Context,
	tx pgx.Tx,
	orderID string,
	paidCents domain.Cents,
	paidAt time.Time,
) error {
	tag, err := tx.Exec(ctx, `
		UPDATE billing_orders
		SET status = 'paid',
		    paid_cny_cents = $2,
		    paid_at = $3,
		    updated_at = $3
		WHERE id = $1
		  AND status = 'pending'
	`, orderID, int64(paidCents), paidAt)
	if err != nil {
		return fmt.Errorf("mark billing order paid: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadySettled
	}
	return nil
}

// CancelPendingOrder moves an owning family's pending order to cancelled.
//
// The parent id is part of the predicate, not a separate check: a cross-family
// cancel must fail in the same statement that would have cancelled the order.
func (r *PostgresRepository) CancelPendingOrder(
	ctx context.Context,
	parentAccountID string,
	orderNo string,
	now time.Time,
) error {
	tag, err := r.database.Exec(ctx, `
		UPDATE billing_orders
		SET status = 'cancelled',
		    cancelled_at = $3,
		    updated_at = $3
		WHERE order_no = $1
		  AND parent_account_id = $2
		  AND status = 'pending'
	`, orderNo, parentAccountID, now)
	if err != nil {
		return fmt.Errorf("cancel billing order: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadySettled
	}
	return nil
}

// ExpireStaleOrders moves due pending orders to expired and returns their ids.
//
// The conditional UPDATE is the guard against a racing callback: an order the
// callback already settled is no longer pending, so it is skipped here and the
// sweep never overwrites a paid order (design doc §5.4).
func (r *PostgresRepository) ExpireStaleOrders(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]string, error) {
	if limit <= 0 {
		limit = batchExpiryLimit
	}
	rows, err := r.database.Query(ctx, `
		WITH due AS (
			SELECT id
			FROM billing_orders
			WHERE status = 'pending'
			  AND expires_at IS NOT NULL
			  AND expires_at < $1
			ORDER BY expires_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE billing_orders AS orders
		SET status = 'expired',
		    closed_at = $1,
		    updated_at = $1
		FROM due
		WHERE orders.id = due.id
		RETURNING orders.id
	`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("expire stale billing orders: %w", err)
	}
	defer rows.Close()
	expired := make([]string, 0)
	for rows.Next() {
		var orderID string
		if err := rows.Scan(&orderID); err != nil {
			return nil, fmt.Errorf("scan expired billing order: %w", err)
		}
		expired = append(expired, orderID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired billing orders: %w", err)
	}
	return expired, nil
}

// CountPendingOrders reports how many orders are still awaiting payment, which
// the Feature Center health probe renders as backlog.
func (r *PostgresRepository) CountPendingOrders(ctx context.Context, now time.Time) (int, int, error) {
	var pending int
	var overdue int
	err := r.database.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'pending'),
			COUNT(*) FILTER (WHERE status = 'pending' AND expires_at < $1)
		FROM billing_orders
	`, now).Scan(&pending, &overdue)
	if err != nil {
		return 0, 0, fmt.Errorf("count pending billing orders: %w", err)
	}
	return pending, overdue, nil
}

// batchExpiryLimit bounds one expiry sweep so a large backlog is drained over
// several runs instead of holding one long lock.
const batchExpiryLimit = 500

// nullableTime renders a zero time as SQL NULL for an optional range bound.
func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

// scanOrder reads one order row, mapping a missing row to domain.ErrOrderNotFound.
func scanOrder(row rowScanner) (*domain.Order, error) {
	order, err := scanOrderFields(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrOrderNotFound
	}
	return order, err
}

// orderScanTargets returns the destinations for orderColumns plus, when
// withTotal is set, the windowed COUNT(*) OVER () column.
func orderScanTargets(order *domain.Order, billingPeriod *string, status *string,
	prices *[5]int64, total *int, withTotal bool) []any {
	targets := []any{
		&order.ID,
		&order.OrderNo,
		&order.ParentAccountID,
		&order.PlanID,
		&order.PlanCode,
		&order.PlanDisplayName,
		billingPeriod,
		&order.PeriodMonths,
		&order.AIQuotaUSD,
		&order.QuotaValidityDays,
		&prices[0],
		&prices[1],
		&prices[2],
		&prices[3],
		&prices[4],
		status,
		&order.IdempotencyKey,
		&order.ExpiresAt,
		&order.PaidAt,
		&order.CancelledAt,
		&order.ClosedAt,
		&order.ClientIP,
		&order.Remark,
		&order.CreatedAt,
		&order.UpdatedAt,
	}
	if withTotal {
		targets = append(targets, total)
	}
	return targets
}

// applyOrderPrices copies the raw BIGINT columns into their typed fields so a
// cent amount never leaves the database as anything but an int64.
func applyOrderPrices(order *domain.Order, billingPeriod string, status string, prices [5]int64) {
	order.BillingPeriod = domain.BillingPeriod(billingPeriod)
	order.Status = domain.OrderStatus(status)
	order.UnitPriceCents = domain.Cents(prices[0])
	order.DiscountCents = domain.Cents(prices[1])
	order.PayableCents = domain.Cents(prices[2])
	order.PaidCents = domain.Cents(prices[3])
	order.RefundedCents = domain.Cents(prices[4])
}

// scanOrderFields reads the orderColumns list in order.
func scanOrderFields(row rowScanner) (*domain.Order, error) {
	var order domain.Order
	var billingPeriod string
	var status string
	var prices [5]int64
	if err := row.Scan(orderScanTargets(&order, &billingPeriod, &status, &prices, nil, false)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan billing order: %w", err)
	}
	applyOrderPrices(&order, billingPeriod, status, prices)
	return &order, nil
}

// collectOrdersWithTotal drains an order page and returns its rows and total.
func collectOrdersWithTotal(rows pgx.Rows) ([]domain.Order, int, error) {
	defer rows.Close()
	orders := make([]domain.Order, 0)
	total := 0
	for rows.Next() {
		var order domain.Order
		var billingPeriod string
		var status string
		var prices [5]int64
		var rowTotal int
		if err := rows.Scan(
			orderScanTargets(&order, &billingPeriod, &status, &prices, &rowTotal, true)...,
		); err != nil {
			return nil, 0, fmt.Errorf("scan billing order: %w", err)
		}
		total = rowTotal
		applyOrderPrices(&order, billingPeriod, status, prices)
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate billing orders: %w", err)
	}
	return orders, total, nil
}

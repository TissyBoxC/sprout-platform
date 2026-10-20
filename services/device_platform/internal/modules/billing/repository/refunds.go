package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
)

const refundColumns = `
	id, refund_no, order_id, payment_id, amount_cny_cents, reason,
	operator_account_id, status, channel_refund_no, requested_at, approved_at,
	completed_at, created_at, updated_at
`

// CreateRefund inserts one refund request.
//
// It takes the caller's transaction because the over-refund guard is only sound
// when the refundable total is summed and the new row written under the same
// order lock.
func (r *PostgresRepository) CreateRefund(ctx context.Context, tx pgx.Tx, refund domain.Refund) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO billing_refunds (
			id, refund_no, order_id, payment_id, amount_cny_cents, reason,
			operator_account_id, status, channel_refund_no, requested_at,
			approved_at, completed_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
	`,
		refund.ID,
		refund.RefundNo,
		refund.OrderID,
		normalizeUUID(refund.PaymentID),
		int64(refund.AmountCents),
		refund.Reason,
		normalizeUUID(refund.OperatorAccountID),
		string(refund.Status),
		refund.ChannelRefundNo,
		refund.RequestedAt,
		refund.ApprovedAt,
		refund.CompletedAt,
		refund.CreatedAt,
		refund.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert billing refund: %w", err)
	}
	return nil
}

// GetRefundByID returns one refund by its database identifier.
func (r *PostgresRepository) GetRefundByID(ctx context.Context, refundID string) (*domain.Refund, error) {
	return scanRefund(r.database.QueryRow(ctx,
		`SELECT `+refundColumns+` FROM billing_refunds WHERE id = $1`, refundID))
}

// GetRefundByNo returns one refund by its guardian-visible number.
func (r *PostgresRepository) GetRefundByNo(ctx context.Context, refundNo string) (*domain.Refund, error) {
	return scanRefund(r.database.QueryRow(ctx,
		`SELECT `+refundColumns+` FROM billing_refunds WHERE refund_no = $1`, refundNo))
}

// LockRefund locks one refund row for a status transition.
//
// The lock serialises approval, rejection, and completion of the same refund so
// two operators cannot approve it twice or complete a rejected one.
func (r *PostgresRepository) LockRefund(ctx context.Context, tx pgx.Tx, refundID string) (*domain.Refund, error) {
	return scanRefund(tx.QueryRow(ctx,
		`SELECT `+refundColumns+` FROM billing_refunds WHERE id = $1 FOR UPDATE`, refundID))
}

// ListRefundsByOrder returns every refund of one order, newest first. Pending or
// approved refunds are included so the guardian sees what is still in flight.
func (r *PostgresRepository) ListRefundsByOrder(
	ctx context.Context,
	orderID string,
) ([]domain.Refund, error) {
	rows, err := r.database.Query(ctx, `
		SELECT `+refundColumns+`
		FROM billing_refunds
		WHERE order_id = $1
		ORDER BY created_at DESC
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list billing refunds: %w", err)
	}
	defer rows.Close()
	refunds := make([]domain.Refund, 0)
	for rows.Next() {
		refund, scanErr := scanRefund(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		refunds = append(refunds, *refund)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate billing refunds: %w", err)
	}
	return refunds, nil
}

// SumRefundCentsByStatus sums refund amounts of one order in the given statuses.
//
// The service asks for every non-rejected status, not only succeeded, because a
// second request must not be able to queue more money than the order still
// holds while the first one is still moving.
func (r *PostgresRepository) SumRefundCentsByStatus(
	ctx context.Context,
	tx pgx.Tx,
	orderID string,
	statuses []domain.RefundStatus,
) (domain.Cents, error) {
	if len(statuses) == 0 {
		return 0, nil
	}
	values := make([]string, 0, len(statuses))
	for _, status := range statuses {
		values = append(values, string(status))
	}
	var sum int64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cny_cents), 0)
		FROM billing_refunds
		WHERE order_id = $1
		  AND status = ANY($2::text[])
	`, orderID, values).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("sum billing refunds: %w", err)
	}
	return domain.Cents(sum), nil
}

// SumSucceededRefundCents sums the money actually returned on one order.
func (r *PostgresRepository) SumSucceededRefundCents(
	ctx context.Context,
	orderID string,
) (domain.Cents, error) {
	var sum int64
	err := r.database.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cny_cents), 0)
		FROM billing_refunds
		WHERE order_id = $1
		  AND status = 'succeeded'
	`, orderID).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("sum succeeded billing refunds: %w", err)
	}
	return domain.Cents(sum), nil
}

// ApproveRefund records the approving operator and time on a requested refund.
func (r *PostgresRepository) ApproveRefund(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
	operatorAccountID string,
	approvedAt time.Time,
) error {
	if normalizeUUID(operatorAccountID) == nil {
		return fmt.Errorf("%w: approving operator is required", ErrOperatorRequired)
	}
	return execExpectingRow(ctx, tx, domain.ErrInvalidOrderTransition, `
		UPDATE billing_refunds
		SET status = 'approved',
		    operator_account_id = $2,
		    approved_at = $3,
		    updated_at = $3
		WHERE id = $1
		  AND status = 'requested'
	`, refundID, operatorAccountID, approvedAt)
}

// RejectRefund rejects a refund that has not moved any money yet.
func (r *PostgresRepository) RejectRefund(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
	operatorAccountID string,
	rejectedAt time.Time,
) error {
	// A rejection after approval must name who decided it, so the schema's
	// approved-requires-operator rule cannot be undone by a null overwrite.
	if normalizeUUID(operatorAccountID) == nil {
		return fmt.Errorf("%w: rejecting operator is required", ErrOperatorRequired)
	}
	return execExpectingRow(ctx, tx, domain.ErrInvalidOrderTransition, `
		UPDATE billing_refunds
		SET status = 'rejected',
		    operator_account_id = $2,
		    updated_at = $3
		WHERE id = $1
		  AND status IN ('requested', 'approved', 'failed')
	`, refundID, operatorAccountID, rejectedAt)
}

// MarkRefundProcessing records that the channel accepted the reversal request.
func (r *PostgresRepository) MarkRefundProcessing(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
	channelRefundNo string,
	now time.Time,
) error {
	return execExpectingRow(ctx, tx, domain.ErrInvalidOrderTransition, `
		UPDATE billing_refunds
		SET status = 'processing',
		    channel_refund_no = CASE WHEN $2 = '' THEN channel_refund_no ELSE $2 END,
		    updated_at = $3
		WHERE id = $1
		  AND status IN ('approved', 'failed')
	`, refundID, channelRefundNo, now)
}

// MarkRefundSucceeded completes a refund whose money is back with the guardian.
func (r *PostgresRepository) MarkRefundSucceeded(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
	channelRefundNo string,
	completedAt time.Time,
) error {
	return execExpectingRow(ctx, tx, domain.ErrInvalidOrderTransition, `
		UPDATE billing_refunds
		SET status = 'succeeded',
		    channel_refund_no = CASE WHEN $2 = '' THEN channel_refund_no ELSE $2 END,
		    completed_at = $3,
		    updated_at = $3
		WHERE id = $1
		  AND status = 'processing'
	`, refundID, channelRefundNo, completedAt)
}

// MarkRefundFailed records a channel rejection so an operator can retry.
func (r *PostgresRepository) MarkRefundFailed(
	ctx context.Context,
	tx pgx.Tx,
	refundID string,
	now time.Time,
) error {
	return execExpectingRow(ctx, tx, domain.ErrInvalidOrderTransition, `
		UPDATE billing_refunds
		SET status = 'failed',
		    updated_at = $2
		WHERE id = $1
		  AND status IN ('approved', 'processing')
	`, refundID, now)
}

// ApplyOrderRefund adds a completed refund to the order's refunded total and
// moves the order to partially_refunded or refunded.
//
// The predicate covers both refundable statuses and the CASE picks the final
// state from the new total, so the order status is derived from the money
// rather than passed in by a caller that could disagree with it.
func (r *PostgresRepository) ApplyOrderRefund(
	ctx context.Context,
	tx pgx.Tx,
	orderID string,
	refundCents domain.Cents,
	now time.Time,
) error {
	return execExpectingRow(ctx, tx, domain.ErrOverRefund, `
		UPDATE billing_orders
		SET refunded_cny_cents = refunded_cny_cents + $2,
		    status = CASE
		        WHEN refunded_cny_cents + $2 >= paid_cny_cents THEN 'refunded'
		        ELSE 'partially_refunded'
		    END,
		    updated_at = $3
		WHERE id = $1
		  AND status IN ('paid', 'partially_refunded')
		  AND refunded_cny_cents + $2 <= paid_cny_cents
	`, orderID, int64(refundCents), now)
}

// scanRefund reads one refund row, mapping a missing row to the domain error.
func scanRefund(row rowScanner) (*domain.Refund, error) {
	var refund domain.Refund
	var paymentID *string
	var operatorID *string
	var statusValue string
	var amount int64
	err := row.Scan(
		&refund.ID,
		&refund.RefundNo,
		&refund.OrderID,
		&paymentID,
		&amount,
		&refund.Reason,
		&operatorID,
		&statusValue,
		&refund.ChannelRefundNo,
		&refund.RequestedAt,
		&refund.ApprovedAt,
		&refund.CompletedAt,
		&refund.CreatedAt,
		&refund.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan billing refund: %w", err)
	}
	if paymentID != nil {
		refund.PaymentID = *paymentID
	}
	if operatorID != nil {
		refund.OperatorAccountID = *operatorID
	}
	refund.Status = domain.RefundStatus(statusValue)
	refund.AmountCents = domain.Cents(amount)
	return &refund, nil
}

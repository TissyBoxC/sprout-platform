package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
)

// PaymentFilter narrows an administrative payment listing.
type PaymentFilter struct {
	Status  domain.PaymentStatus
	Channel domain.PaymentChannel
	Limit   int
	Offset  int
}

const paymentColumns = `
	id, order_id, channel, channel_trade_no, amount_cny_cents, status,
	provider_payload, failure_code, failure_message, paid_at, created_at, updated_at
`

// CreatePayment inserts one payment attempt in the status it carries.
func (r *PostgresRepository) CreatePayment(ctx context.Context, payment domain.Payment) error {
	payload, err := encodePayload(payment.ProviderPayload)
	if err != nil {
		return err
	}
	_, err = r.database.Exec(ctx, `
		INSERT INTO billing_payments (
			id, order_id, channel, channel_trade_no, amount_cny_cents, status,
			provider_payload, failure_code, failure_message, paid_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12
		)
	`,
		payment.ID,
		payment.OrderID,
		string(payment.Channel),
		payment.ChannelTradeNo,
		int64(payment.AmountCents),
		string(payment.Status),
		payload,
		payment.FailureCode,
		payment.FailureMessage,
		payment.PaidAt,
		payment.CreatedAt,
		payment.UpdatedAt,
	)
	if err != nil {
		// One provider trade may only enter the platform once; a second row for
		// the same trade is a duplicate callback, not a new payment.
		if uniqueViolationOn(err, "billing_payments_channel_trade_no_idx") {
			return fmt.Errorf("%w: channel trade %q", domain.ErrDuplicateNotification, payment.ChannelTradeNo)
		}
		return fmt.Errorf("insert billing payment: %w", err)
	}
	return nil
}

// GetPaymentByID returns one payment attempt.
func (r *PostgresRepository) GetPaymentByID(ctx context.Context, paymentID string) (*domain.Payment, error) {
	return scanPayment(r.database.QueryRow(ctx,
		`SELECT `+paymentColumns+` FROM billing_payments WHERE id = $1`, paymentID))
}

// GetPaymentByChannelTradeNo resolves the payment a provider trade settled.
func (r *PostgresRepository) GetPaymentByChannelTradeNo(
	ctx context.Context,
	channel domain.PaymentChannel,
	channelTradeNo string,
) (*domain.Payment, error) {
	return scanPayment(r.database.QueryRow(ctx, `
		SELECT `+paymentColumns+`
		FROM billing_payments
		WHERE channel = $1
		  AND channel_trade_no = $2
	`, string(channel), channelTradeNo))
}

// FindSucceededPayment returns the single successful payment of an order.
//
// The schema allows at most one, so a refund can name the exact payment the
// money came in through without guessing.
func (r *PostgresRepository) FindSucceededPayment(
	ctx context.Context,
	tx pgx.Tx,
	orderID string,
) (*domain.Payment, error) {
	return scanPayment(tx.QueryRow(ctx, `
		SELECT `+paymentColumns+`
		FROM billing_payments
		WHERE order_id = $1
		  AND status = 'succeeded'
	`, orderID))
}

// ListPaymentsByOrder returns every attempt on one order, newest first.
func (r *PostgresRepository) ListPaymentsByOrder(
	ctx context.Context,
	orderID string,
) ([]domain.Payment, error) {
	rows, err := r.database.Query(ctx, `
		SELECT `+paymentColumns+`
		FROM billing_payments
		WHERE order_id = $1
		ORDER BY created_at DESC
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list billing payments: %w", err)
	}
	defer rows.Close()
	payments := make([]domain.Payment, 0)
	for rows.Next() {
		payment, scanErr := scanPayment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		payments = append(payments, *payment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate billing payments: %w", err)
	}
	return payments, nil
}

// UpdatePaymentProcessing moves a created attempt to processing and stores the
// provider trade number and public pre-payment payload.
//
// Only a created attempt may enter processing, so a retried StartPayment cannot
// reopen an attempt the provider already closed.
func (r *PostgresRepository) UpdatePaymentProcessing(
	ctx context.Context,
	payment domain.Payment,
	channelTradeNo string,
) error {
	payload, err := encodePayload(payment.ProviderPayload)
	if err != nil {
		return err
	}
	tradeNo := channelTradeNo
	if tradeNo == "" {
		tradeNo = payment.ChannelTradeNo
	}
	tag, execErr := r.database.Exec(ctx, `
		UPDATE billing_payments
		SET status = 'processing',
		    channel_trade_no = $2,
		    provider_payload = $3::jsonb,
		    updated_at = $4
		WHERE id = $1
		  AND status = 'created'
	`, payment.ID, tradeNo, payload, payment.UpdatedAt)
	if execErr != nil {
		if uniqueViolationOn(execErr, "billing_payments_channel_trade_no_idx") {
			return fmt.Errorf("%w: channel trade %q", domain.ErrDuplicateNotification, tradeNo)
		}
		return fmt.Errorf("mark billing payment processing: %w", execErr)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: payment is no longer open", ErrAlreadySettled)
	}
	return nil
}

// MarkPaymentSucceeded records a settled attempt.
//
// It runs on the caller's transaction because it must commit together with the
// order transition and the quota grant; a payment marked succeeded on its own
// would claim money the ledger never saw.
func (r *PostgresRepository) MarkPaymentSucceeded(
	ctx context.Context,
	tx pgx.Tx,
	paymentID string,
	channelTradeNo string,
	paidAt time.Time,
) error {
	payload, err := encodePayload(map[string]any{})
	if err != nil {
		return err
	}
	tag, execErr := tx.Exec(ctx, `
		UPDATE billing_payments
		SET status = 'succeeded',
		    channel_trade_no = CASE WHEN channel_trade_no = '' THEN $2 ELSE channel_trade_no END,
		    paid_at = $3,
		    provider_payload = $4::jsonb,
		    failure_code = '',
		    failure_message = '',
		    updated_at = $3
		WHERE id = $1
		  AND status IN ('created', 'processing')
	`, paymentID, channelTradeNo, paidAt, payload)
	if execErr != nil {
		if uniqueViolationOn(execErr, "billing_payments_channel_trade_no_idx") {
			return fmt.Errorf("%w: channel trade %q", domain.ErrDuplicateNotification, channelTradeNo)
		}
		// billing_payments_order_success_unique_idx: the order is already
		// settled by another trade. The extra transfer is real money and must be
		// routed to reconciliation and refund, never retried.
		if uniqueViolationOn(execErr, "billing_payments_order_success_unique_idx") {
			return fmt.Errorf("%w: order already has a successful payment", ErrAlreadySettled)
		}
		return fmt.Errorf("mark billing payment succeeded: %w", execErr)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: payment is no longer open", ErrAlreadySettled)
	}
	return nil
}

// MarkPaymentFailed records a failed attempt with an operator-facing reason.
func (r *PostgresRepository) MarkPaymentFailed(
	ctx context.Context,
	paymentID string,
	failureCode string,
	failureMessage string,
	now time.Time,
) error {
	tag, err := r.database.Exec(ctx, `
		UPDATE billing_payments
		SET status = 'failed',
		    failure_code = $2,
		    failure_message = $3,
		    updated_at = $4
		WHERE id = $1
		  AND status IN ('created', 'processing')
	`, paymentID, failureCode, failureMessage, now)
	if err != nil {
		return fmt.Errorf("mark billing payment failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: payment is no longer open", ErrAlreadySettled)
	}
	return nil
}

// MarkPaymentClosed closes an abandoned attempt that was never paid.
func (r *PostgresRepository) MarkPaymentClosed(ctx context.Context, paymentID string, now time.Time) error {
	tag, err := r.database.Exec(ctx, `
		UPDATE billing_payments
		SET status = 'closed',
		    updated_at = $2
		WHERE id = $1
		  AND status IN ('created', 'processing')
	`, paymentID, now)
	if err != nil {
		return fmt.Errorf("close billing payment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: payment is no longer open", ErrAlreadySettled)
	}
	return nil
}

// InsertNotification claims one provider callback in the idempotency ledger.
//
// It uses ON CONFLICT DO NOTHING plus RowsAffected rather than catching a
// unique violation for three reasons: the conflict is the expected path for a
// redelivery, so it must not raise an error; a violation aborts the whole
// transaction, and losing the race is normal rather than fatal; and
// RowsAffected tells the two outcomes apart with one round trip. The caller
// gets duplicate=true and must acknowledge the callback without crediting.
func (r *PostgresRepository) InsertNotification(
	ctx context.Context,
	notification domain.PaymentNotification,
) (duplicate bool, err error) {
	tag, err := r.database.Exec(ctx, `
		INSERT INTO billing_payment_notifications (
			id, channel, notification_id, payload_digest, signature_verified,
			processed_at, result, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (channel, notification_id) DO NOTHING
	`,
		notification.ID,
		string(notification.Channel),
		notification.NotificationID,
		notification.PayloadDigest,
		notification.SignatureVerified,
		notification.ProcessedAt,
		string(notification.Result),
		notification.CreatedAt,
		notification.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("insert billing payment notification: %w", err)
	}
	return tag.RowsAffected() == 0, nil
}

// GetNotification returns the ledger row for a channel notification.
func (r *PostgresRepository) GetNotification(
	ctx context.Context,
	channel domain.PaymentChannel,
	notificationID string,
) (*domain.PaymentNotification, error) {
	var notification domain.PaymentNotification
	var channelValue string
	var resultValue string
	err := r.database.QueryRow(ctx, `
		SELECT id, channel, notification_id, payload_digest, signature_verified,
		       processed_at, result, created_at, updated_at
		FROM billing_payment_notifications
		WHERE channel = $1
		  AND notification_id = $2
	`, string(channel), notificationID).Scan(
		&notification.ID,
		&channelValue,
		&notification.NotificationID,
		&notification.PayloadDigest,
		&notification.SignatureVerified,
		&notification.ProcessedAt,
		&resultValue,
		&notification.CreatedAt,
		&notification.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get billing payment notification: %w", err)
	}
	notification.Channel = domain.PaymentChannel(channelValue)
	notification.Result = domain.NotificationResult(resultValue)
	return &notification, nil
}

// MarkNotificationProcessed records the final outcome of a callback.
//
// processed_at is only written together with a non-received result, because the
// schema forbids an accepted row without a processing time and a redelivery
// must keep saying "still processing" until the first delivery finishes.
func (r *PostgresRepository) MarkNotificationProcessed(
	ctx context.Context,
	notificationID string,
	result domain.NotificationResult,
	processedAt *time.Time,
) error {
	if !isKnownNotificationResult(result) {
		return fmt.Errorf("%w: unknown notification result %q",
			domain.ErrInvalidOrderTransition, result)
	}
	if result != domain.NotificationResultReceived && processedAt == nil {
		return fmt.Errorf("%w: result %q requires a processing time",
			domain.ErrInvalidOrderTransition, result)
	}
	// A row that lost the idempotency insert is always present, so zero rows
	// here means the ledger row was removed underneath us: a missing record
	// rather than a state conflict.
	tag, err := r.database.Exec(ctx, `
		UPDATE billing_payment_notifications
		SET result = $2,
		    processed_at = $3,
		    updated_at = NOW()
		WHERE id = $1
		  AND processed_at IS NULL
	`, notificationID, string(result), processedAt)
	if err != nil {
		return fmt.Errorf("mark billing payment notification processed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: notification %s", domain.ErrOrderNotFound, notificationID)
	}
	return nil
}

// isKnownNotificationResult reports whether a value is one the ledger's CHECK
// accepts, so an unknown result is rejected here with a domain error instead of
// surfacing as a constraint violation.
func isKnownNotificationResult(result domain.NotificationResult) bool {
	switch result {
	case domain.NotificationResultReceived,
		domain.NotificationResultAccepted,
		domain.NotificationResultDuplicate,
		domain.NotificationResultSignatureInvalid,
		domain.NotificationResultAmountMismatch,
		domain.NotificationResultOrderNotFound:
		return true
	default:
		return false
	}
}

// encodePayload renders a redacted provider payload as a JSON object literal.
func encodePayload(payload map[string]any) (string, error) {
	if payload == nil {
		return "{}", nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode billing payment payload: %w", err)
	}
	return string(encoded), nil
}

// scanPayment reads one payment row, mapping a missing row to the domain error.
func scanPayment(row rowScanner) (*domain.Payment, error) {
	var payment domain.Payment
	var channelValue string
	var statusValue string
	var amount int64
	var payload []byte
	err := row.Scan(
		&payment.ID,
		&payment.OrderID,
		&channelValue,
		&payment.ChannelTradeNo,
		&amount,
		&statusValue,
		&payload,
		&payment.FailureCode,
		&payment.FailureMessage,
		&payment.PaidAt,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan billing payment: %w", err)
	}
	payment.Channel = domain.PaymentChannel(channelValue)
	payment.Status = domain.PaymentStatus(statusValue)
	payment.AmountCents = domain.Cents(amount)
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &payment.ProviderPayload); err != nil {
			return nil, fmt.Errorf("decode billing payment payload: %w", err)
		}
	}
	return &payment, nil
}

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/channel"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PaymentService starts payment attempts and settles provider callbacks.
//
// The callback path is the only place money is credited, so it is written as a
// sequence of gates (signature, idempotency, order lock, amount, status) that
// each refuse to credit rather than repair. A refused callback is recorded in
// the notification ledger so operators can reconcile the channel's book against
// the platform's.
type PaymentService struct {
	base
	quota *QuotaLedgerService
}

// NewPaymentService creates the payment service.
func NewPaymentService(options Options) (*PaymentService, error) {
	shared, err := newBase(options)
	if err != nil {
		return nil, err
	}
	quotaService, err := NewQuotaLedgerService(options)
	if err != nil {
		return nil, err
	}
	return &PaymentService{base: shared, quota: quotaService}, nil
}

// StartPaymentResult carries the provider credentials the client needs.
type StartPaymentResult struct {
	Payment domain.Payment
	// Payload is the provider's public pre-payment data, empty for an
	// unconfigured channel.
	Payload map[string]string
}

// StartPayment opens one payment attempt for a pending, unexpired order.
//
// A channel that is not configured is reported as ErrPaymentNotOpenYet and the
// attempt is marked failed: the guardian sees "payment is not open yet" and the
// platform never holds a payment row that looks ready to collect money it
// cannot collect.
func (s *PaymentService) StartPayment(
	ctx context.Context,
	parentAccountID string,
	orderNo string,
	channelName string,
) (StartPaymentResult, error) {
	order, err := s.repository.GetOrderByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return StartPaymentResult{}, err
	}
	if order.ParentAccountID != parentAccountID || parentAccountID == "" {
		return StartPaymentResult{}, fmt.Errorf("%w: order %s", domain.ErrCrossFamilyAccess, order.OrderNo)
	}
	now := s.now()
	if err := domain.ValidateOrderPayable(*order, now); err != nil {
		return StartPaymentResult{}, err
	}
	provider, err := s.resolveChannel(channelName)
	if err != nil {
		return StartPaymentResult{}, err
	}

	payment := domain.Payment{
		ID:              uuid.NewString(),
		OrderID:         order.ID,
		Channel:         domain.PaymentChannel(provider.Name()),
		AmountCents:     order.PayableCents,
		Status:          domain.PaymentStatusCreated,
		ProviderPayload: map[string]any{},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.repository.CreatePayment(ctx, payment); err != nil {
		return StartPaymentResult{}, err
	}

	result, callErr := provider.CreatePayment(ctx, channel.CreatePaymentRequest{
		OrderNo:     order.OrderNo,
		AmountCents: int64(order.PayableCents),
		Subject:     order.PlanDisplayName,
		Description: order.PlanDisplayName,
		ClientIP:    order.ClientIP,
		NotifyURL:   s.paymentCallbackURL(provider.Name()),
		ExpiresAt:   valueOrNow(order.ExpiresAt, now),
	})
	if callErr != nil {
		// The failure is recorded, not swallowed: an operator needs to see that
		// the guardian tried and why nothing was collected.
		code, message := classifyChannelFailure(callErr)
		if markErr := s.repository.MarkPaymentFailed(ctx, payment.ID, code, message, s.now()); markErr != nil {
			return StartPaymentResult{}, fmt.Errorf("record failed payment attempt: %w", markErr)
		}
		if isChannelNotConfigured(callErr) {
			return StartPaymentResult{}, notConfigured(callErr)
		}
		return StartPaymentResult{}, fmt.Errorf("%w: %s", ErrChannelUnavailable, message)
	}
	if result.ChannelTradeNo != "" {
		payment.ChannelTradeNo = result.ChannelTradeNo
	}

	now = s.now()
	if err := s.markPaymentProcessing(ctx, payment, result, now); err != nil {
		return StartPaymentResult{}, err
	}
	payment.Status = domain.PaymentStatusProcessing
	payment.UpdatedAt = now
	s.recordAudit(ctx, BillingAuditEvent{
		Action:          "billing.payment.started",
		ParentAccountID: parentAccountID,
		OrderNo:         order.OrderNo,
		Detail:          map[string]any{"payment_id": payment.ID, "channel": string(payment.Channel)},
		CreatedAt:       now,
	})
	return StartPaymentResult{Payment: payment, Payload: result.Payload}, nil
}

// NotificationOutcome is what the handler should tell the provider.
type NotificationOutcome struct {
	// NotificationID is the ledger row the callback was recorded under.
	NotificationID string
	// Result is the recorded outcome.
	Result domain.NotificationResult
	// Duplicate is true when the callback had already been processed; the
	// provider must still receive a success answer.
	Duplicate bool
	// OrderNo is the order the callback referenced, when it could be resolved.
	OrderNo string
}

// HandleNotification verifies, records, and settles one provider callback.
//
// The sequence is design doc §5.3 and each gate credits nothing on failure:
//  1. verify the signature; a failure is recorded as signature_invalid.
//  2. claim the (channel, notification_id) row; losing the claim is a duplicate
//     and returns idempotent success without crediting again.
//  3. lock the order, compare the settled amount to the payable amount.
//  4. move pending to paid; any other status credits nothing.
//  5. renew the subscription for a periodic plan.
//  6. append the quota ledger entry and raise the provider quota.
//  7. mark the notification accepted.
//
// Steps 3 to 7 share one transaction, so a failure leaves no partial credit.
func (s *PaymentService) HandleNotification(
	ctx context.Context,
	channelName string,
	request channel.NotificationRequest,
) (NotificationOutcome, error) {
	provider, err := s.resolveChannel(channelName)
	if err != nil {
		return NotificationOutcome{}, err
	}
	channelValue := domain.PaymentChannel(provider.Name())
	digest := channel.NotificationDigest(request.RawBody)

	event, err := provider.VerifyNotification(ctx, request)
	if err != nil {
		return s.recordRejectedNotification(ctx, channelValue, digest, event, err)
	}
	ledgerID := uuid.NewString()
	now := s.now()
	duplicate, err := s.repository.InsertNotification(ctx, domain.PaymentNotification{
		ID:                ledgerID,
		Channel:           channelValue,
		NotificationID:    event.NotificationID,
		PayloadDigest:     digest,
		SignatureVerified: true,
		Result:            domain.NotificationResultReceived,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		return NotificationOutcome{}, err
	}
	if duplicate {
		return s.resolveDuplicate(ctx, channelValue, event.NotificationID)
	}

	if !event.Success {
		// A signed callback can say the payment failed. Nothing is credited and
		// the row says why, so a support case can read the whole story.
		_ = s.repository.MarkNotificationProcessed(
			ctx, ledgerID, domain.NotificationResultOrderNotFound, pointerTo(now))
		return NotificationOutcome{
				NotificationID: ledgerID,
				Result:         domain.NotificationResultOrderNotFound,
				OrderNo:        event.OrderNo,
			},
			fmt.Errorf("%w: order %s", ErrNotificationFailed, event.OrderNo)
	}

	result, settleErr := s.settleNotification(ctx, ledgerID, channelValue, event, now)
	return result, settleErr
}

// settleNotification runs the crediting transaction and records its outcome.
//
// The transaction returns a typed sentinel instead of the final ledger result,
// because a refused callback must still be recorded outside the rollback: the
// gateway has to keep its evidence even when nothing was credited.
func (s *PaymentService) settleNotification(
	ctx context.Context,
	ledgerID string,
	channelValue domain.PaymentChannel,
	event channel.NotificationEvent,
	now time.Time,
) (NotificationOutcome, error) {
	outcome := NotificationOutcome{NotificationID: ledgerID, OrderNo: event.OrderNo}
	var refusal *settlementRefusal
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		order, lockErr := s.repository.LockOrderForSettlement(ctx, tx, event.OrderNo)
		if lockErr != nil {
			if errors.Is(lockErr, domain.ErrOrderNotFound) {
				refusal = &settlementRefusal{
					result: domain.NotificationResultOrderNotFound,
					cause:  fmt.Errorf("%w: order %s does not exist", ErrNotificationRejected, event.OrderNo),
				}
				return nil
			}
			return lockErr
		}
		outcome.OrderNo = order.OrderNo
		if amountErr := domain.ValidatePaymentAmount(domain.Cents(event.AmountCents), *order); amountErr != nil {
			refusal = &settlementRefusal{
				result: domain.NotificationResultAmountMismatch,
				cause:  amountErr,
			}
			return nil
		}
		if order.Status != domain.OrderStatusPending {
			// The sweep may have expired it or another callback may have paid
			// it; either way this callback must not change the order.
			refusal = &settlementRefusal{
				result: domain.NotificationResultOrderNotFound,
				cause: fmt.Errorf("%w: order %s is %q",
					ErrNotificationRejected, order.OrderNo, order.Status),
			}
			return nil
		}
		if payErr := s.settlePayment(ctx, tx, channelValue, event, *order, now); payErr != nil {
			return payErr
		}
		if payErr := s.repository.MarkOrderPaid(
			ctx, tx, order.ID, domain.Cents(event.AmountCents), paidAt(event, now),
		); payErr != nil {
			return payErr
		}
		order.Status = domain.OrderStatusPaid
		order.PaidCents = domain.Cents(event.AmountCents)
		if renewErr := s.renewSubscription(ctx, tx, *order, now); renewErr != nil {
			return renewErr
		}
		// The quota grant is the purchase's purpose: an order credited without
		// it would be money taken for nothing, so a grant failure rolls the
		// whole settlement back.
		if _, grantErr := s.quota.GrantPurchaseQuota(ctx, tx, *order, now); grantErr != nil {
			return grantErr
		}
		if markErr := s.repository.MarkNotificationProcessed(
			ctx, ledgerID, domain.NotificationResultAccepted, pointerTo(now),
		); markErr != nil {
			return markErr
		}
		outcome.Result = domain.NotificationResultAccepted
		return nil
	})
	if err != nil {
		return NotificationOutcome{}, err
	}
	if refusal != nil {
		// The refusal is recorded after the rollback on purpose: the callback
		// failed, so nothing may be credited, but the gateway still needs the
		// evidence that it arrived and why it was rejected.
		if markErr := s.repository.MarkNotificationProcessed(
			ctx, ledgerID, refusal.result, pointerTo(s.now()),
		); markErr != nil {
			return NotificationOutcome{}, markErr
		}
		outcome.Result = refusal.result
		s.recordAudit(ctx, BillingAuditEvent{
			Action:  "billing.notification.refused",
			OrderNo: outcome.OrderNo,
			Detail:  map[string]any{"result": string(refusal.result), "channel": string(channelValue)},
		})
		return outcome, refusal.cause
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:  "billing.payment.accepted",
		OrderNo: outcome.OrderNo,
		Detail: map[string]any{
			"channel":         string(channelValue),
			"notification_id": event.NotificationID,
		},
		CreatedAt: now,
	})
	return outcome, nil
}

// settlementRefusal is a gate that declined to credit, with the ledger result it
// must be recorded under.
type settlementRefusal struct {
	result domain.NotificationResult
	cause  error
}

// settlePayment records the successful attempt that settled the order.
//
// A callback may arrive before the client ever asked for a payment row, so the
// attempt is created on demand; the schema then guarantees one successful
// payment per order, and a second transfer is refused rather than credited.
func (s *PaymentService) settlePayment(
	ctx context.Context,
	tx pgx.Tx,
	channelValue domain.PaymentChannel,
	event channel.NotificationEvent,
	order domain.Order,
	now time.Time,
) error {
	paidAtValue := paidAt(event, now)
	if _, err := s.repository.FindSucceededPayment(ctx, tx, order.ID); err == nil {
		return fmt.Errorf("%w: order %s already has a successful payment",
			repository.ErrAlreadySettled, order.OrderNo)
	} else if !errors.Is(err, domain.ErrOrderNotFound) {
		return err
	}

	tradeNo := strings.TrimSpace(event.ChannelTradeNo)
	var payment *domain.Payment
	if tradeNo != "" {
		found, findErr := s.repository.GetPaymentByChannelTradeNo(ctx, channelValue, tradeNo)
		if findErr == nil {
			payment = found
		} else if !errors.Is(findErr, domain.ErrOrderNotFound) {
			return findErr
		}
	}
	if payment == nil {
		payment = &domain.Payment{
			ID:              uuid.NewString(),
			OrderID:         order.ID,
			Channel:         channelValue,
			ChannelTradeNo:  tradeNo,
			AmountCents:     domain.Cents(event.AmountCents),
			Status:          domain.PaymentStatusProcessing,
			ProviderPayload: map[string]any{},
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if createErr := s.repository.CreatePayment(ctx, *payment); createErr != nil {
			return createErr
		}
	}
	if markErr := s.repository.MarkPaymentSucceeded(
		ctx, tx, payment.ID, tradeNo, paidAtValue,
	); markErr != nil {
		return markErr
	}
	return nil
}

// renewSubscription opens or extends the subscription a periodic plan bought.
//
// A one_time plan never creates one, and a plan whose period months are absent
// is treated the same way rather than inventing a one-month term.
func (s *PaymentService) renewSubscription(
	ctx context.Context,
	tx pgx.Tx,
	order domain.Order,
	now time.Time,
) error {
	if order.BillingPeriod == domain.BillingPeriodOneTime || order.PeriodMonths <= 0 {
		return nil
	}
	start := now
	end := start.AddDate(0, order.PeriodMonths, 0)
	return s.repository.UpsertActiveSubscription(ctx, tx, domain.Subscription{
		ID:                 uuid.NewString(),
		ParentAccountID:    order.ParentAccountID,
		PlanID:             order.PlanID,
		PlanCode:           order.PlanCode,
		PlanDisplayName:    order.PlanDisplayName,
		Status:             domain.SubscriptionStatusActive,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   end,
		SourceOrderID:      order.ID,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
}

// markPaymentProcessing stores the provider trade number and moves a created
// attempt to processing.
func (s *PaymentService) markPaymentProcessing(
	ctx context.Context,
	payment domain.Payment,
	result channel.CreatePaymentResult,
	now time.Time,
) error {
	payload := map[string]any{}
	for key, value := range result.Payload {
		payload[key] = value
	}
	// Payload is public pre-payment data only; the schema's payload_object check
	// rejects anything that is not a JSON object.
	updated := payment
	updated.ProviderPayload = payload
	updated.UpdatedAt = now
	return s.repository.UpdatePaymentProcessing(ctx, updated, result.ChannelTradeNo)
}

// recordRejectedNotification records a callback that failed verification.
//
// The row is written whenever the provider gave an id we can key on, so a
// forged or misconfigured callback leaves evidence. The provider always gets the
// error: an unverified callback is never acknowledged as success.
func (s *PaymentService) recordRejectedNotification(
	ctx context.Context,
	channelValue domain.PaymentChannel,
	digest string,
	event channel.NotificationEvent,
	verifyErr error,
) (NotificationOutcome, error) {
	result := domain.NotificationResultOrderNotFound
	accepted := errors.Join(ErrNotificationRejected, verifyErr)
	if isSignatureInvalid(verifyErr) {
		result = domain.NotificationResultSignatureInvalid
	}
	outcome, recordErr := s.recordUnverified(ctx, channelValue, digest, event, result)
	if recordErr != nil {
		return NotificationOutcome{}, recordErr
	}
	if isChannelNotConfigured(verifyErr) {
		// No shared secret exists yet, so the guardian-facing state is "payment
		// not open" rather than "your receipt is forged".
		return outcome, notConfigured(verifyErr)
	}
	return outcome, accepted
}

// recordUnverified writes the ledger row for a callback that never verified.
func (s *PaymentService) recordUnverified(
	ctx context.Context,
	channelValue domain.PaymentChannel,
	digest string,
	event channel.NotificationEvent,
	result domain.NotificationResult,
) (NotificationOutcome, error) {
	now := s.now()
	if strings.TrimSpace(event.NotificationID) == "" {
		// Without an id there is no idempotency key, so there is nothing to
		// record; the caller still receives the verification error.
		return NotificationOutcome{Result: result, OrderNo: event.OrderNo}, nil
	}
	ledgerID := uuid.NewString()
	duplicate, err := s.repository.InsertNotification(ctx, domain.PaymentNotification{
		ID:                ledgerID,
		Channel:           channelValue,
		NotificationID:    event.NotificationID,
		PayloadDigest:     digest,
		SignatureVerified: false,
		ProcessedAt:       &now,
		Result:            result,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		return NotificationOutcome{}, err
	}
	if duplicate {
		// An earlier delivery already owns the row; leaving it untouched keeps
		// the first, authoritative result.
		existing, lookupErr := s.repository.GetNotification(ctx, channelValue, event.NotificationID)
		if lookupErr == nil {
			ledgerID = existing.ID
		}
	}
	return NotificationOutcome{NotificationID: ledgerID, Result: result, OrderNo: event.OrderNo}, nil
}

// resolveDuplicate answers a redelivered callback.
//
// An accepted row is a no-op success. A row that is still unprocessed means the
// first delivery is mid-flight, so the provider is told to retry instead of
// being told it succeeded when nothing was credited yet.
func (s *PaymentService) resolveDuplicate(
	ctx context.Context,
	channelValue domain.PaymentChannel,
	notificationID string,
) (NotificationOutcome, error) {
	existing, err := s.repository.GetNotification(ctx, channelValue, notificationID)
	if err != nil {
		return NotificationOutcome{}, err
	}
	outcome := NotificationOutcome{
		NotificationID: existing.ID,
		Result:         existing.Result,
		Duplicate:      true,
	}
	if existing.Result == domain.NotificationResultAccepted {
		return outcome, nil
	}
	if existing.ProcessedAt == nil {
		return outcome, fmt.Errorf("%w: notification %s", ErrNotificationStillProcessing, notificationID)
	}
	// A refused callback keeps its recorded result; answering success would
	// make the provider stop retrying a delivery the platform never settled.
	return outcome, fmt.Errorf("%w: notification %s was recorded as %s",
		ErrNotificationDuplicateNotification, notificationID, existing.Result)
}

// ErrNotificationDuplicateNotification reports a redelivery of a callback that
// was already recorded with a non-accepted result.
var ErrNotificationDuplicateNotification = errors.New("billing: payment notification was already refused")

// classifyChannelFailure maps a provider error to the columns an operator reads.
func classifyChannelFailure(err error) (code string, message string) {
	if isChannelNotConfigured(err) {
		return "channel_not_configured", "the payment provider is not configured"
	}
	return "channel_error", "the payment provider rejected the request"
}

// paidAt returns the provider's settlement time, falling back to now when the
// callback omitted it.
func paidAt(event channel.NotificationEvent, now time.Time) time.Time {
	if event.PaidAt.IsZero() {
		return now
	}
	return event.PaidAt.UTC()
}

// valueOrNow returns the pointed-to time or the fallback when it is nil.
func valueOrNow(value *time.Time, fallback time.Time) time.Time {
	if value == nil {
		return fallback
	}
	return *value
}

// pointerTo returns a pointer to a copy of value, for a nullable column.
func pointerTo(value time.Time) *time.Time {
	return &value
}

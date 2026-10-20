package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/channel"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RefundService moves money back to a guardian.
//
// The flow is deliberate: a guardian requests, an operator approves, and only
// then is the channel asked. Every money step runs in one transaction with the
// order row locked, because the refundable balance is the only thing that keeps
// a sequence of retries or concurrent approvals from returning more money than
// the guardian paid.
type RefundService struct {
	base
	quota *QuotaLedgerService
}

// NewRefundService creates the refund service.
func NewRefundService(options Options) (*RefundService, error) {
	shared, err := newBase(options)
	if err != nil {
		return nil, err
	}
	quotaService, err := NewQuotaLedgerService(options)
	if err != nil {
		return nil, err
	}
	return &RefundService{base: shared, quota: quotaService}, nil
}

// RequestRefundRequest is a guardian's refund request.
type RequestRefundRequest struct {
	ParentAccountID string
	OrderNo         string
	AmountCents     domain.Cents
	Reason          string
}

// RequestRefund records a guardian refund request.
//
// The amount may not exceed what is still refundable. The check counts every
// refund that is not rejected, not only the succeeded ones, because a second
// request must not queue money while the first is still moving through the
// channel.
func (s *RefundService) RequestRefund(
	ctx context.Context,
	request RequestRefundRequest,
) (*domain.Refund, error) {
	order, err := s.repository.GetOrderByOrderNo(ctx, strings.TrimSpace(request.OrderNo))
	if err != nil {
		return nil, err
	}
	if order.ParentAccountID != request.ParentAccountID || request.ParentAccountID == "" {
		return nil, fmt.Errorf("%w: order %s", domain.ErrCrossFamilyAccess, order.OrderNo)
	}
	if order.Status != domain.OrderStatusPaid &&
		order.Status != domain.OrderStatusPartiallyRefunded {
		return nil, fmt.Errorf("%w: order status is %q", ErrRefundNotRequestable, order.Status)
	}
	if err := domain.ValidateRefundAmount(
		request.AmountCents, order.PaidCents, order.RefundedCents,
	); err != nil {
		return nil, err
	}

	now := s.now()
	refund := domain.Refund{
		ID:          uuid.NewString(),
		RefundNo:    domain.NewRefundNumber(now),
		OrderID:     order.ID,
		AmountCents: request.AmountCents,
		Reason:      strings.TrimSpace(request.Reason),
		Status:      domain.RefundStatusRequested,
		RequestedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		// Locking the order serialises this request against a concurrent one and
		// against completion of an approved refund.
		locked, lockErr := s.repository.LockOrderByID(ctx, tx, order.ID)
		if lockErr != nil {
			return lockErr
		}
		// Only a settled payment can be reversed, and the refund names it so the
		// channel call later knows exactly which trade to refund.
		payment, payErr := s.repository.FindSucceededPayment(ctx, tx, order.ID)
		if payErr != nil {
			return fmt.Errorf("%w: order %s has no settled payment", ErrRefundNotRequestable, order.OrderNo)
		}
		refund.PaymentID = payment.ID
		pending, sumErr := s.repository.SumRefundCentsByStatus(
			ctx, tx, order.ID, refundableStatuses(),
		)
		if sumErr != nil {
			return sumErr
		}
		if overErr := domain.ValidateRefundAmount(
			request.AmountCents, locked.PaidCents, pending,
		); overErr != nil {
			return fmt.Errorf("%w: %s already committed to refunds on order %s",
				domain.ErrOverRefund, pending.Format(), order.OrderNo)
		}
		return s.repository.CreateRefund(ctx, tx, refund)
	})
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:          "billing.refund.requested",
		ParentAccountID: request.ParentAccountID,
		OrderNo:         order.OrderNo,
		RefundNo:        refund.RefundNo,
		Detail:          map[string]any{"amount": int64(refund.AmountCents)},
		CreatedAt:       now,
	})
	return &refund, nil
}

// ApproveRefund approves a requested refund in the operator's name.
//
// The over-refund guard runs again here: a refund requested before an earlier
// one succeeded could otherwise push the total past what was paid.
func (s *RefundService) ApproveRefund(
	ctx context.Context,
	operatorAccountID string,
	refundNo string,
) (*domain.Refund, error) {
	if strings.TrimSpace(operatorAccountID) == "" {
		return nil, ErrOperatorRequired
	}
	now := s.now()
	var approved *domain.Refund
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		refund, lockErr := s.lockRefundByNo(ctx, tx, refundNo)
		if lockErr != nil {
			return lockErr
		}
		if refund.Status != domain.RefundStatusRequested {
			return fmt.Errorf("%w: refund %s is %q",
				domain.ErrInvalidOrderTransition, refund.RefundNo, refund.Status)
		}
		if err := s.assertRefundWithinPaid(ctx, tx, refund); err != nil {
			return err
		}
		if err := s.repository.ApproveRefund(ctx, tx, refund.ID, operatorAccountID, now); err != nil {
			return err
		}
		refund.Status = domain.RefundStatusApproved
		refund.OperatorAccountID = operatorAccountID
		refund.ApprovedAt = &now
		refund.UpdatedAt = now
		approved = refund
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:         "billing.refund.approved",
		ActorAccountID: operatorAccountID,
		RefundNo:       approved.RefundNo,
		CreatedAt:      now,
	})
	return approved, nil
}

// RejectRefund rejects a refund that has not moved money yet.
func (s *RefundService) RejectRefund(
	ctx context.Context,
	operatorAccountID string,
	refundNo string,
) (*domain.Refund, error) {
	if strings.TrimSpace(operatorAccountID) == "" {
		return nil, ErrOperatorRequired
	}
	now := s.now()
	var rejected *domain.Refund
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		refund, lockErr := s.lockRefundByNo(ctx, tx, refundNo)
		if lockErr != nil {
			return lockErr
		}
		if refund.Status != domain.RefundStatusRequested &&
			refund.Status != domain.RefundStatusApproved &&
			refund.Status != domain.RefundStatusFailed {
			return fmt.Errorf("%w: refund %s is %q",
				domain.ErrInvalidOrderTransition, refund.RefundNo, refund.Status)
		}
		if err := s.repository.RejectRefund(ctx, tx, refund.ID, operatorAccountID, now); err != nil {
			return err
		}
		refund.Status = domain.RefundStatusRejected
		refund.UpdatedAt = now
		rejected = refund
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:         "billing.refund.rejected",
		ActorAccountID: operatorAccountID,
		RefundNo:       rejected.RefundNo,
		CreatedAt:      now,
	})
	return rejected, nil
}

// CompleteRefund asks the channel to return the money and records the result.
//
// Only an approved refund is completable: the approval is the operator's
// authorisation, and completing an unreviewed request would move money nobody
// signed off. An unconfigured channel fails before any state changes.
func (s *RefundService) CompleteRefund(
	ctx context.Context,
	refundNo string,
) (*domain.Refund, error) {
	refund, err := s.repository.GetRefundByNo(ctx, strings.TrimSpace(refundNo))
	if err != nil {
		return nil, err
	}
	if refund.Status != domain.RefundStatusApproved &&
		refund.Status != domain.RefundStatusFailed {
		return nil, fmt.Errorf("%w: refund %s is %q",
			ErrRefundNotCompletable, refund.RefundNo, refund.Status)
	}
	order, err := s.repository.GetOrderByID(ctx, refund.OrderID)
	if err != nil {
		return nil, err
	}
	payment, err := s.repository.GetPaymentByID(ctx, refund.PaymentID)
	if err != nil {
		return nil, fmt.Errorf("refund %s has no payment to reverse: %w", refund.RefundNo, err)
	}
	provider, err := s.resolveChannel(string(payment.Channel))
	if err != nil {
		return nil, err
	}

	var completed *domain.Refund
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		locked, lockErr := s.repository.LockRefund(ctx, tx, refund.ID)
		if lockErr != nil {
			return lockErr
		}
		if locked.Status != domain.RefundStatusApproved &&
			locked.Status != domain.RefundStatusFailed {
			return fmt.Errorf("%w: refund %s is %q",
				ErrRefundNotCompletable, locked.RefundNo, locked.Status)
		}
		if err := s.assertRefundWithinPaid(ctx, tx, locked); err != nil {
			return err
		}
		// The result is written before the provider is called so the attempt is
		// visible even if the process dies during the call.
		if markErr := s.repository.MarkRefundProcessing(ctx, tx, locked.ID, "", s.now()); markErr != nil {
			return markErr
		}
		result, callErr := provider.Refund(ctx, channel.RefundRequest{
			OrderNo:        order.OrderNo,
			ChannelTradeNo: locked.ChannelRefundNo,
			RefundNo:       locked.RefundNo,
			AmountCents:    int64(locked.AmountCents),
			Reason:         locked.Reason,
		})
		if callErr != nil {
			return &refundChannelFailure{cause: callErr}
		}
		if !result.Succeeded {
			// The provider accepted the request but the money is not back yet;
			// the refund stays processing until a refund callback arrives.
			locked.Status = domain.RefundStatusProcessing
			locked.UpdatedAt = s.now()
			completed = locked
			return nil
		}
		return s.finishRefundSuccess(ctx, tx, order, locked, result.ChannelRefundNo)
	})
	if err != nil {
		var failure *refundChannelFailure
		if errors.As(err, &failure) {
			// The channel refused or is absent: the refund returns to failed so an
			// operator can retry, and nothing is credited or clawed back.
			_ = s.withTx(ctx, func(tx pgx.Tx) error {
				return s.repository.MarkRefundFailed(ctx, tx, refund.ID, s.now())
			})
			if isChannelNotConfigured(failure.cause) {
				return nil, notConfigured(failure.cause)
			}
			return nil, fmt.Errorf("%w: refund %s", ErrChannelUnavailable, refund.RefundNo)
		}
		return nil, err
	}
	if completed != nil {
		return completed, nil
	}
	return s.repository.GetRefundByNo(ctx, refund.RefundNo)
}

// refundChannelFailure marks a channel call that must not roll back the attempt
// history, only the money.
type refundChannelFailure struct {
	cause error
}

// Error implements error.
func (failure *refundChannelFailure) Error() string {
	return failure.cause.Error()
}

// Unwrap exposes the provider error for errors.Is.
func (failure *refundChannelFailure) Unwrap() error {
	return failure.cause
}

// finishRefundSuccess applies a completed refund to the order and the quota.
//
// The order transition and the clawback share the transaction, so an order can
// never look fully refunded while the quota it bought is still granted.
func (s *RefundService) finishRefundSuccess(
	ctx context.Context,
	tx pgx.Tx,
	order *domain.Order,
	refund *domain.Refund,
	channelRefundNo string,
) error {
	now := s.now()
	if err := s.repository.MarkRefundSucceeded(ctx, tx, refund.ID, channelRefundNo, now); err != nil {
		return err
	}
	if err := s.repository.ApplyOrderRefund(ctx, tx, order.ID, refund.AmountCents, now); err != nil {
		return err
	}
	refund.Status = domain.RefundStatusSucceeded
	refund.CompletedAt = &now
	refund.UpdatedAt = now
	if _, err := s.quota.ClawBackRefundQuota(ctx, tx, *order, *refund, now); err != nil {
		return err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:    "billing.refund.succeeded",
		OrderNo:   order.OrderNo,
		RefundNo:  refund.RefundNo,
		Detail:    map[string]any{"amount": int64(refund.AmountCents)},
		CreatedAt: now,
	})
	return nil
}

// ListRefundsForParent returns the refunds of one order the family owns.
func (s *RefundService) ListRefundsForParent(
	ctx context.Context,
	parentAccountID string,
	orderNo string,
) ([]domain.Refund, error) {
	order, err := s.repository.GetOrderByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, err
	}
	if order.ParentAccountID != parentAccountID || parentAccountID == "" {
		return nil, fmt.Errorf("%w: order %s", domain.ErrCrossFamilyAccess, order.OrderNo)
	}
	return s.repository.ListRefundsByOrder(ctx, order.ID)
}

// lockRefundByNo resolves and locks one refund for a status change.
func (s *RefundService) lockRefundByNo(
	ctx context.Context,
	tx pgx.Tx,
	refundNo string,
) (*domain.Refund, error) {
	refund, err := s.repository.GetRefundByNo(ctx, strings.TrimSpace(refundNo))
	if err != nil {
		return nil, err
	}
	return s.repository.LockRefund(ctx, tx, refund.ID)
}

// assertRefundWithinPaid re-checks the over-refund guard under the order lock,
// so an approval cannot slip past a refund that completed in the meantime.
//
// The refund under review is excluded from the running total and its own amount
// is compared against what remains: every other counted refund is money already
// settled or in flight, and the sum may never exceed the paid total.
func (s *RefundService) assertRefundWithinPaid(
	ctx context.Context,
	tx pgx.Tx,
	refund *domain.Refund,
) error {
	order, err := s.repository.LockOrderByID(ctx, tx, refund.OrderID)
	if err != nil {
		return err
	}
	committed, err := s.repository.SumRefundCentsByStatus(ctx, tx, order.ID, refundableStatuses())
	if err != nil {
		return err
	}
	others := int64(committed) - int64(refund.AmountCents)
	remaining := int64(order.PaidCents) - others
	// remaining is what this refund may still claim. A negative remaining means
	// earlier refunds already overdraw the order, which fails just as loudly.
	if refund.AmountCents <= 0 || refund.AmountCents > domain.Cents(remaining) {
		return fmt.Errorf("%w: refund %s of %s exceeds %s left on order %s",
			domain.ErrOverRefund, refund.RefundNo, refund.AmountCents.Format(),
			domain.Cents(remaining).Format(), order.OrderNo)
	}
	return nil
}

// refundableStatuses lists the refund states that hold money against an order.
//
// A rejected refund releases its amount; every other state, including failed
// and processing, still reserves it, so a retry cannot double-book the same
// money while a channel call is in flight.
func refundableStatuses() []domain.RefundStatus {
	return []domain.RefundStatus{
		domain.RefundStatusRequested,
		domain.RefundStatusApproved,
		domain.RefundStatusProcessing,
		domain.RefundStatusSucceeded,
		domain.RefundStatusFailed,
	}
}

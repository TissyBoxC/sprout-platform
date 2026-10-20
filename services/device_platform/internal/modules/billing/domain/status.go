package domain

import "fmt"

// PaymentStatus is the lifecycle of one payment attempt (design doc §3.3).
//
// An order may have several attempts: a channel timeout is retried on another
// channel, so a previous failure never blocks a new attempt.
type PaymentStatus string

const (
	PaymentStatusCreated    PaymentStatus = "created"
	PaymentStatusProcessing PaymentStatus = "processing"
	PaymentStatusSucceeded  PaymentStatus = "succeeded"
	PaymentStatusFailed     PaymentStatus = "failed"
	PaymentStatusClosed     PaymentStatus = "closed"
)

var paymentTransitions = map[PaymentStatus][]PaymentStatus{
	PaymentStatusCreated: {
		PaymentStatusProcessing,
		PaymentStatusFailed,
		PaymentStatusClosed,
	},
	PaymentStatusProcessing: {
		PaymentStatusSucceeded,
		PaymentStatusFailed,
		PaymentStatusClosed,
	},
	// A succeeded payment is settled money: it only ends through a refund,
	// which is not a payment-status change.
	PaymentStatusSucceeded: {},
	PaymentStatusFailed:    {},
	PaymentStatusClosed:    {},
}

// PaymentStatuses lists every declared payment status for exhaustive checks.
var PaymentStatuses = []PaymentStatus{
	PaymentStatusCreated,
	PaymentStatusProcessing,
	PaymentStatusSucceeded,
	PaymentStatusFailed,
	PaymentStatusClosed,
}

// CanTransition reports whether a payment may move from one status to another.
func (status PaymentStatus) CanTransition(to PaymentStatus) bool {
	return CanTransitionPayment(status, to)
}

// IsKnown reports whether the value is a declared payment status.
func (status PaymentStatus) IsKnown() bool {
	_, ok := paymentTransitions[status]
	return ok
}

// CanTransitionPayment reports whether from may legally become to for a
// payment attempt. Self-transitions are rejected for the same reason as orders.
func CanTransitionPayment(from, to PaymentStatus) bool {
	for _, allowed := range paymentTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// TransitionPayment returns nil when legal, or an error wrapping
// ErrInvalidOrderTransition.
func TransitionPayment(from, to PaymentStatus) error {
	if CanTransitionPayment(from, to) {
		return nil
	}
	return fmt.Errorf("%w: payment %q cannot become %q", ErrInvalidOrderTransition, from, to)
}

// RefundStatus is the lifecycle of a refund approval (design doc §3.5).
type RefundStatus string

const (
	RefundStatusRequested  RefundStatus = "requested"
	RefundStatusApproved   RefundStatus = "approved"
	RefundStatusProcessing RefundStatus = "processing"
	RefundStatusSucceeded  RefundStatus = "succeeded"
	RefundStatusFailed     RefundStatus = "failed"
	RefundStatusRejected   RefundStatus = "rejected"
)

var refundTransitions = map[RefundStatus][]RefundStatus{
	RefundStatusRequested: {
		RefundStatusApproved,
		RefundStatusRejected,
	},
	RefundStatusApproved: {
		RefundStatusProcessing,
		// An approved refund may still be rejected before money moves, for
		// example when a channel blocks the reversal after approval.
		RefundStatusRejected,
		RefundStatusFailed,
	},
	// A failed channel call is retried from processing, never from requested:
	// retrying must not re-run the approval that authorised the money.
	RefundStatusProcessing: {
		RefundStatusSucceeded,
		RefundStatusFailed,
	},
	RefundStatusFailed: {
		RefundStatusProcessing,
		RefundStatusRejected,
	},
	RefundStatusSucceeded: {},
	RefundStatusRejected:  {},
}

// RefundStatuses lists every declared refund status for exhaustive checks.
var RefundStatuses = []RefundStatus{
	RefundStatusRequested,
	RefundStatusApproved,
	RefundStatusProcessing,
	RefundStatusSucceeded,
	RefundStatusFailed,
	RefundStatusRejected,
}

// CanTransition reports whether a refund may move from one status to another.
func (status RefundStatus) CanTransition(to RefundStatus) bool {
	return CanTransitionRefund(status, to)
}

// IsKnown reports whether the value is a declared refund status.
func (status RefundStatus) IsKnown() bool {
	_, ok := refundTransitions[status]
	return ok
}

// IsTerminal reports whether the refund has reached its final outcome.
func (status RefundStatus) IsTerminal() bool {
	return status == RefundStatusSucceeded || status == RefundStatusRejected
}

// CanTransitionRefund reports whether from may legally become to for a refund.
func CanTransitionRefund(from, to RefundStatus) bool {
	for _, allowed := range refundTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// TransitionRefund returns nil when legal, or an error wrapping
// ErrInvalidOrderTransition.
func TransitionRefund(from, to RefundStatus) error {
	if CanTransitionRefund(from, to) {
		return nil
	}
	return fmt.Errorf("%w: refund %q cannot become %q", ErrInvalidOrderTransition, from, to)
}

package domain

import (
	"errors"
	"testing"
)

// legalOrderEdges is the complete order state machine of design doc §3.2,
// written independently of the production table so the test cannot pass by
// copying a bug.
var legalOrderEdges = map[OrderStatus]map[OrderStatus]struct{}{
	OrderStatusPending: {
		OrderStatusPaid:      {},
		OrderStatusCancelled: {},
		OrderStatusExpired:   {},
	},
	OrderStatusPaid: {
		OrderStatusRefunded:          {},
		OrderStatusPartiallyRefunded: {},
	},
	OrderStatusPartiallyRefunded: {
		OrderStatusRefunded: {},
	},
	OrderStatusCancelled: {},
	OrderStatusExpired:   {},
	OrderStatusRefunded:  {},
}

func TestCanTransitionCoversEveryPairOfTheSixBySixMatrix(t *testing.T) {
	t.Parallel()

	if len(OrderStatuses) != 6 {
		t.Fatalf("order status count = %d, want 6", len(OrderStatuses))
	}
	legal := 0
	for _, from := range OrderStatuses {
		for _, to := range OrderStatuses {
			_, want := legalOrderEdges[from][to]
			got := CanTransition(from, to)
			if got != want {
				t.Fatalf("CanTransition(%q, %q) = %v, want %v", from, to, got, want)
			}
			if got {
				legal++
				if err := Transition(from, to); err != nil {
					t.Fatalf("Transition(%q, %q) error = %v, want nil", from, to, err)
				}
				continue
			}
			if err := Transition(from, to); !errors.Is(err, ErrInvalidOrderTransition) {
				t.Fatalf("Transition(%q, %q) error = %v, want ErrInvalidOrderTransition", from, to, err)
			}
		}
	}
	// pending: 3 legal, paid: 2, partially_refunded: 1, the three terminals: 0.
	if legal != 6 {
		t.Fatalf("legal order transitions = %d, want exactly 6", legal)
	}
}

func TestCanTransitionRejectsSameStateAndUnknownStatus(t *testing.T) {
	t.Parallel()

	for _, status := range OrderStatuses {
		if CanTransition(status, status) {
			t.Fatalf("CanTransition(%q, %q) must be false: re-applying a status is not an edge", status, status)
		}
	}
	unknown := OrderStatus("refunding")
	for _, from := range OrderStatuses {
		if CanTransition(from, unknown) {
			t.Fatalf("CanTransition(%q, unknown) must be false", from)
		}
	}
	for _, to := range OrderStatuses {
		if CanTransition(unknown, to) {
			t.Fatalf("CanTransition(unknown, %q) must be false", to)
		}
	}
	if CanTransition(unknown, unknown) {
		t.Fatal("unknown status must not transition to itself")
	}
	if unknown.IsKnown() {
		t.Fatal("unknown order status reported as known")
	}
	for _, status := range OrderStatuses {
		if !status.IsKnown() {
			t.Fatalf("declared order status %q reported as unknown", status)
		}
	}
}

func TestOrderTerminalStatusesHaveNoOutgoingEdge(t *testing.T) {
	t.Parallel()

	for _, status := range OrderStatuses {
		hasEdge := false
		for _, to := range OrderStatuses {
			if CanTransition(status, to) {
				hasEdge = true
			}
		}
		if status.IsTerminal() == hasEdge {
			t.Fatalf("IsTerminal(%q) = %v but hasOutgoingEdge = %v", status, status.IsTerminal(), hasEdge)
		}
	}
	if !OrderStatusRefunded.IsTerminal() {
		t.Fatal("refunded must be terminal")
	}
}

func TestPaidAndPartiallyRefundedCanNeverBeCancelledOrExpired(t *testing.T) {
	t.Parallel()

	// Design doc §3.2: "paid、partially_refunded 不可再取消".
	for _, from := range []OrderStatus{OrderStatusPaid, OrderStatusPartiallyRefunded} {
		for _, to := range []OrderStatus{OrderStatusCancelled, OrderStatusExpired, OrderStatusPending} {
			if CanTransition(from, to) {
				t.Fatalf("CanTransition(%q, %q) must be false", from, to)
			}
		}
	}
}

func TestPaymentCanTransitionIsExhaustive(t *testing.T) {
	t.Parallel()

	legal := map[PaymentStatus]map[PaymentStatus]struct{}{
		PaymentStatusCreated: {
			PaymentStatusProcessing: {},
			PaymentStatusFailed:     {},
			PaymentStatusClosed:     {},
		},
		PaymentStatusProcessing: {
			PaymentStatusSucceeded: {},
			PaymentStatusFailed:    {},
			PaymentStatusClosed:    {},
		},
		PaymentStatusSucceeded: {},
		PaymentStatusFailed:    {},
		PaymentStatusClosed:    {},
	}
	if len(PaymentStatuses) != 5 {
		t.Fatalf("payment status count = %d, want 5", len(PaymentStatuses))
	}
	for _, from := range PaymentStatuses {
		for _, to := range PaymentStatuses {
			_, want := legal[from][to]
			if got := CanTransitionPayment(from, to); got != want {
				t.Fatalf("CanTransitionPayment(%q, %q) = %v, want %v", from, to, got, want)
			}
			if got := from.CanTransition(to); got != want {
				t.Fatalf("PaymentStatus(%q).CanTransition(%q) = %v, want %v", from, to, got, want)
			}
			if want {
				if err := TransitionPayment(from, to); err != nil {
					t.Fatalf("TransitionPayment(%q, %q) error = %v", from, to, err)
				}
				continue
			}
			if err := TransitionPayment(from, to); !errors.Is(err, ErrInvalidOrderTransition) {
				t.Fatalf("TransitionPayment(%q, %q) error = %v, want ErrInvalidOrderTransition", from, to, err)
			}
		}
	}
	if CanTransitionPayment(PaymentStatus("settled"), PaymentStatusSucceeded) {
		t.Fatal("unknown payment status must not transition")
	}
}

func TestRefundCanTransitionIsExhaustive(t *testing.T) {
	t.Parallel()

	legal := map[RefundStatus]map[RefundStatus]struct{}{
		RefundStatusRequested: {
			RefundStatusApproved: {},
			RefundStatusRejected: {},
		},
		RefundStatusApproved: {
			RefundStatusProcessing: {},
			RefundStatusRejected:   {},
			RefundStatusFailed:     {},
		},
		RefundStatusProcessing: {
			RefundStatusSucceeded: {},
			RefundStatusFailed:    {},
		},
		RefundStatusFailed: {
			RefundStatusProcessing: {},
			RefundStatusRejected:   {},
		},
		RefundStatusSucceeded: {},
		RefundStatusRejected:  {},
	}
	if len(RefundStatuses) != 6 {
		t.Fatalf("refund status count = %d, want 6", len(RefundStatuses))
	}
	for _, from := range RefundStatuses {
		for _, to := range RefundStatuses {
			_, want := legal[from][to]
			if got := CanTransitionRefund(from, to); got != want {
				t.Fatalf("CanTransitionRefund(%q, %q) = %v, want %v", from, to, got, want)
			}
			if got := from.CanTransition(to); got != want {
				t.Fatalf("RefundStatus(%q).CanTransition(%q) = %v, want %v", from, to, got, want)
			}
			if want {
				if err := TransitionRefund(from, to); err != nil {
					t.Fatalf("TransitionRefund(%q, %q) error = %v", from, to, err)
				}
				continue
			}
			if err := TransitionRefund(from, to); !errors.Is(err, ErrInvalidOrderTransition) {
				t.Fatalf("TransitionRefund(%q, %q) error = %v, want ErrInvalidOrderTransition", from, to, err)
			}
		}
	}
	for _, status := range RefundStatuses {
		want := status == RefundStatusSucceeded || status == RefundStatusRejected
		if status.IsTerminal() != want {
			t.Fatalf("RefundStatus(%q).IsTerminal() = %v, want %v", status, status.IsTerminal(), want)
		}
	}
}

func TestRefundCannotRestartFromSucceeded(t *testing.T) {
	t.Parallel()

	for _, to := range RefundStatuses {
		if CanTransitionRefund(RefundStatusSucceeded, to) {
			t.Fatalf("succeeded refund must not move to %q", to)
		}
	}
}

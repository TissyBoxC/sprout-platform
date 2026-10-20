package domain

import "fmt"

// OrderStatus is the lifecycle of a billing order (design doc §3.2).
type OrderStatus string

const (
	OrderStatusPending           OrderStatus = "pending"
	OrderStatusPaid              OrderStatus = "paid"
	OrderStatusCancelled         OrderStatus = "cancelled"
	OrderStatusExpired           OrderStatus = "expired"
	OrderStatusRefunded          OrderStatus = "refunded"
	OrderStatusPartiallyRefunded OrderStatus = "partially_refunded"
)

// orderTransitions is the complete legal order state machine from design doc
// §3.2, transcribed node by node.
//
//	pending ──支付成功──> paid ──全额退款──> refunded
//	   │                   │
//	   │                   └──部分退款──> partially_refunded
//	   ├──用户取消──> cancelled
//	   └──超时未付──> expired
//
// The doc also states paid and partially_refunded can no longer be cancelled
// and cannot change amount, so neither may leave for cancelled or expired.
// Every state not listed here is terminal and every absent edge is illegal.
var orderTransitions = map[OrderStatus][]OrderStatus{
	OrderStatusPending: {
		OrderStatusPaid,
		OrderStatusCancelled,
		OrderStatusExpired,
	},
	OrderStatusPaid: {
		OrderStatusRefunded,
		OrderStatusPartiallyRefunded,
	},
	OrderStatusPartiallyRefunded: {
		OrderStatusRefunded,
	},
	OrderStatusCancelled: {},
	OrderStatusExpired:   {},
	OrderStatusRefunded:  {},
}

// OrderStatuses lists every declared order status for exhaustive checks.
var OrderStatuses = []OrderStatus{
	OrderStatusPending,
	OrderStatusPaid,
	OrderStatusCancelled,
	OrderStatusExpired,
	OrderStatusRefunded,
	OrderStatusPartiallyRefunded,
}

// IsTerminal reports whether the order can never change state again.
func (status OrderStatus) IsTerminal() bool {
	return status == OrderStatusCancelled ||
		status == OrderStatusExpired ||
		status == OrderStatusRefunded
}

// IsKnown reports whether the value is a declared order status, so a status
// read back from storage or an API can never be treated as pending by default.
func (status OrderStatus) IsKnown() bool {
	_, ok := orderTransitions[status]
	return ok
}

// CanTransition reports whether from may legally become to.
//
// A same-state transition is rejected: re-applying the current status is not a
// legal edge, so a duplicated callback cannot re-run a settlement side effect
// that is supposed to happen once.
func CanTransition(from, to OrderStatus) bool {
	for _, allowed := range orderTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Transition returns nil when the change is legal and an error wrapping
// ErrInvalidOrderTransition otherwise.
func Transition(from, to OrderStatus) error {
	if CanTransition(from, to) {
		return nil
	}
	return fmt.Errorf("%w: order %q cannot become %q", ErrInvalidOrderTransition, from, to)
}

package service

import (
	"context"
)

// ExpiryService closes orders and subscriptions whose time ran out.
//
// It exists as its own service because it is scheduled rather than requested:
// the job runs on a timer, must be safe to run concurrently on several replicas,
// and must never overwrite a payment that won the race.
type ExpiryService struct {
	base
}

// NewExpiryService creates the expiry service.
func NewExpiryService(options Options) (*ExpiryService, error) {
	shared, err := newBase(options)
	if err != nil {
		return nil, err
	}
	return &ExpiryService{base: shared}, nil
}

// ExpireResult reports what one sweep closed.
type ExpireResult struct {
	ExpiredOrderIDs      []string
	ExpiredSubscriptions int
}

// ExpireOrders moves due pending orders to expired and returns them.
//
// The update is conditional on status = 'pending' inside the repository, so an
// order a callback settled a moment earlier is skipped rather than reopened.
// SKIP LOCKED lets several replicas drain one backlog without waiting on each
// other.
func (s *ExpiryService) ExpireOrders(ctx context.Context) (ExpireResult, error) {
	now := s.now()
	orderIDs, err := s.repository.ExpireStaleOrders(ctx, now, 0)
	if err != nil {
		return ExpireResult{}, err
	}
	result := ExpireResult{ExpiredOrderIDs: orderIDs}
	for _, orderID := range orderIDs {
		s.recordAudit(ctx, BillingAuditEvent{
			Action:    "billing.order.expired",
			Detail:    map[string]any{"order_id": orderID},
			CreatedAt: now,
		})
	}
	return result, nil
}

// ExpireSubscriptions moves subscriptions whose period ended to expired.
func (s *ExpiryService) ExpireSubscriptions(ctx context.Context) (int, error) {
	return s.repository.ExpireDueSubscriptions(ctx, s.now())
}

// ExpireDue runs the whole sweep: orders and subscriptions in one pass.
func (s *ExpiryService) ExpireDue(ctx context.Context) (ExpireResult, error) {
	result, err := s.ExpireOrders(ctx)
	if err != nil {
		return ExpireResult{}, err
	}
	expiredSubscriptions, err := s.ExpireSubscriptions(ctx)
	if err != nil {
		return result, err
	}
	result.ExpiredSubscriptions = expiredSubscriptions
	return result, nil
}

// Backlog reports how many orders are pending and how many are already overdue,
// which the Feature Center probe renders as billing_orders health.
func (s *ExpiryService) Backlog(ctx context.Context) (pending int, overdue int, err error) {
	return s.repository.CountPendingOrders(ctx, s.now())
}

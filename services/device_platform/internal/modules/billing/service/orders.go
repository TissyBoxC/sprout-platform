package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
	"github.com/google/uuid"
)

// OrderService creates, reads, and cancels guardian orders.
//
// Every guardian-facing read is scoped to the authenticated family: an order
// number is guessable enough that ownership must be checked on every path, so a
// cross-family read returns domain.ErrCrossFamilyAccess rather than 404.
type OrderService struct {
	base
}

// NewOrderService creates the order service.
func NewOrderService(options Options) (*OrderService, error) {
	shared, err := newBase(options)
	if err != nil {
		return nil, err
	}
	return &OrderService{base: shared}, nil
}

// CreateOrderRequest is one guardian checkout.
type CreateOrderRequest struct {
	ParentAccountID string
	PlanCode        string
	// IdempotencyKey is the client's retry key for this checkout attempt.
	IdempotencyKey string
	ClientIP       string
	// ConsentVersion is the terms version the guardian confirmed; empty means
	// the current SpendingConsentVersion.
	ConsentVersion string
	// DiscountCents is an operator-granted discount, zero for a normal sale.
	DiscountCents domain.Cents
}

// CreateOrderResult reports the created order and the quota grant outcome.
type CreateOrderResult struct {
	Order domain.Order
	// Reused is true when a retry resolved to the order created earlier.
	Reused bool
}

// CreateOrder creates one pending order for an active plan.
//
// Order of checks matters: the spending consent comes first because a guardian
// who has not agreed may not spend, and the idempotency key is honoured before
// any plan lookup so a retry returns the original order even if the plan has
// since been archived or repriced.
func (s *OrderService) CreateOrder(
	ctx context.Context,
	request CreateOrderRequest,
) (CreateOrderResult, error) {
	parentAccountID := strings.TrimSpace(request.ParentAccountID)
	if parentAccountID == "" {
		return CreateOrderResult{}, invalidRequest("parent account is required")
	}
	idempotencyKey := strings.TrimSpace(request.IdempotencyKey)
	if idempotencyKey == "" {
		return CreateOrderResult{}, ErrIdempotencyKeyRequired
	}

	consent, err := s.activeConsent(ctx, parentAccountID, request.ConsentVersion)
	if err != nil {
		return CreateOrderResult{}, err
	}
	if consent == nil {
		return CreateOrderResult{}, domain.ErrSpendingConsentRequired
	}

	existing, err := s.repository.GetOrderByIdentityOrIdempotency(ctx, parentAccountID, idempotencyKey)
	if err == nil {
		return CreateOrderResult{Order: *existing, Reused: true}, nil
	}
	if !errors.Is(err, domain.ErrOrderNotFound) {
		return CreateOrderResult{}, err
	}

	plan, err := s.repository.GetPlanByCode(ctx, strings.TrimSpace(request.PlanCode))
	if err != nil {
		return CreateOrderResult{}, err
	}
	if plan.Status != domain.PlanStatusActive {
		return CreateOrderResult{}, fmt.Errorf("%w: plan %q is %s",
			domain.ErrPlanNotActive, plan.Code, plan.Status)
	}
	if request.DiscountCents < 0 {
		return CreateOrderResult{}, invalidRequest("discount must not be negative")
	}
	payable, err := plan.PriceCents.Sub(request.DiscountCents)
	if err != nil {
		return CreateOrderResult{}, fmt.Errorf("%w: %v", domain.ErrInvalidOrderAmounts, err)
	}
	if payable <= 0 {
		// A free order has no payment path: it would need a separate fulfilment
		// path, and settling it through a callback would be fabricated success.
		return CreateOrderResult{}, fmt.Errorf(
			"%w: payable amount %s is not positive", domain.ErrInvalidOrderAmounts, payable.Format())
	}

	now := s.now()
	expiresAt := now.Add(OrderPaymentWindow)
	order := domain.Order{
		ID:              uuid.NewString(),
		OrderNo:         domain.NewOrderNumber(now),
		ParentAccountID: parentAccountID,
		PlanID:          plan.ID,
		PlanCode:        plan.Code,
		PlanDisplayName: plan.DisplayName,
		BillingPeriod:   plan.BillingPeriod,
		PeriodMonths:    plan.PeriodMonths,
		AIQuotaUSD:      plan.AIQuotaUSD,
		// Snapshot the validity window with the quota itself, so a later plan
		// edit cannot change when this order's already-paid quota lapses.
		QuotaValidityDays: plan.QuotaValidityDays,
		UnitPriceCents:    plan.PriceCents,
		DiscountCents:     request.DiscountCents,
		PayableCents:      payable,
		Status:            domain.OrderStatusPending,
		IdempotencyKey:    idempotencyKey,
		ExpiresAt:         &expiresAt,
		ClientIP:          strings.TrimSpace(request.ClientIP),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := domain.ValidateOrderAmounts(order); err != nil {
		return CreateOrderResult{}, err
	}
	created, err := s.repository.CreateOrder(ctx, order)
	if err != nil {
		return CreateOrderResult{}, err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:          "billing.order.created",
		ParentAccountID: parentAccountID,
		OrderNo:         created.OrderNo,
		Detail: map[string]any{
			"plan_code":  created.PlanCode,
			"payable":    int64(created.PayableCents),
			"consent_id": consent.ID,
		},
		CreatedAt: now,
	})
	return CreateOrderResult{Order: *created, Reused: created.ID != order.ID}, nil
}

// GetOrderForParent returns one order that must belong to the calling family.
//
// A mismatch is reported as domain.ErrCrossFamilyAccess, not as "not found":
// the order exists, the caller simply has no right to see it, and an operator
// debugging an authorization report needs to tell those two cases apart.
func (s *OrderService) GetOrderForParent(
	ctx context.Context,
	parentAccountID string,
	orderNo string,
) (*domain.Order, error) {
	order, err := s.repository.GetOrderByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, err
	}
	if order.ParentAccountID != parentAccountID || parentAccountID == "" {
		return nil, fmt.Errorf("%w: order %s", domain.ErrCrossFamilyAccess, order.OrderNo)
	}
	return order, nil
}

// ListOrdersForParent returns one family's orders with the total count.
func (s *OrderService) ListOrdersForParent(
	ctx context.Context,
	parentAccountID string,
	page Page,
) ([]domain.Order, int, error) {
	if strings.TrimSpace(parentAccountID) == "" {
		return nil, 0, invalidRequest("parent account is required")
	}
	normalized := page.normalized()
	return s.repository.ListOrdersByParent(ctx, parentAccountID, normalized.Limit, normalized.Offset)
}

// ListOrders returns a page of orders for the operator console.
func (s *OrderService) ListOrders(
	ctx context.Context,
	filter repository.OrderFilter,
) ([]domain.Order, int, error) {
	if filter.Status != "" && !filter.Status.IsKnown() {
		return nil, 0, invalidRequest("unknown order status %q", filter.Status)
	}
	page := Page{Limit: filter.Limit, Offset: filter.Offset}.normalized()
	filter.Limit = page.Limit
	filter.Offset = page.Offset
	return s.repository.ListOrders(ctx, filter)
}

// CancelOrder cancels a pending order of the calling family.
//
// A paid order is refused here: money already taken can only come back through
// a refund, which is a different, audited path.
func (s *OrderService) CancelOrder(
	ctx context.Context,
	parentAccountID string,
	orderNo string,
) (*domain.Order, error) {
	order, err := s.GetOrderForParent(ctx, parentAccountID, orderNo)
	if err != nil {
		return nil, err
	}
	if order.Status != domain.OrderStatusPending {
		return nil, fmt.Errorf("%w: order status is %q", ErrOrderNotCancellable, order.Status)
	}
	now := s.now()
	if err := s.repository.CancelPendingOrder(ctx, parentAccountID, order.OrderNo, now); err != nil {
		if errors.Is(err, repository.ErrAlreadySettled) {
			// The callback won the race between the check and the update.
			return nil, fmt.Errorf("%w: order changed state concurrently", ErrOrderNotCancellable)
		}
		return nil, err
	}
	order.Status = domain.OrderStatusCancelled
	order.CancelledAt = &now
	order.UpdatedAt = now
	s.recordAudit(ctx, BillingAuditEvent{
		Action:          "billing.order.cancelled",
		ParentAccountID: parentAccountID,
		OrderNo:         order.OrderNo,
		CreatedAt:       now,
	})
	return order, nil
}

// activeConsent returns the family's live spending consent, or nil.
//
// A caller may name an older terms version; that version's live row is then
// required, so consent to one text never silently authorises a different one.
func (s *OrderService) activeConsent(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
) (*domain.SpendingConsent, error) {
	version := strings.TrimSpace(consentVersion)
	if version == "" {
		version = SpendingConsentVersion
	}
	return s.repository.GetActiveSpendingConsent(ctx, parentAccountID, version)
}

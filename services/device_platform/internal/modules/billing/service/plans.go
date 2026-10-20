package service

import (
	"context"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
	"github.com/google/uuid"
)

// PlanService maintains the purchasable catalogue: guardian-facing listings and
// operator editing with optimistic concurrency.
type PlanService struct {
	base
}

// NewPlanService creates the plan service.
func NewPlanService(options Options) (*PlanService, error) {
	shared, err := newBase(options)
	if err != nil {
		return nil, err
	}
	return &PlanService{base: shared}, nil
}

// ListActivePlans returns the plans a guardian may buy, in display order.
func (s *PlanService) ListActivePlans(ctx context.Context) ([]domain.Plan, error) {
	return s.repository.ListActivePlans(ctx)
}

// GetPlan returns one plan by id, or domain.ErrPlanNotFound.
func (s *PlanService) GetPlan(ctx context.Context, planID string) (*domain.Plan, error) {
	return s.repository.GetPlanByID(ctx, planID)
}

// ListPlans returns a page of plans for the operator console.
func (s *PlanService) ListPlans(
	ctx context.Context,
	status domain.PlanStatus,
	search string,
	page Page,
) ([]domain.Plan, int, error) {
	if status != "" && !isKnownPlanStatus(status) {
		return nil, 0, invalidRequest("unknown plan status %q", status)
	}
	normalized := page.normalized()
	return s.repository.ListPlans(ctx, repository.PlanFilter{
		Status: status,
		Search: strings.TrimSpace(search),
		Limit:  normalized.Limit,
		Offset: normalized.Offset,
	})
}

// CreatePlan inserts a plan in draft unless the operator asks for active.
//
// Publishing straight from creation is allowed only for an active plan with a
// positive price, because an active plan is purchasable the moment it exists.
func (s *PlanService) CreatePlan(ctx context.Context, input PlanInput) (*domain.Plan, error) {
	now := s.now()
	status := input.Status
	if status == "" {
		status = domain.PlanStatusDraft
	}
	plan := domain.Plan{
		ID:                 uuid.NewString(),
		Code:               strings.TrimSpace(input.Code),
		DisplayName:        strings.TrimSpace(input.DisplayName),
		Description:        strings.TrimSpace(input.Description),
		BillingPeriod:      input.BillingPeriod,
		PeriodMonths:       input.PeriodMonths,
		PriceCents:         input.PriceCents,
		OriginalPriceCents: input.OriginalPriceCents,
		AIQuotaUSD:         normalizeQuotaText(input.AIQuotaUSD),
		QuotaValidityDays:  input.QuotaValidityDays,
		Status:             status,
		SortOrder:          input.SortOrder,
		Version:            1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if plan.Status == domain.PlanStatusActive {
		publishedAt := now
		plan.PublishedAt = &publishedAt
	}
	if err := domain.ValidatePlan(plan); err != nil {
		return nil, err
	}
	if err := s.repository.CreatePlan(ctx, plan); err != nil {
		return nil, err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:    "billing.plan.created",
		Detail:    map[string]any{"plan_code": plan.Code, "status": string(plan.Status)},
		CreatedAt: now,
	})
	return &plan, nil
}

// UpdatePlan saves an edit only when expectedVersion still matches.
//
// The version comes from the client, not from a fresh read: that is what makes
// two operators editing the same published price a detected conflict instead of
// a silent overwrite.
func (s *PlanService) UpdatePlan(
	ctx context.Context,
	planID string,
	expectedVersion int,
	input PlanInput,
) (*domain.Plan, error) {
	if expectedVersion < 1 {
		return nil, invalidRequest("expected version must be positive")
	}
	existing, err := s.repository.GetPlanByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	status := input.Status
	if status == "" {
		status = existing.Status
	}
	updated := *existing
	updated.DisplayName = strings.TrimSpace(input.DisplayName)
	updated.Description = strings.TrimSpace(input.Description)
	updated.BillingPeriod = input.BillingPeriod
	updated.PeriodMonths = input.PeriodMonths
	updated.PriceCents = input.PriceCents
	updated.OriginalPriceCents = input.OriginalPriceCents
	updated.AIQuotaUSD = normalizeQuotaText(input.AIQuotaUSD)
	updated.QuotaValidityDays = input.QuotaValidityDays
	updated.Status = status
	updated.SortOrder = input.SortOrder
	updated.UpdatedAt = now
	// The code is the stable external identifier and never changes; the input
	// code is only accepted when it matches, so a client cannot rename history.
	if input.Code != "" && strings.TrimSpace(input.Code) != existing.Code {
		return nil, invalidRequest("plan code %q is immutable", input.Code)
	}
	if updated.Status == domain.PlanStatusActive && updated.PublishedAt == nil {
		updated.PublishedAt = &now
	}
	if err := domain.ValidatePlan(updated); err != nil {
		return nil, err
	}
	if err := s.repository.UpdatePlan(ctx, updated, expectedVersion); err != nil {
		return nil, err
	}
	updated.Version = expectedVersion + 1
	s.recordAudit(ctx, BillingAuditEvent{
		Action:    "billing.plan.updated",
		Detail:    map[string]any{"plan_code": updated.Code, "version": updated.Version},
		CreatedAt: now,
	})
	return &updated, nil
}

// ArchivePlan retires a plan, keeping its code reserved for history.
func (s *PlanService) ArchivePlan(ctx context.Context, planID string) (*domain.Plan, error) {
	now := s.now()
	if err := s.repository.ArchivePlan(ctx, planID, now); err != nil {
		return nil, err
	}
	plan, err := s.repository.GetPlanByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:    "billing.plan.archived",
		Detail:    map[string]any{"plan_code": plan.Code},
		CreatedAt: now,
	})
	return plan, nil
}

// PlanInput is the editable surface of a plan.
//
// Every amount is already in cents and the quota is the provider's decimal
// string; there is no place in this struct where a float could carry money.
type PlanInput struct {
	Code               string
	DisplayName        string
	Description        string
	BillingPeriod      domain.BillingPeriod
	PeriodMonths       int
	PriceCents         domain.Cents
	OriginalPriceCents domain.Cents
	AIQuotaUSD         string
	// QuotaValidityDays bounds how long the purchased quota stays usable;
	// zero means it never expires.
	QuotaValidityDays int
	Status            domain.PlanStatus
	SortOrder         int
}

// normalizeQuotaText turns an empty quota into "0" and trims the rest, which
// keeps the NUMERIC round trip stable.
func normalizeQuotaText(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

// isKnownPlanStatus reports whether the status is one the schema accepts.
func isKnownPlanStatus(status domain.PlanStatus) bool {
	switch status {
	case domain.PlanStatusDraft, domain.PlanStatusActive, domain.PlanStatusArchived:
		return true
	default:
		return false
	}
}

// publishedAtOrNil keeps a zero time out of the nullable published_at column.
func publishedAtOrNil(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

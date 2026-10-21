package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
	"github.com/google/uuid"
)

// MonthPeriod is one half-open calendar-month window in UTC.
//
// The server resolves the window, never the client: two app instances in
// different time zones must agree on which payments count against the cap.
type MonthPeriod struct {
	From time.Time
	To   time.Time
}

// Start is the first instant of the month.
func (period MonthPeriod) Start() time.Time {
	return period.From
}

// MonthPeriodOf returns the calendar month containing moment, in UTC.
func MonthPeriodOf(moment time.Time) MonthPeriod {
	utc := moment.UTC()
	from := time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	return MonthPeriod{From: from, To: from.AddDate(0, 1, 0)}
}

// SpendingLimitService owns the guardian-set family monthly spending cap.
//
// These caps are the platform's only enforcement of the minor-spending rule
// beyond hiding payment entry points on the device: every checkout consults
// them, and a refusal is recorded for compliance rather than silently failing.
type SpendingLimitService struct {
	base
}

// NewSpendingLimitService creates the family spending limit service.
func NewSpendingLimitService(options Options) (*SpendingLimitService, error) {
	shared, err := newBase(options)
	if err != nil {
		return nil, err
	}
	return &SpendingLimitService{base: shared}, nil
}

// GetLimit returns one family's cap together with what it has already spent
// this month.
//
// A family with no stored row is reported as an unlimited limit, not as an
// error: never having set a cap is the normal first state.
func (s *SpendingLimitService) GetLimit(
	ctx context.Context,
	familyID string,
) (SpendingLimitView, error) {
	trimmed := strings.TrimSpace(familyID)
	if trimmed == "" {
		return SpendingLimitView{}, invalidRequest("family is required")
	}
	limit, err := s.currentLimit(ctx, trimmed)
	if err != nil {
		return SpendingLimitView{}, err
	}
	period := MonthPeriodOf(s.now())
	consumed, err := s.repository.SumMonthlyPaidCents(ctx, trimmed, period.From, period.To)
	if err != nil {
		return SpendingLimitView{}, err
	}
	return SpendingLimitView{
		Limit: limit,
		Spending: domain.MonthlySpending{
			FamilyID:      trimmed,
			Period:        period.From,
			ConsumedCents: consumed,
		},
	}, nil
}

// SetLimit stores a new monthly cap after validating its range.
//
// The caller passes the authenticated account so the row records who changed
// the cap; a store failure leaves the previous cap in force, which keeps the
// family covered by whichever limit was last saved successfully.
func (s *SpendingLimitService) SetLimit(
	ctx context.Context,
	familyID string,
	actorAccountID string,
	monthlyLimitCents domain.Cents,
) (SpendingLimitView, error) {
	trimmed := strings.TrimSpace(familyID)
	if trimmed == "" {
		return SpendingLimitView{}, invalidRequest("family is required")
	}
	if err := domain.ValidateMonthlySpendingLimit(monthlyLimitCents); err != nil {
		return SpendingLimitView{}, err
	}
	now := s.now()
	previous, err := s.currentLimit(ctx, trimmed)
	if err != nil {
		return SpendingLimitView{}, err
	}
	stored := domain.FamilySpendingLimit{
		FamilyID:          trimmed,
		MonthlyLimitCents: monthlyLimitCents,
		UpdatedBy:         strings.TrimSpace(actorAccountID),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.repository.UpsertSpendingLimit(ctx, stored); err != nil {
		return SpendingLimitView{}, err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:          "billing.spending_limit.updated",
		ActorAccountID:  stored.UpdatedBy,
		ParentAccountID: trimmed,
		Detail: map[string]any{
			"previous_monthly_limit_cents": int64(previous.MonthlyLimitCents),
			"monthly_limit_cents":          int64(monthlyLimitCents),
		},
		CreatedAt: now,
	})
	return s.GetLimit(ctx, trimmed)
}

// CheckOrderSpending refuses a checkout that would take the family past its cap.
//
// The requested amount is the order's payable total, so a discounted checkout
// counts what the guardian actually pays. A refusal is recorded before the
// error is returned: the record is the compliance evidence and must not depend
// on whether the caller retries.
func (s *SpendingLimitService) CheckOrderSpending(
	ctx context.Context,
	familyID string,
	requested domain.Cents,
	planCode string,
) error {
	trimmed := strings.TrimSpace(familyID)
	if trimmed == "" {
		return nil
	}
	limit, err := s.currentLimit(ctx, trimmed)
	if err != nil {
		// A cap that cannot be read must fail the checkout rather than let an
		// unenforced purchase through: the guardian set this cap precisely so
		// that it applies.
		return err
	}
	if limit.IsUnlimited() {
		return nil
	}
	period := MonthPeriodOf(s.now())
	consumed, err := s.repository.SumMonthlyPaidCents(ctx, trimmed, period.From, period.To)
	if err != nil {
		return err
	}
	if !domain.ExceedsLimit(limit.MonthlyLimitCents, consumed, requested) {
		return nil
	}
	now := s.now()
	rejection := domain.SpendingLimitRejection{
		ID:                uuid.NewString(),
		FamilyID:          trimmed,
		MonthlyLimitCents: limit.MonthlyLimitCents,
		ConsumedCents:     consumed,
		RequestedCents:    requested,
		PlanCode:          strings.TrimSpace(planCode),
		CreatedAt:         now,
	}
	s.recordRejection(ctx, rejection)
	return refusalError(trimmed, limit.MonthlyLimitCents, consumed, requested)
}

// refusalError builds the stable over-limit error with the amounts involved, so
// logs show why the checkout was refused without any guardian data.
func refusalError(
	familyID string,
	limit domain.Cents,
	consumed domain.Cents,
	requested domain.Cents,
) error {
	return fmt.Errorf(
		"%w: family %s spent %s of %s and requested %s",
		domain.ErrSpendingLimitExceeded, familyID, consumed.Format(),
		limit.Format(), requested.Format())
}

// ListRejections returns one family's refusals inside the calendar month that
// contains moment, newest first.
func (s *SpendingLimitService) ListRejections(
	ctx context.Context,
	familyID string,
	moment time.Time,
	page Page,
) ([]domain.SpendingLimitRejection, int, MonthPeriod, error) {
	trimmed := strings.TrimSpace(familyID)
	if trimmed == "" {
		return nil, 0, MonthPeriod{}, invalidRequest("family is required")
	}
	if moment.IsZero() {
		moment = s.now()
	}
	period := MonthPeriodOf(moment)
	normalized := page.normalized()
	rejections, total, err := s.repository.ListSpendingLimitRejections(
		ctx,
		trimmed,
		period.From,
		period.To,
		normalized.Limit,
		normalized.Offset,
	)
	if err != nil {
		return nil, 0, MonthPeriod{}, err
	}
	return rejections, total, period, nil
}

// Readiness reports whether both cap tables are readable.
func (s *SpendingLimitService) Readiness(ctx context.Context) error {
	if s == nil || s.repository == nil {
		return errors.New("billing: spending limit repository is unavailable")
	}
	return s.repository.SpendingLimitReadiness(ctx)
}

// currentLimit loads the stored cap or returns the unlimited default.
func (s *SpendingLimitService) currentLimit(
	ctx context.Context,
	familyID string,
) (domain.FamilySpendingLimit, error) {
	limit, err := s.repository.GetSpendingLimit(ctx, familyID)
	if errors.Is(err, repository.ErrSpendingLimitNotFound) {
		return domain.FamilySpendingLimit{FamilyID: familyID}, nil
	}
	if err != nil {
		return domain.FamilySpendingLimit{}, err
	}
	return *limit, nil
}

// recordRejection writes the refusal audit and its durable row.
//
// Both are deliberately non-fatal: the guardian is already being told "over
// limit", and turning that correct answer into a generic failure because the
// trail write failed would hide the actual reason. A missing row stays visible
// in the Feature Center rejection count.
func (s *SpendingLimitService) recordRejection(
	ctx context.Context,
	rejection domain.SpendingLimitRejection,
) {
	s.recordAudit(ctx, BillingAuditEvent{
		Action:          "billing.spending_limit.exceeded",
		ParentAccountID: rejection.FamilyID,
		Detail: map[string]any{
			"monthly_limit_cents": int64(rejection.MonthlyLimitCents),
			"consumed_cents":      int64(rejection.ConsumedCents),
			"requested_cents":     int64(rejection.RequestedCents),
			"plan_code":           rejection.PlanCode,
		},
		CreatedAt: rejection.CreatedAt,
	})
	_ = s.repository.AppendSpendingLimitRejection(ctx, rejection)
}

// SpendingLimitView is the guardian-facing projection of the cap and this
// month's spend.
type SpendingLimitView struct {
	Limit    domain.FamilySpendingLimit
	Spending domain.MonthlySpending
}

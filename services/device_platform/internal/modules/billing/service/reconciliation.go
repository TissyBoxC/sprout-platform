package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/channel"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReconciliationService compares one channel business day against the platform.
//
// The algorithm is written so it works the moment a real provider is plugged
// in, and fails honestly until then: with no channel configured the run is
// recorded as failed with a reason, never as a balanced day.
type ReconciliationService struct {
	base
}

// NewReconciliationService creates the reconciliation service.
func NewReconciliationService(options Options) (*ReconciliationService, error) {
	shared, err := newBase(options)
	if err != nil {
		return nil, err
	}
	return &ReconciliationService{base: shared}, nil
}

// RunReconciliation downloads one business day from the channel and records
// every difference.
//
// businessDate is the provider's day, so the platform window is derived from it
// in UTC. The run is reserved before the download so two operators cannot
// reconcile the same day twice, and it always ends in completed or failed with
// a finished_at time, which is what the Feature Center health probe reads.
func (s *ReconciliationService) RunReconciliation(
	ctx context.Context,
	channelName string,
	businessDate time.Time,
) (*domain.ReconciliationRun, error) {
	if businessDate.IsZero() {
		return nil, invalidRequest("business date is required")
	}
	provider, err := s.resolveChannel(channelName)
	if err != nil {
		return nil, err
	}
	channelValue := domain.PaymentChannel(provider.Name())
	day := businessDate.UTC().Truncate(24 * time.Hour)
	now := s.now()
	run := domain.ReconciliationRun{
		ID:           uuid.NewString(),
		Channel:      channelValue,
		BusinessDate: day,
		Status:       domain.ReconciliationStatusRunning,
		StartedAt:    now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if createErr := s.repository.CreateRun(ctx, run); createErr != nil {
		return nil, createErr
	}

	statement, fetchErr := provider.FetchStatement(ctx, day)
	if fetchErr != nil {
		return s.failRun(ctx, run, describeStatementFailure(fetchErr), fetchErr)
	}
	if len(statement.Entries) == 0 {
		// An empty statement for a day the platform settled proves nothing, and
		// treating it as "nothing to match" would report a balanced day forever.
		return s.failRun(ctx, run, "the channel returned no settled entries", ErrStatementEmpty)
	}
	expected, totalErr := sumStatementEntries(statement)
	if totalErr != nil {
		return s.failRun(ctx, run, totalErr.Error(), totalErr)
	}

	platformSettled, err := s.repository.ListPaymentsForReconciliation(ctx, channelValue, day, day.AddDate(0, 0, 1))
	if err != nil {
		return s.failRun(ctx, run, "the platform payments could not be read", err)
	}
	items, summary := s.match(statement.Entries, platformSettled, expected)
	// The matcher builds items before the run is finished, so it cannot know the
	// run id; stamping it here keeps every item attached to its run.
	for index := range items {
		items[index].RunID = run.ID
	}

	run.Status = domain.ReconciliationStatusCompleted
	run.ChannelTotalCents = summary.channelTotal
	run.PlatformTotalCents = summary.platformTotal
	run.DifferenceCents = summary.difference
	run.MatchedCount = summary.matched
	run.MissingCount = summary.platformOnly
	run.ExtraCount = summary.channelOnly
	finishedAt := s.now()
	run.FinishedAt = &finishedAt
	run.UpdatedAt = finishedAt
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		for _, item := range items {
			if insertErr := s.repository.InsertItem(ctx, tx, item); insertErr != nil {
				return insertErr
			}
		}
		return s.repository.FinishRun(ctx, run)
	})
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action: "billing.reconciliation.completed",
		Detail: map[string]any{
			"channel":       string(channelValue),
			"business_date": day.Format(time.DateOnly),
			"difference":    int64(summary.difference),
		},
		CreatedAt: finishedAt,
	})
	return &run, nil
}

// ListRuns returns the recorded reconciliation runs for the operator console.
func (s *ReconciliationService) ListRuns(
	ctx context.Context,
	page Page,
) ([]domain.ReconciliationRun, error) {
	normalized := page.normalized()
	return s.repository.ListRuns(ctx, normalized.Limit, normalized.Offset)
}

// LatestRun returns the most recently started run, or nil when no run exists.
//
// It is the read the Feature Center health probe uses: the newest run decides
// whether today's books have been verified, so a completed balance from last
// week must not stand in for a failed run yesterday.
func (s *ReconciliationService) LatestRun(ctx context.Context) (*domain.ReconciliationRun, error) {
	runs, err := s.repository.ListRuns(ctx, latestRunLimit, 0)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	// ListRuns orders by business date; the run actually started last is the
	// newest attempt even when an older business day was reconciled later.
	newest := runs[0]
	for index := 1; index < len(runs); index++ {
		if runs[index].StartedAt.After(newest.StartedAt) {
			newest = runs[index]
		}
	}
	return &newest, nil
}

// latestRunLimit is how many runs are scanned to find the newest started one.
// A day's runs for a handful of channels are far fewer than this.
const latestRunLimit = 50

// ListItems returns one run's items, unresolved discrepancies first.
func (s *ReconciliationService) ListItems(
	ctx context.Context,
	runID string,
	unresolvedOnly bool,
) ([]domain.ReconciliationItem, error) {
	return s.repository.ListItems(ctx, runID, unresolvedOnly)
}

// ResolveItem marks one discrepancy as investigated by an operator.
func (s *ReconciliationService) ResolveItem(
	ctx context.Context,
	itemID string,
	operatorAccountID string,
) error {
	if strings.TrimSpace(operatorAccountID) == "" {
		return ErrOperatorRequired
	}
	now := s.now()
	if err := s.repository.MarkItemResolved(ctx, itemID, true, now); err != nil {
		return err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:         "billing.reconciliation.item_resolved",
		ActorAccountID: operatorAccountID,
		Detail:         map[string]any{"item_id": itemID},
		CreatedAt:      now,
	})
	return nil
}

// match classifies every transaction of one day.
//
// The join key is the channel trade number, the only identifier both sides
// share and the only one a provider guarantees. A platform settlement the
// channel does not list is "platform_only" (the channel may have reversed it);
// a channel trade the platform does not know is "channel_only" (money arrived
// that no callback credited); a shared trade with a different amount is
// "amount_mismatch" and is treated as a payable difference.
func (s *ReconciliationService) match(
	channelEntries []channel.StatementEntry,
	platformSettled []repository.SettledPayment,
	channelTotal domain.Cents,
) ([]domain.ReconciliationItem, reconciliationSummary) {
	now := s.now()
	platformByTrade := make(map[string]repository.SettledPayment, len(platformSettled))
	for _, payment := range platformSettled {
		platformByTrade[payment.ChannelTradeNo] = payment
	}
	items := make([]domain.ReconciliationItem, 0, len(channelEntries))
	summary := reconciliationSummary{channelTotal: channelTotal}
	seen := make(map[string]bool, len(channelEntries))

	for _, entry := range channelEntries {
		seen[entry.ChannelTradeNo] = true
		channelCents := domain.Cents(entry.AmountCents)
		payment, found := platformByTrade[entry.ChannelTradeNo]
		item := domain.ReconciliationItem{
			ID:             uuid.NewString(),
			ChannelTradeNo: entry.ChannelTradeNo,
			OrderNo:        entry.OrderNo,
			ChannelCents:   channelCents,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		switch {
		case !found:
			item.Discrepancy = domain.ReconciliationChannelOnly
			item.PlatformCents = 0
			summary.channelOnly++
			summary.platformTotal += 0
		case payment.AmountCents != channelCents:
			item.Discrepancy = domain.ReconciliationAmountMismatch
			item.PlatformCents = payment.AmountCents
			summary.amountMismatch++
			summary.platformTotal += payment.AmountCents
		default:
			item.Discrepancy = domain.ReconciliationMatched
			item.PlatformCents = payment.AmountCents
			summary.matched++
			summary.platformTotal += payment.AmountCents
		}
		item.OrderNo = firstNonEmpty(item.OrderNo, payment.OrderNo)
		items = append(items, item)
	}

	// A payment the platform settled that the channel never listed is the most
	// serious case: the platform may have granted quota for money that was
	// reversed. It is recorded even though it has no channel entry.
	for _, payment := range platformSettled {
		if seen[payment.ChannelTradeNo] {
			continue
		}
		summary.platformOnly++
		summary.platformTotal += payment.AmountCents
		items = append(items, domain.ReconciliationItem{
			ID:             uuid.NewString(),
			ChannelTradeNo: payment.ChannelTradeNo,
			OrderNo:        payment.OrderNo,
			ChannelCents:   0,
			PlatformCents:  payment.AmountCents,
			Discrepancy:    domain.ReconciliationPlatformOnly,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}
	summary.difference = summary.channelTotal - summary.platformTotal
	return items, summary
}

// reconciliationSummary is the money and counts of one matched day.
type reconciliationSummary struct {
	channelTotal   domain.Cents
	platformTotal  domain.Cents
	difference     domain.Cents
	matched        int
	amountMismatch int
	platformOnly   int
	channelOnly    int
}

// failRun records a failed run and returns the operator-facing error.
//
// The run row is written before returning so the console and the health probe
// can see that the day was attempted and why it stayed unverified.
func (s *ReconciliationService) failRun(
	ctx context.Context,
	run domain.ReconciliationRun,
	message string,
	cause error,
) (*domain.ReconciliationRun, error) {
	now := s.now()
	run.Status = domain.ReconciliationStatusFailed
	run.ErrorMessage = message
	run.FinishedAt = &now
	run.UpdatedAt = now
	if err := s.repository.FinishRun(ctx, run); err != nil {
		return nil, err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action: "billing.reconciliation.failed",
		Detail: map[string]any{
			"channel":       string(run.Channel),
			"business_date": run.BusinessDate.Format(time.DateOnly),
			"message":       message,
		},
		CreatedAt: now,
	})
	if isChannelNotConfigured(cause) {
		return &run, fmt.Errorf("%w: %s", ErrReconciliationUnavailable, message)
	}
	return &run, fmt.Errorf("%w: %s: %w", ErrReconciliationUnavailable, message, cause)
}

// describeStatementFailure turns a provider error into an operator message.
func describeStatementFailure(err error) string {
	if isChannelNotConfigured(err) {
		return "the payment channel is not configured, so no statement could be downloaded"
	}
	return "the channel statement could not be downloaded"
}

// sumStatementEntries totals a statement's entries.
//
// The total is summed from the entries rather than trusted from a provider
// summary, so a statement whose own arithmetic disagrees still reconciles
// against what it actually lists; a negative amount is a malformed statement and
// is refused instead of silently reducing the day's total.
func sumStatementEntries(statement channel.Statement) (domain.Cents, error) {
	total := domain.Cents(0)
	for index, entry := range statement.Entries {
		if entry.AmountCents < 0 {
			return 0, fmt.Errorf("%w: entry %d has a negative amount", ErrStatementNotBalanced, index)
		}
		next, err := total.Add(domain.Cents(entry.AmountCents))
		if err != nil {
			return 0, fmt.Errorf("%w: %v", ErrStatementNotBalanced, err)
		}
		total = next
	}
	return total, nil
}

// firstNonEmpty returns the first non-empty value, preferring the statement's
// order number over the platform's when both are present.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

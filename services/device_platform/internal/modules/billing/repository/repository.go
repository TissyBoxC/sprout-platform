package repository

import (
	"context"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/jackc/pgx/v5"
)

// Repository is the billing persistence contract the service layer depends on.
//
// Every method that changes money takes the caller's pgx.Tx, because the
// caller owns the order lock and must commit the settlement and its side
// effects together. Reads take no transaction and are independent statements.
type Repository interface {
	// WithTx runs fn inside one transaction and commits only when fn returns
	// nil.
	WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error

	CreatePlan(ctx context.Context, plan domain.Plan) error
	UpdatePlan(ctx context.Context, plan domain.Plan, expectedVersion int) error
	ArchivePlan(ctx context.Context, planID string, now time.Time) error
	GetPlanByID(ctx context.Context, planID string) (*domain.Plan, error)
	GetPlanByCode(ctx context.Context, code string) (*domain.Plan, error)
	ListActivePlans(ctx context.Context) ([]domain.Plan, error)
	ListPlans(ctx context.Context, filter PlanFilter) ([]domain.Plan, int, error)

	CreateOrder(ctx context.Context, order domain.Order) (*domain.Order, error)
	GetOrderByID(ctx context.Context, orderID string) (*domain.Order, error)
	GetOrderByOrderNo(ctx context.Context, orderNo string) (*domain.Order, error)
	GetOrderByIdentityOrIdempotency(
		ctx context.Context,
		parentAccountID string,
		idempotencyKey string,
	) (*domain.Order, error)
	ListOrdersByParent(
		ctx context.Context,
		parentAccountID string,
		limit int,
		offset int,
	) ([]domain.Order, int, error)
	ListOrders(ctx context.Context, filter OrderFilter) ([]domain.Order, int, error)
	LockOrderForSettlement(ctx context.Context, tx pgx.Tx, orderNo string) (*domain.Order, error)
	LockOrderByID(ctx context.Context, tx pgx.Tx, orderID string) (*domain.Order, error)
	MarkOrderPaid(ctx context.Context, tx pgx.Tx, orderID string, paidCents domain.Cents, paidAt time.Time) error
	CancelPendingOrder(ctx context.Context, parentAccountID string, orderNo string, now time.Time) error
	ExpireStaleOrders(ctx context.Context, now time.Time, limit int) ([]string, error)
	CountPendingOrders(ctx context.Context, now time.Time) (int, int, error)

	CreatePayment(ctx context.Context, payment domain.Payment) error
	GetPaymentByID(ctx context.Context, paymentID string) (*domain.Payment, error)
	GetPaymentByChannelTradeNo(
		ctx context.Context,
		channel domain.PaymentChannel,
		channelTradeNo string,
	) (*domain.Payment, error)
	FindSucceededPayment(ctx context.Context, tx pgx.Tx, orderID string) (*domain.Payment, error)
	ListPaymentsByOrder(ctx context.Context, orderID string) ([]domain.Payment, error)
	UpdatePaymentProcessing(ctx context.Context, payment domain.Payment, channelTradeNo string) error
	MarkPaymentSucceeded(
		ctx context.Context,
		tx pgx.Tx,
		paymentID string,
		channelTradeNo string,
		paidAt time.Time,
	) error
	MarkPaymentFailed(ctx context.Context, paymentID string, failureCode string, failureMessage string, now time.Time) error
	MarkPaymentClosed(ctx context.Context, paymentID string, now time.Time) error
	InsertNotification(ctx context.Context, notification domain.PaymentNotification) (bool, error)
	GetNotification(
		ctx context.Context,
		channel domain.PaymentChannel,
		notificationID string,
	) (*domain.PaymentNotification, error)
	MarkNotificationProcessed(
		ctx context.Context,
		notificationID string,
		result domain.NotificationResult,
		processedAt *time.Time,
	) error

	CreateRefund(ctx context.Context, tx pgx.Tx, refund domain.Refund) error
	GetRefundByID(ctx context.Context, refundID string) (*domain.Refund, error)
	GetRefundByNo(ctx context.Context, refundNo string) (*domain.Refund, error)
	LockRefund(ctx context.Context, tx pgx.Tx, refundID string) (*domain.Refund, error)
	ListRefundsByOrder(ctx context.Context, orderID string) ([]domain.Refund, error)
	ListRefunds(ctx context.Context, filter RefundFilter) ([]RefundListing, int, error)
	SumRefundCentsByStatus(
		ctx context.Context,
		tx pgx.Tx,
		orderID string,
		statuses []domain.RefundStatus,
	) (domain.Cents, error)
	SumSucceededRefundCents(ctx context.Context, orderID string) (domain.Cents, error)
	ApproveRefund(ctx context.Context, tx pgx.Tx, refundID string, operatorAccountID string, approvedAt time.Time) error
	RejectRefund(ctx context.Context, tx pgx.Tx, refundID string, operatorAccountID string, rejectedAt time.Time) error
	MarkRefundProcessing(ctx context.Context, tx pgx.Tx, refundID string, channelRefundNo string, now time.Time) error
	MarkRefundSucceeded(ctx context.Context, tx pgx.Tx, refundID string, channelRefundNo string, completedAt time.Time) error
	MarkRefundFailed(ctx context.Context, tx pgx.Tx, refundID string, now time.Time) error
	ApplyOrderRefund(ctx context.Context, tx pgx.Tx, orderID string, refundCents domain.Cents, now time.Time) error

	UpsertActiveSubscription(ctx context.Context, tx pgx.Tx, subscription domain.Subscription) error
	GetActiveSubscriptions(ctx context.Context, parentAccountID string, now time.Time) ([]domain.Subscription, error)
	ExpireDueSubscriptions(ctx context.Context, now time.Time) (int, error)

	GrantSpendingConsent(ctx context.Context, consent domain.SpendingConsent) (*domain.SpendingConsent, error)
	RevokeSpendingConsent(ctx context.Context, parentAccountID string, consentVersion string, revokedAt time.Time) (bool, error)
	GetActiveSpendingConsent(ctx context.Context, parentAccountID string, consentVersion string) (*domain.SpendingConsent, error)
	GetLatestSpendingConsent(ctx context.Context, parentAccountID string) (*domain.SpendingConsent, error)

	// The family spending cap: one row per family, and a monthly total derived
	// from the order ledger so the cap never depends on a second running total
	// that could drift from what was actually settled.
	GetSpendingLimit(ctx context.Context, familyID string) (*domain.FamilySpendingLimit, error)
	UpsertSpendingLimit(ctx context.Context, limit domain.FamilySpendingLimit) error
	SumMonthlyPaidCents(ctx context.Context, familyID string, from time.Time, to time.Time) (domain.Cents, error)
	AppendSpendingLimitRejection(ctx context.Context, rejection domain.SpendingLimitRejection) error
	ListSpendingLimitRejections(
		ctx context.Context,
		familyID string,
		from time.Time,
		to time.Time,
		limit int,
		offset int,
	) ([]domain.SpendingLimitRejection, int, error)
	SpendingLimitMetrics(ctx context.Context) (SpendingLimitMetrics, error)
	SpendingLimitReadiness(ctx context.Context) error

	CreateRun(ctx context.Context, run domain.ReconciliationRun) error
	FinishRun(ctx context.Context, run domain.ReconciliationRun) error
	ListRuns(ctx context.Context, limit int, offset int) ([]domain.ReconciliationRun, error)
	GetLatestRun(ctx context.Context, channel domain.PaymentChannel) (*domain.ReconciliationRun, error)
	InsertItem(ctx context.Context, tx pgx.Tx, item domain.ReconciliationItem) error
	ListItems(ctx context.Context, runID string, unresolvedOnly bool) ([]domain.ReconciliationItem, error)
	MarkItemResolved(ctx context.Context, itemID string, resolved bool, now time.Time) error
	ListPaymentsForReconciliation(
		ctx context.Context,
		channel domain.PaymentChannel,
		from time.Time,
		to time.Time,
	) ([]SettledPayment, error)
	GetOrderStatusesByNumbers(ctx context.Context, orderNumbers []string) (map[string]domain.OrderStatus, error)

	AppendQuotaLedgerEntry(ctx context.Context, tx pgx.Tx, entry domain.QuotaLedgerEntry) error
	LockQuotaLedgerEntry(ctx context.Context, tx pgx.Tx, entryID string) (*domain.QuotaLedgerEntry, error)
	MarkQuotaLedgerGrantState(ctx context.Context, querier Querier, entryID string, state string, now time.Time) error
	ListQuotaLedger(
		ctx context.Context,
		parentAccountID string,
		limit int,
		offset int,
	) ([]domain.QuotaLedgerEntry, int, error)
	SumQuotaLedgerForParent(ctx context.Context, parentAccountID string) (QuotaLedgerTotal, error)
	LatestObservedProviderBalance(ctx context.Context, parentAccountID string) (string, bool, error)
}

var _ Repository = (*PostgresRepository)(nil)

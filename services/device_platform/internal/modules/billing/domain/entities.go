package domain

import "time"

// BillingPeriod identifies how a plan recurs (design doc §3.1).
type BillingPeriod string

const (
	BillingPeriodMonthly   BillingPeriod = "monthly"
	BillingPeriodQuarterly BillingPeriod = "quarterly"
	BillingPeriodYearly    BillingPeriod = "yearly"
	BillingPeriodOneTime   BillingPeriod = "one_time"
)

// BillingPeriods lists every declared billing period for exhaustive checks.
var BillingPeriods = []BillingPeriod{
	BillingPeriodMonthly,
	BillingPeriodQuarterly,
	BillingPeriodYearly,
	BillingPeriodOneTime,
}

// PlanStatus is the catalogue lifecycle of a plan.
type PlanStatus string

const (
	PlanStatusDraft    PlanStatus = "draft"
	PlanStatusActive   PlanStatus = "active"
	PlanStatusArchived PlanStatus = "archived"
)

// PaymentChannel identifies a payment provider.
type PaymentChannel string

const (
	PaymentChannelWeChat PaymentChannel = "wechat"
	PaymentChannelAlipay PaymentChannel = "alipay"
	// PaymentChannelManual is an operator-recorded settlement, used for
	// offline collection; it shares the callback and refund paths.
	PaymentChannelManual PaymentChannel = "manual"
)

// NotificationResult is the recorded outcome of one provider callback.
//
// NotificationResultReceived is not in the design doc's list: it is the state a
// row holds between winning the idempotency insert and finishing processing,
// so a concurrent redelivery can answer "still processing" instead of
// "accepted".
type NotificationResult string

const (
	NotificationResultReceived         NotificationResult = "received"
	NotificationResultAccepted         NotificationResult = "accepted"
	NotificationResultDuplicate        NotificationResult = "duplicate"
	NotificationResultSignatureInvalid NotificationResult = "signature_invalid"
	NotificationResultAmountMismatch   NotificationResult = "amount_mismatch"
	NotificationResultOrderNotFound    NotificationResult = "order_not_found"
)

// ReconciliationStatus is the lifecycle of one daily reconciliation run.
type ReconciliationStatus string

const (
	ReconciliationStatusRunning   ReconciliationStatus = "running"
	ReconciliationStatusCompleted ReconciliationStatus = "completed"
	ReconciliationStatusFailed    ReconciliationStatus = "failed"
)

// ReconciliationDiscrepancy classifies one reconciled transaction.
type ReconciliationDiscrepancy string

const (
	ReconciliationMatched        ReconciliationDiscrepancy = "matched"
	ReconciliationAmountMismatch ReconciliationDiscrepancy = "amount_mismatch"
	ReconciliationPlatformOnly   ReconciliationDiscrepancy = "platform_only"
	ReconciliationChannelOnly    ReconciliationDiscrepancy = "channel_only"
)

// SubscriptionStatus is the lifecycle of a periodic purchase.
type SubscriptionStatus string

const (
	SubscriptionStatusActive    SubscriptionStatus = "active"
	SubscriptionStatusExpired   SubscriptionStatus = "expired"
	SubscriptionStatusCancelled SubscriptionStatus = "cancelled"
)

// Plan is one purchasable bundle (design doc §3.1).
//
// Price fields are yuan cents. AIQuotaUSD is the model-cost quota granted
// after payment, in the AI gateway's provider unit: it is not derived from the
// price and the two must never be converted into each other.
type Plan struct {
	ID            string
	Code          string
	DisplayName   string
	Description   string
	BillingPeriod BillingPeriod
	PeriodMonths  int
	PriceCents    Cents
	// OriginalPriceCents is the strike-through price; zero means "not shown".
	OriginalPriceCents Cents
	AIQuotaUSD         string
	// QuotaValidityDays is how long a purchased quota grant stays usable;
	// zero means it never expires. Without a window a monthly plan's unused
	// allowance would accumulate forever, so one month could bank years of
	// quota and the expiry ledger entry would have no source to draw from.
	QuotaValidityDays int
	Status            PlanStatus
	SortOrder         int
	// Version is the optimistic-concurrency counter, starting at 1.
	Version     int
	PublishedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Order is one guardian purchase (design doc §3.2).
//
// Every plan field is a snapshot taken at checkout, so editing or archiving the
// plan later never rewrites a historical order. All amounts are yuan cents.
type Order struct {
	ID              string
	OrderNo         string
	ParentAccountID string
	PlanID          string
	PlanCode        string
	PlanDisplayName string
	BillingPeriod   BillingPeriod
	PeriodMonths    int
	AIQuotaUSD      string
	// QuotaValidityDays is snapshotted with the rest of the plan so a later
	// plan edit cannot retroactively change when an existing order's quota
	// lapses.
	QuotaValidityDays int
	UnitPriceCents    Cents
	DiscountCents     Cents
	PayableCents      Cents
	PaidCents         Cents
	RefundedCents     Cents
	Status            OrderStatus
	// IdempotencyKey deduplicates checkout retries per family.
	IdempotencyKey string
	ExpiresAt      *time.Time
	PaidAt         *time.Time
	CancelledAt    *time.Time
	ClosedAt       *time.Time
	ClientIP       string
	Remark         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// RemainingRefundableCents is the refundable amount left on the order.
func (order Order) RemainingRefundableCents() Cents {
	remaining := order.PaidCents - order.RefundedCents
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Payment is one payment attempt (design doc §3.3).
//
// ProviderPayload holds the already-redacted provider response. It must never
// carry a merchant secret, certificate, or private key.
type Payment struct {
	ID              string
	OrderID         string
	Channel         PaymentChannel
	ChannelTradeNo  string
	AmountCents     Cents
	Status          PaymentStatus
	ProviderPayload map[string]any
	FailureCode     string
	FailureMessage  string
	PaidAt          *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PaymentNotification is the callback idempotency ledger row (§3.4).
//
// The row must be inserted before the callback is processed; losing the
// (Channel, NotificationID) race is what makes a redelivery a no-op.
type PaymentNotification struct {
	ID             string
	Channel        PaymentChannel
	NotificationID string
	// PayloadDigest is the SHA-256 of the raw callback body, so the same id
	// arriving with different bytes is detectable.
	PayloadDigest     string
	SignatureVerified bool
	// ProcessedAt is nil while the callback is received but unfinished.
	ProcessedAt *time.Time
	Result      NotificationResult
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Refund is one refund approval and its channel outcome (design doc §3.5).
type Refund struct {
	ID          string
	RefundNo    string
	OrderID     string
	PaymentID   string
	AmountCents Cents
	Reason      string
	// OperatorAccountID is the administrator who approved the refund.
	OperatorAccountID string
	Status            RefundStatus
	ChannelRefundNo   string
	RequestedAt       time.Time
	ApprovedAt        *time.Time
	CompletedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Subscription is the current purchased period of a recurring plan (§3.8).
type Subscription struct {
	ID                 string
	ParentAccountID    string
	PlanID             string
	PlanCode           string
	PlanDisplayName    string
	Status             SubscriptionStatus
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	SourceOrderID      string
	// AutoRenew is a reserved field. No recurring charge is implemented, so it
	// never causes a debit on its own.
	AutoRenew bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ReconciliationRun is one daily comparison against a channel statement.
type ReconciliationRun struct {
	ID                 string
	Channel            PaymentChannel
	BusinessDate       time.Time
	Status             ReconciliationStatus
	ChannelTotalCents  Cents
	PlatformTotalCents Cents
	// DifferenceCents is signed: negative means the platform recorded more
	// than the channel settled.
	DifferenceCents Cents
	MatchedCount    int
	MissingCount    int
	ExtraCount      int
	StartedAt       time.Time
	FinishedAt      *time.Time
	ErrorMessage    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ReconciliationItem is one transaction inside a run.
type ReconciliationItem struct {
	ID             string
	RunID          string
	ChannelTradeNo string
	OrderNo        string
	ChannelCents   Cents
	PlatformCents  Cents
	Discrepancy    ReconciliationDiscrepancy
	Resolved       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SpendingConsent records guardian agreement to spending (design doc §3.7).
//
// RevokedAt nil means the consent is in force. Rows are never edited in place
// apart from being revoked, so the evidence survives a later re-grant.
type SpendingConsent struct {
	ID              string
	ParentAccountID string
	ConsentVersion  string
	GrantedAt       time.Time
	RevokedAt       *time.Time
	IPAddress       string
	// Method is the confirmation channel, currently only "app_confirm".
	Method    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsActive reports whether the consent is currently in force.
func (consent SpendingConsent) IsActive() bool {
	return consent.RevokedAt == nil
}

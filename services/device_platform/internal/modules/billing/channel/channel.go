package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// HealthStatus is the payment channel probe result shown in Feature Center.
// There is deliberately no "unknown" value defaulting to healthy: an
// unimplemented probe must return unavailable, because AGENTS.md §Feature Center
// forbids fabricating a healthy status.
type HealthStatus string

const (
	HealthStatusHealthy     HealthStatus = "healthy"
	HealthStatusDegraded    HealthStatus = "degraded"
	HealthStatusUnavailable HealthStatus = "unavailable"
)

// IsKnown reports whether values read back from storage/API are one of the
// three declared statuses, so a typo can never be rendered as healthy.
func (status HealthStatus) IsKnown() bool {
	switch status {
	case HealthStatusHealthy, HealthStatusDegraded, HealthStatusUnavailable:
		return true
	default:
		return false
	}
}

// String keeps the wire value stable for Feature Center and logs.
func (status HealthStatus) String() string {
	return string(status)
}

// Channel is the single abstraction over a payment provider (design doc §4).
// Implementations must satisfy three invariants:
//  1. A failed signature always returns an error wrapping ErrSignatureInvalid.
//  2. No method returns success unless the provider actually accepted the call.
//  3. ChannelTradeNo and Payload never carry merchant secrets.
//
// Implementations must be safe for concurrent use: the callback handler and the
// reconciliation job call them from different goroutines.
type Channel interface {
	// Name returns the channel key. It must equal the database `channel` value
	// (`wechat`, `alipay`, `manual`) because notification idempotency is keyed
	// on (channel, notification_id).
	Name() string

	// CreatePayment places the order with the provider and returns the
	// credentials the client uses to open the pay flow. An unconfigured channel
	// must return an error wrapping ErrChannelNotConfigured, never a result.
	CreatePayment(ctx context.Context, request CreatePaymentRequest) (CreatePaymentResult, error)

	// VerifyNotification verifies the callback signature and maps the raw body
	// to a provider-neutral event. It must never consult order state: amount
	// and order checks belong to the handler, which has the database.
	VerifyNotification(ctx context.Context, request NotificationRequest) (NotificationEvent, error)

	// Refund asks the provider to return money for a paid trade. Unconfigured
	// channels return an error wrapping ErrChannelNotConfigured.
	Refund(ctx context.Context, request RefundRequest) (RefundResult, error)

	// FetchStatement downloads the settled statement for businessDate for
	// reconciliation. Unconfigured channels return ErrChannelNotConfigured.
	FetchStatement(ctx context.Context, businessDate time.Time) (Statement, error)

	// HealthCheck reports the live provider connectivity. It must be cheap
	// enough for a periodic Feature Center probe and must not mutate state.
	// An unconfigured channel reports HealthStatusUnavailable.
	HealthCheck(ctx context.Context) (HealthStatus, error)
}

// NotificationDigest returns the "sha256:<hex>" digest of a callback raw body.
// It is stored in billing_payment_notifications.payload_digest so that a replay
// of the same notification_id with different bytes is detectable. Hashing is
// over the untouched bytes, so callers must pass RawBody verbatim.
func NotificationDigest(rawBody []byte) string {
	sum := sha256.Sum256(rawBody)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// IsDuplicateNotification reports whether err came from the callback
// idempotency ledger. Duplicates are acked to the provider as success; they are
// not a failure to retry.
func IsDuplicateNotification(err error) bool {
	return errors.Is(err, ErrDuplicateNotification)
}

package channel

import "time"

// INTEGRATION SEAM (money) — AmountCents is the channel-local money type.
// billing/domain.Cents is `type Cents int64` in the same unit (人民币分), so the
// adapter is a plain int64 conversion: domain.Cents(uint64 → int64).
// Money is int64 cents everywhere; float64 is forbidden by design doc §2.
type AmountCents = int64

// CreatePaymentRequest is one attempt to place an order with a provider.
// AmountCents must be > 0 and OrderNo must be unique per platform order.
// NotifyURL is the platform endpoint the provider will call back on.
type CreatePaymentRequest struct {
	OrderNo     string
	AmountCents AmountCents
	Subject     string
	Description string
	ClientIP    string
	NotifyURL   string
	ExpiresAt   time.Time
}

// CreatePaymentResult carries the credentials the client needs to open the
// provider SDK/pay page. Payload is public data only: it must never contain a
// merchant secret, certificate, private key or API key (AGENTS.md §9.1).
// ChannelTradeNo is empty when the provider only assigns it after payment.
type CreatePaymentResult struct {
	ChannelTradeNo string
	Payload        map[string]string
}

// NotificationRequest is the raw provider callback exactly as received.
// RawBody must be the untouched bytes: verifying a re-serialized body breaks
// HMAC/XML canonicalization. Headers keys should be canonical MIME form.
type NotificationRequest struct {
	Headers    map[string]string
	RawBody    []byte
	ReceivedAt time.Time
}

// NotificationEvent is the provider-neutral outcome of a verified callback.
// Success=false is a well-formed callback reporting a failed payment: it is
// signed and valid but must never credit the order.
// NotificationID is the provider's idempotency key (design doc §3.4).
type NotificationEvent struct {
	NotificationID string
	ChannelTradeNo string
	OrderNo        string
	AmountCents    AmountCents
	PaidAt         time.Time
	Success        bool
}

// RefundRequest asks the provider to return money for a previously paid
// payment. RefundNo is the platform-generated idempotency key.
type RefundRequest struct {
	OrderNo        string
	ChannelTradeNo string
	RefundNo       string
	AmountCents    AmountCents
	Reason         string
}

// RefundResult reports the provider's answer. Succeeded=false with a nil error
// means the provider accepted the request but the money is not back yet; the
// caller must keep the refund in processing and wait for the refund callback.
type RefundResult struct {
	ChannelRefundNo string
	Succeeded       bool
}

// Statement is one provider business day of settled transactions, used by the
// reconciliation job (design doc §3.6). BusinessDate is the provider's day,
// not the platform timezone's day.
type Statement struct {
	BusinessDate time.Time
	Entries      []StatementEntry
}

// StatementEntry is one settled channel trade inside a Statement.
// AmountCents is the amount the provider actually settled.
type StatementEntry struct {
	ChannelTradeNo string
	OrderNo        string
	AmountCents    AmountCents
	PaidAt         time.Time
}

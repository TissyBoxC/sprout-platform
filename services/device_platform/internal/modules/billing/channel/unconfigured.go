package channel

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// UnconfiguredChannel stands in for a real provider until the WeChat Pay or
// Alipay integration ships. It carries the REAL channel name so database rows,
// order history and Feature Center entries are already correct, while every
// outbound call fails loudly with ErrChannelNotConfigured instead of pretending
// to succeed (design doc §4, §9 item 8).
//
// VerifyNotification is the one method that does real work: it runs the
// configured SignatureVerifier and parses the verified body. With the default
// RejectAllVerifier every callback is rejected, which is the correct behaviour
// for a channel that has no shared secret yet.
type UnconfiguredChannel struct {
	name              string
	verifier          SignatureVerifier
	parseNotification func(NotificationRequest) (NotificationEvent, error)
}

// SupportedChannelNames lists the channel keys this round can register.
// `manual` is excluded on purpose: it is settled by an administrator, not by a
// provider callback, so it has no Channel implementation.
var SupportedChannelNames = []string{"wechat", "alipay"}

// NewUnconfiguredChannel builds the placeholder for a real provider name.
// verifier may be nil, in which case RejectAllVerifier (fail closed) is used;
// a nil verifier is never treated as "skip verification".
func NewUnconfiguredChannel(name string, verifier SignatureVerifier) (*UnconfiguredChannel, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if !isSupportedChannelName(normalized) {
		return nil, fmt.Errorf("%w: unsupported channel name %q", ErrInvalidRequest, name)
	}
	if verifier == nil {
		verifier = RejectAllVerifier{}
	}
	return &UnconfiguredChannel{name: normalized, verifier: verifier}, nil
}

// NewUnconfiguredChannels builds every supported placeholder with the fail-closed
// verifier. The composition root asks this package for providers so it does not
// hard-code provider names; a later real implementation swaps one entry.
func NewUnconfiguredChannels() []*UnconfiguredChannel {
	channels := make([]*UnconfiguredChannel, 0, len(SupportedChannelNames))
	for _, name := range SupportedChannelNames {
		// Names come from the package's own allowlist, so this cannot fail.
		channel, err := NewUnconfiguredChannel(name, nil)
		if err != nil {
			continue
		}
		channels = append(channels, channel)
	}
	return channels
}

// WithNotificationParser attaches the provider-specific decoder that converts a
// signature-verified raw body into a NotificationEvent. It returns the receiver
// so construction reads as one expression. Once a real decoder exists, an
// UnconfiguredChannel can verify callbacks end to end before the outbound HTTP
// layer ships.
func (channel *UnconfiguredChannel) WithNotificationParser(parser func(NotificationRequest) (NotificationEvent, error)) *UnconfiguredChannel {
	channel.parseNotification = parser
	return channel
}

// Name returns the real provider key so persisted rows are already canonical.
func (channel *UnconfiguredChannel) Name() string {
	return channel.name
}

// CreatePayment fails loudly. No HTTP call is made and no credentials exist, so
// returning a result would be a fabricated success.
func (channel *UnconfiguredChannel) CreatePayment(ctx context.Context, request CreatePaymentRequest) (CreatePaymentResult, error) {
	return CreatePaymentResult{}, channel.notConfigured("create payment for order "+request.OrderNo, ctx)
}

// Refund fails loudly; a refund that silently "succeeded" would corrupt the
// refunded total and the AI quota clawback.
func (channel *UnconfiguredChannel) Refund(ctx context.Context, request RefundRequest) (RefundResult, error) {
	return RefundResult{}, channel.notConfigured("refund "+request.RefundNo, ctx)
}

// FetchStatement fails loudly so reconciliation reports a failed run instead of
// comparing the platform against an empty, falsely balanced statement.
func (channel *UnconfiguredChannel) FetchStatement(ctx context.Context, businessDate time.Time) (Statement, error) {
	return Statement{}, channel.notConfigured("fetch statement for "+businessDate.UTC().Format(time.DateOnly), ctx)
}

// VerifyNotification checks the callback signature and then decodes the event.
// With the default RejectAllVerifier it always fails closed, returning an error
// that wraps BOTH ErrSignatureInvalid and ErrChannelNotConfigured.
// Seam for the integration agent: when a real decoder is supplied via
// WithNotificationParser, this method already enforces "no signature, no event";
// only the provider-specific field mapping is still missing.
func (channel *UnconfiguredChannel) VerifyNotification(ctx context.Context, request NotificationRequest) (NotificationEvent, error) {
	if err := ctx.Err(); err != nil {
		return NotificationEvent{}, err
	}
	if channel.verifier == nil {
		// Defensive: a struct built without the constructor still fails closed.
		return NotificationEvent{}, fmt.Errorf("%w: %w", ErrSignatureInvalid, ErrChannelNotConfigured)
	}
	if err := channel.verifier.Verify(httpHeaderFromFlat(request.Headers), request.RawBody); err != nil {
		return NotificationEvent{}, err
	}
	// A verified body must never end up as a zero-value event, because a zero
	// event would look like an unpaid order with empty identifiers.
	if channel.parseNotification == nil {
		return NotificationEvent{}, fmt.Errorf("%w: %w: callback parser is not implemented", ErrChannelNotConfigured, ErrInvalidRequest)
	}
	event, err := channel.parseNotification(request)
	if err != nil {
		return NotificationEvent{}, err
	}
	if err := validateNotificationEvent(event); err != nil {
		return NotificationEvent{}, err
	}
	return event, nil
}

// HealthCheck always reports unavailable: no credentials, no endpoint, no
// observable connectivity. Reporting anything else would violate the rule that
// health must never be invented.
func (channel *UnconfiguredChannel) HealthCheck(ctx context.Context) (HealthStatus, error) {
	return HealthStatusUnavailable, fmt.Errorf("%w: %s payment provider is not integrated", ErrChannelNotConfigured, channel.name)
}

// notConfigured centralizes the loud failure so every outbound method reports
// the missing integration identically and never returns a zero-value success.
func (channel *UnconfiguredChannel) notConfigured(action string, ctx context.Context) error {
	// A cancelled request is reported first: the caller should retry rather than
	// conclude the channel is missing.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrChannelNotConfigured, action, err)
	}
	return fmt.Errorf("%w: %s payment provider cannot %s", ErrChannelNotConfigured, channel.name, action)
}

// isSupportedChannelName keeps provider keys in one allowlist so a typo cannot
// become a new persisted channel value.
func isSupportedChannelName(name string) bool {
	for _, supported := range SupportedChannelNames {
		if name == supported {
			return true
		}
	}
	return false
}

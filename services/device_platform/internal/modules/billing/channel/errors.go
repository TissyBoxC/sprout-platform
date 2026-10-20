package channel

import "errors"

// INTEGRATION SEAM (errors) — these sentinels mirror the billing domain package,
// which is owned by a different agent and imported by the integration layer.
// billing/domain.ErrChannelNotConfigured, ErrSignatureInvalid and
// ErrDuplicateNotification must be satisfied by these errors via errors.Is once
// the domain sentinels are wrapped/unwrapped across the seam. Do not add a
// `billingdomain` import here: this package must compile on its own.
var (
	// ErrChannelNotConfigured means the payment provider was never wired up,
	// so no outbound call can be made. Callers must surface it as "payment not
	// open yet" and never as a payment success.
	ErrChannelNotConfigured = errors.New("payment channel is not configured")

	// ErrSignatureInvalid means the callback signature could not be verified.
	// Callers must record signature_invalid, refuse to credit, and never retry
	// it as a transient failure.
	ErrSignatureInvalid = errors.New("payment callback signature is invalid")

	// ErrDuplicateNotification means the callback was already accepted under
	// the same (channel, notification_id) unique key. It is a success outcome
	// for the provider, so callers ack it without crediting twice.
	ErrDuplicateNotification = errors.New("payment callback is a duplicate")

	// ErrInvalidRequest means a request field is missing or inconsistent
	// (empty order number, non-positive amount, unknown channel name, ...).
	// It is a programming error, not a provider failure.
	ErrInvalidRequest = errors.New("payment channel request is invalid")

	// ErrChannelNotFound means the registry has no implementation for the
	// requested channel name.
	ErrChannelNotFound = errors.New("payment channel is not registered")
)

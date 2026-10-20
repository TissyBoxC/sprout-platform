// Package domain contains billing money, order, payment, refund, subscription,
// reconciliation, consent, and quota-ledger types with their validation rules.
//
// Two amount systems exist in the platform and this package owns only one of
// them. Guardian revenue is 人民币分: integer cents, Go int64, never a
// floating point value, because 0.1 + 0.2 != 0.3 corrupts reconciliation.
// The AI gateway's model-cost balance stays entirely in its own package.
//
// The package is pure: no database, HTTP, or clock access. Callers pass the
// current time so every rule is deterministic under test.
package domain

import "errors"

var (
	// ErrInvalidOrderTransition rejects a status change outside the order,
	// payment, or refund state machine.
	ErrInvalidOrderTransition = errors.New("billing: invalid status transition")
	// ErrOrderNotFound is returned when an order id or number resolves to no
	// order visible to the caller.
	ErrOrderNotFound = errors.New("billing: order not found")
	// ErrOrderExpired rejects payment on an order past its payment deadline.
	ErrOrderExpired = errors.New("billing: order expired")
	// ErrOrderNotPayable rejects payment on an order that is not pending.
	ErrOrderNotPayable = errors.New("billing: order is not payable")
	// ErrAmountMismatch rejects a callback whose amount differs from the order.
	ErrAmountMismatch = errors.New("billing: amount does not match the order")
	// ErrSpendingConsentRequired rejects checkout without a live guardian
	// spending consent.
	ErrSpendingConsentRequired = errors.New("billing: guardian spending consent required")
	// ErrOverRefund rejects a refund that would exceed the paid total.
	ErrOverRefund = errors.New("billing: refund exceeds paid amount")
	// ErrPlanNotFound is returned when a plan code or id resolves to no plan.
	ErrPlanNotFound = errors.New("billing: plan not found")
	// ErrPlanNotActive rejects purchase of a draft or archived plan.
	ErrPlanNotActive = errors.New("billing: plan is not active")
	// ErrChannelNotConfigured reports that no payment provider is wired up.
	// Callers surface it as "payment not open yet" and never as success.
	ErrChannelNotConfigured = errors.New("billing: payment channel is not configured")
	// ErrSignatureInvalid reports a callback whose signature failed
	// verification. Such a callback is recorded and never credited.
	ErrSignatureInvalid = errors.New("billing: payment signature is invalid")
	// ErrDuplicateNotification reports a callback already accepted under the
	// same channel and notification id. It is a success for the provider.
	ErrDuplicateNotification = errors.New("billing: payment notification is a duplicate")
	// ErrCrossFamilyAccess rejects any order or subscription read for a
	// family other than the authenticated one.
	ErrCrossFamilyAccess = errors.New("billing: resource belongs to another family")
)

// CurrencyCNY is the only currency this package holds amounts in.
//
// The unit rule: every monetary value is an integer number of 分 (1/100 CNY)
// stored as Cents, serialized as an integer cent count, and rendered as
// "¥X.XX" only at the display edge. No field in this package is a decimal or
// floating point amount, and no bank-side currency other than CNY is accepted.
const CurrencyCNY = "CNY"

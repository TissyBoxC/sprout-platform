package channel

import (
	"fmt"
	"net/http"
	"strings"
)

// httpHeaderFromFlat converts the flat notification headers into http.Header.
// Keys are canonicalized so a provider sending `x-signature` and a handler
// reading `X-Signature` agree; repeated keys keep the first value only because a
// duplicated security header is not something a verifier should silently join.
func httpHeaderFromFlat(headers map[string]string) http.Header {
	converted := make(http.Header, len(headers))
	for key, value := range headers {
		canonicalKey := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if canonicalKey == "" {
			continue
		}
		if _, exists := converted[canonicalKey]; exists {
			continue
		}
		converted[canonicalKey] = []string{value}
	}
	return converted
}

// validateNotificationEvent rejects a verified callback that is missing the
// identifiers the idempotency ledger and order lookup depend on. Returning the
// event anyway would let the handler insert an unusable ledger row.
// AmountCents may be zero here: a failure notice legitimately carries no money,
// and the amount check against the order belongs to the handler transaction.
func validateNotificationEvent(event NotificationEvent) error {
	if strings.TrimSpace(event.NotificationID) == "" {
		return fmt.Errorf("%w: notification_id is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(event.OrderNo) == "" {
		return fmt.Errorf("%w: order_no is required", ErrInvalidRequest)
	}
	if event.AmountCents < 0 {
		return fmt.Errorf("%w: amount must not be negative", ErrInvalidRequest)
	}
	return nil
}

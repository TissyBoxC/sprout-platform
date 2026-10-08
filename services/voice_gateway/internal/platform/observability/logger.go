// Package observability owns logs, metrics, traces, and safe error values.
package observability

import (
	"errors"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/moderation"
)

// Logger is the service logging abstraction.
type Logger interface {
	Info(message string, fields ...any)
	Error(message string, fields ...any)
}

// SafeError preserves an error chain while making Error() safe for logs,
// metrics, API responses, and audit records.
type SafeError struct {
	message string
	cause   error
}

// NewSafeError wraps cause with a stable, already-sanitized message.
func NewSafeError(message string, cause error) error {
	message = strings.TrimSpace(RedactString(message))
	if message == "" {
		message = "operation failed"
	}
	return &SafeError{message: message, cause: cause}
}

func (e *SafeError) Error() string {
	if e == nil {
		return "operation failed"
	}
	return e.message
}

func (e *SafeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// RedactString removes credentials, contact data, and child identifiers from
// a free-form value.
func RedactString(value string) string {
	return moderation.Redact(value)
}

// SafeErrorString renders any error without exposing secrets or child data.
func SafeErrorString(err error) string {
	if err == nil {
		return ""
	}
	var safe *SafeError
	if errors.As(err, &safe) {
		return safe.Error()
	}
	return RedactString(err.Error())
}

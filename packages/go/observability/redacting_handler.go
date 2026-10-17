package observability

import (
	"context"
	"log/slog"
)

// NewRedactingHandler wraps a slog handler so all fields pass through the
// shared redaction rules before they reach a sink.
func NewRedactingHandler(next slog.Handler) slog.Handler {
	if next == nil {
		return slog.Default().Handler()
	}
	return &redactingHandler{next: next}
}

type redactingHandler struct {
	next slog.Handler
}

func (handler *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.next.Enabled(ctx, level)
}

func (handler *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	sanitized := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attribute slog.Attr) bool {
		sanitized.AddAttrs(sanitizeAttribute(attribute))
		return true
	})
	return handler.next.Handle(ctx, sanitized)
}

func (handler *redactingHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, len(attributes))
	for index, attribute := range attributes {
		sanitized[index] = sanitizeAttribute(attribute)
	}
	return &redactingHandler{next: handler.next.WithAttrs(sanitized)}
}

func (handler *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{next: handler.next.WithGroup(name)}
}

func sanitizeAttribute(attribute slog.Attr) slog.Attr {
	if isSensitiveField(attribute.Key) {
		return slog.String(attribute.Key, redactedValue)
	}
	return slog.Attr{
		Key:   attribute.Key,
		Value: redactSlogValue(attribute.Value, 0),
	}
}

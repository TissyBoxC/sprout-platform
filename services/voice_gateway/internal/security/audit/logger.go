package audit

import "log/slog"

// SlogSink writes each audit event as one structured log record.
//
// The fields are exactly the content-free Event values, so the sink cannot
// leak speech, prompts, audio, or identifiers even if the logger is configured
// to keep full records.
type SlogSink struct {
	logger *slog.Logger
}

// NewSlogSink creates a structured-log audit sink.
func NewSlogSink(logger *slog.Logger) *SlogSink {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlogSink{logger: logger}
}

// Record implements Sink.
func (sink *SlogSink) Record(event Event) {
	if sink == nil || sink.logger == nil {
		return
	}
	event = normalize(event)
	sink.logger.Info(
		"child safety audit",
		"reason", event.Reason,
		"direction", string(event.Direction),
		"action", event.Action,
		"policy_version", event.PolicyVersion,
		"recorded_at", event.RecordedAt,
	)
}

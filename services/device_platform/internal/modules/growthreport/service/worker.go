package service

import (
	"context"
	"time"
)

// Worker generates reports on a timer so a guardian finds a fresh report at the
// start of a new week or month without asking for one.
type Worker struct {
	service *Service
	// interval is how often the worker looks for a period that has rolled over.
	// The generator is idempotent, so a short interval costs a rewrite of the
	// same row rather than a duplicate.
	interval time.Duration
}

// NewWorker creates a worker. A non-positive interval falls back to one hour,
// which is frequent enough for weekly and monthly windows.
func NewWorker(service *Service, interval time.Duration) *Worker {
	if interval <= 0 {
		interval = time.Hour
	}
	return &Worker{service: service, interval: interval}
}

// Run generates reports until the context is cancelled.
//
// The first pass runs immediately: a freshly deployed worker should backfill
// the current period rather than wait an hour, and the very first report a
// guardian sees should not depend on when the container happened to start.
func (w *Worker) Run(ctx context.Context) {
	w.runOnce(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

// runOnce performs one generation pass and logs the outcome without failing the
// loop: a transient database error must not stop future periods.
func (w *Worker) runOnce(ctx context.Context) {
	generated, err := w.service.GenerateAll(ctx, time.Now().UTC())
	if err != nil {
		w.service.logger.Warn(
			"growth report pass finished with failures",
			"generated",
			generated,
			"error",
			err.Error(),
		)
		return
	}
	w.service.logger.Info(
		"growth report pass finished",
		"generated",
		generated,
	)
}

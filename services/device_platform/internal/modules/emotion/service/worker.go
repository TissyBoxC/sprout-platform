package service

import (
	"context"
	"time"
)

// RetentionWorker removes samples past each family's retention window.
//
// The window is a promise, not a preference: a family that chose seven days must
// not still hold a month of samples because a worker never ran. The worker runs
// on the order of hours rather than seconds because nothing reads expired rows:
// the summary already filters by window, so a late sweep delays deletion but
// never exposes old data.
type RetentionWorker struct {
	service  *Service
	interval time.Duration
}

// DefaultRetentionInterval is how often the sweep runs when none is given.
const DefaultRetentionInterval = 6 * time.Hour

// NewRetentionWorker builds a worker that sweeps on the given interval.
func NewRetentionWorker(service *Service, interval time.Duration) *RetentionWorker {
	if interval <= 0 {
		interval = DefaultRetentionInterval
	}
	return &RetentionWorker{service: service, interval: interval}
}

// Run sweeps until the context is cancelled.
//
// One sweep runs immediately: a process that restarts every few minutes would
// otherwise never reach its first tick and would never delete anything.
func (worker *RetentionWorker) Run(ctx context.Context) {
	worker.sweep(ctx)
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.sweep(ctx)
		}
	}
}

// sweep runs one retention pass and logs the removed total.
func (worker *RetentionWorker) sweep(ctx context.Context) {
	removed, err := worker.service.PurgeExpired(ctx, time.Now().UTC())
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		worker.service.logger.Warn("emotion retention sweep failed", "error", err)
	}
	// Only the count is logged. A per-family line would describe which household
	// is sensing, which is itself information about a child.
	if removed > 0 {
		worker.service.logger.Info("emotion retention sweep removed samples",
			"removed", removed)
	}
}

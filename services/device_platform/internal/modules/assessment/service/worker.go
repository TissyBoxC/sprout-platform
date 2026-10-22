package service

import (
	"context"
	"time"
)

// ExpiryWorker closes assessments that ran past their deadline.
//
// It is the backstop for a device that went to sleep mid-practice: the answer
// path already refuses an answer after the deadline, so this worker closes the
// record and releases the single-active slot rather than enforcing the limit.
type ExpiryWorker struct {
	service  *Service
	interval time.Duration
}

// NewExpiryWorker builds a worker that scans on the given interval.
func NewExpiryWorker(service *Service, interval time.Duration) *ExpiryWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &ExpiryWorker{service: service, interval: interval}
}

// Run scans until the context is cancelled.
func (worker *ExpiryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// ExpireDue returns ErrExpiryFailed when part of the sweep failed, so
			// a single broken row cannot make the whole sweep look healthy.
			if _, err := worker.service.ExpireDue(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				worker.service.logger.Warn("assessment expiry scan failed", "error", err)
			}
		}
	}
}

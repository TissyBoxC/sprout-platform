package service

import (
	"context"
	"time"
)

// ExpiryWorker closes care-view sessions that ran past their deadline.
//
// It is the safety net for a guardian who closed the app mid-view. The device
// enforces its own deadline too, so this worker is the second independent stop
// rather than the only one.
type ExpiryWorker struct {
	service  *Service
	interval time.Duration
}

// NewExpiryWorker builds a worker that scans on the given interval.
func NewExpiryWorker(service *Service, interval time.Duration) *ExpiryWorker {
	if interval <= 0 {
		interval = 15 * time.Second
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
			if _, err := worker.service.ExpireDue(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				worker.service.logger.Warn("care view expiry scan failed", "error", err)
			}
		}
	}
}

package service

import (
	"context"
	"log/slog"
	"time"
)

// ExpiryWorker closes calls that ran past their deadline.
//
// Both the ring timeout and the maximum talk time are enforced server-side as
// well as on the device, because a device that lost power or network would
// otherwise leave a call open forever and block the next one.
type ExpiryWorker struct {
	service  *Service
	interval time.Duration
	logger   *slog.Logger
}

// NewExpiryWorker builds a worker that sweeps on the given interval.
func NewExpiryWorker(service *Service, interval time.Duration) *ExpiryWorker {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	logger := slog.Default()
	if service != nil && service.logger != nil {
		logger = service.logger
	}
	return &ExpiryWorker{service: service, interval: interval, logger: logger}
}

// Run sweeps until the context is cancelled.
func (worker *ExpiryWorker) Run(ctx context.Context) {
	if worker.service == nil {
		return
	}
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			closed, err := worker.service.ExpireDue(ctx)
			if err != nil {
				// A failed sweep is retried on the next tick; the calls stay open
				// in the database and are still refused by the relay because they
				// are past their deadline.
				worker.logger.Error("intercom expiry sweep failed", "error", err)
				continue
			}
			if closed > 0 {
				worker.logger.Info("intercom expiry sweep closed calls", "closed", closed)
			}
		}
	}
}

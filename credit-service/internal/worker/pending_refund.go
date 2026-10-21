// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented pending refund background worker with alerting and resolution logging per sourceOfTruth.md Phases 6 & 8.
// Author review: PENDING - reviewer to complete

package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"foc/credit-service/internal/credit"
)

// PendingRefundWorker periodically claims and retries blocked pending refunds.
type PendingRefundWorker struct {
	repo          credit.PendingRefundRepository
	retryInterval time.Duration
	alertAfter    time.Duration
	batchSize     int
	logger        *slog.Logger
}

// NewPendingRefundWorker constructs a pending refund worker with configured intervals and limits.
func NewPendingRefundWorker(repo credit.PendingRefundRepository, retryInterval, alertAfter time.Duration, batchSize int, logger *slog.Logger) *PendingRefundWorker {
	if retryInterval <= 0 {
		retryInterval = 30 * time.Second
	}
	if alertAfter <= 0 {
		alertAfter = 5 * time.Minute
	}
	if batchSize <= 0 {
		batchSize = 50
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PendingRefundWorker{
		repo:          repo,
		retryInterval: retryInterval,
		alertAfter:    alertAfter,
		batchSize:     batchSize,
		logger:        logger,
	}
}

// ProcessCycle runs a single cycle of pending refund processing and overdue alerting.
func (w *PendingRefundWorker) ProcessCycle(ctx context.Context) (int, error) {
	dueItems, err := w.repo.ClaimDuePendingRefunds(ctx, w.batchSize)
	if err != nil {
		return 0, fmt.Errorf("claim due pending refunds: %w", err)
	}

	processed := 0
	for _, item := range dueItems {
		// Overdue alert check
		if item.OverdueAlertedAt == nil && time.Since(item.FirstSeenAt) >= w.alertAfter {
			w.logger.Error("pending refund overdue",
				"stableKey", fmt.Sprintf("pending-refund-overdue:%s", item.ErrandID),
				"errandId", item.ErrandID.String(),
				"eventId", item.EventID.String(),
				"reason", string(item.Reason),
				"age", time.Since(item.FirstSeenAt).String(),
				"attemptCount", item.AttemptCount,
			)
			_ = w.repo.RecordPendingRefundOverdue(ctx, item.ID, time.Now().UTC())
		}

		if err := w.repo.ProcessPendingRefund(ctx, item.ID, w.retryInterval); err != nil {
			w.logger.Error("process pending refund failed", "id", item.ID.String(), "error", err)
			continue
		}

		if item.OverdueAlertedAt != nil {
			w.logger.Info("pending refund resolved",
				"stableKey", fmt.Sprintf("pending-refund-overdue:%s", item.ErrandID),
				"errandId", item.ErrandID.String(),
			)
		}
		processed++
	}

	return processed, nil
}

// Run executes the worker loop on retryInterval until context cancellation.
func (w *PendingRefundWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.retryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := w.ProcessCycle(ctx); err != nil {
				w.logger.Error("pending refund worker cycle error", "error", err)
			}
		}
	}
}

// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented pending payout background worker with alerting and resolution logging per sourceOfTruth.md Phase 8.
// Author review: PENDING - reviewer to complete

package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"foc/credit-service/internal/credit"
)

// PendingPayoutWorker periodically claims and retries delayed payouts.
type PendingPayoutWorker struct {
	repo          credit.PendingPayoutRepository
	retryInterval time.Duration
	alertAfter    time.Duration
	batchSize     int
	logger        *slog.Logger
}

// NewPendingPayoutWorker constructs a pending payout worker with configured intervals and limits.
func NewPendingPayoutWorker(repo credit.PendingPayoutRepository, retryInterval, alertAfter time.Duration, batchSize int, logger *slog.Logger) *PendingPayoutWorker {
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
	return &PendingPayoutWorker{
		repo:          repo,
		retryInterval: retryInterval,
		alertAfter:    alertAfter,
		batchSize:     batchSize,
		logger:        logger,
	}
}

// ProcessCycle runs a single cycle of pending payout processing and overdue alerting.
func (w *PendingPayoutWorker) ProcessCycle(ctx context.Context) (int, error) {
	dueItems, err := w.repo.ClaimDuePendingPayouts(ctx, w.batchSize)
	if err != nil {
		return 0, fmt.Errorf("claim due pending payouts: %w", err)
	}

	processed := 0
	for _, item := range dueItems {
		// Overdue alert check
		if item.OverdueAlertedAt == nil && time.Since(item.FirstSeenAt) >= w.alertAfter {
			w.logger.Error("pending payout overdue",
				"stableKey", fmt.Sprintf("pending-payout-overdue:%s", item.ErrandID),
				"errandId", item.ErrandID.String(),
				"eventId", item.EventID.String(),
				"courierId", item.CourierID.String(),
				"age", time.Since(item.FirstSeenAt).String(),
				"attemptCount", item.AttemptCount,
			)
			_ = w.repo.RecordPendingPayoutOverdue(ctx, item.ID, time.Now().UTC())
		}

		if err := w.repo.ProcessPendingPayout(ctx, item.ID, w.retryInterval); err != nil {
			w.logger.Error("process pending payout failed", "id", item.ID.String(), "error", err)
			continue
		}

		if item.OverdueAlertedAt != nil {
			w.logger.Info("pending payout resolved",
				"stableKey", fmt.Sprintf("pending-payout-overdue:%s", item.ErrandID),
				"errandId", item.ErrandID.String(),
			)
		}
		processed++
	}

	return processed, nil
}

// Run executes the worker loop on retryInterval until context cancellation.
func (w *PendingPayoutWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.retryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := w.ProcessCycle(ctx); err != nil {
				w.logger.Error("pending payout worker cycle error", "error", err)
			}
		}
	}
}

// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Implemented ReconciliationWorker with dedicated session advisory lock, batch scanning, and discrepancy flagging per sourceOfTruth.md Phase 10.
// Author review: PENDING - reviewer to complete

package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
)

// ReconciliationWorker runs periodic background audit scans to detect ledger discrepancies.
type ReconciliationWorker struct {
	repo      credit.ReconciliationRepository
	batchSize int
	interval  time.Duration
	logger    *slog.Logger
}

// NewReconciliationWorker creates a new reconciliation worker.
func NewReconciliationWorker(repo credit.ReconciliationRepository, interval time.Duration, batchSize int, logger *slog.Logger) *ReconciliationWorker {
	if batchSize <= 0 {
		batchSize = 500
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &ReconciliationWorker{
		repo:      repo,
		batchSize: batchSize,
		interval:  interval,
		logger:    logger,
	}
}

// Run begins the periodic reconciliation worker loop until context is cancelled.
func (w *ReconciliationWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		w.logger.Error("reconciliation worker cycle failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				w.logger.Error("reconciliation worker cycle failed", "error", err)
			}
		}
	}
}

// RunOnce executes a single reconciliation audit cycle.
func (w *ReconciliationWorker) RunOnce(ctx context.Context) error {
	unlock, acquired, err := w.repo.AcquireReconciliationLock(ctx)
	if err != nil {
		return err
	}
	if !acquired {
		w.logger.Debug("reconciliation advisory lock held by another replica, skipping cycle")
		return nil
	}
	defer unlock()

	var (
		lastSeenUserID *uuid.UUID
		totalChecked   int
		totalFlagged   int
	)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		userIDs, nextCursor, err := w.repo.ScanAccountsForReconciliation(ctx, w.batchSize, lastSeenUserID)
		if err != nil {
			return err
		}
		if len(userIDs) == 0 {
			break
		}

		for _, userID := range userIDs {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			flagged, err := w.repo.ReconcileAccount(ctx, userID)
			if err != nil {
				w.logger.Error("error reconciling account", "userId", userID, "error", err)
				continue
			}
			totalChecked++
			if flagged {
				totalFlagged++
				w.logger.Error("account flagged for financial integrity violation", "userId", userID)
			}
		}

		if nextCursor == nil {
			break
		}
		lastSeenUserID = nextCursor
	}

	w.logger.Info("reconciliation cycle completed", "checked", totalChecked, "flagged", totalFlagged)
	return nil
}

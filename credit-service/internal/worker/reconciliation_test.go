// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Added unit tests for ReconciliationWorker lifecycle, advisory lock skipping, and batch scanning per sourceOfTruth.md Phase 10.
// Author review: PENDING - reviewer to complete

package worker_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"foc/credit-service/internal/worker"

	"github.com/google/uuid"
)

type mockReconciliationRepo struct {
	acquireLockFn func(ctx context.Context) (func(), bool, error)
	scanFn        func(ctx context.Context, batchSize int, lastSeenUserID *uuid.UUID) ([]uuid.UUID, *uuid.UUID, error)
	reconcileFn   func(ctx context.Context, userID uuid.UUID) (bool, error)
}

func (m *mockReconciliationRepo) AcquireReconciliationLock(ctx context.Context) (func(), bool, error) {
	if m.acquireLockFn != nil {
		return m.acquireLockFn(ctx)
	}
	return func() {}, true, nil
}

func (m *mockReconciliationRepo) ScanAccountsForReconciliation(ctx context.Context, batchSize int, lastSeenUserID *uuid.UUID) ([]uuid.UUID, *uuid.UUID, error) {
	if m.scanFn != nil {
		return m.scanFn(ctx, batchSize, lastSeenUserID)
	}
	return nil, nil, nil
}

func (m *mockReconciliationRepo) ReconcileAccount(ctx context.Context, userID uuid.UUID) (bool, error) {
	if m.reconcileFn != nil {
		return m.reconcileFn(ctx, userID)
	}
	return false, nil
}

func TestReconciliationWorker_AdvisoryLockSkipping(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	t.Run("skips cycle when lock not acquired", func(t *testing.T) {
		repo := &mockReconciliationRepo{
			acquireLockFn: func(ctx context.Context) (func(), bool, error) {
				return nil, false, nil
			},
			scanFn: func(ctx context.Context, batchSize int, lastSeenUserID *uuid.UUID) ([]uuid.UUID, *uuid.UUID, error) {
				t.Fatal("scan should not be called when lock is not acquired")
				return nil, nil, nil
			},
		}

		w := worker.NewReconciliationWorker(repo, 5*time.Minute, 10, logger)
		if err := w.RunOnce(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("propagates lock acquisition error", func(t *testing.T) {
		wantErr := errors.New("connection failed")
		repo := &mockReconciliationRepo{
			acquireLockFn: func(ctx context.Context) (func(), bool, error) {
				return nil, false, wantErr
			},
		}

		w := worker.NewReconciliationWorker(repo, 5*time.Minute, 10, logger)
		err := w.RunOnce(context.Background())
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want %v", err, wantErr)
		}
	})
}

func TestReconciliationWorker_BatchScanningAndFlagging(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	u1 := uuid.New()
	u2 := uuid.New()
	u3 := uuid.New()

	scans := 0
	reconciledUsers := make(map[uuid.UUID]bool)
	unlocked := false

	repo := &mockReconciliationRepo{
		acquireLockFn: func(ctx context.Context) (func(), bool, error) {
			return func() { unlocked = true }, true, nil
		},
		scanFn: func(ctx context.Context, batchSize int, lastSeenUserID *uuid.UUID) ([]uuid.UUID, *uuid.UUID, error) {
			scans++
			if lastSeenUserID == nil {
				return []uuid.UUID{u1, u2}, &u2, nil
			}
			if *lastSeenUserID == u2 {
				return []uuid.UUID{u3}, nil, nil
			}
			return nil, nil, nil
		},
		reconcileFn: func(ctx context.Context, userID uuid.UUID) (bool, error) {
			reconciledUsers[userID] = true
			if userID == u2 {
				return true, nil // Flag u2
			}
			return false, nil
		},
	}

	w := worker.NewReconciliationWorker(repo, 5*time.Minute, 2, logger)
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !unlocked {
		t.Error("advisory lock was not unlocked")
	}
	if scans != 2 {
		t.Errorf("scans = %d, want 2", scans)
	}
	if len(reconciledUsers) != 3 {
		t.Errorf("reconciled user count = %d, want 3", len(reconciledUsers))
	}
	if !reconciledUsers[u1] || !reconciledUsers[u2] || !reconciledUsers[u3] {
		t.Errorf("expected all 3 users reconciled, got: %+v", reconciledUsers)
	}
}

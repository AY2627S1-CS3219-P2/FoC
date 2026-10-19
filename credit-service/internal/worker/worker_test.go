// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Unit tests for pending payout and refund background workers per sourceOfTruth.md Phase 8.
// Author review: PENDING - reviewer to complete

package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
)

type mockPayoutRepo struct {
	claimed     []credit.PendingPayout
	claimErr    error
	processedID uuid.UUID
	processErr  error
	overdueID   uuid.UUID
}

func (m *mockPayoutRepo) ClaimDuePendingPayouts(_ context.Context, _ int) ([]credit.PendingPayout, error) {
	return m.claimed, m.claimErr
}

func (m *mockPayoutRepo) ProcessPendingPayout(_ context.Context, id uuid.UUID, _ time.Duration) error {
	m.processedID = id
	return m.processErr
}

func (m *mockPayoutRepo) RecordPendingPayoutOverdue(_ context.Context, id uuid.UUID, _ time.Time) error {
	m.overdueID = id
	return nil
}

type mockRefundRepo struct {
	claimed     []credit.PendingRefund
	claimErr    error
	processedID uuid.UUID
	processErr  error
	overdueID   uuid.UUID
}

func (m *mockRefundRepo) ClaimDuePendingRefunds(_ context.Context, _ int) ([]credit.PendingRefund, error) {
	return m.claimed, m.claimErr
}

func (m *mockRefundRepo) ProcessPendingRefund(_ context.Context, id uuid.UUID, _ time.Duration) error {
	m.processedID = id
	return m.processErr
}

func (m *mockRefundRepo) RecordPendingRefundOverdue(_ context.Context, id uuid.UUID, _ time.Time) error {
	m.overdueID = id
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPendingPayoutWorker(t *testing.T) {
	t.Run("processes empty due items", func(t *testing.T) {
		repo := &mockPayoutRepo{}
		w := NewPendingPayoutWorker(repo, 30*time.Second, 5*time.Minute, 50, discardLogger())

		count, err := w.ProcessCycle(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected 0 processed, got %d", count)
		}
	})

	t.Run("processes due item not overdue", func(t *testing.T) {
		payoutID := uuid.New()
		errandID := uuid.New()
		repo := &mockPayoutRepo{
			claimed: []credit.PendingPayout{
				{
					ID:          payoutID,
					ErrandID:    errandID,
					FirstSeenAt: time.Now().Add(-1 * time.Minute),
				},
			},
		}
		w := NewPendingPayoutWorker(repo, 30*time.Second, 5*time.Minute, 50, discardLogger())

		count, err := w.ProcessCycle(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 processed, got %d", count)
		}
		if repo.processedID != payoutID {
			t.Fatalf("expected process ID %v, got %v", payoutID, repo.processedID)
		}
		if repo.overdueID != uuid.Nil {
			t.Fatalf("expected no overdue record, got %v", repo.overdueID)
		}
	})

	t.Run("records overdue alert for aged item", func(t *testing.T) {
		payoutID := uuid.New()
		errandID := uuid.New()
		repo := &mockPayoutRepo{
			claimed: []credit.PendingPayout{
				{
					ID:          payoutID,
					ErrandID:    errandID,
					FirstSeenAt: time.Now().Add(-10 * time.Minute), // older than 5m
				},
			},
		}
		w := NewPendingPayoutWorker(repo, 30*time.Second, 5*time.Minute, 50, discardLogger())

		count, err := w.ProcessCycle(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 processed, got %d", count)
		}
		if repo.overdueID != payoutID {
			t.Fatalf("expected overdue record for %v, got %v", payoutID, repo.overdueID)
		}
	})

	t.Run("propagates claim error", func(t *testing.T) {
		repo := &mockPayoutRepo{claimErr: errors.New("db error")}
		w := NewPendingPayoutWorker(repo, 30*time.Second, 5*time.Minute, 50, discardLogger())

		_, err := w.ProcessCycle(context.Background())
		if err == nil {
			t.Fatal("expected claim error")
		}
	})
}

func TestPendingRefundWorker(t *testing.T) {
	t.Run("processes empty due items", func(t *testing.T) {
		repo := &mockRefundRepo{}
		w := NewPendingRefundWorker(repo, 30*time.Second, 5*time.Minute, 50, discardLogger())

		count, err := w.ProcessCycle(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected 0 processed, got %d", count)
		}
	})

	t.Run("records overdue alert for aged refund item", func(t *testing.T) {
		refundID := uuid.New()
		errandID := uuid.New()
		repo := &mockRefundRepo{
			claimed: []credit.PendingRefund{
				{
					ID:          refundID,
					ErrandID:    errandID,
					Reason:      credit.TransactionReasonErrandCancelled,
					FirstSeenAt: time.Now().Add(-6 * time.Minute),
				},
			},
		}
		w := NewPendingRefundWorker(repo, 30*time.Second, 5*time.Minute, 50, discardLogger())

		count, err := w.ProcessCycle(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 processed, got %d", count)
		}
		if repo.overdueID != refundID {
			t.Fatalf("expected overdue record for %v, got %v", refundID, repo.overdueID)
		}
	})

	t.Run("propagates claim error", func(t *testing.T) {
		repo := &mockRefundRepo{claimErr: errors.New("db error")}
		w := NewPendingRefundWorker(repo, 30*time.Second, 5*time.Minute, 50, discardLogger())

		_, err := w.ProcessCycle(context.Background())
		if err == nil {
			t.Fatal("expected claim error")
		}
	})
}

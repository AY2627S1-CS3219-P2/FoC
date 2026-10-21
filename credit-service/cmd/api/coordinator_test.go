// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Unit tests for first-error coordinator per sourceOfTruth.md Phase 12.
// Author review: PENDING - reviewer to complete

package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCoordinator_IgnoresNilAndCancellation(t *testing.T) {
	var intakeCancelled atomic.Bool
	cancel := func() { intakeCancelled.Store(true) }

	coord := newCoordinator(cancel, nil)
	coord.report("http", nil)
	coord.report("http", context.Canceled)
	coord.report("http", http.ErrServerClosed)

	if coord.firstError() != nil {
		t.Errorf("firstError() = %v, want nil", coord.firstError())
	}
	if intakeCancelled.Load() {
		t.Error("intake was cancelled unexpectedly")
	}
}

func TestCoordinator_RecordsFirstErrorAndCancelsIntake(t *testing.T) {
	var intakeCancelled atomic.Bool
	cancel := func() { intakeCancelled.Store(true) }

	coord := newCoordinator(cancel, nil)
	expectedErr := errors.New("database connection failed")

	coord.report("postgres", expectedErr)

	if !errors.Is(coord.firstError(), expectedErr) {
		t.Errorf("firstError() = %v, want %v", coord.firstError(), expectedErr)
	}
	if !intakeCancelled.Load() {
		t.Error("expected intake to be cancelled on first fatal error")
	}
}

func TestCoordinator_LogsSecondaryErrorsWithoutOverwriting(t *testing.T) {
	var cancelCount atomic.Int32
	cancel := func() { cancelCount.Add(1) }

	coord := newCoordinator(cancel, nil)
	err1 := errors.New("fatal component 1")
	err2 := errors.New("secondary component 2")

	coord.report("comp1", err1)
	coord.report("comp2", err2)

	if !errors.Is(coord.firstError(), err1) {
		t.Errorf("firstError() = %v, want %v", coord.firstError(), err1)
	}
	if cancelCount.Load() != 1 {
		t.Errorf("cancelCount = %d, want 1", cancelCount.Load())
	}
}

func TestCoordinator_ConcurrentReports(t *testing.T) {
	var cancelCount atomic.Int32
	cancel := func() { cancelCount.Add(1) }

	coord := newCoordinator(cancel, nil)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			coord.report("comp", errors.New("concurrent error"))
		}(i)
	}

	wg.Wait()

	if coord.firstError() == nil {
		t.Fatal("expected firstError to be non-nil")
	}
	if cancelCount.Load() != 1 {
		t.Errorf("cancelCount = %d, want 1", cancelCount.Load())
	}
}

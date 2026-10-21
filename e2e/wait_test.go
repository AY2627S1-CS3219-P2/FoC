//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Polling with a deadline for the suite's asynchronous effects (root
//   AGENTS.md §7: no sleeping to wait), and the check that the stack runs
//   with the timers the suite expects (D-049).
// Author review: James Teo — reviewed and verified.

package e2e

import (
	"context"
	"testing"
	"time"
)

const (
	pollInterval = 250 * time.Millisecond
	// AI-generated (edited by James Teo).
	// eventDeadline bounds how long an asynchronous effect may take to show:
	// a credit account after a student verifies their email, or credits after
	// the event an action emits. NFR5.2 asks for 5 s at p99; the rest is slack
	// for a stack on a laptop.
	eventDeadline = 15 * time.Second
	// sweepInterval is how often order-service expires and auto-confirms
	// errands (D-049).
	sweepInterval = 5 * time.Second
	// clockMargin is waited past a deadline before acting on it, so the
	// precision of a timestamp cannot make the suite act a moment early.
	clockMargin = time.Second
	// timerTolerance is how far a deadline the stack reports may sit from the
	// suite's expectation: the gap between the transition and the response.
	timerTolerance = 2 * time.Second
)

// poll calls check every pollInterval until check asks to stop or ctx ends.
// It reports whether check asked to stop, and the status check last gave
// while ctx was live, for failure messages.
func poll(ctx context.Context, check func(context.Context) (stop bool, status string)) (string, bool) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	last := "no answer yet"
	for {
		stop, status := check(ctx)
		if stop {
			return status, true
		}
		if ctx.Err() == nil {
			last = status
		}
		select {
		case <-ctx.Done():
			return last, false
		case <-ticker.C:
		}
	}
}

// eventually fails the test unless check reports done within timeout.
func eventually(t *testing.T, timeout time.Duration, what string, check func(context.Context) (done bool, status string)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	if last, ok := poll(ctx, check); !ok {
		t.Fatalf("%s: not within %s; last seen: %s", what, timeout, last)
	}
}

// requireTimer fails at once when the stack runs with another timer than the
// one the suite expects, rather than waiting out the wrong one. got is the
// deadline's distance from the server time in the response that set it.
func requireTimer(t *testing.T, name, serviceVar, suiteVar string, got, want time.Duration) {
	t.Helper()
	if diff := got - want; diff < -timerTolerance || diff > timerTolerance {
		t.Fatalf("order-service runs with %s ≈ %s (%s), but this suite expects %s (%s); "+
			"start the stack as README.md shows, or set %s to the stack's value",
			name, got.Round(time.Second), serviceVar, want, suiteVar, suiteVar)
	}
}

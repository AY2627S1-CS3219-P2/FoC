// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The one sweeper that expires OPEN errands past T and auto-confirms
//   DELIVERED errands past C every 5 seconds (D-049, D-050, F3.14.1, F3.9.4).
// Author review: James Teo — reviewed and verified.

// Package sweeper runs order-service's background deadline check (D-049):
// AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — adds due-scheduled opening to the existing sweep.
// every interval it opens due scheduled errands, expires OPEN errands whose
// deadline has passed, and auto-confirms DELIVERED errands past their window.
// It decides nothing itself. Each due id goes through the order service, which
// re-checks the errand under its conditional write (F3.14.1), so a user action
// or a second sweeper racing this one cannot move an errand twice.
package sweeper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/order"
)

// DefaultInterval is D-049's sweep period.
const DefaultInterval = 5 * time.Second

// correlationPrefix marks the correlation ids the sweeper mints, so a log or
// event line caused by a sweep is told apart from one caused by a request.
const correlationPrefix = "sweep-"

// Source lists scheduled errands and lifecycle deadlines that are due, oldest
// first. order.Repository satisfies it.
type Source interface {
	DueForScheduled(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
	DueForExpiry(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
	DueForAutoConfirm(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
}

// Actions runs the two system transitions. *order.Service satisfies it. Each
// reports applied=false when the errand was no longer due or had already been
// moved by someone else.
type Actions interface {
	OpenScheduled(ctx context.Context, id uuid.UUID, correlationID string) (bool, error)
	Expire(ctx context.Context, id uuid.UUID, correlationID string) (bool, error)
	AutoConfirm(ctx context.Context, id uuid.UUID, correlationID string) (bool, error)
}

// Sweeper opens, expires and auto-confirms due errands. It holds no state between
// sweeps, so it is safe to run more than one, in one process or in several:
// the order service's conditional writes let exactly one of them move each
// errand.
type Sweeper struct {
	src      Source
	actions  Actions
	clock    order.Clock
	log      *slog.Logger
	interval time.Duration
}

// New returns a ready-to-run Sweeper. A non-positive interval falls back to
// DefaultInterval, because a ticker cannot run on one, and a nil log discards.
func New(src Source, actions Actions, clock order.Clock, log *slog.Logger, interval time.Duration) *Sweeper {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Sweeper{src: src, actions: actions, clock: clock, log: log, interval: interval}
}

// Run sweeps once straight away, so nothing due waits a whole interval after a
// restart, then every interval until ctx is done. A failed sweep is logged and
// the next one runs on schedule. A sweep that overruns the interval delays the
// next one rather than overlapping it.
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if _, _, err := s.SweepOnce(ctx); err != nil && ctx.Err() == nil {
			s.log.ErrorContext(ctx, "sweep failed; the next sweep retries", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// SweepOnce expires and auto-confirms due errands, then opens due scheduled
// errands, up to order.SweepBatchLimit of each; anything beyond that waits for
// the next sweep. It returns expiry and auto-confirm counts.
//
// err reports a due list that could not be read, or ctx ending mid-sweep. An
// error on one errand is logged, not returned, and does not stop the sweep:
// that errand is still due, so the next sweep retries it.
func (s *Sweeper) SweepOnce(ctx context.Context) (expired, confirmed int, err error) {
	now := s.clock.Now()
	expired, expireErr := s.sweep(ctx, now, "expire", s.src.DueForExpiry, s.actions.Expire)
	if ctx.Err() != nil {
		return expired, 0, expireErr
	}
	confirmed, confirmErr := s.sweep(ctx, now, "auto-confirm", s.src.DueForAutoConfirm, s.actions.AutoConfirm)
	if ctx.Err() != nil {
		return expired, confirmed, errors.Join(expireErr, confirmErr)
	}
	_, scheduledErr := s.sweep(ctx, now, "open-scheduled", s.src.DueForScheduled, s.actions.OpenScheduled)
	return expired, confirmed, errors.Join(expireErr, confirmErr, scheduledErr)
}

// sweep lists one kind of due errand and runs act on each. action names the
// transition in logs and errors only.
func (s *Sweeper) sweep(
	ctx context.Context,
	now time.Time,
	action string,
	due func(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error),
	act func(ctx context.Context, id uuid.UUID, correlationID string) (bool, error),
) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("%s sweep stopped: %w", action, err)
	}
	ids, err := due(ctx, now, order.SweepBatchLimit)
	if err != nil {
		return 0, fmt.Errorf("list errands due to %s: %w", action, err)
	}
	moved := 0
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return moved, fmt.Errorf("%s sweep stopped: %w", action, err)
		}
		// One id per errand rather than per sweep, so an errand's history
		// row, event and log lines share an id no unrelated errand carries.
		correlationID := correlationPrefix + uuid.NewString()
		itemCtx := order.WithCorrelationID(ctx, correlationID)
		applied, err := act(itemCtx, id, correlationID)
		switch {
		case err != nil && ctx.Err() != nil:
			return moved, fmt.Errorf("%s sweep stopped: %w", action, ctx.Err())
		case err != nil:
			s.log.ErrorContext(itemCtx, "sweeper could not move errand; it stays due and the next sweep retries it",
				"action", action, "errand_id", id, "request_id", correlationID, "err", err)
		case applied:
			moved++
			s.log.InfoContext(itemCtx, "sweeper moved errand",
				"action", action, "errand_id", id, "request_id", correlationID)
		default:
			s.log.InfoContext(itemCtx, "sweeper left errand: no longer due, or already moved by someone else",
				"action", action, "errand_id", id, "request_id", correlationID)
		}
	}
	return moved, nil
}

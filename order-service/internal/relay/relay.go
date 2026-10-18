// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The outbox relay loop: claims due outbox rows one at a time and hands
//   each to the broker publisher, retrying failures (D-051, NFR5.2, NFR5.4).
// Author review: James Teo — reviewed and verified.

// Package relay publishes order-service's outbox to RabbitMQ (D-051). Every
// lifecycle event is written to the outbox in the same transaction as the state
// change that caused it; the relay claims one due row at a time, publishes it
// with a broker confirm, and records the outcome. A failed publish stays in the
// outbox and is retried, so events committed while the broker is down reach it
// once it is back (NFR5.4).
package relay

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Record is one outbox row as the relay publishes it.
type Record struct {
	EventID    uuid.UUID
	RoutingKey string
	// Payload is the event's JSON body, sent as stored.
	Payload       []byte
	CorrelationID string
	// Attempts counts the failed publishes before this one.
	Attempts int
}

// Store hands out due outbox rows one at a time.
type Store interface {
	// ClaimNext locks the oldest due, unpublished row and calls publish with it
	// while the lock is held. A nil from publish marks the row published; an
	// error is recorded on the row, which is retried once retryDelay has
	// passed. claimed reports whether a row was due. Publish errors are
	// recorded, never returned: err is for the store's own failures.
	ClaimNext(ctx context.Context, retryDelay time.Duration, publish func(ctx context.Context, r Record) error) (claimed bool, err error)
}

// Publisher sends one record to the broker.
type Publisher interface {
	// Publish returns nil only once the broker has confirmed the record and
	// routed it to at least one queue.
	Publish(ctx context.Context, r Record) error
}

// The values Options' zero fields take.
const (
	DefaultPollInterval = 250 * time.Millisecond
	DefaultRetryDelay   = 30 * time.Second
)

// maxErrorPause caps the pause between claims while the store keeps failing.
// It keeps the first claim after the database comes back within NFR5.2's
// budget, while a long outage logs one ERROR per maxErrorPause instead of one
// per PollInterval.
const maxErrorPause = 2 * time.Second

// Options tunes a Relay. A zero or negative field takes its default.
type Options struct {
	// PollInterval is how long the relay waits after finding nothing due
	// before it looks again. It bounds how late a new event starts publishing
	// (NFR5.2). It is also the pause after a first store error; each further
	// error in a row doubles that pause, up to maxErrorPause or PollInterval,
	// whichever is longer.
	PollInterval time.Duration
	// RetryDelay is how long a row waits after a failed publish.
	RetryDelay time.Duration
}

// Relay moves due outbox rows to the broker. Several relays may run against
// one outbox: a row is locked while it is published, so each is published by
// one relay at a time.
type Relay struct {
	store Store
	pub   Publisher
	log   *slog.Logger
	opts  Options
}

// New returns a Relay ready to Run. A nil log discards the relay's logs.
func New(store Store, pub Publisher, log *slog.Logger, opts Options) *Relay {
	if opts.PollInterval <= 0 {
		opts.PollInterval = DefaultPollInterval
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = DefaultRetryDelay
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Relay{store: store, pub: pub, log: log, opts: opts}
}

// Run publishes due rows until ctx is done, then returns. While rows are due
// it claims them back to back; otherwise it looks again every PollInterval.
// Publish and store failures are logged and retried; they never stop it.
// Store failures in a row back off: the pause doubles after each, up to
// maxErrorPause, so a database outage does not flood the log. The first claim
// the store answers, whether or not a row was due, restores PollInterval.
func (r *Relay) Run(ctx context.Context) {
	errPause := r.opts.PollInterval
	for {
		claimed, err := r.store.ClaimNext(ctx, r.opts.RetryDelay, r.publish)
		if ctx.Err() != nil {
			return
		}
		wait := r.opts.PollInterval
		if err != nil {
			r.log.ErrorContext(ctx, "outbox claim failed; will retry", "err", err)
			wait = errPause
			errPause = nextErrorPause(errPause, r.opts.PollInterval)
		} else {
			errPause = r.opts.PollInterval
			if claimed {
				// Another row may already be due: drain before waiting.
				continue
			}
		}
		if !pause(ctx, wait) {
			return
		}
	}
}

// nextErrorPause returns the pause after the next store error in a row, given
// prev, the pause after the latest one: twice prev, at most maxErrorPause, and
// never less than pollInterval, so a relay that polls more slowly than
// maxErrorPause keeps its own pace.
func nextErrorPause(prev, pollInterval time.Duration) time.Duration {
	return max(min(2*prev, maxErrorPause), pollInterval)
}

// publish is the callback ClaimNext runs under the row lock.
func (r *Relay) publish(ctx context.Context, rec Record) error {
	err := r.pub.Publish(ctx, rec)
	if err == nil {
		// NFR7.1: an event's correlation id reaches the logs when it leaves.
		r.log.InfoContext(ctx, "outbox event published",
			"event_id", rec.EventID, "routing_key", rec.RoutingKey, "request_id", rec.CorrelationID)
		return nil
	}
	// A publish cut short by shutdown is not a broker failure worth an alert.
	if ctx.Err() == nil {
		r.log.ErrorContext(ctx, "outbox publish failed; will retry",
			"event_id", rec.EventID, "routing_key", rec.RoutingKey, "request_id", rec.CorrelationID,
			"attempt", rec.Attempts+1, "err", err)
	}
	return err
}

// pause blocks for d or until ctx is done, and reports whether d elapsed.
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

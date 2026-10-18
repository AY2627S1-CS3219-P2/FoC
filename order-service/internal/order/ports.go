// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The interfaces the domain depends on: clock, supplier-service,
//   credit-service and the repository, with the semantics each must keep.
// Author review: James Teo — reviewed and verified.

package order

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Clock is injected so deadlines are testable without waiting
// (order-service/AGENTS.md, root AGENTS.md §7).
type Clock interface {
	Now() time.Time
}

// SystemClock is the real clock, in UTC.
type SystemClock struct{}

// Now returns the current time in UTC.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// Supplier is the part of supplier-service's record an errand needs.
type Supplier struct {
	ID                  uuid.UUID
	Name                string
	Building            string
	Floor               string
	LocationDescription string
	Available           bool
}

// SupplierDirectory looks a supplier up in supplier-service (D-052).
type SupplierDirectory interface {
	// Supplier returns ErrSupplierNotFound for an unknown or deleted supplier.
	// Any other error means supplier-service did not give an answer.
	Supplier(ctx context.Context, id uuid.UUID) (Supplier, error)
}

// Reservation asks credit-service to hold a requester's credits for an errand
// (D-047). ErrandID is the errand's id, generated before the call.
type Reservation struct {
	ErrandID      uuid.UUID
	RequesterID   uuid.UUID
	Amount        int
	CorrelationID string
}

// CreditClient calls credit-service synchronously. Settlement is by events
// only (D-048), so these are the only two calls.
type CreditClient interface {
	// AI-generated (edited by James Teo).
	// Reserve returns nil once the credits are held, ErrInsufficientCredits on
	// credit's 422 to the first attempt, ErrCreditRejected on another definite
	// refusal, and any other error when the outcome is not known, a 422 to a
	// retry included. After a retry even ErrCreditRejected cannot prove
	// nothing was held, so callers rely on D-047's queued refund either way.
	Reserve(ctx context.Context, r Reservation) error
	// Adjust sets the held amount for an errand to newReward (an absolute
	// amount, D-057). It returns nil when applied, ErrInsufficientCredits on
	// 422, ErrCreditRejected on another definite refusal, and any other error
	// when the outcome is not known. Calls are made as the errand's requester:
	// ctx carries their own access token, which the adapter forwards (D-056).
	Adjust(ctx context.Context, errandID, requesterID uuid.UUID, newReward int, correlationID string) error
}

// OutboxEvent is one row of the outbox (D-051). AvailableAt delays publishing;
// D-047's queued refund uses it.
type OutboxEvent struct {
	EventID       uuid.UUID
	RoutingKey    string
	Payload       EventPayload
	CorrelationID string
	AvailableAt   time.Time
}

// Transition is one conditional write (D-046). The repository applies it only
// if the row still matches every expectation, and writes the history row and
// the optional event in the same transaction.
type Transition struct {
	OrderID   uuid.UUID
	FromState State
	// The people the check in Go relied on; nil means "not checked".
	ExpectRequesterID *uuid.UUID
	ExpectCourierID   *uuid.UUID
	// ExpectVersion is the version the check in Go read; nil means "not
	// checked". Without it, an amend committing between the read and this
	// write would let a settling event carry the old reward.
	ExpectVersion *int
	// Deadline conditions, each nil when not checked.
	OpenBefore   *time.Time // expires_at > this (user actions on an OPEN errand)
	ExpiryDueBy  *time.Time // expires_at <= this (Expire)
	ConfirmDueBy *time.Time // confirm_by <= this (AutoConfirm)
	ToState      State
	CourierID    *uuid.UUID // the courier after the write; nil clears it
	ExpiresAt    *time.Time // the deadline after the write; nil clears it
	ConfirmBy    *time.Time // the confirmation window after the write; nil clears it
	Now          time.Time
	History      HistoryEntry
	Event        *OutboxEvent
}

// AcceptCommand is D-046's accept: one conditional write, with a per-courier
// lock in the same transaction enforcing K.
type AcceptCommand struct {
	OrderID         uuid.UUID
	CourierID       uuid.UUID
	ExpectedVersion int
	Now             time.Time
	MaxActive       int
	History         HistoryEntry
}

// AmendResult is what an amend writes under the row lock. A nil *AmendResult
// means nothing changed and nothing is written.
type AmendResult struct {
	Order   Order // the full row after the amendment; Version already incremented
	History HistoryEntry
	// Event is D-057's errand.reward_set when the reward changed, written in
	// the same transaction; nil otherwise.
	Event *OutboxEvent
}

// ListLimits caps list sizes (D-053).
const (
	OpenListLimit    = 100
	AdminListLimit   = 100
	HistoryListLimit = 50
	SweepBatchLimit  = 100
)

// Repository is order-service's persistence port, implemented by
// internal/repository against PostgreSQL and by fakes in tests.
//
// AI-generated (edited by James Teo).
// Every Order it returns, and every one it passes to a callback, has
// WithdrawnCouriers filled from the errand's history as committed, plus the
// courier of a withdrawal the same call writes (D-060).
type Repository interface {
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (Order, error)
	// History returns an errand's history, oldest first.
	History(ctx context.Context, id uuid.UUID) ([]HistoryEntry, error)
	// FindByRequestKey returns ErrNotFound when the requester has no errand
	// with that X-Request-Id.
	FindByRequestKey(ctx context.Context, requesterID uuid.UUID, key string) (Order, error)

	// ListOpen returns OPEN errands whose deadline is after now, newest first.
	ListOpen(ctx context.Context, now time.Time, limit int) ([]Order, error)
	// ListByRequester returns the requester's errands in the given states,
	// most recently changed first.
	ListByRequester(ctx context.Context, requesterID uuid.UUID, states []State, limit int) ([]Order, error)
	// ListByCourier returns errands the courier currently holds in the given
	// states, most recently changed first.
	ListByCourier(ctx context.Context, courierID uuid.UUID, states []State, limit int) ([]Order, error)
	// ListAssignmentHistory returns the courier's ended assignments, most
	// recent first: COMPLETED and CANCELLED errands they held, and every
	// withdrawal they made (RELEASED).
	ListAssignmentHistory(ctx context.Context, courierID uuid.UUID, limit int) ([]Assignment, error)
	// ListActive returns every errand that is not final, oldest state change first.
	ListActive(ctx context.Context, limit int) ([]Order, error)
	// CountActiveBySupplier counts errands that are not final and name the supplier.
	CountActiveBySupplier(ctx context.Context, supplierID uuid.UUID) (int, error)
	// AI-generated (edited by James Teo).
	// CountOpenByRequester counts the requester's errands that are OPEN with a
	// deadline after now (D-058).
	CountOpenByRequester(ctx context.Context, requesterID uuid.UUID, now time.Time) (int, error)
	// DueForExpiry and DueForAutoConfirm return ids whose deadline is at or
	// before now, oldest deadline first. They take no locks: each id is
	// re-checked under the write (F3.14.1).
	DueForExpiry(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
	DueForAutoConfirm(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)

	// QueueEvent inserts an outbox row in its own transaction (D-047's queued refund).
	QueueEvent(ctx context.Context, e OutboxEvent) error
	// Create saves a new errand and its CREATE history row, and deletes the
	// queued refund guardEventID, in one transaction. It returns ErrGuardGone
	// (and writes nothing) unless exactly one unpublished guard row was deleted,
	// and ErrDuplicateRequestKey (and writes nothing) when the requester already
	// used o.RequestKey.
	//
	// AI-generated (edited by James Teo).
	// It also returns ErrTooManyOpen (and writes nothing) when saving o would
	// give the requester more than MaxOpenErrands errands OPEN with a deadline
	// after o.CreatedAt. A per-requester lock in the same transaction makes
	// concurrent creates count one after another, so none can pass the limit
	// (D-058). A duplicate request key is reported before the limit.
	Create(ctx context.Context, o Order, h HistoryEntry, guardEventID uuid.UUID) error
	// Transition applies t (see Transition) and returns the row after it, or
	// ErrNoMatch when the row no longer matches.
	Transition(ctx context.Context, t Transition) (Order, error)
	// Accept applies c (see AcceptCommand) and returns the row after it. It
	// returns ErrCourierAtCapacity when the courier already holds MaxActive
	// errands in ACCEPTED or PICKED_UP, and ErrNoMatch when the errand is no
	// longer OPEN, has expired, belongs to the courier, or its version moved.
	Accept(ctx context.Context, c AcceptCommand) (Order, error)
	// Amend locks the row (SELECT ... FOR UPDATE), calls fn with it while the
	// lock is held, then writes fn's result and history row in the same
	// transaction, with fn's event when it has one (D-057). fn's error
	// rolls back and is returned unchanged. It returns ErrNotFound for an
	// unknown id, and the unchanged row when fn returns a nil result.
	Amend(ctx context.Context, id uuid.UUID, fn func(current Order) (*AmendResult, error)) (Order, error)
	// QueueLocked locks the row, calls fn with it as committed, and inserts
	// the outbox event fn returns in the same transaction, so the event reflects
	// the row no later write has yet changed (D-057). It returns ErrNotFound for
	// an unknown id and fn's error unchanged.
	//
	// AI-generated (edited by James Teo).
	// A nil event from fn inserts nothing.
	QueueLocked(ctx context.Context, id uuid.UUID, fn func(current Order) (*OutboxEvent, error)) error
}

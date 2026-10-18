// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Posting an errand: validation (F3.1.2-F3.1.4), supplier check (D-052),
//   duplicate-submission replay and reserve-before-save with a queued
//   refund (D-047).
// Author review: James Teo — reviewed and verified.

package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// CreateInput is a new errand as the requester submitted it (F3.1.1).
type CreateInput struct {
	SupplierID       uuid.UUID
	ItemDescription  string
	DeliveryLocation string
	DeliveryNote     string
	Reward           int
	// RequestKey is the request's X-Request-Id; empty when it had none (D-047).
	RequestKey    string
	CorrelationID string
}

// Create posts an errand in OPEN (F3.1.7). replayed is true when the requester
// already created an errand with in.RequestKey: that errand is returned and no
// credits are touched (D-047).
func (s *Service) Create(ctx context.Context, c Caller, in CreateInput) (o Order, replayed bool, err error) {
	if c.Role != RoleStudent {
		return Order{}, false, forbidden("only students can post errands")
	}
	in.ItemDescription = strings.TrimSpace(in.ItemDescription)
	in.DeliveryNote = strings.TrimSpace(in.DeliveryNote)
	if err := validateCreate(in); err != nil {
		return Order{}, false, err
	}

	if in.RequestKey != "" {
		// D-047: a twin with the same key waits here until this create has
		// returned, then finds its errand and replays it without touching
		// credit. The lock is held until repo.Create below has returned.
		unlock, err := s.createKeys.lock(ctx, c.UserID.String()+"\x00"+in.RequestKey)
		if err != nil {
			return Order{}, false, unavailable("could not post the errand; please try again", err)
		}
		defer unlock()
		existing, err := s.repo.FindByRequestKey(ctx, c.UserID, in.RequestKey)
		if err == nil {
			return existing, true, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Order{}, false, unavailable("could not post the errand; please try again", err)
		}
	}

	snapshot, err := s.supplierSnapshot(ctx, in.SupplierID)
	if err != nil {
		return Order{}, false, err
	}

	now := s.clock.Now()
	o = Order{
		ID:               uuid.New(),
		RequesterID:      c.UserID,
		Supplier:         snapshot,
		ItemDescription:  in.ItemDescription,
		DeliveryLocation: in.DeliveryLocation,
		DeliveryNote:     in.DeliveryNote,
		Reward:           in.Reward,
		State:            StateOpen,
		Version:          1,
		RequestKey:       in.RequestKey,
		CreatedAt:        now,
		UpdatedAt:        now,
		StateChangedAt:   now,
		ExpiresAt:        timePtr(now.Add(s.cfg.ExpiryPeriod)),
	}

	// D-047: queue the refund first, so every way this request can end leaves
	// exactly one of {errand saved, refund sent}. Saving the errand deletes it.
	guard := newEvent(uuid.New(), RoutingFailed, o, nil, now, in.CorrelationID)
	guard.AvailableAt = now.Add(s.cfg.RefundDelay)
	if err := s.repo.QueueEvent(ctx, *guard); err != nil {
		return Order{}, false, unavailable("could not post the errand; please try again", err)
	}

	err = s.credit.Reserve(ctx, Reservation{
		ErrandID:      o.ID,
		RequesterID:   o.RequesterID,
		Amount:        o.Reward,
		CorrelationID: in.CorrelationID,
	})
	if errors.Is(err, ErrInsufficientCredits) {
		// The queued refund still fires; credit acks it as a no-op (pending Seth, D-047).
		return Order{}, false, insufficientCredits(err)
	}
	if errors.Is(err, ErrCreditRejected) {
		// A definite refusal (e.g. 401 while credit-service does not yet read
		// a forwarded token, D-056) is a misconfiguration retrying cannot fix.
		s.log.ErrorContext(ctx, "credit-service refused a reservation; queued refund will release any hold",
			"errand_id", o.ID, "request_id", in.CorrelationID, "err", err)
		return Order{}, false, unavailable("credits are unavailable right now; please try again", err)
	}
	if err != nil {
		s.log.WarnContext(ctx, "credit reservation failed or outcome unknown; queued refund will release any hold",
			"errand_id", o.ID, "request_id", in.CorrelationID, "err", err)
		return Order{}, false, unavailable("credits are unavailable right now; please try again", err)
	}

	history := HistoryEntry{
		OrderID:       o.ID,
		Action:        ActionCreate,
		ToState:       StateOpen,
		ActorID:       uuidPtr(c.UserID),
		ActorKind:     ActorRequester,
		OccurredAt:    now,
		CorrelationID: in.CorrelationID,
	}
	err = s.repo.Create(ctx, o, history, guard.EventID)
	switch {
	case err == nil:
		return o, false, nil
	case errors.Is(err, ErrDuplicateRequestKey):
		// A twin with the same X-Request-Id won on another replica, where the
		// lock above does not reach; this attempt's reservation is released
		// by its queued refund.
		existing, ferr := s.repo.FindByRequestKey(ctx, c.UserID, in.RequestKey)
		if ferr != nil {
			return Order{}, false, unavailable("could not post the errand; please try again", ferr)
		}
		return existing, true, nil
	case errors.Is(err, ErrGuardGone):
		return Order{}, false, unavailable("posting took too long and was undone; your credits will be returned", err)
	default:
		return Order{}, false, unavailable("could not post the errand; please try again", err)
	}
}

func validateCreate(in CreateInput) error {
	if in.SupplierID == uuid.Nil {
		return invalid("choose a supplier")
	}
	if err := validateItem(in.ItemDescription); err != nil {
		return err
	}
	if err := validateLocation(in.DeliveryLocation); err != nil {
		return err
	}
	if err := validateNote(in.DeliveryNote); err != nil {
		return err
	}
	return validateReward(in.Reward)
}

func validateItem(item string) error {
	if item == "" {
		return invalid("describe the item to pick up")
	}
	// NUL is the one character a PostgreSQL text column cannot store.
	if strings.ContainsRune(item, 0) {
		return invalid("the item description cannot contain a NUL character")
	}
	if charLen(item) > MaxItemDescription {
		return invalid("the item description can be at most 280 characters")
	}
	return nil
}

func validateLocation(loc string) error {
	if !isDeliveryLocation(loc) {
		return invalid("choose a delivery location from the campus list")
	}
	return nil
}

func validateNote(note string) error {
	if strings.ContainsRune(note, 0) {
		return invalid("the note cannot contain a NUL character")
	}
	if charLen(note) > MaxDeliveryNote {
		return invalid("the note can be at most 140 characters")
	}
	return nil
}

// validateReward enforces F3.1.2: a positive whole number of credits.
func validateReward(reward int) error {
	if reward < 1 {
		return invalid("the reward must be at least 1 credit")
	}
	if reward > MaxReward {
		return invalid("the reward is too large")
	}
	return nil
}

// keyLocks serializes the creates that share a key within this process, so a
// twin waits for the first to finish instead of reserving credit beside it
// (D-047). With more than one replica, the unique request key, the replay
// after ErrDuplicateRequestKey and D-047's queued refund still cover a twin
// that lands on another one.
type keyLocks struct {
	mu      sync.Mutex
	entries map[string]*keyLock
}

// keyLock is one key's lock. An entry is removed once nobody holds or waits
// on it, so the map holds only the keys in use.
type keyLock struct {
	held chan struct{} // 1-buffered: full while a create holds the key
	refs int           // creates holding or waiting on the key; guarded by keyLocks.mu
}

func newKeyLocks() *keyLocks {
	return &keyLocks{entries: map[string]*keyLock{}}
}

// lock waits until key is free, then holds it. It gives up with ctx's error if
// ctx is done first. On success the caller must call unlock exactly once.
func (l *keyLocks) lock(ctx context.Context, key string) (unlock func(), err error) {
	l.mu.Lock()
	e, ok := l.entries[key]
	if !ok {
		e = &keyLock{held: make(chan struct{}, 1)}
		l.entries[key] = e
	}
	e.refs++
	l.mu.Unlock()

	select {
	case e.held <- struct{}{}:
		return func() {
			<-e.held
			l.release(key, e)
		}, nil
	case <-ctx.Done():
		l.release(key, e)
		return nil, fmt.Errorf("wait for an earlier create with the same request key: %w", ctx.Err())
	}
}

// release drops one reference to key's entry, and removes the entry once
// nobody holds or waits on it.
func (l *keyLocks) release(key string, e *keyLock) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.refs--
	if e.refs == 0 {
		delete(l.entries, key)
	}
}

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Accept (D-046), the courier, requester and admin transitions, and the
//   sweeper's expire and auto-confirm (D-045, D-046, D-048 to D-050).
// Author review: James Teo — reviewed and verified.

package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Accept assigns c as the errand's courier (F3.3). version is the errand
// version c was shown; if it moved, the errand changed and c must refresh.
//
// AI-generated (edited by James Teo).
// The accept is judged as of when it arrived: now is read once, before the
// errand is loaded, and the conditional write compares the deadline with it.
// An accept that arrived before T but then waited on the errand's row lock,
// held by an amend across its credit call (D-057), still goes through after T,
// and is recorded at its arrival time.
func (s *Service) Accept(ctx context.Context, c Caller, id uuid.UUID, version int, correlationID string) (Order, error) {
	now := s.clock.Now()
	by := userActor(c)
	o, err := s.load(ctx, id)
	if err != nil {
		return Order{}, err
	}
	if err := check(o, ActionAccept, by, now); err != nil {
		return Order{}, err
	}
	if o.Version != version {
		return Order{}, errChanged()
	}
	// AI-generated (edited by PENDING): D-093, a completed errand must be rated
	// before accepting another.
	if err := s.requireRated(ctx, c); err != nil {
		return Order{}, err
	}
	updated, err := s.repo.Accept(ctx, AcceptCommand{
		OrderID:         id,
		CourierID:       c.UserID,
		ExpectedVersion: version,
		Now:             now,
		MaxActive:       s.cfg.MaxActive,
		History: HistoryEntry{
			OrderID:       id,
			Action:        ActionAccept,
			FromState:     StateOpen,
			ToState:       StateAccepted,
			ActorID:       uuidPtr(c.UserID),
			ActorKind:     ActorCourier,
			CourierID:     uuidPtr(c.UserID),
			OccurredAt:    now,
			CorrelationID: correlationID,
		},
	})
	switch {
	case err == nil:
		return updated, nil
	case errors.Is(err, ErrCourierAtCapacity):
		return Order{}, conflict(fmt.Sprintf(
			"you already have %d errands in progress; deliver one before accepting another", s.cfg.MaxActive))
	case errors.Is(err, ErrNoMatch):
		return Order{}, s.explainNoMatch(ctx, id, ActionAccept, by, version)
	default:
		return Order{}, fmt.Errorf("accept errand %s: %w", id, err)
	}
}

// Withdraw gives an ACCEPTED errand back; it returns to OPEN with a fresh
// deadline (F3.12, D-049).
func (s *Service) Withdraw(ctx context.Context, c Caller, id uuid.UUID, correlationID string) (Order, error) {
	return s.userTransition(ctx, c, id, func(Order) Action { return ActionWithdraw }, correlationID)
}

// PickUp marks the item collected (F3.8).
func (s *Service) PickUp(ctx context.Context, c Caller, id uuid.UUID, correlationID string) (Order, error) {
	return s.userTransition(ctx, c, id, func(Order) Action { return ActionPickUp }, correlationID)
}

// Deliver marks the errand delivered and starts the confirmation window C (F3.9).
func (s *Service) Deliver(ctx context.Context, c Caller, id uuid.UUID, correlationID string) (Order, error) {
	return s.userTransition(ctx, c, id, func(Order) Action { return ActionDeliver }, correlationID)
}

// Confirm completes a DELIVERED errand for its requester (F3.9.2, F3.9.3).
func (s *Service) Confirm(ctx context.Context, c Caller, id uuid.UUID, correlationID string) (Order, error) {
	return s.userTransition(ctx, c, id, func(Order) Action { return ActionConfirm }, correlationID)
}

// Cancel cancels an errand: the requester's cancel while OPEN (F3.11), or an
// ADMIN's cancel of any errand that is not final (D-045). Which one applies is
// decided by the caller's relationship to the errand, not by a flag.
func (s *Service) Cancel(ctx context.Context, c Caller, id uuid.UUID, correlationID string) (Order, error) {
	return s.userTransition(ctx, c, id, func(o Order) Action {
		if RelationshipOf(o, c) == RelAdmin {
			return ActionAdminCancel
		}
		return ActionCancel
	}, correlationID)
}

// Complete is an ADMIN completing a PICKED_UP or DELIVERED errand (D-045).
func (s *Service) Complete(ctx context.Context, c Caller, id uuid.UUID, correlationID string) (Order, error) {
	return s.userTransition(ctx, c, id, func(Order) Action { return ActionAdminComplete }, correlationID)
}

// Expire moves an OPEN errand past its deadline to EXPIRED (F3.10.1, F3.14.1).
// applied is false when the errand was not due or no longer OPEN.
func (s *Service) Expire(ctx context.Context, id uuid.UUID, correlationID string) (applied bool, err error) {
	return s.systemTransition(ctx, id, ActionExpire, correlationID)
}

// AutoConfirm completes a DELIVERED errand once C has passed (F3.9.4).
// applied is false when the errand was not due or no longer DELIVERED.
func (s *Service) AutoConfirm(ctx context.Context, id uuid.UUID, correlationID string) (applied bool, err error) {
	return s.systemTransition(ctx, id, ActionAutoConfirm, correlationID)
}

func (s *Service) userTransition(ctx context.Context, c Caller, id uuid.UUID, pick func(Order) Action, correlationID string) (Order, error) {
	now := s.clock.Now()
	by := userActor(c)
	o, err := s.load(ctx, id)
	if err != nil {
		return Order{}, err
	}
	a := pick(o)
	if err := check(o, a, by, now); err != nil {
		return Order{}, err
	}
	updated, err := s.repo.Transition(ctx, s.buildTransition(o, a, by, now, correlationID))
	switch {
	case err == nil:
		return updated, nil
	case errors.Is(err, ErrNoMatch):
		return Order{}, s.explainNoMatch(ctx, id, a, by, -1)
	default:
		return Order{}, fmt.Errorf("%s errand %s: %w", a, id, err)
	}
}

func (s *Service) systemTransition(ctx context.Context, id uuid.UUID, a Action, correlationID string) (bool, error) {
	now := s.clock.Now()
	o, err := s.repo.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load errand %s: %w", id, err)
	}
	if err := check(o, a, systemActor(), now); err != nil {
		// Not due, or someone else moved it first: nothing to do.
		return false, nil
	}
	_, err = s.repo.Transition(ctx, s.buildTransition(o, a, systemActor(), now, correlationID))
	if errors.Is(err, ErrNoMatch) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s errand %s: %w", a, id, err)
	}
	return true, nil
}

// buildTransition turns a checked action into D-046's conditional write: the
// expected state, people and deadline, the row after the action, its history
// row and, for the four settlement outcomes, its event (D-048).
//
// AI-generated (edited by James Teo).
// A user action is judged as of when it arrived, as Accept is: now is the
// time read before the errand was loaded, and the deadline the write checks,
// the state change and the history row all carry it, however long the write
// then waits on the row lock.
func (s *Service) buildTransition(o Order, a Action, by actor, now time.Time, correlationID string) Transition {
	r, _ := ruleFor(a)
	version := o.Version
	t := Transition{
		OrderID:       o.ID,
		FromState:     o.State,
		ExpectVersion: &version,
		ToState:       r.to,
		CourierID:     o.CourierID,
		Now:           now,
	}
	switch by.relationship(o) {
	case RelRequester:
		t.ExpectRequesterID = uuidPtr(o.RequesterID)
	case RelCourier:
		t.ExpectCourierID = o.CourierID
	}
	if o.State == StateOpen && a != ActionExpire {
		t.OpenBefore = timePtr(now)
	}
	switch a {
	case ActionWithdraw:
		t.CourierID = nil
		t.ExpiresAt = timePtr(now.Add(s.cfg.ExpiryPeriod)) // D-049
	case ActionDeliver:
		t.ConfirmBy = timePtr(now.Add(s.cfg.ConfirmWindow))
	case ActionExpire:
		t.ExpiryDueBy = timePtr(now)
	case ActionAutoConfirm:
		t.ConfirmDueBy = timePtr(now)
	}

	h := HistoryEntry{
		OrderID:       o.ID,
		Action:        a,
		FromState:     o.State,
		ToState:       r.to,
		CourierID:     o.CourierID,
		OccurredAt:    now,
		CorrelationID: correlationID,
	}
	if by.caller != nil {
		h.ActorID = uuidPtr(by.caller.UserID)
	}
	h.ActorKind = actorKind(by.relationship(o))
	t.History = h

	switch r.to {
	case StateCompleted:
		t.Event = newEvent(uuid.New(), RoutingCompleted, o, o.CourierID, now, correlationID)
	case StateCancelled:
		t.Event = newEvent(uuid.New(), RoutingCancelled, o, nil, now, correlationID)
	case StateExpired:
		t.Event = newEvent(uuid.New(), RoutingExpired, o, nil, now, correlationID)
	}
	return t
}

func actorKind(rel Relationship) ActorKind {
	switch rel {
	case RelRequester:
		return ActorRequester
	case RelCourier:
		return ActorCourier
	case RelAdmin:
		return ActorAdmin
	default:
		return ActorSystem
	}
}

// explainNoMatch re-reads an errand whose conditional write matched no row and
// says why (D-046). version is the version the caller sent, or -1.
func (s *Service) explainNoMatch(ctx context.Context, id uuid.UUID, a Action, by actor, version int) error {
	o, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	// Only another courier on the errand means another accept won. One
	// cancelled or expired while OPEN has no courier; check below names its state.
	if a == ActionAccept && o.CourierID != nil && by.relationship(o) == RelNone {
		return conflict("someone else accepted this errand first")
	}
	if err := check(o, a, by, s.clock.Now()); err != nil {
		return err
	}
	if version >= 0 && o.Version != version {
		return errChanged()
	}
	return conflict("this errand changed while you were acting on it; refresh and try again")
}

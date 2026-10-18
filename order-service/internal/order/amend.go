// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Amending an OPEN errand (F3.7): validation, supplier check on every
//   amend (D-052), credit adjust under the row lock and the
//   errand.reward_set that brings credit's hold back in line (D-057).
// Author review: James Teo — reviewed and verified.

package order

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AmendInput holds the fields a requester changes; nil means "leave as is".
// Version is the errand version the requester last saw (D-046).
type AmendInput struct {
	Version          int
	SupplierID       *uuid.UUID
	ItemDescription  *string
	DeliveryLocation *string
	DeliveryNote     *string
	Reward           *int
	CorrelationID    string
}

// queueTimeout bounds queueing D-057's errand.reward_set after a failed amend,
// which runs after the request may already have been cancelled.
const queueTimeout = 5 * time.Second

// adjustOutcome tracks the credit PATCH inside an amend, so the caller knows
// whether credit may now differ from the committed reward.
type adjustOutcome int

const (
	adjustNotSent adjustOutcome = iota
	adjustApplied
	adjustRefused
	adjustUnknown
)

// Amend changes an OPEN errand for its requester (F3.7). The deadline does not
// move (F3.7.6), and the changed fields are recorded (F3.7.7).
func (s *Service) Amend(ctx context.Context, c Caller, id uuid.UUID, in AmendInput) (Order, error) {
	if err := normalizeAmend(&in); err != nil {
		return Order{}, err
	}
	now := s.clock.Now()
	o, err := s.load(ctx, id)
	if err != nil {
		return Order{}, err
	}
	if err := check(o, ActionAmend, userActor(c), now); err != nil {
		return Order{}, err
	}
	if o.Version != in.Version {
		return Order{}, errChanged()
	}

	// D-052: the supplier is checked on every amend, before the row is locked
	// and before credit is touched.
	supplierID := o.Supplier.ID
	if in.SupplierID != nil {
		supplierID = *in.SupplierID
	}
	snapshot, err := s.supplierSnapshot(ctx, supplierID)
	if err != nil {
		return Order{}, err
	}

	outcome := adjustNotSent
	updated, err := s.repo.Amend(ctx, id, func(cur Order) (*AmendResult, error) {
		// The deadline is judged again as of now, under the lock: the supplier
		// lookup and the lock wait may have run past it (D-049, F3.10.2).
		lockedNow := s.clock.Now()
		if err := check(cur, ActionAmend, userActor(c), lockedNow); err != nil {
			return nil, err
		}
		if cur.Version != in.Version {
			return nil, errChanged()
		}
		next, changes := applyAmend(cur, in, snapshot)
		if len(changes) == 0 && next.Supplier == cur.Supplier {
			return nil, nil
		}
		if next.Reward != cur.Reward {
			err := s.credit.Adjust(ctx, cur.ID, cur.RequesterID, next.Reward, in.CorrelationID)
			switch {
			case err == nil:
				outcome = adjustApplied
			case errors.Is(err, ErrInsufficientCredits):
				outcome = adjustRefused
				return nil, insufficientCredits(err)
			case errors.Is(err, ErrCreditRejected):
				outcome = adjustRefused
				s.log.ErrorContext(ctx, "credit-service refused a reward adjustment",
					"errand_id", cur.ID, "request_id", in.CorrelationID, "err", err)
				return nil, unavailable("credits are unavailable right now; please try again", err)
			default:
				outcome = adjustUnknown
				return nil, unavailable("credits are unavailable right now; please try again", err)
			}
		}
		next.Version = cur.Version + 1
		next.UpdatedAt = lockedNow
		res := &AmendResult{
			Order: next,
			History: HistoryEntry{
				OrderID:       cur.ID,
				Action:        ActionAmend,
				FromState:     StateOpen,
				ToState:       StateOpen,
				ActorID:       uuidPtr(c.UserID),
				ActorKind:     ActorRequester,
				Changes:       changes,
				OccurredAt:    lockedNow,
				CorrelationID: in.CorrelationID,
			},
		}
		if next.Reward != cur.Reward {
			// D-057: credit converges on this event even if the PATCH's answer
			// above was the only thing it saw.
			res.Event = rewardSetEvent(next, lockedNow, in.CorrelationID)
		}
		return res, nil
	})
	if err == nil {
		return updated, nil
	}
	// D-057: the PATCH was sent but the change was not saved (its outcome is
	// unknown, or it applied and the write then failed), so credit may hold a
	// reward the errand does not show. Queue a reward_set for the errand as
	// saved; credit applies it once the relay reaches it.
	if outcome == adjustUnknown || outcome == adjustApplied {
		s.queueRewardSet(ctx, id, in.CorrelationID)
	}
	if errors.Is(err, ErrNotFound) {
		return Order{}, notFound()
	}
	if KindOf(err) != 0 {
		return Order{}, err
	}
	return Order{}, unavailable("could not amend the errand; please try again", err)
}

// queueRewardSet queues D-057's errand.reward_set for the errand as saved,
// under its row lock, so the event carries the reward and version no later
// amend has yet replaced. It runs on a context the request's cancellation does
// not reach, bounded by queueTimeout, because the client may already have
// gone. A failure is logged: credit's hold may then differ from the errand's
// reward until the requester's next reward change.
//
// AI-generated (edited by James Teo).
// Nothing is queued once the errand is final: its settlement event, already
// written, settles by its own amount (D-059), and a reward_set behind it could
// only put back a hold that nothing would release.
func (s *Service) queueRewardSet(ctx context.Context, id uuid.UUID, correlationID string) {
	qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), queueTimeout)
	defer cancel()
	// AI-generated (edited by James Teo).
	err := s.repo.QueueLocked(qctx, id, func(cur Order) (*OutboxEvent, error) {
		if cur.State.Final() {
			return nil, nil
		}
		return rewardSetEvent(cur, s.clock.Now(), correlationID), nil
	})
	if err != nil {
		s.log.ErrorContext(ctx, "could not queue errand.reward_set after a failed amend; credit's hold may differ from the errand's reward",
			"errand_id", id, "request_id", correlationID, "err", err)
	}
}

// normalizeAmend trims and validates the fields present, with the same rules
// as create (F3.7.2).
func normalizeAmend(in *AmendInput) error {
	if in.SupplierID == nil && in.ItemDescription == nil && in.DeliveryLocation == nil &&
		in.DeliveryNote == nil && in.Reward == nil {
		return invalid("send at least one field to change")
	}
	if in.SupplierID != nil && *in.SupplierID == uuid.Nil {
		return invalid("choose a supplier")
	}
	if in.ItemDescription != nil {
		v := strings.TrimSpace(*in.ItemDescription)
		in.ItemDescription = &v
		if err := validateItem(v); err != nil {
			return err
		}
	}
	if in.DeliveryLocation != nil {
		if err := validateLocation(*in.DeliveryLocation); err != nil {
			return err
		}
	}
	if in.DeliveryNote != nil {
		v := strings.TrimSpace(*in.DeliveryNote)
		in.DeliveryNote = &v
		if err := validateNote(v); err != nil {
			return err
		}
	}
	if in.Reward != nil {
		if err := validateReward(*in.Reward); err != nil {
			return err
		}
	}
	return nil
}

// applyAmend returns cur with in applied and the supplier copy refreshed, plus
// the fields the requester changed. A refreshed copy of the same supplier is
// not a change the requester made, so it is not listed.
func applyAmend(cur Order, in AmendInput, snapshot SupplierSnapshot) (Order, []FieldChange) {
	next := cur
	var changes []FieldChange
	if snapshot.ID != cur.Supplier.ID {
		changes = append(changes, FieldChange{Field: FieldSupplier, From: cur.Supplier.Name, To: snapshot.Name})
	}
	next.Supplier = snapshot
	if in.ItemDescription != nil && *in.ItemDescription != cur.ItemDescription {
		changes = append(changes, FieldChange{Field: FieldItemDescription, From: cur.ItemDescription, To: *in.ItemDescription})
		next.ItemDescription = *in.ItemDescription
	}
	if in.DeliveryLocation != nil && *in.DeliveryLocation != cur.DeliveryLocation {
		changes = append(changes, FieldChange{Field: FieldDeliveryLocation, From: cur.DeliveryLocation, To: *in.DeliveryLocation})
		next.DeliveryLocation = *in.DeliveryLocation
	}
	if in.DeliveryNote != nil && *in.DeliveryNote != cur.DeliveryNote {
		changes = append(changes, FieldChange{Field: FieldDeliveryNote, From: cur.DeliveryNote, To: *in.DeliveryNote})
		next.DeliveryNote = *in.DeliveryNote
	}
	if in.Reward != nil && *in.Reward != cur.Reward {
		changes = append(changes, FieldChange{Field: FieldReward, From: strconv.Itoa(cur.Reward), To: strconv.Itoa(*in.Reward)})
		next.Reward = *in.Reward
	}
	return next, changes
}

func errChanged() error {
	return conflict("this errand has changed since you loaded it; refresh and try again")
}

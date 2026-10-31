// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Amending an OPEN errand (F3.7): validation, supplier check on every
//   amend (D-052), the raise's credit call under the row lock, the
//   errand.reward_set saved with every reward change, and the release call
//   after a saved decrease (D-085).
// Author review: James Teo — reviewed and verified.

package order

import (
	"context"
	"errors"
	"strconv"
	"strings"

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

	// The reward_set a decrease commits, for the release call after it.
	var release *OutboxEvent
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
		// D-085: only a raise waits for credit-service, which must hold the
		// extra before it is saved. A decrease is released after the commit,
		// below.
		if next.Reward > cur.Reward {
			err := s.credit.Adjust(ctx, cur.ID, cur.RequesterID, next.Reward, cur.Version, in.CorrelationID)
			switch {
			case err == nil:
			case errors.Is(err, ErrInsufficientCredits):
				return nil, insufficientCredits(err)
			case errors.Is(err, ErrCreditRejected):
				s.log.ErrorContext(ctx, "credit-service refused a reward raise",
					"errand_id", cur.ID, "request_id", in.CorrelationID, "err", err)
				return nil, unavailable("credits are unavailable right now; please try again", err)
			default:
				// Nothing is saved or queued: if credit-service held the extra, it
				// stays held until the next saved reward change or the errand ends.
				s.log.WarnContext(ctx, "credit-service's answer to a reward raise is unknown; any extra it held stays held",
					"errand_id", cur.ID, "request_id", in.CorrelationID, "err", err)
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
			// D-085: credit-service lowers its hold to this reward if it holds
			// more, so this releases a decrease, and any extra a raise that was
			// never saved left behind.
			res.Event = rewardSetEvent(next, lockedNow, in.CorrelationID)
			if next.Reward < cur.Reward {
				release = res.Event
			}
		}
		return res, nil
	})
	if err == nil {
		if release != nil {
			s.releaseNow(ctx, updated, release)
		}
		return updated, nil
	}
	if errors.Is(err, ErrNotFound) {
		return Order{}, notFound()
	}
	if KindOf(err) != 0 {
		return Order{}, err
	}
	return Order{}, unavailable("could not amend the errand; please try again", err)
}

// releaseNow asks credit-service to release a saved decrease at once, so the
// requester's credits are back before the reply, rather than when the relay's
// event reaches credit-service (D-085). The event, already committed, is the
// guarantee: it carries the same operationID, so credit-service applies one
// of the two. A failure here only delays the release, so it is logged and
// the amend still succeeds.
func (s *Service) releaseNow(ctx context.Context, o Order, event *OutboxEvent) {
	err := s.credit.Release(ctx, o.ID, o.RequesterID, o.Reward, o.Version-1, event.EventID, event.CorrelationID)
	if err != nil {
		s.log.WarnContext(ctx, "credit-service did not release a lowered reward at once; its errand.reward_set will",
			"errand_id", o.ID, "request_id", event.CorrelationID, "event_id", event.EventID, "err", err)
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

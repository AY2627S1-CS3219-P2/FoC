// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Maps request bodies to the order service's inputs and orders to the
//   contract's JSON shapes (api/openapi.yaml): who sees the requester and the
//   note, deadlines by state, allowed actions and history (D-045, D-052, D-053).
// Author review: James Teo — reviewed and verified.

package httpapi

import (
	"errors"
	"time"

	"foc/order-service/internal/gen"
	"foc/order-service/internal/order"
)

// amendBody is PATCH /{errandId}'s body: the generated AmendErrandRequest with
// its version shadowed by a pointer (encoding/json fills the shallower field),
// so a body without a version is a 400 instead of a stale-version 409.
type amendBody struct {
	gen.AmendErrandRequest
	Version   *int    `json:"version"`
	Scheduled *string `json:"scheduled"`
}

// input maps the body to the service's input. ok is false when the body has
// no version.
func (b amendBody) input(correlationID string) (in order.AmendInput, ok bool) {
	if b.Version == nil {
		return order.AmendInput{}, false
	}
	in = order.AmendInput{
		Version:          *b.Version,
		SupplierID:       b.SupplierId,
		ItemDescription:  b.ItemDescription,
		DeliveryLocation: b.DeliveryLocation,
		DeliveryNote:     b.DeliveryNote,
		Reward:           b.Reward,
		CorrelationID:    correlationID,
	}
	if b.Scheduled != nil {
		change := &order.ScheduledChange{}
		if *b.Scheduled != "" {
			at, err := time.Parse(time.RFC3339Nano, *b.Scheduled)
			if err != nil {
				return order.AmendInput{}, false
			}
			_, offset := at.Zone()
			if offset != 0 {
				return order.AmendInput{}, false
			}
			change.At = &at
		}
		in.Scheduled = change
	}
	return in, true
}

// versionBody is POST /{errandId}/accept's body, with version a pointer for the
// same reason as amendBody's.
type versionBody struct {
	Version *int `json:"version"`
}

// createInput maps POST /'s body to the service's input. requestKey is the
// client's own X-Request-Id, if any (D-047).
func createInput(b gen.CreateErrandRequest, requestKey, correlationID string) (order.CreateInput, error) {
	in := order.CreateInput{
		SupplierID:       b.SupplierId,
		ItemDescription:  b.ItemDescription,
		DeliveryLocation: b.DeliveryLocation,
		Reward:           b.Reward,
		RequestKey:       requestKey,
		CorrelationID:    correlationID,
	}
	if b.DeliveryNote != nil {
		in.DeliveryNote = *b.DeliveryNote
	}
	// AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — parse the optional
	// UTC input at the HTTP boundary; the domain checks its scheduling window.
	if b.Scheduled != nil && *b.Scheduled != "" {
		scheduledAt, err := time.Parse(time.RFC3339Nano, *b.Scheduled)
		if err != nil {
			return order.CreateInput{}, errors.New("scheduled must be an RFC 3339 UTC timestamp")
		}
		_, offset := scheduledAt.Zone()
		if offset != 0 {
			return order.CreateInput{}, errors.New("scheduled must be an RFC 3339 UTC timestamp")
		}
		in.ScheduledAt = &scheduledAt
	}
	return in, nil
}

// toErrand shows o to c at now. The requester and the note are only for a
// party to the errand or an ADMIN; each deadline only in the state it runs in
// (D-050); actions are exactly what c may do now (D-053).
func toErrand(o order.Order, c order.Caller, now time.Time) gen.Errand {
	e := gen.Errand{
		Id:               o.ID,
		State:            gen.ErrandState(o.State),
		Version:          o.Version,
		Supplier:         toSupplier(o.Supplier),
		ItemDescription:  o.ItemDescription,
		DeliveryLocation: o.DeliveryLocation,
		Reward:           o.Reward,
		// AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — normal errands
		// serialize their scheduled value as the contract's empty string.
		Scheduled:      "",
		CreatedAt:      o.CreatedAt.UTC(),
		UpdatedAt:      o.UpdatedAt.UTC(),
		StateChangedAt: o.StateChangedAt.UTC(),
		Actions:        toActions(order.AllowedActions(o, c, now)),
	}
	if o.ScheduledAt != nil {
		e.Scheduled = o.ScheduledAt.UTC().Format(time.RFC3339Nano)
	}
	if order.CanSeePrivate(o, c) {
		e.RequesterId = ptr(o.RequesterID)
		e.DeliveryNote = ptr(o.DeliveryNote)
	}
	if o.CourierID != nil {
		e.CourierId = ptr(*o.CourierID)
	}
	if o.State == order.StateOpen && o.ExpiresAt != nil {
		e.ExpiresAt = ptr(o.ExpiresAt.UTC())
	}
	if o.State == order.StateDelivered && o.ConfirmBy != nil {
		e.ConfirmBy = ptr(o.ConfirmBy.UTC())
	}
	return e
}

func toErrands(orders []order.Order, c order.Caller, now time.Time) []gen.Errand {
	out := make([]gen.Errand, 0, len(orders))
	for _, o := range orders {
		out = append(out, toErrand(o, c, now))
	}
	return out
}

// toDetail is GET /{errandId}'s body, also sent after every action.
func toDetail(o order.Order, history []order.HistoryEntry, c order.Caller, now time.Time) gen.ErrandDetail {
	// History arrives already filtered for c by order.Service.Get.
	return gen.ErrandDetail{
		Errand:     toErrand(o, c, now),
		History:    toHistory(history),
		ServerTime: now.UTC(),
	}
}

// toHistory maps history entries without their actor, courier or correlation
// id: who did what is the action itself.
func toHistory(history []order.HistoryEntry) []gen.HistoryEntry {
	out := make([]gen.HistoryEntry, 0, len(history))
	for _, h := range history {
		e := gen.HistoryEntry{
			Action:     gen.HistoryAction(h.Action),
			ToState:    gen.ErrandState(h.ToState),
			OccurredAt: h.OccurredAt.UTC(),
		}
		if h.FromState != "" {
			e.FromState = ptr(gen.ErrandState(h.FromState))
		}
		// The contract promises changes on every AMEND entry (F3.7.7). It is
		// empty when none of them is the viewer's to see, or when the amend
		// only refreshed the supplier copy.
		if h.Action == order.ActionAmend || len(h.Changes) > 0 {
			changes := make([]gen.FieldChange, 0, len(h.Changes))
			for _, fc := range h.Changes {
				changes = append(changes, gen.FieldChange{Field: gen.FieldChangeField(fc.Field), From: fc.From, To: fc.To})
			}
			e.Changes = &changes
		}
		out = append(out, e)
	}
	return out
}

func toActions(actions []order.APIAction) []gen.ErrandAction {
	out := make([]gen.ErrandAction, 0, len(actions))
	for _, a := range actions {
		out = append(out, gen.ErrandAction(a))
	}
	return out
}

func toAssignments(assignments []order.Assignment) []gen.AssignmentRecord {
	out := make([]gen.AssignmentRecord, 0, len(assignments))
	for _, a := range assignments {
		out = append(out, gen.AssignmentRecord{
			ErrandId:         a.OrderID,
			Outcome:          gen.AssignmentRecordOutcome(a.Outcome),
			EndedAt:          a.EndedAt.UTC(),
			Supplier:         toSupplier(a.Supplier),
			ItemDescription:  a.ItemDescription,
			DeliveryLocation: a.DeliveryLocation,
			Reward:           a.Reward,
		})
	}
	return out
}

func toSupplier(s order.SupplierSnapshot) gen.SupplierSnapshot {
	return gen.SupplierSnapshot{Id: s.ID, Name: s.Name, Location: s.Location}
}

func ptr[T any](v T) *T { return &v }

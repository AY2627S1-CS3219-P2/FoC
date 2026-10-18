// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: In-memory fakes of every order port (repository, credit, supplier,
//   clock) that keep the same semantics as the real adapters, so rules are
//   tested with no database (order-service/AGENTS.md, root AGENTS.md §7).
// Author review: James Teo — reviewed and verified.

// Package ordertest holds in-memory fakes of the order package's ports, for
// tests in any order-service package.
package ordertest

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/order"
)

// Clock is a settable order.Clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a Clock stopped at t.
func NewClock(t time.Time) *Clock { return &Clock{now: t.UTC()} }

// Now returns the clock's current time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Suppliers is an order.SupplierDirectory backed by a map.
type Suppliers struct {
	mu   sync.Mutex
	byID map[uuid.UUID]order.Supplier
	err  error
}

// NewSuppliers returns a directory holding the given suppliers.
func NewSuppliers(list ...order.Supplier) *Suppliers {
	s := &Suppliers{byID: map[uuid.UUID]order.Supplier{}}
	for _, sup := range list {
		s.byID[sup.ID] = sup
	}
	return s
}

// Put adds or replaces a supplier.
func (s *Suppliers) Put(sup order.Supplier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[sup.ID] = sup
}

// FailWith makes every lookup fail with err, as if supplier-service were
// down; nil restores normal answers.
func (s *Suppliers) FailWith(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// Supplier implements order.SupplierDirectory.
func (s *Suppliers) Supplier(_ context.Context, id uuid.UUID) (order.Supplier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return order.Supplier{}, s.err
	}
	sup, ok := s.byID[id]
	if !ok {
		return order.Supplier{}, order.ErrSupplierNotFound
	}
	return sup, nil
}

// AdjustCall records one Credit.Adjust call.
type AdjustCall struct {
	ErrandID      uuid.UUID
	RequesterID   uuid.UUID
	NewReward     int
	CorrelationID string
}

// Credit is an order.CreditClient that keeps the amount held per errand, the
// way credit-service's reservation would.
type Credit struct {
	mu       sync.Mutex
	holds    map[uuid.UUID]int
	reserves []order.Reservation
	adjusts  []AdjustCall
	// ReserveHook, when set, decides a Reserve: apply says whether credit
	// actually holds the amount, err is what the caller sees. A lost response
	// is apply=true with a non-nil err.
	ReserveHook func(r order.Reservation) (apply bool, err error)
	// AdjustHook does the same for Adjust.
	AdjustHook func(c AdjustCall) (apply bool, err error)
}

// NewCredit returns a Credit that accepts every call.
func NewCredit() *Credit { return &Credit{holds: map[uuid.UUID]int{}} }

// Reserve implements order.CreditClient.
func (c *Credit) Reserve(_ context.Context, r order.Reservation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reserves = append(c.reserves, r)
	apply, err := true, error(nil)
	if c.ReserveHook != nil {
		apply, err = c.ReserveHook(r)
	}
	if apply {
		c.holds[r.ErrandID] = r.Amount
	}
	return err
}

// Adjust implements order.CreditClient.
func (c *Credit) Adjust(_ context.Context, errandID, requesterID uuid.UUID, newReward int, correlationID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	call := AdjustCall{ErrandID: errandID, RequesterID: requesterID, NewReward: newReward, CorrelationID: correlationID}
	c.adjusts = append(c.adjusts, call)
	apply, err := true, error(nil)
	if c.AdjustHook != nil {
		apply, err = c.AdjustHook(call)
	}
	if apply {
		c.holds[errandID] = newReward
	}
	return err
}

// Hold returns the amount held for an errand and whether any is.
func (c *Credit) Hold(errandID uuid.UUID) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.holds[errandID]
	return v, ok
}

// Reserves returns every Reserve call so far.
func (c *Credit) Reserves() []order.Reservation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.reserves)
}

// Adjusts returns every Adjust call so far.
func (c *Credit) Adjusts() []AdjustCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.adjusts)
}

type outboxRow struct {
	event     order.OutboxEvent
	published bool
}

// Repository is an in-memory order.Repository. One mutex serializes every
// method, which is how the conditional writes stay atomic here.
type Repository struct {
	mu      sync.Mutex
	orders  map[uuid.UUID]order.Order
	history []order.HistoryEntry
	outbox  map[uuid.UUID]*outboxRow
	nextID  int64
	fail    map[string]error
	// AfterAmendFn, when set, is returned by Amend after fn has run and
	// before anything is written: a commit that fails after credit was called.
	AfterAmendFn error
}

// NewRepository returns an empty Repository.
func NewRepository() *Repository {
	return &Repository{orders: map[uuid.UUID]order.Order{}, outbox: map[uuid.UUID]*outboxRow{}, fail: map[string]error{}}
}

// FailNext makes the next call to the named method (e.g. "Create") return err.
func (r *Repository) FailNext(method string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail[method] = err
}

func (r *Repository) takeFail(method string) error {
	err := r.fail[method]
	delete(r.fail, method)
	return err
}

// Put stores o as is, for tests that start from a given state.
func (r *Repository) Put(o order.Order) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orders[o.ID] = o
}

// Events returns every outbox event with whether it was published, oldest first.
func (r *Repository) Events() []order.OutboxEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]order.OutboxEvent, 0, len(r.outbox))
	for _, row := range r.outbox {
		out = append(out, row.event)
	}
	slices.SortFunc(out, func(a, b order.OutboxEvent) int { return a.AvailableAt.Compare(b.AvailableAt) })
	return out
}

// EventsFor returns the events whose payload names the errand.
func (r *Repository) EventsFor(errandID uuid.UUID) []order.OutboxEvent {
	var out []order.OutboxEvent
	for _, e := range r.Events() {
		if e.Payload.ErrandID == errandID {
			out = append(out, e)
		}
	}
	return out
}

// MarkPublished marks an outbox event published, as the relay would.
func (r *Repository) MarkPublished(eventID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row, ok := r.outbox[eventID]; ok {
		row.published = true
	}
}

// Get implements order.Repository.
func (r *Repository) Get(_ context.Context, id uuid.UUID) (order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("Get"); err != nil {
		return order.Order{}, err
	}
	o, ok := r.orders[id]
	if !ok {
		return order.Order{}, order.ErrNotFound
	}
	return o, nil
}

// History implements order.Repository.
func (r *Repository) History(_ context.Context, id uuid.UUID) ([]order.HistoryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []order.HistoryEntry
	for _, h := range r.history {
		if h.OrderID == id {
			out = append(out, h)
		}
	}
	return out, nil
}

// FindByRequestKey implements order.Repository.
func (r *Repository) FindByRequestKey(_ context.Context, requesterID uuid.UUID, key string) (order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.orders {
		if o.RequesterID == requesterID && o.RequestKey != "" && o.RequestKey == key {
			return o, nil
		}
	}
	return order.Order{}, order.ErrNotFound
}

func (r *Repository) filter(keep func(order.Order) bool) []order.Order {
	var out []order.Order
	for _, o := range r.orders {
		if keep(o) {
			out = append(out, o)
		}
	}
	return out
}

func limited[T any](s []T, limit int) []T {
	if limit > 0 && len(s) > limit {
		return s[:limit]
	}
	return s
}

// ListOpen implements order.Repository.
func (r *Repository) ListOpen(_ context.Context, now time.Time, limit int) ([]order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.filter(func(o order.Order) bool {
		return o.State == order.StateOpen && o.ExpiresAt != nil && o.ExpiresAt.After(now)
	})
	slices.SortFunc(out, func(a, b order.Order) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return limited(out, limit), nil
}

// ListByRequester implements order.Repository.
func (r *Repository) ListByRequester(_ context.Context, requesterID uuid.UUID, states []order.State, limit int) ([]order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.filter(func(o order.Order) bool { return o.RequesterID == requesterID && slices.Contains(states, o.State) })
	slices.SortFunc(out, func(a, b order.Order) int { return b.StateChangedAt.Compare(a.StateChangedAt) })
	return limited(out, limit), nil
}

// ListByCourier implements order.Repository.
func (r *Repository) ListByCourier(_ context.Context, courierID uuid.UUID, states []order.State, limit int) ([]order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.filter(func(o order.Order) bool {
		return o.CourierID != nil && *o.CourierID == courierID && slices.Contains(states, o.State)
	})
	slices.SortFunc(out, func(a, b order.Order) int { return b.StateChangedAt.Compare(a.StateChangedAt) })
	return limited(out, limit), nil
}

// ListAssignmentHistory implements order.Repository.
func (r *Repository) ListAssignmentHistory(_ context.Context, courierID uuid.UUID, limit int) ([]order.Assignment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []order.Assignment
	for _, h := range r.history {
		if h.CourierID == nil || *h.CourierID != courierID {
			continue
		}
		var outcome order.AssignmentOutcome
		switch {
		case h.Action == order.ActionWithdraw:
			outcome = order.OutcomeReleased
		case h.ToState == order.StateCompleted:
			outcome = order.OutcomeCompleted
		case h.ToState == order.StateCancelled:
			outcome = order.OutcomeCancelled
		default:
			continue
		}
		o := r.orders[h.OrderID]
		out = append(out, order.Assignment{
			HistoryID:        h.ID,
			OrderID:          h.OrderID,
			Outcome:          outcome,
			EndedAt:          h.OccurredAt,
			Supplier:         o.Supplier,
			ItemDescription:  o.ItemDescription,
			DeliveryLocation: o.DeliveryLocation,
			Reward:           o.Reward,
		})
	}
	slices.SortFunc(out, func(a, b order.Assignment) int {
		return cmp.Or(b.EndedAt.Compare(a.EndedAt), cmp.Compare(b.HistoryID, a.HistoryID))
	})
	return limited(out, limit), nil
}

// ListActive implements order.Repository.
func (r *Repository) ListActive(_ context.Context, limit int) ([]order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.filter(func(o order.Order) bool { return !o.State.Final() })
	slices.SortFunc(out, func(a, b order.Order) int { return a.StateChangedAt.Compare(b.StateChangedAt) })
	return limited(out, limit), nil
}

// CountActiveBySupplier implements order.Repository.
func (r *Repository) CountActiveBySupplier(_ context.Context, supplierID uuid.UUID) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.filter(func(o order.Order) bool { return o.Supplier.ID == supplierID && !o.State.Final() })), nil
}

// DueForExpiry implements order.Repository.
func (r *Repository) DueForExpiry(_ context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	due := r.filter(func(o order.Order) bool {
		return o.State == order.StateOpen && o.ExpiresAt != nil && !o.ExpiresAt.After(now)
	})
	slices.SortFunc(due, func(a, b order.Order) int { return a.ExpiresAt.Compare(*b.ExpiresAt) })
	return ids(limited(due, limit)), nil
}

// DueForAutoConfirm implements order.Repository.
func (r *Repository) DueForAutoConfirm(_ context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	due := r.filter(func(o order.Order) bool {
		return o.State == order.StateDelivered && o.ConfirmBy != nil && !o.ConfirmBy.After(now)
	})
	slices.SortFunc(due, func(a, b order.Order) int { return a.ConfirmBy.Compare(*b.ConfirmBy) })
	return ids(limited(due, limit)), nil
}

func ids(os []order.Order) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(os))
	for _, o := range os {
		out = append(out, o.ID)
	}
	return out
}

// QueueEvent implements order.Repository.
func (r *Repository) QueueEvent(_ context.Context, e order.OutboxEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("QueueEvent"); err != nil {
		return err
	}
	r.outbox[e.EventID] = &outboxRow{event: e}
	return nil
}

// Create implements order.Repository.
func (r *Repository) Create(_ context.Context, o order.Order, h order.HistoryEntry, guardEventID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("Create"); err != nil {
		return err
	}
	guard, ok := r.outbox[guardEventID]
	if !ok || guard.published {
		return order.ErrGuardGone
	}
	if o.RequestKey != "" {
		for _, existing := range r.orders {
			if existing.RequesterID == o.RequesterID && existing.RequestKey == o.RequestKey {
				return order.ErrDuplicateRequestKey
			}
		}
	}
	delete(r.outbox, guardEventID)
	r.orders[o.ID] = o
	r.appendHistory(h)
	return nil
}

func (r *Repository) appendHistory(h order.HistoryEntry) {
	r.nextID++
	h.ID = r.nextID
	r.history = append(r.history, h)
}

// Transition implements order.Repository.
func (r *Repository) Transition(_ context.Context, t order.Transition) (order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("Transition"); err != nil {
		return order.Order{}, err
	}
	o, ok := r.orders[t.OrderID]
	if !ok || !matches(o, t) {
		return order.Order{}, order.ErrNoMatch
	}
	o.State = t.ToState
	o.CourierID = t.CourierID
	o.ExpiresAt = t.ExpiresAt
	o.ConfirmBy = t.ConfirmBy
	o.StateChangedAt = t.Now
	o.UpdatedAt = t.Now
	o.Version++
	r.orders[o.ID] = o
	r.appendHistory(t.History)
	if t.Event != nil {
		r.outbox[t.Event.EventID] = &outboxRow{event: *t.Event}
	}
	return o, nil
}

func matches(o order.Order, t order.Transition) bool {
	switch {
	case o.State != t.FromState:
		return false
	case t.ExpectRequesterID != nil && o.RequesterID != *t.ExpectRequesterID:
		return false
	case t.ExpectCourierID != nil && (o.CourierID == nil || *o.CourierID != *t.ExpectCourierID):
		return false
	case t.ExpectVersion != nil && o.Version != *t.ExpectVersion:
		return false
	case t.OpenBefore != nil && (o.ExpiresAt == nil || !o.ExpiresAt.After(*t.OpenBefore)):
		return false
	case t.ExpiryDueBy != nil && (o.ExpiresAt == nil || o.ExpiresAt.After(*t.ExpiryDueBy)):
		return false
	case t.ConfirmDueBy != nil && (o.ConfirmBy == nil || o.ConfirmBy.After(*t.ConfirmDueBy)):
		return false
	}
	return true
}

// Accept implements order.Repository.
func (r *Repository) Accept(_ context.Context, c order.AcceptCommand) (order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("Accept"); err != nil {
		return order.Order{}, err
	}
	active := r.filter(func(o order.Order) bool {
		return o.CourierID != nil && *o.CourierID == c.CourierID &&
			(o.State == order.StateAccepted || o.State == order.StatePickedUp)
	})
	if len(active) >= c.MaxActive {
		return order.Order{}, order.ErrCourierAtCapacity
	}
	o, ok := r.orders[c.OrderID]
	if !ok || o.State != order.StateOpen || o.ExpiresAt == nil || !o.ExpiresAt.After(c.Now) ||
		o.RequesterID == c.CourierID || o.Version != c.ExpectedVersion {
		return order.Order{}, order.ErrNoMatch
	}
	courier := c.CourierID
	o.State = order.StateAccepted
	o.CourierID = &courier
	o.ExpiresAt = nil
	o.StateChangedAt = c.Now
	o.UpdatedAt = c.Now
	o.Version++
	r.orders[o.ID] = o
	r.appendHistory(c.History)
	return o, nil
}

// Amend implements order.Repository. fn runs with the repository locked, the
// way the real adapter holds the row lock.
func (r *Repository) Amend(_ context.Context, id uuid.UUID, fn func(current order.Order) (*order.AmendResult, error)) (order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.orders[id]
	if !ok {
		return order.Order{}, order.ErrNotFound
	}
	res, err := fn(cur)
	if err != nil {
		return order.Order{}, err
	}
	if res == nil {
		return cur, nil
	}
	if r.AfterAmendFn != nil {
		err := r.AfterAmendFn
		r.AfterAmendFn = nil
		return order.Order{}, err
	}
	r.orders[id] = res.Order
	r.appendHistory(res.History)
	if res.Event != nil {
		r.outbox[res.Event.EventID] = &outboxRow{event: *res.Event}
	}
	return res.Order, nil
}

// QueueLocked implements order.Repository. fn runs with the repository locked,
// the way the real adapter holds the row lock.
func (r *Repository) QueueLocked(_ context.Context, id uuid.UUID, fn func(current order.Order) (order.OutboxEvent, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.takeFail("QueueLocked"); err != nil {
		return err
	}
	cur, ok := r.orders[id]
	if !ok {
		return order.ErrNotFound
	}
	e, err := fn(cur)
	if err != nil {
		return err
	}
	r.outbox[e.EventID] = &outboxRow{event: e}
	return nil
}

// ErrInjected is a ready-made error for FailNext and the hooks.
var ErrInjected = errors.New("injected failure")

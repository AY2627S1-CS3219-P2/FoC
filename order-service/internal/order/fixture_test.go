// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Shared fixture for the order domain's unit tests: the Service on the
//   ordertest fakes, with seams to slip a competing write in before a
//   conditional write and record what each write carries (D-046), and to see
//   the row lock (D-057).
// Author review: James Teo — reviewed and verified.

package order_test

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
)

// The lifecycle values tests run with unless they say otherwise: D-049's T, K
// and C, and D-047's refund delay.
const (
	expiryPeriod  = 2 * time.Hour
	maxActive     = 2
	confirmWindow = 24 * time.Hour
	refundDelay   = 60 * time.Second
)

// seededLocation is how the fixture supplier's place reads on an errand (D-052).
const seededLocation = "Yusof Ishak House, level 1 — Opp UHC bus stop"

// changedMsg is what a caller acting on a version they did not see is told (D-046).
const changedMsg = "this errand has changed since you loaded it; refresh and try again"

func defaultConfig() order.Config {
	return order.Config{
		ExpiryPeriod:  expiryPeriod,
		MaxActive:     maxActive,
		ConfirmWindow: confirmWindow,
		RefundDelay:   refundDelay,
	}
}

// startTime is where every fixture's clock starts.
func startTime() time.Time { return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC) }

func ptr[T any](v T) *T { return &v }

func student() order.Caller { return order.Caller{UserID: uuid.New(), Role: order.RoleStudent} }

func allStates() []order.State { return slices.Concat(order.ActiveStates(), order.FinalStates()) }

// party is how a caller relates to the errand under test (D-046).
type party int

const (
	asRequester party = iota
	asCourier
	asStranger
	asAdmin
)

func parties() []party { return []party{asRequester, asCourier, asStranger, asAdmin} }

func (p party) String() string {
	switch p {
	case asRequester:
		return "requester"
	case asCourier:
		return "courier"
	case asStranger:
		return "another student"
	default:
		return "admin"
	}
}

// fixture is one Service on fresh fakes. Every test, and every subtest that
// writes, builds its own, so no test can see another's state.
type fixture struct {
	t         *testing.T
	ctx       context.Context
	cfg       order.Config
	clock     *ordertest.Clock
	repo      *hookedRepo
	credit    *ordertest.Credit
	suppliers *countingSuppliers
	svc       *order.Service

	requester order.Caller
	courier   order.Caller // assigned on every seeded errand past OPEN
	stranger  order.Caller // a student with no part in any seeded errand
	admin     order.Caller

	supplier       order.Supplier // the seeded and posted errands' supplier
	otherSupplier  order.Supplier // also taking errands
	closedSupplier order.Supplier // not taking errands
}

func newFixture(t *testing.T) *fixture { return newFixtureWith(t, defaultConfig()) }

func newFixtureWith(t *testing.T, cfg order.Config) *fixture {
	t.Helper()
	f := &fixture{
		t:         t,
		ctx:       t.Context(),
		cfg:       cfg,
		clock:     ordertest.NewClock(startTime()),
		repo:      &hookedRepo{Repository: ordertest.NewRepository()},
		credit:    ordertest.NewCredit(),
		requester: student(),
		courier:   student(),
		stranger:  student(),
		admin:     order.Caller{UserID: uuid.New(), Role: order.RoleAdmin},
		supplier: order.Supplier{
			ID: uuid.New(), Name: "Kaya Toast Stall", Building: "Yusof Ishak House",
			Floor: "1", LocationDescription: "Opp UHC bus stop", Available: true,
		},
		otherSupplier: order.Supplier{
			ID: uuid.New(), Name: "Campus Coffee Cart", Building: "Central Library",
			Floor: "1", LocationDescription: "Beside the entrance", Available: true,
		},
		closedSupplier: order.Supplier{
			ID: uuid.New(), Name: "Closed Kiosk", Building: "COM3", Available: false,
		},
	}
	f.suppliers = &countingSuppliers{
		Suppliers: ordertest.NewSuppliers(f.supplier, f.otherSupplier, f.closedSupplier),
	}
	f.svc = order.NewService(f.repo, f.repo, f.credit, f.suppliers, f.clock, cfg, slog.New(slog.DiscardHandler))
	return f
}

// as returns the fixture's caller for p.
func (f *fixture) as(p party) order.Caller {
	switch p {
	case asRequester:
		return f.requester
	case asCourier:
		return f.courier
	case asStranger:
		return f.stranger
	default:
		return f.admin
	}
}

// seed stores an errand in state s, shaped the way the lifecycle leaves one:
// f.courier assigned from ACCEPTED on (a CANCELLED seed was cancelled by an
// ADMIN after acceptance), a deadline only while OPEN and a confirmation
// window only while DELIVERED (D-049, D-050). Neither deadline is due yet:
// OPEN expires in an hour, DELIVERED confirms itself in twelve.
func (f *fixture) seed(s order.State) order.Order {
	f.t.Helper()
	now := f.clock.Now()
	posted := now.Add(-30 * time.Minute)
	o := order.Order{
		ID:               uuid.New(),
		RequesterID:      f.requester.UserID,
		Supplier:         order.SupplierSnapshot{ID: f.supplier.ID, Name: f.supplier.Name, Location: seededLocation},
		ItemDescription:  "Two kaya toast sets",
		DeliveryLocation: "COM3 Lobby",
		DeliveryNote:     "Blue shirt, by the lift",
		Reward:           5,
		State:            s,
		Version:          3,
		CreatedAt:        posted,
		UpdatedAt:        posted,
		StateChangedAt:   posted,
	}
	switch s {
	case order.StateOpen:
		o.ExpiresAt = ptr(now.Add(time.Hour))
	case order.StateDelivered:
		o.CourierID = ptr(f.courier.UserID)
		o.ConfirmBy = ptr(now.Add(12 * time.Hour))
	case order.StateAccepted, order.StatePickedUp, order.StateCompleted, order.StateCancelled:
		o.CourierID = ptr(f.courier.UserID)
	}
	f.repo.Put(o)
	// AI-generated (edited by PENDING): a seeded COMPLETED errand stands for one
	// finished earlier, which under D-093 both sides have rated, or neither
	// could post or accept again. seedUnrated leaves it unrated.
	if s == order.StateCompleted {
		f.rateAsBoth(o)
	}
	return o
}

// AI-generated (edited by PENDING): D-093's fixtures.
// seedUnrated is seed for a COMPLETED errand that neither side has rated.
func (f *fixture) seedUnrated() order.Order {
	f.t.Helper()
	o := f.seed(order.StateOpen)
	o.State, o.ExpiresAt, o.CourierID = order.StateCompleted, nil, ptr(f.courier.UserID)
	f.repo.Put(o)
	return o
}

// rateAsBoth stores the requester's and the courier's rating of o, as if each
// had rated it.
func (f *fixture) rateAsBoth(o order.Order) {
	f.t.Helper()
	f.rateAsCourier(o)
	require.NoError(f.t, f.repo.SaveRating(f.ctx, order.Rating{
		OrderID: o.ID, RaterID: o.RequesterID, RateeID: *o.CourierID, RaterRole: order.RaterRequester, Stars: 5, RatedAt: f.clock.Now(),
	}))
}

// rateAsCourier stores o's courier's rating of its requester.
func (f *fixture) rateAsCourier(o order.Order) {
	f.t.Helper()
	require.NoError(f.t, f.repo.SaveRating(f.ctx, order.Rating{
		OrderID: o.ID, RaterID: *o.CourierID, RateeID: o.RequesterID, RaterRole: order.RaterCourier, Stars: 5, RatedAt: f.clock.Now(),
	}))
}

// createInput is a valid errand from f.supplier (F3.1.1), with no X-Request-Id.
func (f *fixture) createInput() order.CreateInput {
	return order.CreateInput{
		SupplierID:       f.supplier.ID,
		ItemDescription:  "Two kaya toast sets",
		DeliveryLocation: "COM3 Lobby",
		DeliveryNote:     "Blue shirt, by the lift",
		Reward:           5,
		CorrelationID:    "corr-create",
	}
}

// post creates createInput's errand through the service, as f.requester.
func (f *fixture) post() order.Order {
	f.t.Helper()
	o, replayed, err := f.svc.Create(f.ctx, f.requester, f.createInput())
	require.NoError(f.t, err)
	require.False(f.t, replayed)
	return o
}

// stored is everything the repository holds about one errand.
type stored struct {
	order   order.Order
	history []order.HistoryEntry
	events  []order.OutboxEvent
	writes  int // how many writes the service had attempted by then
}

func (f *fixture) stored(id uuid.UUID) stored {
	f.t.Helper()
	o, err := f.repo.Get(f.ctx, id)
	require.NoError(f.t, err)
	return stored{order: o, history: f.history(id), events: f.repo.EventsFor(id), writes: len(f.repo.attemptedWrites())}
}

func (f *fixture) history(id uuid.UUID) []order.HistoryEntry {
	f.t.Helper()
	h, err := f.repo.History(f.ctx, id)
	require.NoError(f.t, err)
	return h
}

// requireUnchanged fails unless the repository still holds exactly before:
// a rejected action writes no row, no history and no event (D-046).
func (f *fixture) requireUnchanged(before stored) {
	f.t.Helper()
	after := f.stored(before.order.ID)
	require.Equal(f.t, before.order, after.order, "the errand row changed")
	require.Equal(f.t, before.history, after.history, "a history row was written")
	require.ElementsMatch(f.t, before.events, after.events, "an outbox event was written")
}

// requireNoWrite fails if the service attempted any write: D-046 checks the
// rule in Go and refuses before it writes.
func (f *fixture) requireNoWrite() {
	f.t.Helper()
	require.Empty(f.t, f.repo.attemptedWrites(), "the check in Go refuses before any write")
}

// requireNoWriteSince is requireNoWrite for the writes after before was read.
func (f *fixture) requireNoWriteSince(before stored) {
	f.t.Helper()
	require.Empty(f.t, f.repo.attemptedWrites()[before.writes:], "the check in Go refuses before any write")
}

// errandsOf lists every errand c has requested, in any state.
func (f *fixture) errandsOf(c order.Caller) []order.Order {
	f.t.Helper()
	out, err := f.repo.ListByRequester(f.ctx, c.UserID, allStates(), 0)
	require.NoError(f.t, err)
	return out
}

// requireQueuedRefund fails unless the only event in the outbox is D-047's
// queued refund for r: errand.failed for r's errand, requester and amount,
// held back until delay after the create began.
func (f *fixture) requireQueuedRefund(r order.Reservation, postedAt time.Time, delay time.Duration) {
	f.t.Helper()
	events := f.repo.Events()
	require.Len(f.t, events, 1, "exactly one queued refund")
	e := events[0]
	assert.Equal(f.t, postedAt.Add(delay), e.AvailableAt, "the refund waits out the refund delay")
	unsaved := order.Order{ID: r.ErrandID, RequesterID: r.RequesterID, Reward: r.Amount}
	assertEvent(f.t, e, order.RoutingFailed, unsaved, nil, postedAt, r.CorrelationID)
}

// assertEvent checks one outbox event against D-051's payload: credit-service's
// fields plus eventType and correlationId, naming errand o.
func assertEvent(t *testing.T, e order.OutboxEvent, key string, o order.Order, courier *uuid.UUID, at time.Time, corr string) {
	t.Helper()
	assert.Equal(t, key, e.RoutingKey)
	assert.NotEqual(t, uuid.Nil, e.EventID)
	p := e.Payload
	assert.Equal(t, e.EventID, p.EventID, "eventId is the outbox event's id")
	assert.Equal(t, key, p.EventType, "eventType is the routing key")
	assert.Equal(t, o.ID, p.ErrandID, "errandId")
	assert.Equal(t, o.RequesterID, p.RequesterID, "requesterId")
	assert.Equal(t, o.Reward, p.Amount, "amount is the errand's reward")
	assert.Equal(t, at, p.OccurredAt, "occurredAt")
	assert.Equal(t, courier, p.CourierID, "courierId")
	assert.Equal(t, corr, p.CorrelationID, "correlationId")
	assert.Equal(t, corr, e.CorrelationID, "the outbox row's correlation id")
}

// requireKind fails unless err is a domain error of kind want, the value the
// transport maps to a status code.
func requireKind(t *testing.T, err error, want order.Kind) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, kindName(want), kindName(order.KindOf(err)), "error: %v", err)
}

func kindName(k order.Kind) string {
	switch k {
	case order.KindValidation:
		return "validation (400)"
	case order.KindForbidden:
		return "forbidden (403)"
	case order.KindNotFound:
		return "not found (404)"
	case order.KindConflict:
		return "conflict (409)"
	case order.KindInsufficientCredits:
		return "insufficient credits (422)"
	case order.KindUnavailable:
		return "unavailable (503)"
	case 0:
		return "not a domain error (500)"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

func ids(list []order.Order) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(list))
	for _, o := range list {
		out = append(out, o.ID)
	}
	return out
}

func rewards(calls []ordertest.AdjustCall) []int {
	out := make([]int, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.NewReward)
	}
	return out
}

// hookedRepo is the ordertest fake with four seams. A hook set with
// beforeNext runs once, just before the named write reaches the fake, so a
// test can let a competing request finish between a service's read and its
// conditional write (D-046). Every write the service attempts is recorded,
// with each Transition and AcceptCommand in full, so a test can see that a
// refused action wrote nothing and read the conditions a write carries.
// locked is true while Amend or QueueLocked holds an errand's row, so a test
// can see credit called, and a reward_set built, under the row lock (D-057).
// queueCtxDone records, for each QueueLocked, whether its context was
// already done, so a test can see the queueing outlive a cancelled request. And the key lookup and
// the reads the fake cannot fail can be made to fail. The limit each capped
// list was asked for is recorded too, so a test can hold it to the contract
// (D-053).
type hookedRepo struct {
	*ordertest.Repository

	mu          sync.Mutex
	before      map[string]func()
	findErr     error
	readErr     error
	writes      []string
	transitions []order.Transition
	accepts     []order.AcceptCommand
	limits      map[string]int

	locked       atomic.Bool
	queueCtxDone []bool
}

// recordLimit notes the limit the service passed to a list method.
func (r *hookedRepo) recordLimit(method string, limit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.limits == nil {
		r.limits = map[string]int{}
	}
	r.limits[method] = limit
}

// sentLimits returns, per list method called so far, the last limit it got.
func (r *hookedRepo) sentLimits() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.limits)
}

// sentTransitions returns every Transition the service has sent, oldest first.
func (r *hookedRepo) sentTransitions() []order.Transition {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.transitions)
}

// sentAccepts returns every AcceptCommand the service has sent, oldest first.
func (r *hookedRepo) sentAccepts() []order.AcceptCommand {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.accepts)
}

// beforeNext makes fn run once, just before the next call to method.
func (r *hookedRepo) beforeNext(method string, fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.before == nil {
		r.before = map[string]func(){}
	}
	r.before[method] = fn
}

// attemptedWrites names every write the service has attempted, oldest first.
func (r *hookedRepo) attemptedWrites() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.writes)
}

// AI-generated (edited by James Teo).
// attempt records a write to method, then runs its beforeNext hook, if any.
func (r *hookedRepo) attempt(method string) {
	r.mu.Lock()
	r.writes = append(r.writes, method)
	r.mu.Unlock()
	r.runBefore(method)
}

// AI-generated (edited by James Teo).
// runBefore runs, and clears, method's beforeNext hook, if any.
func (r *hookedRepo) runBefore(method string) {
	r.mu.Lock()
	fn := r.before[method]
	delete(r.before, method)
	r.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// failFindWith makes every FindByRequestKey fail with err, as if the database
// did not answer.
func (r *hookedRepo) failFindWith(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.findErr = err
}

// failReadsWith makes History, every list and the supplier count fail with
// err, as if the database did not answer.
func (r *hookedRepo) failReadsWith(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readErr = err
}

func (r *hookedRepo) readFailure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readErr
}

func (r *hookedRepo) History(ctx context.Context, id uuid.UUID) ([]order.HistoryEntry, error) {
	if err := r.readFailure(); err != nil {
		return nil, err
	}
	return r.Repository.History(ctx, id)
}

func (r *hookedRepo) ListOpen(ctx context.Context, now time.Time, limit int) ([]order.Order, error) {
	r.recordLimit("ListOpen", limit)
	if err := r.readFailure(); err != nil {
		return nil, err
	}
	return r.Repository.ListOpen(ctx, now, limit)
}

func (r *hookedRepo) ListByRequester(ctx context.Context, requesterID uuid.UUID, states []order.State, limit int) ([]order.Order, error) {
	r.recordLimit("ListByRequester", limit)
	if err := r.readFailure(); err != nil {
		return nil, err
	}
	return r.Repository.ListByRequester(ctx, requesterID, states, limit)
}

func (r *hookedRepo) ListByCourier(ctx context.Context, courierID uuid.UUID, states []order.State, limit int) ([]order.Order, error) {
	if err := r.readFailure(); err != nil {
		return nil, err
	}
	return r.Repository.ListByCourier(ctx, courierID, states, limit)
}

func (r *hookedRepo) ListAssignmentHistory(ctx context.Context, courierID uuid.UUID, limit int) ([]order.Assignment, error) {
	r.recordLimit("ListAssignmentHistory", limit)
	if err := r.readFailure(); err != nil {
		return nil, err
	}
	return r.Repository.ListAssignmentHistory(ctx, courierID, limit)
}

func (r *hookedRepo) ListActive(ctx context.Context, limit int) ([]order.Order, error) {
	r.recordLimit("ListActive", limit)
	if err := r.readFailure(); err != nil {
		return nil, err
	}
	return r.Repository.ListActive(ctx, limit)
}

func (r *hookedRepo) CountActiveBySupplier(ctx context.Context, supplierID uuid.UUID) (int, error) {
	if err := r.readFailure(); err != nil {
		return 0, err
	}
	return r.Repository.CountActiveBySupplier(ctx, supplierID)
}

// AI-generated (edited by James Teo).
// CountOpenByRequester runs its beforeNext hook, which is not a write, so a
// test can land a competing create between create's key lookup and its count.
func (r *hookedRepo) CountOpenByRequester(ctx context.Context, requesterID uuid.UUID, now time.Time) (int, error) {
	r.runBefore("CountOpenByRequester")
	return r.Repository.CountOpenByRequester(ctx, requesterID, now)
}

func (r *hookedRepo) FindByRequestKey(ctx context.Context, requesterID uuid.UUID, key string) (order.Order, error) {
	r.mu.Lock()
	err := r.findErr
	r.mu.Unlock()
	if err != nil {
		return order.Order{}, err
	}
	return r.Repository.FindByRequestKey(ctx, requesterID, key)
}

func (r *hookedRepo) Create(ctx context.Context, o order.Order, h order.HistoryEntry, guardEventID uuid.UUID) error {
	r.attempt("Create")
	return r.Repository.Create(ctx, o, h, guardEventID)
}

func (r *hookedRepo) Accept(ctx context.Context, c order.AcceptCommand) (order.Order, error) {
	r.mu.Lock()
	r.accepts = append(r.accepts, c)
	r.mu.Unlock()
	r.attempt("Accept")
	return r.Repository.Accept(ctx, c)
}

func (r *hookedRepo) Transition(ctx context.Context, tr order.Transition) (order.Order, error) {
	r.mu.Lock()
	r.transitions = append(r.transitions, tr)
	r.mu.Unlock()
	r.attempt("Transition")
	return r.Repository.Transition(ctx, tr)
}

func (r *hookedRepo) Amend(ctx context.Context, id uuid.UUID, fn func(order.Order) (*order.AmendResult, error)) (order.Order, error) {
	r.attempt("Amend")
	return r.Repository.Amend(ctx, id, func(cur order.Order) (*order.AmendResult, error) {
		r.locked.Store(true)
		defer r.locked.Store(false)
		return fn(cur)
	})
}

// AI-generated (edited by James Teo): fn returns a nil event for "queue nothing".
func (r *hookedRepo) QueueLocked(ctx context.Context, id uuid.UUID, fn func(order.Order) (*order.OutboxEvent, error)) error {
	r.mu.Lock()
	r.queueCtxDone = append(r.queueCtxDone, ctx.Err() != nil)
	r.mu.Unlock()
	r.attempt("QueueLocked")
	return r.Repository.QueueLocked(ctx, id, func(cur order.Order) (*order.OutboxEvent, error) {
		r.locked.Store(true)
		defer r.locked.Store(false)
		return fn(cur)
	})
}

// queueContexts returns queueCtxDone so far.
func (r *hookedRepo) queueContexts() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.queueCtxDone)
}

// countingSuppliers counts lookups in supplier-service, so a test can see a
// replayed create never made one (D-047).
type countingSuppliers struct {
	*ordertest.Suppliers
	calls atomic.Int64
}

func (s *countingSuppliers) Supplier(ctx context.Context, id uuid.UUID) (order.Supplier, error) {
	s.calls.Add(1)
	return s.Suppliers.Supplier(ctx, id)
}

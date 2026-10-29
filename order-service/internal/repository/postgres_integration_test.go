//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Integration-test harness for the PostgreSQL order.Repository (one
//   postgres:16-alpine container, fixtures, raw-SQL helpers) and the tests of
//   Migrate and every read and list (D-047, D-049, D-052, D-053, F3.5.3).
// Author review: James Teo — reviewed and verified.

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"foc/order-service/internal/order"
)

// TestPostgres runs every repository test against one PostgreSQL container.
// Each test empties the tables before it starts, so none relies on another's
// rows or on the order they run in.
func TestPostgres(t *testing.T) {
	e := newEnv(t)

	t.Run("Migrate", e.testMigrate)
	t.Run("Get", e.testGet)
	t.Run("FindByRequestKey", e.testFindByRequestKey)
	t.Run("History", e.testHistory)
	t.Run("ListOpen", e.testListOpen)
	t.Run("ListByRequester", e.testListByRequester)
	t.Run("ListByCourier", e.testListByCourier)
	t.Run("ListActive", e.testListActive)
	t.Run("ListAssignmentHistory", e.testListAssignmentHistory)
	t.Run("CountActiveBySupplier", e.testCountActiveBySupplier)
	// AI-generated (edited by James Teo).
	t.Run("CountOpenByRequester", e.testCountOpenByRequester)
	t.Run("WithdrawnCouriers", e.testWithdrawnCouriers)
	t.Run("DueForExpiry", e.testDueForExpiry)
	t.Run("DueForAutoConfirm", e.testDueForAutoConfirm)

	t.Run("QueueEvent", e.testQueueEvent)
	t.Run("Create", e.testCreate)
	t.Run("Transition", e.testTransition)
	t.Run("Accept", e.testAccept)
	t.Run("Amend", e.testAmend)
	t.Run("QueueLocked", e.testQueueLocked)
	t.Run("CheckConstraints", e.testCheckConstraints)
	t.Run("ServiceLifecycle", e.testServiceLifecycle)

	t.Run("ConcurrentAcceptsOneWinner", e.testConcurrentAcceptsOneWinner)
	t.Run("ConcurrentAcceptsRespectK", e.testConcurrentAcceptsRespectK)
	t.Run("AcceptLatency", e.testAcceptLatency)
	t.Run("AmendHoldsRowLock", e.testAmendHoldsRowLock)
	t.Run("QueueLockedHoldsRowLock", e.testQueueLockedHoldsRowLock)
	// AI-generated (edited by James Teo).
	t.Run("ConcurrentCreatesRespectTheOpenLimit", e.testConcurrentCreatesRespectTheOpenLimit)
	t.Run("AcceptIsJudgedAtArrival", e.testAcceptIsJudgedAtArrival)
	t.Run("NoRewardSetBehindASettlement", e.testNoRewardSetBehindASettlement)

	// AI-generated (edited by PENDING): ratings and Find errands' records (D-092, D-093).
	t.Run("Ratings", e.testRatings)
	t.Run("Activity", e.testActivity)
	t.Run("SupplierCopy", e.testSupplierCopy)
	t.Run("RankingSource", e.testRankingSource)
}

// env is one test run's database: its URL, a pool and the adapter over it.
type env struct {
	dbURL string
	pool  *pgxpool.Pool
	repo  *Postgres
}

// newEnv starts PostgreSQL, applies the migrations with Migrate and opens a
// pool large enough for every goroutine in the concurrency tests to hold a
// connection at once, so they contend on locks rather than on the pool.
func newEnv(t *testing.T) *env {
	t.Helper()
	dbURL := startPostgres(t)
	require.NoError(t, Migrate(dbURL, "../../migrations"), "apply migrations")

	cfg, err := pgxpool.ParseConfig(dbURL)
	require.NoError(t, err)
	cfg.MaxConns = 64
	// Scan timestamps in a zone that is not UTC, so a time the adapter forgot
	// to convert fails these tests whatever zone the machine running them is in.
	notUTC := time.FixedZone("UTC+8", 8*60*60)
	cfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name:  "timestamptz",
			OID:   pgtype.TimestamptzOID,
			Codec: &pgtype.TimestamptzCodec{ScanLocation: notUTC},
		})
		return nil
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return &env{dbURL: dbURL, pool: pool, repo: NewPostgres(pool)}
}

// startPostgres starts postgres:16-alpine for the test and returns its URL,
// as user-service's integration tests do.
func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "orders",
				"POSTGRES_PASSWORD": "postgres",
				"POSTGRES_USER":     "postgres",
			},
			// The server restarts once after initdb; the second line is the real one.
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err, "start PostgreSQL container")
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	return "postgres://postgres:postgres@" + net.JoinHostPort(host, port.Port()) + "/orders?sslmode=disable"
}

// reset empties every table.
func (e *env) reset(t *testing.T) {
	t.Helper()
	_, err := e.pool.Exec(context.Background(), `TRUNCATE errand_ratings, errand_views, errand_searches, order_history, outbox, orders RESTART IDENTITY`)
	require.NoError(t, err, "empty the tables")
}

// at is a fixed whole-second instant plus d. TIMESTAMPTZ keeps microseconds,
// so values built from it come back from the database exactly.
func at(d time.Duration) time.Time {
	return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC).Add(d)
}

func ptr[T any](v T) *T { return &v }

// The fixture errand's item (F3.1.1).
const latte = "Iced latte, less sugar"

// openErrand is an OPEN errand posted at created that expires T = 2h later
// (D-049).
func openErrand(requester uuid.UUID, created time.Time) order.Order {
	return order.Order{
		ID:          uuid.New(),
		RequesterID: requester,
		Supplier: order.SupplierSnapshot{
			ID:       uuid.New(),
			Name:     "Spinelli Coffee",
			Location: "COM3, level 1 — next to the lift",
		},
		ItemDescription:  latte,
		DeliveryLocation: "COM3 Lobby",
		DeliveryNote:     "Black backpack",
		Reward:           5,
		State:            order.StateOpen,
		Version:          1,
		CreatedAt:        created,
		UpdatedAt:        created,
		StateChangedAt:   created,
		ExpiresAt:        ptr(created.Add(2 * time.Hour)),
	}
}

// heldBy is o accepted by courier and moved to state (ACCEPTED, PICKED_UP or
// DELIVERED) at changed, with C = 24h to confirm once DELIVERED (D-049).
func heldBy(o order.Order, courier uuid.UUID, state order.State, changed time.Time) order.Order {
	o.State = state
	o.CourierID = ptr(courier)
	o.ExpiresAt = nil
	o.ConfirmBy = nil
	if state == order.StateDelivered {
		o.ConfirmBy = ptr(changed.Add(24 * time.Hour))
	}
	o.StateChangedAt = changed
	o.UpdatedAt = changed
	return o
}

// endedAs is o in a final state since changed. A courier it had stays on the row.
func endedAs(o order.Order, state order.State, changed time.Time) order.Order {
	o.State = state
	o.ExpiresAt = nil
	o.ConfirmBy = nil
	o.StateChangedAt = changed
	o.UpdatedAt = changed
	return o
}

// inState is o in any state since changed: held by courier while ACCEPTED,
// PICKED_UP or DELIVERED, and ended without one when final.
func inState(o order.Order, state order.State, courier uuid.UUID, changed time.Time) order.Order {
	switch state {
	case order.StateOpen:
		o.StateChangedAt = changed
		o.UpdatedAt = changed
		return o
	case order.StateAccepted, order.StatePickedUp, order.StateDelivered:
		return heldBy(o, courier, state, changed)
	default:
		return endedAs(o, state, changed)
	}
}

// allStates is every state, in D-045's order.
func allStates() []order.State {
	return append(order.ActiveStates(), order.FinalStates()...)
}

// insert writes errands with SQL of its own rather than the adapter's, so a
// column mix-up in the adapter's scan shows up when it reads them back.
func (e *env) insert(t *testing.T, orders ...order.Order) {
	t.Helper()
	for _, o := range orders {
		var key *string
		if o.RequestKey != "" {
			key = ptr(o.RequestKey)
		}
		_, err := e.pool.Exec(t.Context(), `
			INSERT INTO orders (id, requester_id, courier_id, supplier_id, supplier_name,
				supplier_location, item_description, delivery_location, delivery_note,
				reward, state, version, request_key, created_at, updated_at,
				state_changed_at, expires_at, confirm_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
			o.ID, o.RequesterID, o.CourierID, o.Supplier.ID, o.Supplier.Name,
			o.Supplier.Location, o.ItemDescription, o.DeliveryLocation, o.DeliveryNote,
			o.Reward, string(o.State), o.Version, key, o.CreatedAt, o.UpdatedAt,
			o.StateChangedAt, o.ExpiresAt, o.ConfirmBy)
		require.NoError(t, err, "insert errand %s", o.ID)
	}
}

// insertHistory writes a history row with SQL of its own and returns its id.
func (e *env) insertHistory(t *testing.T, h order.HistoryEntry) int64 {
	t.Helper()
	var from, changes *string
	if h.FromState != "" {
		from = ptr(string(h.FromState))
	}
	if len(h.Changes) > 0 {
		b, err := json.Marshal(h.Changes)
		require.NoError(t, err)
		changes = ptr(string(b))
	}
	var id int64
	err := e.pool.QueryRow(t.Context(), `
		INSERT INTO order_history (order_id, action, from_state, to_state, actor_id,
			actor_kind, courier_id, changes, occurred_at, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		h.OrderID, string(h.Action), from, string(h.ToState), h.ActorID,
		string(h.ActorKind), h.CourierID, changes, h.OccurredAt, h.CorrelationID).Scan(&id)
	require.NoError(t, err, "insert history row")
	return id
}

func (e *env) get(t *testing.T, id uuid.UUID) order.Order {
	t.Helper()
	o, err := e.repo.Get(t.Context(), id)
	require.NoError(t, err)
	return o
}

func (e *env) history(t *testing.T, id uuid.UUID) []order.HistoryEntry {
	t.Helper()
	h, err := e.repo.History(t.Context(), id)
	require.NoError(t, err)
	return h
}

// count runs a SELECT count(*) query.
func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, e.pool.QueryRow(t.Context(), query, args...).Scan(&n), "count: %s", query)
	return n
}

// requireUntouched checks a write that failed left o as it was, with no
// history row and no event.
func (e *env) requireUntouched(t *testing.T, o order.Order) {
	t.Helper()
	require.Equal(t, o, e.get(t, o.ID), "the row is unchanged")
	require.Empty(t, e.history(t, o.ID), "no history row")
	require.Zero(t, e.count(t, `SELECT count(*) FROM outbox`), "no event")
}

// outboxRow is one outbox row as stored.
type outboxRow struct {
	routingKey    string
	payload       string
	correlationID string
	availableAt   time.Time
	published     bool
	attempts      int
}

func (e *env) outbox(t *testing.T, eventID uuid.UUID) outboxRow {
	t.Helper()
	var r outboxRow
	err := e.pool.QueryRow(t.Context(), `
		SELECT routing_key, payload::text, correlation_id, available_at,
			published_at IS NOT NULL, attempts
		FROM outbox WHERE event_id = $1`, eventID).
		Scan(&r.routingKey, &r.payload, &r.correlationID, &r.availableAt, &r.published, &r.attempts)
	require.NoError(t, err, "read outbox row %s", eventID)
	return r
}

// rewardSetEvent is the reward_set the domain writes for o (D-057).
func rewardSetEvent(o order.Order, now time.Time, correlationID string) order.OutboxEvent {
	e := outboxEvent(order.RoutingRewardSet, o, nil, now, correlationID)
	e.Payload.Version = ptr(o.Version)
	return e
}

// outboxEvent is the event the domain writes for o (internal/order/events.go).
func outboxEvent(routingKey string, o order.Order, courier *uuid.UUID, now time.Time, correlationID string) order.OutboxEvent {
	id := uuid.New()
	return order.OutboxEvent{
		EventID:    id,
		RoutingKey: routingKey,
		Payload: order.EventPayload{
			EventID:       id,
			EventType:     routingKey,
			ErrandID:      o.ID,
			RequesterID:   o.RequesterID,
			Amount:        o.Reward,
			OccurredAt:    now,
			CourierID:     courier,
			CorrelationID: correlationID,
		},
		CorrelationID: correlationID,
		AvailableAt:   now,
	}
}

// wantPayload is p as credit-service reads it (D-051), written out by hand so
// a renamed or missing JSON field fails the test.
func wantPayload(p order.EventPayload) string {
	courier := ""
	if p.CourierID != nil {
		courier = fmt.Sprintf(`, "courierId": %q`, p.CourierID.String())
	}
	return fmt.Sprintf(`{"eventId": %q, "eventType": %q, "errandId": %q, "requesterId": %q,
		"amount": %d, "occurredAt": %q, "correlationId": %q%s}`,
		p.EventID.String(), p.EventType, p.ErrandID.String(), p.RequesterID.String(),
		p.Amount, p.OccurredAt.UTC().Format(time.RFC3339Nano), p.CorrelationID, courier)
}

// createdEntry is o's CREATE history row (F3.13.1).
func createdEntry(o order.Order) order.HistoryEntry {
	return order.HistoryEntry{
		OrderID:       o.ID,
		Action:        order.ActionCreate,
		ToState:       order.StateOpen,
		ActorID:       ptr(o.RequesterID),
		ActorKind:     order.ActorRequester,
		OccurredAt:    o.CreatedAt,
		CorrelationID: "corr-create",
	}
}

// amendEntry is the AMEND history row of o's requester changing fields (F3.7.7).
func amendEntry(o order.Order, now time.Time, changes []order.FieldChange) order.HistoryEntry {
	return order.HistoryEntry{
		OrderID:       o.ID,
		Action:        order.ActionAmend,
		FromState:     order.StateOpen,
		ToState:       order.StateOpen,
		ActorID:       ptr(o.RequesterID),
		ActorKind:     order.ActorRequester,
		Changes:       changes,
		OccurredAt:    now,
		CorrelationID: "corr-amend",
	}
}

// AI-generated (edited by James Teo).
// withdrawalEntry is the WITHDRAW history row of courier giving o back (F3.12),
// the row D-060 reads.
func withdrawalEntry(o order.Order, courier uuid.UUID, now time.Time) order.HistoryEntry {
	return order.HistoryEntry{
		OrderID:       o.ID,
		Action:        order.ActionWithdraw,
		FromState:     order.StateAccepted,
		ToState:       order.StateOpen,
		ActorID:       ptr(courier),
		ActorKind:     order.ActorCourier,
		CourierID:     ptr(courier),
		OccurredAt:    now,
		CorrelationID: "corr-withdraw",
	}
}

func ids(orders ...order.Order) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(orders))
	for _, o := range orders {
		out = append(out, o.ID)
	}
	return out
}

// requireList checks a list's result: exactly want, in order.
func requireList[T any](t *testing.T, want, got []T) {
	t.Helper()
	if len(want) == 0 {
		require.Empty(t, got)
		return
	}
	require.Equal(t, want, got)
}

func (e *env) testMigrate(t *testing.T) {
	t.Run("a second run finds nothing to apply", func(t *testing.T) {
		require.NoError(t, Migrate(e.dbURL, "../../migrations"))
		var (
			version int
			dirty   bool
		)
		require.NoError(t, e.pool.QueryRow(t.Context(), `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty))
		// AI-generated (edited by PENDING): 000004 adds ratings and Find errands' records; 000005 the supplier's type and position.
		require.Equal(t, 5, version)
		require.False(t, dirty)
	})
	t.Run("a missing directory is an error", func(t *testing.T) {
		require.Error(t, Migrate(e.dbURL, filepath.Join(t.TempDir(), "missing")))
	})
	t.Run("a database that does not answer is an error", func(t *testing.T) {
		require.Error(t, Migrate("postgres://postgres:postgres@127.0.0.1:1/orders?sslmode=disable&connect_timeout=2", "../../migrations"))
	})
}

func (e *env) testGet(t *testing.T) {
	e.reset(t)
	requester, courier := uuid.New(), uuid.New()
	open := openErrand(requester, at(0))
	open.RequestKey = "key-get"
	delivered := heldBy(openErrand(requester, at(time.Minute)), courier, order.StateDelivered, at(30*time.Minute))
	delivered.Version = 4
	e.insert(t, open, delivered)

	for _, want := range []order.Order{open, delivered} {
		got, err := e.repo.Get(t.Context(), want.ID)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}

	t.Run("returns times in UTC", func(t *testing.T) {
		got := e.get(t, delivered.ID)
		for _, tm := range []time.Time{got.CreatedAt, got.UpdatedAt, got.StateChangedAt, *got.ConfirmBy, *e.get(t, open.ID).ExpiresAt} {
			require.Equal(t, time.UTC, tm.Location())
		}
	})
	t.Run("an unknown id is ErrNotFound, not pgx's error", func(t *testing.T) {
		_, err := e.repo.Get(t.Context(), uuid.New())
		require.ErrorIs(t, err, order.ErrNotFound)
		require.NotErrorIs(t, err, pgx.ErrNoRows)
	})
}

func (e *env) testFindByRequestKey(t *testing.T) {
	e.reset(t)
	requester := uuid.New()
	keyed := openErrand(requester, at(0))
	keyed.RequestKey = "key-1"
	keyless := openErrand(requester, at(time.Minute))
	e.insert(t, keyed, keyless)

	got, err := e.repo.FindByRequestKey(t.Context(), requester, "key-1")
	require.NoError(t, err)
	require.Equal(t, keyed, got)

	for _, tc := range []struct {
		name      string
		requester uuid.UUID
		key       string
	}{
		{"another requester's key", uuid.New(), "key-1"},
		{"a key not used", requester, "key-2"},
		{"the empty key of an errand posted without one", requester, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.repo.FindByRequestKey(t.Context(), tc.requester, tc.key)
			require.ErrorIs(t, err, order.ErrNotFound)
		})
	}
}

func (e *env) testHistory(t *testing.T) {
	e.reset(t)
	requester := uuid.New()
	o := openErrand(requester, at(0))
	other := openErrand(requester, at(0))
	e.insert(t, o, other)

	want := []order.HistoryEntry{
		createdEntry(o),
		amendEntry(o, at(time.Minute), []order.FieldChange{
			{Field: order.FieldReward, From: "5", To: "8"},
			{Field: order.FieldDeliveryNote, From: "Black backpack", To: ""},
		}),
		{
			OrderID: o.ID, Action: order.ActionExpire, FromState: order.StateOpen,
			ToState: order.StateExpired, ActorKind: order.ActorSystem,
			OccurredAt: at(2 * time.Hour), CorrelationID: "corr-sweep",
		},
	}
	for i := range want {
		want[i].ID = e.insertHistory(t, want[i])
	}
	e.insertHistory(t, createdEntry(other))

	t.Run("oldest first, with NULL from_state, actor and changes read back as empty", func(t *testing.T) {
		got, err := e.repo.History(t.Context(), o.ID)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
	t.Run("an errand with no rows has an empty history", func(t *testing.T) {
		got, err := e.repo.History(t.Context(), uuid.New())
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

func (e *env) testListOpen(t *testing.T) {
	e.reset(t)
	now := at(time.Hour)
	requester := uuid.New()
	oldest := openErrand(requester, at(0))
	middle := openErrand(requester, at(time.Minute))
	newest := openErrand(requester, at(2*time.Minute))
	dueNow := openErrand(requester, at(3*time.Minute))
	dueNow.ExpiresAt = ptr(now)
	overdue := openErrand(requester, at(4*time.Minute))
	overdue.ExpiresAt = ptr(now.Add(-time.Minute)) // the sweeper has not caught up yet
	accepted := heldBy(openErrand(requester, at(5*time.Minute)), uuid.New(), order.StateAccepted, at(6*time.Minute))
	cancelled := endedAs(openErrand(requester, at(7*time.Minute)), order.StateCancelled, at(8*time.Minute))
	e.insert(t, oldest, middle, newest, dueNow, overdue, accepted, cancelled)

	for _, tc := range []struct {
		name  string
		limit int
		want  []order.Order
	}{
		{"OPEN before its deadline, newest first", order.OpenListLimit, []order.Order{newest, middle, oldest}},
		{"capped at the limit", 2, []order.Order{newest, middle}},
		{"no cap at zero, as in the fake", 0, []order.Order{newest, middle, oldest}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.repo.ListOpen(t.Context(), now, tc.limit)
			require.NoError(t, err)
			requireList(t, tc.want, got)
		})
	}
}

func (e *env) testListByRequester(t *testing.T) {
	e.reset(t)
	requester, courier := uuid.New(), uuid.New()
	posted := func(i int) order.Order { return openErrand(requester, at(time.Duration(i)*time.Minute)) }
	open := posted(0)
	accepted := heldBy(posted(1), courier, order.StateAccepted, at(50*time.Minute))
	delivered := heldBy(posted(2), courier, order.StateDelivered, at(30*time.Minute))
	completed := endedAs(heldBy(posted(3), courier, order.StateAccepted, at(10*time.Minute)), order.StateCompleted, at(40*time.Minute))
	expired := endedAs(posted(4), order.StateExpired, at(20*time.Minute))
	cancelled := endedAs(posted(5), order.StateCancelled, at(60*time.Minute))
	someoneElses := openErrand(uuid.New(), at(70*time.Minute))
	e.insert(t, open, accepted, delivered, completed, expired, cancelled, someoneElses)

	for _, tc := range []struct {
		name   string
		states []order.State
		limit  int
		want   []order.Order
	}{
		{"active, most recently changed first", order.ActiveStates(), 100, []order.Order{accepted, delivered, open}},
		{"history, most recently changed first", order.FinalStates(), 50, []order.Order{cancelled, completed, expired}},
		{"capped at the limit", order.ActiveStates(), 1, []order.Order{accepted}},
		{"no states, no errands", nil, 100, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.repo.ListByRequester(t.Context(), requester, tc.states, tc.limit)
			require.NoError(t, err)
			requireList(t, tc.want, got)
		})
	}
}

func (e *env) testListByCourier(t *testing.T) {
	e.reset(t)
	requester, courier, other := uuid.New(), uuid.New(), uuid.New()
	posted := func(i int) order.Order { return openErrand(requester, at(time.Duration(i)*time.Minute)) }
	accepted := heldBy(posted(0), courier, order.StateAccepted, at(10*time.Minute))
	pickedUp := heldBy(posted(1), courier, order.StatePickedUp, at(30*time.Minute))
	delivered := heldBy(posted(2), courier, order.StateDelivered, at(20*time.Minute))
	completed := endedAs(heldBy(posted(3), courier, order.StateAccepted, at(4*time.Minute)), order.StateCompleted, at(40*time.Minute))
	othersAccepted := heldBy(posted(5), other, order.StateAccepted, at(50*time.Minute))
	unassigned := posted(6)
	e.insert(t, accepted, pickedUp, delivered, completed, othersAccepted, unassigned)

	held := []order.State{order.StateAccepted, order.StatePickedUp, order.StateDelivered}
	for _, tc := range []struct {
		name   string
		states []order.State
		limit  int
		want   []order.Order
	}{
		{"held, most recently changed first", held, 100, []order.Order{pickedUp, delivered, accepted}},
		{"only the states asked for", []order.State{order.StateCompleted}, 100, []order.Order{completed}},
		{"capped at the limit", held, 2, []order.Order{pickedUp, delivered}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.repo.ListByCourier(t.Context(), courier, tc.states, tc.limit)
			require.NoError(t, err)
			requireList(t, tc.want, got)
		})
	}
}

func (e *env) testListActive(t *testing.T) {
	e.reset(t)
	courier := uuid.New()
	states := allStates()
	var want []order.Order
	// Each state's errand changed later than the next one's, so an order that
	// comes back oldest first is the query's doing, not the insert order's.
	for i, s := range states {
		o := inState(openErrand(uuid.New(), at(0)), s, courier, at(time.Duration(len(states)-i)*time.Minute))
		e.insert(t, o)
		if !s.Final() {
			want = append(want, o)
		}
	}
	slices.Reverse(want)

	for _, tc := range []struct {
		name  string
		limit int
		want  []order.Order
	}{
		{"every errand not final, oldest change first", order.AdminListLimit, want},
		{"capped at the limit", 2, want[:2]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.repo.ListActive(t.Context(), tc.limit)
			require.NoError(t, err)
			requireList(t, tc.want, got)
		})
	}
}

func (e *env) testListAssignmentHistory(t *testing.T) {
	e.reset(t)
	requester, courier, other, admin := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	errand := func(item string, reward int) order.Order {
		o := openErrand(requester, at(0))
		o.ItemDescription = item
		o.Reward = reward
		return o
	}
	// Each errand as it stands now; the history rows below are how it got there.
	released := errand("Kaya toast", 3)
	releasedAgain := errand("Teh peng", 2)
	completed := endedAs(heldBy(errand("Chicken rice", 7), courier, order.StateDelivered, at(5*time.Minute)), order.StateCompleted, at(6*time.Minute))
	cancelled := endedAs(heldBy(errand("Mala xiang guo", 9), courier, order.StateAccepted, at(7*time.Minute)), order.StateCancelled, at(8*time.Minute))
	othersCompleted := endedAs(heldBy(errand("Bubble tea", 4), other, order.StateDelivered, at(5*time.Minute)), order.StateCompleted, at(9*time.Minute))
	requesterCancelled := endedAs(errand("Waffle", 3), order.StateCancelled, at(10*time.Minute))
	e.insert(t, released, releasedAgain, completed, cancelled, othersCompleted, requesterCancelled)

	row := func(o order.Order, a order.Action, from, to order.State, kind order.ActorKind, actor, held *uuid.UUID, when time.Time) int64 {
		return e.insertHistory(t, order.HistoryEntry{
			OrderID: o.ID, Action: a, FromState: from, ToState: to, ActorID: actor,
			ActorKind: kind, CourierID: held, OccurredAt: when, CorrelationID: "corr-history",
		})
	}
	open, accepted, pickedUp := order.StateOpen, order.StateAccepted, order.StatePickedUp
	deliveredState := order.StateDelivered
	courierKind := order.ActorCourier

	// Accepting, picking up and delivering are not endings, so they stay out.
	row(released, order.ActionAccept, open, accepted, courierKind, &courier, &courier, at(time.Minute))
	withdrawn := row(released, order.ActionWithdraw, accepted, open, courierKind, &courier, &courier, at(2*time.Minute))
	row(releasedAgain, order.ActionAccept, open, accepted, courierKind, &courier, &courier, at(time.Minute))
	// The same instant as the first withdrawal: the later row comes first.
	withdrawnAgain := row(releasedAgain, order.ActionWithdraw, accepted, open, courierKind, &courier, &courier, at(2*time.Minute))
	row(completed, order.ActionAccept, open, accepted, courierKind, &courier, &courier, at(3*time.Minute))
	row(completed, order.ActionPickUp, accepted, pickedUp, courierKind, &courier, &courier, at(4*time.Minute))
	row(completed, order.ActionDeliver, pickedUp, deliveredState, courierKind, &courier, &courier, at(5*time.Minute))
	confirmed := row(completed, order.ActionConfirm, deliveredState, order.StateCompleted, order.ActorRequester, &requester, &courier, at(6*time.Minute))
	row(cancelled, order.ActionAccept, open, accepted, courierKind, &courier, &courier, at(7*time.Minute))
	adminCancelled := row(cancelled, order.ActionAdminCancel, accepted, order.StateCancelled, order.ActorAdmin, &admin, &courier, at(8*time.Minute))
	// Another courier's ending, and a cancel with no courier at all.
	othersConfirmed := row(othersCompleted, order.ActionConfirm, deliveredState, order.StateCompleted, order.ActorRequester, &requester, &other, at(9*time.Minute))
	row(requesterCancelled, order.ActionCancel, open, order.StateCancelled, order.ActorRequester, &requester, nil, at(10*time.Minute))

	assignment := func(historyID int64, o order.Order, outcome order.AssignmentOutcome, ended time.Time) order.Assignment {
		return order.Assignment{
			HistoryID: historyID, OrderID: o.ID, Outcome: outcome, EndedAt: ended,
			Supplier: o.Supplier, ItemDescription: o.ItemDescription,
			DeliveryLocation: o.DeliveryLocation, Reward: o.Reward,
		}
	}
	want := []order.Assignment{
		assignment(adminCancelled, cancelled, order.OutcomeCancelled, at(8*time.Minute)),
		assignment(confirmed, completed, order.OutcomeCompleted, at(6*time.Minute)),
		assignment(withdrawnAgain, releasedAgain, order.OutcomeReleased, at(2*time.Minute)),
		assignment(withdrawn, released, order.OutcomeReleased, at(2*time.Minute)),
	}

	for _, tc := range []struct {
		name    string
		courier uuid.UUID
		limit   int
		want    []order.Assignment
	}{
		{"withdrawals, completions and admin cancels, most recent first", courier, order.HistoryListLimit, want},
		{"capped at the limit", courier, 2, want[:2]},
		{"only the courier's own", other, order.HistoryListLimit, []order.Assignment{
			assignment(othersConfirmed, othersCompleted, order.OutcomeCompleted, at(9*time.Minute)),
		}},
		{"a courier with no assignments", uuid.New(), order.HistoryListLimit, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.repo.ListAssignmentHistory(t.Context(), tc.courier, tc.limit)
			require.NoError(t, err)
			requireList(t, tc.want, got)
		})
	}
}

func (e *env) testCountActiveBySupplier(t *testing.T) {
	e.reset(t)
	supplier := uuid.New()
	for i, s := range allStates() {
		o := inState(openErrand(uuid.New(), at(0)), s, uuid.New(), at(time.Duration(i)*time.Minute))
		o.Supplier.ID = supplier
		e.insert(t, o)
	}
	e.insert(t, openErrand(uuid.New(), at(0))) // another supplier's

	for _, tc := range []struct {
		name     string
		supplier uuid.UUID
		want     int
	}{
		{"counts OPEN, ACCEPTED, PICKED_UP and DELIVERED only", supplier, 4},
		{"a supplier no errand names", uuid.New(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.repo.CountActiveBySupplier(t.Context(), tc.supplier)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// AI-generated (edited by James Teo).
func (e *env) testCountOpenByRequester(t *testing.T) {
	e.reset(t)
	now := at(time.Hour)
	requester := uuid.New()
	for i, s := range allStates() {
		e.insert(t, inState(openErrand(requester, at(0)), s, uuid.New(), at(time.Duration(i)*time.Minute)))
	}
	another := openErrand(requester, at(time.Minute))
	dueNow := openErrand(requester, at(2*time.Minute))
	dueNow.ExpiresAt = ptr(now) // the sweeper has not caught up yet
	e.insert(t, another, dueNow, openErrand(uuid.New(), at(0)))

	for _, tc := range []struct {
		name      string
		requester uuid.UUID
		want      int
	}{
		{"counts OPEN errands with a deadline after now only", requester, 2},
		{"a requester with none", uuid.New(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.repo.CountOpenByRequester(t.Context(), tc.requester, now)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// AI-generated (edited by James Teo).
// testWithdrawnCouriers is D-060's read side: every read of an errand lists
// the couriers its WITHDRAW history rows name, oldest first, and an errand
// with none reads back nil, as in the fake.
func (e *env) testWithdrawnCouriers(t *testing.T) {
	e.reset(t)
	requester, first, second, holder := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	o := openErrand(requester, at(0))
	o.RequestKey = "key-withdrawn"
	plain := openErrand(requester, at(time.Minute))
	e.insert(t, o, plain)
	e.insertHistory(t, createdEntry(o))
	e.insertHistory(t, withdrawalEntry(o, first, at(time.Minute)))
	e.insertHistory(t, withdrawalEntry(o, second, at(2*time.Minute)))
	// An ACCEPT row names its courier too, but is no withdrawal.
	e.insertHistory(t, acceptCmd(o, holder, at(3*time.Minute), 2).History)
	want := o
	want.WithdrawnCouriers = []uuid.UUID{first, second}

	require.Equal(t, want, e.get(t, o.ID), "Get")
	require.Nil(t, e.get(t, plain.ID).WithdrawnCouriers, "an errand nobody withdrew from")
	found, err := e.repo.FindByRequestKey(t.Context(), requester, "key-withdrawn")
	require.NoError(t, err)
	require.Equal(t, want, found, "FindByRequestKey")
	open, err := e.repo.ListOpen(t.Context(), at(time.Hour), 0)
	require.NoError(t, err)
	requireList(t, []order.Order{plain, want}, open)
	requested, err := e.repo.ListByRequester(t.Context(), requester, order.ActiveStates(), 0)
	require.NoError(t, err)
	requireList(t, []order.Order{plain, want}, requested)
	active, err := e.repo.ListActive(t.Context(), 0)
	require.NoError(t, err)
	requireList(t, []order.Order{want, plain}, active)
	var underAmend, underQueue order.Order
	_, err = e.repo.Amend(t.Context(), o.ID, func(cur order.Order) (*order.AmendResult, error) {
		underAmend = cur
		return nil, nil
	})
	require.NoError(t, err)
	require.NoError(t, e.repo.QueueLocked(t.Context(), o.ID, func(cur order.Order) (*order.OutboxEvent, error) {
		underQueue = cur
		return nil, nil
	}))
	require.Equal(t, want, underAmend, "the row Amend locks")
	require.Equal(t, want, underQueue, "the row QueueLocked locks")

	t.Run("an accept's row, and a withdrawal written now, read back the same", func(t *testing.T) {
		accepted, err := e.repo.Accept(t.Context(), acceptCmd(want, holder, at(4*time.Minute), 2))
		require.NoError(t, err)
		require.Equal(t, []uuid.UUID{first, second}, accepted.WithdrawnCouriers, "Accept's RETURNING")

		tr := move(accepted, order.ActionWithdraw, order.StateOpen, at(5*time.Minute))
		tr.ExpectCourierID = ptr(holder)
		tr.CourierID = nil
		tr.ExpiresAt = ptr(at(5*time.Minute + 2*time.Hour))
		withdrawn, err := e.repo.Transition(t.Context(), tr)
		require.NoError(t, err)
		require.Equal(t, []uuid.UUID{first, second, holder}, withdrawn.WithdrawnCouriers, "the withdrawal it wrote counts")
		require.Equal(t, withdrawn, e.get(t, o.ID), "as a read now has it")
	})
}

func (e *env) testDueForExpiry(t *testing.T) {
	e.reset(t)
	now := at(3 * time.Hour)
	requester := uuid.New()
	expiring := func(created, deadline time.Time) order.Order {
		o := openErrand(requester, created)
		o.ExpiresAt = ptr(deadline)
		return o
	}
	early := expiring(at(0), at(time.Hour))
	earliest := expiring(at(time.Minute), at(30*time.Minute))
	atNow := expiring(at(2*time.Minute), now)
	notYet := expiring(at(3*time.Minute), now.Add(time.Second))
	accepted := heldBy(openErrand(requester, at(0)), uuid.New(), order.StateAccepted, at(time.Minute))
	expired := endedAs(openErrand(requester, at(0)), order.StateExpired, at(2*time.Hour))
	e.insert(t, early, earliest, atNow, notYet, accepted, expired)

	for _, tc := range []struct {
		name  string
		limit int
		want  []uuid.UUID
	}{
		{"OPEN with a deadline at or before now, oldest deadline first", order.SweepBatchLimit, ids(earliest, early, atNow)},
		{"capped at the limit", 2, ids(earliest, early)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.repo.DueForExpiry(t.Context(), now, tc.limit)
			require.NoError(t, err)
			requireList(t, tc.want, got)
		})
	}
}

func (e *env) testDueForAutoConfirm(t *testing.T) {
	e.reset(t)
	now := at(48 * time.Hour)
	requester, courier := uuid.New(), uuid.New()
	confirming := func(window time.Time) order.Order {
		o := heldBy(openErrand(requester, at(0)), courier, order.StateDelivered, at(time.Minute))
		o.ConfirmBy = ptr(window)
		return o
	}
	early := confirming(at(30 * time.Hour))
	earliest := confirming(at(25 * time.Hour))
	atNow := confirming(now)
	notYet := confirming(now.Add(time.Second))
	completed := endedAs(heldBy(openErrand(requester, at(0)), courier, order.StateDelivered, at(time.Minute)), order.StateCompleted, at(2*time.Minute))
	pickedUp := heldBy(openErrand(requester, at(0)), courier, order.StatePickedUp, at(time.Minute))
	e.insert(t, early, earliest, atNow, notYet, completed, pickedUp)

	for _, tc := range []struct {
		name  string
		limit int
		want  []uuid.UUID
	}{
		{"DELIVERED with a window ending at or before now, oldest first", order.SweepBatchLimit, ids(earliest, early, atNow)},
		{"capped at the limit", 1, ids(earliest)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.repo.DueForAutoConfirm(t.Context(), now, tc.limit)
			require.NoError(t, err)
			requireList(t, tc.want, got)
		})
	}
}

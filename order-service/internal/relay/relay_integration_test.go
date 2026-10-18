//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Integration tests of the outbox relay against real PostgreSQL and
//   RabbitMQ in testcontainers: claiming under a lock, retries, routing,
//   confirms, reconnection and concurrent relays (D-047, D-051, NFR5.4).
// Author review: James Teo — reviewed and verified.

package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	brokerUser  = "foc"
	brokerPass  = "relay-test-password"
	brokerVhost = "foc"
	// The exchange and credit-service queue that
	// rabbitmq/definitions.json declares.
	eventsExchange = "foc.events"
	lifecycleQueue = "credit.errand.lifecycle"

	// waitLimit bounds every wait on the database or the broker.
	waitLimit = 20 * time.Second
	pollTick  = 20 * time.Millisecond

	// lockNotAvailable is PostgreSQL's SQLSTATE for a lock_timeout expiring.
	lockNotAvailable = "55P03"
)

// lifecycleKeys are the routing keys definitions.json binds to lifecycleQueue.
func lifecycleKeys() []string {
	return []string{"errand.completed", "errand.cancelled", "errand.expired", "errand.failed"}
}

// stack is the PostgreSQL and RabbitMQ every subtest shares. Each subtest
// starts by emptying the outbox and the queue, so none depends on another.
type stack struct {
	dsn     string
	pool    *pgxpool.Pool
	amqpURL string
	rabbit  testcontainers.Container
}

func TestRelayIntegration(t *testing.T) {
	ctx := context.Background()
	s := &stack{}
	s.dsn, s.pool = startPostgres(t, ctx)
	s.amqpURL, s.rabbit = startRabbitMQ(t, ctx)
	declareLifecycleTopology(t, s.amqpURL)

	t.Run("PostgresStore", func(t *testing.T) {
		t.Run("claims nothing from an empty outbox", func(t *testing.T) {
			s.reset(t)
			called := false
			claimed, err := NewPostgresStore(s.pool).ClaimNext(ctx, time.Minute, func(context.Context, Record) error {
				called = true
				return nil
			})
			require.NoError(t, err)
			assert.False(t, claimed)
			assert.False(t, called)
		})

		t.Run("publishes the oldest due row while holding its lock", func(t *testing.T) {
			s.reset(t)
			oldest := newOutboxEvent("errand.failed")
			s.insert(t, oldest, 0)
			s.exec(t, `UPDATE outbox SET attempts = 2 WHERE event_id = $1`, oldest.id)
			next := newOutboxEvent("errand.completed")
			s.insert(t, next, 0)
			notDue := newOutboxEvent("errand.expired")
			s.insert(t, notDue, time.Hour)

			var got, gotBySecondRelay Record
			claimed, err := NewPostgresStore(s.pool).ClaimNext(ctx, time.Minute, func(ctx context.Context, r Record) error {
				got = r
				// D-047: create's delete of a queued refund waits for an
				// in-flight publish of it.
				code, err := deleteWithLockTimeout(ctx, s.pool, r.EventID)
				require.NoError(t, err)
				assert.Equal(t, lockNotAvailable, code, "the row must stay locked while it is published")
				// A second relay skips the locked row and takes the next due one.
				claimed, err := NewPostgresStore(s.pool).ClaimNext(ctx, time.Minute, func(_ context.Context, r Record) error {
					gotBySecondRelay = r
					return nil
				})
				require.NoError(t, err)
				assert.True(t, claimed)
				return nil
			})
			require.NoError(t, err)
			assert.True(t, claimed)

			assert.Equal(t, oldest.id, got.EventID)
			assert.Equal(t, oldest.routingKey, got.RoutingKey)
			assert.JSONEq(t, oldest.payload, string(got.Payload))
			assert.Equal(t, oldest.correlationID, got.CorrelationID)
			assert.Equal(t, 2, got.Attempts)
			assert.Equal(t, next.id, gotBySecondRelay.EventID)

			for _, id := range []uuid.UUID{oldest.id, next.id} {
				row := s.row(t, id)
				assert.NotNil(t, row.publishedAt)
				assert.Nil(t, row.lastError)
			}
			assert.Nil(t, s.row(t, notDue.id).publishedAt, "a row not yet due must not be claimed")
		})

		t.Run("records a failed publish and holds the row back", func(t *testing.T) {
			s.reset(t)
			e := newOutboxEvent("errand.cancelled")
			s.insert(t, e, 0)
			store := NewPostgresStore(s.pool)

			failure := errors.New(strings.Repeat("é", maxLastError+100))
			claimed, err := store.ClaimNext(ctx, 10*time.Minute, func(context.Context, Record) error { return failure })
			require.NoError(t, err, "a publish error is recorded, not returned")
			assert.True(t, claimed)

			row := s.row(t, e.id)
			assert.Nil(t, row.publishedAt)
			assert.Equal(t, 1, row.attempts)
			require.NotNil(t, row.lastError)
			assert.Equal(t, strings.Repeat("é", maxLastError), *row.lastError)
			assert.True(t, s.dueBetween(t, e.id, 9*time.Minute, 10*time.Minute), "available_at moves RetryDelay ahead")

			claimed, err = store.ClaimNext(ctx, time.Minute, func(context.Context, Record) error {
				t.Error("a row held back after a failure was claimed before it was due")
				return nil
			})
			require.NoError(t, err)
			assert.False(t, claimed)
		})

		t.Run("leaves the row untouched when its context ends mid-publish", func(t *testing.T) {
			s.reset(t)
			e := newOutboxEvent("errand.expired")
			s.insert(t, e, 0)
			before := s.row(t, e.id)

			cctx, cancel := context.WithCancel(ctx)
			defer cancel()
			claimed, err := NewPostgresStore(s.pool).ClaimNext(cctx, time.Minute, func(ctx context.Context, _ Record) error {
				cancel()
				return ctx.Err()
			})
			assert.ErrorIs(t, err, context.Canceled)
			assert.False(t, claimed)
			assert.Equal(t, before, s.row(t, e.id), "a publish cut short by shutdown is not a failed attempt")
		})

		t.Run("records a confirmed publish even if its context ends just after", func(t *testing.T) {
			s.reset(t)
			e := newOutboxEvent("errand.completed")
			s.insert(t, e, 0)

			cctx, cancel := context.WithCancel(ctx)
			defer cancel()
			claimed, err := NewPostgresStore(s.pool).ClaimNext(cctx, time.Minute, func(context.Context, Record) error {
				cancel()
				return nil
			})
			require.NoError(t, err)
			assert.True(t, claimed)
			assert.NotNil(t, s.row(t, e.id).publishedAt, "else a restart would publish the event again")
		})
	})

	t.Run("AMQPPublisher", func(t *testing.T) {
		t.Run("reports an unroutable key and keeps its channel", func(t *testing.T) {
			s.reset(t)
			p := s.publisher(t, s.amqpURL, eventsExchange)

			unbound := newOutboxEvent("errand.unbound").record()
			err := p.Publish(ctx, unbound)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "could not route")
			assert.Contains(t, err.Error(), "NO_ROUTE")

			bound := newOutboxEvent("errand.completed").record()
			require.NoError(t, p.Publish(ctx, bound))
			assert.Equal(t, []string{bound.EventID.String()}, messageIDs(s.drain(t)))
		})

		t.Run("reopens its channel after the broker closes it", func(t *testing.T) {
			s.reset(t)
			const exchange = "foc.relay-test-missing"
			p := s.publisher(t, s.amqpURL, exchange)
			dials := countDials(p)

			err := p.Publish(ctx, newOutboxEvent("errand.completed").record())
			require.Error(t, err, "publishing to an exchange that does not exist")
			assert.Contains(t, err.Error(), "NOT_FOUND")

			admin := brokerChannel(t, s.amqpURL)
			require.NoError(t, admin.ExchangeDeclare(exchange, "topic", false, false, false, false, nil))
			t.Cleanup(func() { _ = admin.ExchangeDelete(exchange, false, false) })
			q, err := admin.QueueDeclare("", false, true, true, false, nil)
			require.NoError(t, err)
			require.NoError(t, admin.QueueBind(q.Name, "errand.completed", exchange, false, nil))

			second := newOutboxEvent("errand.completed").record()
			require.NoError(t, p.Publish(ctx, second))
			assert.EqualValues(t, 1, dials.Load(), "only the channel is reopened, not the connection")
			d, ok, err := admin.Get(q.Name, true)
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, second.EventID.String(), d.MessageId)
		})

		t.Run("redials after the broker closes the connection", func(t *testing.T) {
			s.reset(t)
			p := s.publisher(t, s.amqpURL, eventsExchange)
			dials := countDials(p)

			first := newOutboxEvent("errand.completed").record()
			require.NoError(t, p.Publish(ctx, first))
			require.EqualValues(t, 1, dials.Load())

			s.closeAllConnections(t)

			second := newOutboxEvent("errand.cancelled").record()
			require.Eventually(t, func() bool { return p.Publish(ctx, second) == nil }, waitLimit, 100*time.Millisecond,
				"the publisher must reconnect once the broker is back")
			assert.EqualValues(t, 2, dials.Load())
			assert.Contains(t, messageIDs(s.drain(t)), second.EventID.String())
		})
	})

	t.Run("Relay", func(t *testing.T) {
		t.Run("publishes every lifecycle key with the event's properties", func(t *testing.T) {
			s.reset(t)
			events := map[string]outboxEvent{}
			var ids []uuid.UUID
			for _, key := range lifecycleKeys() {
				e := newOutboxEvent(key)
				s.insert(t, e, 0)
				events[e.id.String()] = e
				ids = append(ids, e.id)
			}
			rr := runRelay(t, New(NewPostgresStore(s.pool), s.publisher(t, s.amqpURL, eventsExchange), nil,
				Options{PollInterval: pollTick}))
			s.waitUntil(t, "every event is marked published", func() bool { return s.published(ids...) })
			rr.stop()

			deliveries := s.drain(t)
			require.Len(t, deliveries, len(events))
			for _, d := range deliveries {
				e, ok := events[d.MessageId]
				require.True(t, ok, "unexpected message %q", d.MessageId)
				assert.Equal(t, s.storedPayload(t, e.id), string(d.Body), "the body is the payload as stored")
				assert.JSONEq(t, e.payload, string(d.Body))
				assert.Equal(t, e.correlationID, d.CorrelationId)
				assert.Equal(t, "application/json", d.ContentType)
				assert.Equal(t, amqp.Persistent, d.DeliveryMode)
				assert.Equal(t, eventsExchange, d.Exchange)
				assert.Equal(t, e.routingKey, d.RoutingKey)
				assert.Equal(t, e.routingKey, d.Type)
				assert.False(t, d.Timestamp.IsZero())

				row := s.row(t, e.id)
				assert.Zero(t, row.attempts)
				assert.Nil(t, row.lastError)
			}
		})

		t.Run("holds a row until it is due", func(t *testing.T) {
			s.reset(t)
			due := newOutboxEvent("errand.completed")
			s.insert(t, due, 0)
			later := newOutboxEvent("errand.failed")
			s.insert(t, later, time.Hour)

			store := idleSignal{Store: NewPostgresStore(s.pool), idle: make(chan struct{}, 1)}
			rr := runRelay(t, New(store, s.publisher(t, s.amqpURL, eventsExchange), nil, Options{PollInterval: pollTick}))
			s.waitUntil(t, "the due event is published", func() bool { return s.published(due.id) })
			// The relay claimed the due row first, so this poll came after it:
			// the later row was in the outbox and the relay found nothing due.
			select {
			case <-store.idle:
			case <-time.After(waitLimit):
				t.Fatal("the relay did not poll again after publishing the due event")
			}
			assert.Nil(t, s.row(t, later.id).publishedAt, "a row must not be published before its available_at")

			s.exec(t, `UPDATE outbox SET available_at = now() WHERE event_id = $1`, later.id)
			s.waitUntil(t, "the delayed event is published once due", func() bool { return s.published(later.id) })
			rr.stop()
			assert.ElementsMatch(t, []string{due.id.String(), later.id.String()}, messageIDs(s.drain(t)))
		})

		t.Run("retries an unroutable key instead of dropping it", func(t *testing.T) {
			s.reset(t)
			unbound := newOutboxEvent("errand.unbound")
			s.insert(t, unbound, 0)
			bound := newOutboxEvent("errand.completed")
			s.insert(t, bound, 0)

			logs := &lockedBuffer{}
			rr := runRelay(t, New(NewPostgresStore(s.pool), s.publisher(t, s.amqpURL, eventsExchange), jsonLogger(logs),
				Options{PollInterval: pollTick, RetryDelay: time.Hour}))
			s.waitUntil(t, "the unroutable event's failure is recorded and the other is published", func() bool {
				row, err := s.tryRow(unbound.id)
				return err == nil && row.attempts == 1 && s.published(bound.id)
			})
			rr.stop()

			row := s.row(t, unbound.id)
			assert.Nil(t, row.publishedAt, "an unroutable event must not be marked published")
			assert.Equal(t, 1, row.attempts)
			require.NotNil(t, row.lastError)
			assert.Contains(t, *row.lastError, "errand.unbound")
			assert.Contains(t, *row.lastError, "NO_ROUTE")
			assert.True(t, s.dueBetween(t, unbound.id, 59*time.Minute, time.Hour), "available_at moves RetryDelay ahead")

			failures := withMessage(logs.entries(t), "ERROR", "outbox publish failed; will retry")
			require.Len(t, failures, 1)
			assert.Equal(t, unbound.id.String(), failures[0]["event_id"])
			assert.Equal(t, "errand.unbound", failures[0]["routing_key"])
			assert.Equal(t, unbound.correlationID, failures[0]["request_id"])
			assert.EqualValues(t, 1, failures[0]["attempt"])

			assert.Equal(t, []string{bound.id.String()}, messageIDs(s.drain(t)))
		})

		t.Run("keeps running while the broker is unreachable", func(t *testing.T) {
			s.reset(t)
			e := newOutboxEvent("errand.cancelled")
			s.insert(t, e, 0)

			const password = "dead-broker-password"
			deadURL := fmt.Sprintf("amqp://%s:%s@%s/%s", brokerUser, password, closedAddr(t), brokerVhost)
			rr := runRelay(t, New(NewPostgresStore(s.pool), s.publisher(t, deadURL, eventsExchange), nil,
				Options{PollInterval: 10 * time.Millisecond, RetryDelay: 50 * time.Millisecond}))
			s.waitUntil(t, "the relay has tried the event three times", func() bool {
				row, err := s.tryRow(e.id)
				return err == nil && row.attempts >= 3
			})
			assert.True(t, rr.running(), "the relay must keep running while the broker is down")
			rr.stop()

			row := s.row(t, e.id)
			assert.Nil(t, row.publishedAt)
			require.NotNil(t, row.lastError)
			assert.Contains(t, *row.lastError, "RabbitMQ")
			assert.NotContains(t, *row.lastError, password)
		})

		t.Run("two relays never publish the same row twice", func(t *testing.T) {
			s.reset(t)
			const n = 200
			secondPool, err := pgxpool.New(ctx, s.dsn)
			require.NoError(t, err)
			t.Cleanup(secondPool.Close)

			pubA := &countingPublisher{Publisher: s.publisher(t, s.amqpURL, eventsExchange)}
			pubB := &countingPublisher{Publisher: s.publisher(t, s.amqpURL, eventsExchange)}
			rrA := runRelay(t, New(NewPostgresStore(s.pool), pubA, nil, Options{PollInterval: 5 * time.Millisecond}))
			rrB := runRelay(t, New(NewPostgresStore(secondPool), pubB, nil, Options{PollInterval: 5 * time.Millisecond}))

			// One statement, so both relays find every row due at once.
			s.exec(t, `
				INSERT INTO outbox (event_id, routing_key, payload)
				SELECT gen_random_uuid(),
				       (ARRAY['errand.completed', 'errand.cancelled', 'errand.expired', 'errand.failed'])[1 + g % 4],
				       jsonb_build_object('n', g)
				FROM generate_series(1, $1::int) AS g`, n)
			s.waitUntil(t, "every row is published", func() bool {
				var unpublished, total int
				err := s.pool.QueryRow(ctx,
					`SELECT count(*) FILTER (WHERE published_at IS NULL), count(*) FROM outbox`).Scan(&unpublished, &total)
				return err == nil && total == n && unpublished == 0
			})
			rrA.stop()
			rrB.stop()

			deliveries := s.drain(t)
			perEvent := map[string]int{}
			for _, d := range deliveries {
				perEvent[d.MessageId]++
			}
			assert.Len(t, deliveries, n)
			assert.Len(t, perEvent, n)
			for id, count := range perEvent {
				assert.Equal(t, 1, count, "event %s was delivered %d times", id, count)
			}
			assert.Equal(t, n, pubA.count()+pubB.count())
			assert.Positive(t, pubA.count(), "both relays must have taken rows for the test to show anything")
			assert.Positive(t, pubB.count(), "both relays must have taken rows for the test to show anything")
		})
	})
}

// startPostgres starts PostgreSQL, applies order-service's migrations and
// returns its DSN and a pool on it.
func startPostgres(t *testing.T, ctx context.Context) (string, *pgxpool.Pool) {
	t.Helper()
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "orders",
				"POSTGRES_USER":     "postgres",
				"POSTGRES_PASSWORD": "postgres",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err, "start PostgreSQL")
	host, err := ctr.Host(ctx)
	require.NoError(t, err)
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	hostPort := net.JoinHostPort(host, port.Port())

	migrations, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	require.NoError(t, err)
	m, err := migrate.New((&url.URL{Scheme: "file", Path: filepath.ToSlash(migrations)}).String(),
		"pgx5://postgres:postgres@"+hostPort+"/orders?sslmode=disable")
	require.NoError(t, err, "open migrations")
	upErr := m.Up()
	srcErr, dbErr := m.Close()
	require.NoError(t, upErr, "apply migrations")
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	dsn := "postgres://postgres:postgres@" + hostPort + "/orders?sslmode=disable"
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return dsn, pool
}

// startRabbitMQ starts a broker with the foc vhost and its own user (the
// broker refuses the guest user from outside the container) and returns its
// AMQP URL.
func startRabbitMQ(t *testing.T, ctx context.Context) (string, testcontainers.Container) {
	t.Helper()
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rabbitmq:3.13-management",
			ExposedPorts: []string{"5672/tcp"},
			Env: map[string]string{
				"RABBITMQ_DEFAULT_USER":  brokerUser,
				"RABBITMQ_DEFAULT_PASS":  brokerPass,
				"RABBITMQ_DEFAULT_VHOST": brokerVhost,
			},
			WaitingFor: wait.ForLog("Server startup complete").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err, "start RabbitMQ")
	host, err := ctr.Host(ctx)
	require.NoError(t, err)
	port, err := ctr.MappedPort(ctx, "5672/tcp")
	require.NoError(t, err)
	return fmt.Sprintf("amqp://%s:%s@%s/%s", brokerUser, brokerPass, net.JoinHostPort(host, port.Port()), brokerVhost), ctr
}

// declareLifecycleTopology declares what rabbitmq/definitions.json does for
// order events: the topic exchange, credit-service's queue and its four bindings.
func declareLifecycleTopology(t *testing.T, amqpURL string) {
	t.Helper()
	ch := brokerChannel(t, amqpURL)
	require.NoError(t, ch.ExchangeDeclare(eventsExchange, "topic", true, false, false, false, nil))
	_, err := ch.QueueDeclare(lifecycleQueue, true, false, false, false, nil)
	require.NoError(t, err)
	for _, key := range lifecycleKeys() {
		require.NoError(t, ch.QueueBind(lifecycleQueue, key, eventsExchange, false, nil))
	}
}

// brokerChannel opens a channel on a connection of its own, closed when the
// test ends.
func brokerChannel(t *testing.T, amqpURL string) *amqp.Channel {
	t.Helper()
	conn, err := amqp.Dial(amqpURL)
	require.NoError(t, err, "connect to RabbitMQ")
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	require.NoError(t, err)
	return ch
}

// reset empties the outbox and the lifecycle queue.
func (s *stack) reset(t *testing.T) {
	t.Helper()
	s.exec(t, `TRUNCATE outbox`)
	_, err := brokerChannel(t, s.amqpURL).QueuePurge(lifecycleQueue, false)
	require.NoError(t, err)
}

func (s *stack) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

// publisher returns an AMQPPublisher closed when the test ends.
func (s *stack) publisher(t *testing.T, amqpURL, exchange string) *AMQPPublisher {
	t.Helper()
	p := NewAMQPPublisher(amqpURL, exchange, nil)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// outboxEvent is a row the tests insert, with a payload shaped like D-051's.
type outboxEvent struct {
	id            uuid.UUID
	routingKey    string
	payload       string
	correlationID string
}

func newOutboxEvent(routingKey string) outboxEvent {
	id := uuid.New()
	correlationID := "corr-" + id.String()[:8]
	payload := fmt.Sprintf(
		`{"eventId":%q,"eventType":%q,"errandId":%q,"requesterId":%q,"amount":5,"occurredAt":"2026-10-14T10:00:00Z","correlationId":%q}`,
		id, routingKey, uuid.New(), uuid.New(), correlationID)
	return outboxEvent{id: id, routingKey: routingKey, payload: payload, correlationID: correlationID}
}

func (e outboxEvent) record() Record {
	return Record{EventID: e.id, RoutingKey: e.routingKey, Payload: []byte(e.payload), CorrelationID: e.correlationID}
}

// insert adds e to the outbox, due once delay has passed.
func (s *stack) insert(t *testing.T, e outboxEvent, delay time.Duration) {
	t.Helper()
	s.exec(t, `
		INSERT INTO outbox (event_id, routing_key, payload, correlation_id, available_at)
		VALUES ($1, $2, $3::jsonb, $4, now() + $5::interval)`,
		e.id, e.routingKey, e.payload, e.correlationID, delay)
}

// outboxRow is the part of an outbox row the relay changes.
type outboxRow struct {
	publishedAt *time.Time
	attempts    int
	lastError   *string
	availableAt time.Time
}

// tryRow reads a row without failing the test, for polling conditions.
func (s *stack) tryRow(id uuid.UUID) (outboxRow, error) {
	var r outboxRow
	err := s.pool.QueryRow(context.Background(),
		`SELECT published_at, attempts, last_error, available_at FROM outbox WHERE event_id = $1`, id).
		Scan(&r.publishedAt, &r.attempts, &r.lastError, &r.availableAt)
	return r, err
}

func (s *stack) row(t *testing.T, id uuid.UUID) outboxRow {
	t.Helper()
	r, err := s.tryRow(id)
	require.NoError(t, err)
	return r
}

// published reports whether every row in ids is marked published.
func (s *stack) published(ids ...uuid.UUID) bool {
	var n int
	err := s.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE event_id = ANY($1) AND published_at IS NOT NULL`, ids).Scan(&n)
	return err == nil && n == len(ids)
}

// dueBetween reports whether the row's available_at lies between from and to
// ahead of now.
func (s *stack) dueBetween(t *testing.T, id uuid.UUID, from, to time.Duration) bool {
	t.Helper()
	var ok bool
	err := s.pool.QueryRow(context.Background(),
		`SELECT available_at BETWEEN now() + $2::interval AND now() + $3::interval FROM outbox WHERE event_id = $1`,
		id, from, to).Scan(&ok)
	require.NoError(t, err)
	return ok
}

// storedPayload is the row's payload as PostgreSQL returns it.
func (s *stack) storedPayload(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var payload string
	err := s.pool.QueryRow(context.Background(), `SELECT payload::text FROM outbox WHERE event_id = $1`, id).Scan(&payload)
	require.NoError(t, err)
	return payload
}

func (s *stack) waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, waitLimit, pollTick, "timed out waiting until %s", what)
}

// drain takes every message waiting in the lifecycle queue. Run only after
// the relays have stopped: a confirmed publish is already in the queue.
func (s *stack) drain(t *testing.T) []amqp.Delivery {
	t.Helper()
	ch := brokerChannel(t, s.amqpURL)
	var out []amqp.Delivery
	for {
		d, ok, err := ch.Get(lifecycleQueue, true)
		require.NoError(t, err)
		if !ok {
			return out
		}
		out = append(out, d)
	}
}

// closeAllConnections has the broker close every connection to the vhost, as
// a broker restart or network failure would.
func (s *stack) closeAllConnections(t *testing.T) {
	t.Helper()
	code, out, err := s.rabbit.Exec(context.Background(),
		[]string{"rabbitmqctl", "close_all_connections", "-p", brokerVhost, "relay integration test"},
		tcexec.Multiplexed())
	require.NoError(t, err)
	if code != 0 {
		text, _ := io.ReadAll(out)
		t.Fatalf("rabbitmqctl close_all_connections exited %d: %s", code, text)
	}
}

func messageIDs(deliveries []amqp.Delivery) []string {
	ids := make([]string, 0, len(deliveries))
	for _, d := range deliveries {
		ids = append(ids, d.MessageId)
	}
	return ids
}

// deleteWithLockTimeout runs D-047's delete of a queued refund for id with a
// short lock timeout, then rolls it back. It returns the SQLSTATE the delete
// failed with, or "" if it went through.
func deleteWithLockTimeout(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (string, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '200ms'`); err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `DELETE FROM outbox WHERE event_id = $1 AND published_at IS NULL`, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code, nil
	}
	return "", err
}

// closedAddr returns a local address that nothing listens on.
func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

// countDials counts p's dial attempts from here on.
func countDials(p *AMQPPublisher) *atomic.Int32 {
	var n atomic.Int32
	dial := p.dial
	p.dial = func(url string) (*amqp.Connection, error) {
		n.Add(1)
		return dial(url)
	}
	return &n
}

// idleSignal is a Store that signals, without blocking, each poll that found
// nothing due.
type idleSignal struct {
	Store
	idle chan struct{}
}

func (s idleSignal) ClaimNext(ctx context.Context, retryDelay time.Duration, publish func(ctx context.Context, r Record) error) (bool, error) {
	claimed, err := s.Store.ClaimNext(ctx, retryDelay, publish)
	if !claimed && err == nil {
		select {
		case s.idle <- struct{}{}:
		default:
		}
	}
	return claimed, err
}

// countingPublisher counts the records its Publisher published.
type countingPublisher struct {
	Publisher
	mu sync.Mutex
	n  int
}

func (c *countingPublisher) Publish(ctx context.Context, r Record) error {
	err := c.Publisher.Publish(ctx, r)
	if err == nil {
		c.mu.Lock()
		c.n++
		c.mu.Unlock()
	}
	return err
}

func (c *countingPublisher) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

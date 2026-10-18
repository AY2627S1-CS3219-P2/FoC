//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: Integration tests of the outbox relay against real PostgreSQL and
//   RabbitMQ in testcontainers (claiming under a lock, retries, routing to
//   credit.registration, an unreachable broker), adapted from
//   order-service/internal/relay to the standard testing package.
// Author review: James Teo — reviewed and verified.

package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"foc/user-service/internal/repository"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	brokerUser  = "foc"
	brokerPass  = "relay-test-password"
	brokerVhost = "foc"
	// The exchange, credit-service queue and binding that
	// rabbitmq/definitions.json declares for user.registered.
	eventsExchange    = "foc.events"
	registrationQueue = "credit.registration"

	// waitLimit bounds every wait on the database or the broker.
	waitLimit = 20 * time.Second
	pollTick  = 20 * time.Millisecond

	// lockNotAvailable is PostgreSQL's SQLSTATE for FOR UPDATE NOWAIT finding
	// the row locked.
	lockNotAvailable = "55P03"
)

// stack is the PostgreSQL and RabbitMQ every subtest shares. Each subtest
// starts by emptying the tables and the queue, so none depends on another.
type stack struct {
	pool    *pgxpool.Pool
	amqpURL string
}

func TestRelayIntegration(t *testing.T) {
	ctx := context.Background()
	s := &stack{pool: startPostgres(t, ctx), amqpURL: startRabbitMQ(t, ctx)}
	declareRegistrationTopology(t, s.amqpURL)

	t.Run("PostgresStore", func(t *testing.T) {
		t.Run("claims nothing from an empty outbox", func(t *testing.T) {
			s.reset(t)
			called := false
			claimed, err := NewPostgresStore(s.pool).ClaimNext(ctx, time.Minute, func(context.Context, Record) error {
				called = true
				return nil
			})
			if err != nil {
				t.Fatalf("ClaimNext: %v", err)
			}
			if claimed || called {
				t.Fatalf("claimed = %t, publish called = %t; want neither on an empty outbox", claimed, called)
			}
		})

		t.Run("publishes the oldest due row while holding its lock", func(t *testing.T) {
			s.reset(t)
			oldest := newOutboxEvent(user.RoutingUserRegistered)
			s.insert(t, oldest, 0)
			s.exec(t, `UPDATE outbox_events SET attempts = 2 WHERE event_id = $1`, oldest.id)
			next := newOutboxEvent(user.RoutingUserRegistered)
			s.insert(t, next, 0)
			notDue := newOutboxEvent(user.RoutingUserRegistered)
			s.insert(t, notDue, time.Hour)

			var got, gotBySecondRelay Record
			claimed, err := NewPostgresStore(s.pool).ClaimNext(ctx, time.Minute, func(ctx context.Context, r Record) error {
				got = r
				if code := s.lockCode(t, r.EventID); code != lockNotAvailable {
					t.Errorf("locking the row being published: SQLSTATE %q, want %q (row locked)", code, lockNotAvailable)
				}
				// A second relay skips the locked row and takes the next due one.
				claimed, err := NewPostgresStore(s.pool).ClaimNext(ctx, time.Minute, func(_ context.Context, r Record) error {
					gotBySecondRelay = r
					return nil
				})
				if err != nil || !claimed {
					t.Errorf("second relay: claimed = %t, err = %v; want the next due row", claimed, err)
				}
				return nil
			})
			if err != nil || !claimed {
				t.Fatalf("ClaimNext: claimed = %t, err = %v", claimed, err)
			}

			if got.EventID != oldest.id || got.RoutingKey != oldest.routingKey || got.CorrelationID != oldest.correlationID || got.Attempts != 2 {
				t.Errorf("claimed record = %+v, want the oldest row %+v with 2 attempts", got, oldest)
			}
			assertJSONEqual(t, oldest.payload, string(got.Payload))
			if gotBySecondRelay.EventID != next.id {
				t.Errorf("second relay claimed %s, want %s", gotBySecondRelay.EventID, next.id)
			}
			for _, id := range []uuid.UUID{oldest.id, next.id} {
				if row := s.row(t, id); row.publishedAt == nil || row.lastError != nil {
					t.Errorf("row %s = %+v, want published with no error", id, row)
				}
			}
			if s.row(t, notDue.id).publishedAt != nil {
				t.Error("a row not yet due was claimed")
			}
		})

		t.Run("records a failed publish and holds the row back", func(t *testing.T) {
			s.reset(t)
			e := newOutboxEvent(user.RoutingUserRegistered)
			s.insert(t, e, 0)
			store := NewPostgresStore(s.pool)

			failure := errors.New(strings.Repeat("é", maxLastError+100))
			claimed, err := store.ClaimNext(ctx, 10*time.Minute, func(context.Context, Record) error { return failure })
			if err != nil {
				t.Fatalf("a publish error is recorded, not returned: got %v", err)
			}
			if !claimed {
				t.Fatal("the due row was not claimed")
			}

			row := s.row(t, e.id)
			if row.publishedAt != nil || row.attempts != 1 {
				t.Errorf("row = %+v, want unpublished with 1 attempt", row)
			}
			if row.lastError == nil || *row.lastError != strings.Repeat("é", maxLastError) {
				t.Errorf("last_error was not the publish error cut to %d characters", maxLastError)
			}
			if !s.dueBetween(t, e.id, 9*time.Minute, 10*time.Minute) {
				t.Error("available_at did not move RetryDelay ahead")
			}

			claimed, err = store.ClaimNext(ctx, time.Minute, func(context.Context, Record) error {
				t.Error("a row held back after a failure was claimed before it was due")
				return nil
			})
			if err != nil || claimed {
				t.Errorf("claim after the failure: claimed = %t, err = %v; want nothing due", claimed, err)
			}
		})

		t.Run("leaves the row untouched when its context ends mid-publish", func(t *testing.T) {
			s.reset(t)
			e := newOutboxEvent(user.RoutingUserRegistered)
			s.insert(t, e, 0)
			before := s.row(t, e.id)

			cctx, cancel := context.WithCancel(ctx)
			defer cancel()
			claimed, err := NewPostgresStore(s.pool).ClaimNext(cctx, time.Minute, func(ctx context.Context, _ Record) error {
				cancel()
				return ctx.Err()
			})
			if !errors.Is(err, context.Canceled) || claimed {
				t.Errorf("claimed = %t, err = %v; want an interrupted claim", claimed, err)
			}
			if after := s.row(t, e.id); !after.equal(before) {
				t.Errorf("row = %+v, want it unchanged at %+v: a publish cut short by shutdown is not a failed attempt", after, before)
			}
		})

		t.Run("records a confirmed publish even if its context ends just after", func(t *testing.T) {
			s.reset(t)
			e := newOutboxEvent(user.RoutingUserRegistered)
			s.insert(t, e, 0)

			cctx, cancel := context.WithCancel(ctx)
			defer cancel()
			claimed, err := NewPostgresStore(s.pool).ClaimNext(cctx, time.Minute, func(context.Context, Record) error {
				cancel()
				return nil
			})
			if err != nil || !claimed {
				t.Fatalf("claimed = %t, err = %v", claimed, err)
			}
			if s.row(t, e.id).publishedAt == nil {
				t.Error("the row was not marked published, so a restart would publish the event again")
			}
		})
	})

	t.Run("Relay", func(t *testing.T) {
		t.Run("publishes an activation's user.registered event to credit.registration", func(t *testing.T) {
			s.reset(t)
			users := repository.NewPostgresRepository(s.pool)
			account := &user.User{
				Username:     "relayactivation",
				Email:        "relay-activation@u.nus.edu",
				PasswordHash: "hashed-password",
				PhoneNum:     "+6512345678",
			}
			if err := users.Create(ctx, account); err != nil {
				t.Fatalf("create pending account: %v", err)
			}
			if err := users.ActivateUser(ctx, account.Email); err != nil {
				t.Fatalf("activate account: %v", err)
			}
			var eventID uuid.UUID
			if err := s.pool.QueryRow(ctx, `SELECT event_id FROM outbox_events`).Scan(&eventID); err != nil {
				t.Fatalf("read the queued event: %v", err)
			}

			rr := runRelay(t, New(NewPostgresStore(s.pool), s.publisher(t, s.amqpURL), nil, Options{PollInterval: pollTick}))
			s.waitUntil(t, "the event is marked published", func() bool { return s.published(eventID) })
			rr.stop()

			deliveries := s.drain(t)
			if len(deliveries) != 1 {
				t.Fatalf("messages on %s = %d, want 1", registrationQueue, len(deliveries))
			}
			d := deliveries[0]
			if d.MessageId != eventID.String() || d.Exchange != eventsExchange || d.RoutingKey != user.RoutingUserRegistered || d.Type != user.RoutingUserRegistered {
				t.Errorf("message id/exchange/key/type = %q/%q/%q/%q, want %s/%s/%s/%s",
					d.MessageId, d.Exchange, d.RoutingKey, d.Type, eventID, eventsExchange, user.RoutingUserRegistered, user.RoutingUserRegistered)
			}
			if d.ContentType != "application/json" || d.DeliveryMode != amqp.Persistent || d.Timestamp.IsZero() {
				t.Errorf("content type/delivery mode/timestamp = %q/%d/%s, want application/json, persistent, set",
					d.ContentType, d.DeliveryMode, d.Timestamp)
			}
			if d.CorrelationId != "" {
				t.Errorf("correlation id = %q, want the row's empty correlation_id", d.CorrelationId)
			}
			if body := string(d.Body); body != s.storedPayload(t, eventID) {
				t.Errorf("body = %s, want the payload as stored", body)
			}
			var payload user.UserRegisteredEvent
			if err := json.Unmarshal(d.Body, &payload); err != nil {
				t.Fatalf("decode body %s: %v", d.Body, err)
			}
			if payload.EventID != eventID || payload.EventType != user.RoutingUserRegistered || payload.UserID != account.UID || payload.OccurredAt.IsZero() {
				t.Errorf("payload = %+v, want event %s for user %s", payload, eventID, account.UID)
			}
		})

		t.Run("retries an unroutable key instead of dropping it", func(t *testing.T) {
			s.reset(t)
			unbound := newOutboxEvent("user.unbound")
			s.insert(t, unbound, 0)
			bound := newOutboxEvent(user.RoutingUserRegistered)
			s.insert(t, bound, 0)

			rr := runRelay(t, New(NewPostgresStore(s.pool), s.publisher(t, s.amqpURL), nil,
				Options{PollInterval: pollTick, RetryDelay: time.Hour}))
			s.waitUntil(t, "the unroutable event's failure is recorded and the other is published", func() bool {
				row, err := s.tryRow(unbound.id)
				return err == nil && row.attempts == 1 && s.published(bound.id)
			})
			rr.stop()

			row := s.row(t, unbound.id)
			if row.publishedAt != nil {
				t.Error("an unroutable event was marked published")
			}
			if row.lastError == nil || !strings.Contains(*row.lastError, "user.unbound") || !strings.Contains(*row.lastError, "NO_ROUTE") {
				t.Errorf("last_error = %v, want the unroutable key and NO_ROUTE", row.lastError)
			}
			if !s.dueBetween(t, unbound.id, 59*time.Minute, time.Hour) {
				t.Error("available_at did not move RetryDelay ahead")
			}
			deliveries := s.drain(t)
			if len(deliveries) != 1 || deliveries[0].MessageId != bound.id.String() {
				t.Errorf("messages on %s = %d, want only %s", registrationQueue, len(deliveries), bound.id)
			}
		})

		t.Run("keeps running while the broker is unreachable", func(t *testing.T) {
			s.reset(t)
			e := newOutboxEvent(user.RoutingUserRegistered)
			s.insert(t, e, 0)

			const password = "dead-broker-password"
			deadURL := fmt.Sprintf("amqp://%s:%s@%s/%s", brokerUser, password, closedAddr(t), brokerVhost)
			rr := runRelay(t, New(NewPostgresStore(s.pool), s.publisher(t, deadURL), nil,
				Options{PollInterval: 10 * time.Millisecond, RetryDelay: 50 * time.Millisecond}))
			s.waitUntil(t, "the relay has tried the event three times", func() bool {
				row, err := s.tryRow(e.id)
				return err == nil && row.attempts >= 3
			})
			if !rr.running() {
				t.Error("the relay stopped while the broker was down")
			}
			rr.stop()

			row := s.row(t, e.id)
			if row.publishedAt != nil {
				t.Error("an event was marked published with no broker")
			}
			if row.lastError == nil || !strings.Contains(*row.lastError, "RabbitMQ") {
				t.Errorf("last_error = %v, want a RabbitMQ connection failure", row.lastError)
			}
			if row.lastError != nil && strings.Contains(*row.lastError, password) {
				t.Errorf("last_error %q leaks the broker password", *row.lastError)
			}
		})
	})
}

// startPostgres starts PostgreSQL, applies user-service's migrations and
// returns a pool on it.
func startPostgres(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "user_service",
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
	if err != nil {
		t.Fatalf("start PostgreSQL: %v", err)
	}
	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("get PostgreSQL host: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("get PostgreSQL port: %v", err)
	}
	dsn := "postgres://postgres:postgres@" + net.JoinHostPort(host, port.Port()) + "/user_service?sslmode=disable"

	migrations, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	if err := repository.Apply(dsn, (&url.URL{Scheme: "file", Path: filepath.ToSlash(migrations)}).String()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// startRabbitMQ starts a broker with the foc vhost and its own user (the
// broker refuses the guest user from outside the container) and returns its
// AMQP URL.
func startRabbitMQ(t *testing.T, ctx context.Context) string {
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
	if err != nil {
		t.Fatalf("start RabbitMQ: %v", err)
	}
	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("get RabbitMQ host: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "5672/tcp")
	if err != nil {
		t.Fatalf("get RabbitMQ port: %v", err)
	}
	return fmt.Sprintf("amqp://%s:%s@%s/%s", brokerUser, brokerPass, net.JoinHostPort(host, port.Port()), brokerVhost)
}

// declareRegistrationTopology declares what rabbitmq/definitions.json does
// for user.registered: the topic exchange, credit-service's queue and its
// binding.
func declareRegistrationTopology(t *testing.T, amqpURL string) {
	t.Helper()
	ch := brokerChannel(t, amqpURL)
	if err := ch.ExchangeDeclare(eventsExchange, "topic", true, false, false, false, nil); err != nil {
		t.Fatalf("declare exchange: %v", err)
	}
	if _, err := ch.QueueDeclare(registrationQueue, true, false, false, false, nil); err != nil {
		t.Fatalf("declare queue: %v", err)
	}
	if err := ch.QueueBind(registrationQueue, user.RoutingUserRegistered, eventsExchange, false, nil); err != nil {
		t.Fatalf("bind queue: %v", err)
	}
}

// brokerChannel opens a channel on a connection of its own, closed when the
// test ends.
func brokerChannel(t *testing.T, amqpURL string) *amqp.Channel {
	t.Helper()
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		t.Fatalf("connect to RabbitMQ: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("open RabbitMQ channel: %v", err)
	}
	return ch
}

// reset empties the outbox, the accounts and the registration queue.
func (s *stack) reset(t *testing.T) {
	t.Helper()
	s.exec(t, `TRUNCATE outbox_events, sessions, users`)
	if _, err := brokerChannel(t, s.amqpURL).QueuePurge(registrationQueue, false); err != nil {
		t.Fatalf("purge %s: %v", registrationQueue, err)
	}
}

func (s *stack) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// publisher returns an AMQPPublisher to eventsExchange, closed when the test
// ends.
func (s *stack) publisher(t *testing.T, amqpURL string) *AMQPPublisher {
	t.Helper()
	p := NewAMQPPublisher(amqpURL, eventsExchange, nil)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// outboxEvent is a row the tests insert, with a payload shaped like
// user.registered's.
type outboxEvent struct {
	id            uuid.UUID
	routingKey    string
	payload       string
	correlationID string
}

func newOutboxEvent(routingKey string) outboxEvent {
	id := uuid.New()
	payload := fmt.Sprintf(`{"eventId":%q,"eventType":%q,"userId":%q,"occurredAt":"2026-10-16T10:00:00Z"}`,
		id, routingKey, uuid.New())
	return outboxEvent{id: id, routingKey: routingKey, payload: payload, correlationID: "corr-" + id.String()[:8]}
}

// insert adds e to the outbox, due once delay has passed.
func (s *stack) insert(t *testing.T, e outboxEvent, delay time.Duration) {
	t.Helper()
	s.exec(t, `
		INSERT INTO outbox_events (event_id, routing_key, payload, correlation_id, available_at)
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

// equal reports whether r and o hold the same values; == would compare the
// pointers, not what they point at.
func (r outboxRow) equal(o outboxRow) bool {
	samePublished := (r.publishedAt == nil) == (o.publishedAt == nil) &&
		(r.publishedAt == nil || r.publishedAt.Equal(*o.publishedAt))
	sameError := (r.lastError == nil) == (o.lastError == nil) &&
		(r.lastError == nil || *r.lastError == *o.lastError)
	return samePublished && sameError && r.attempts == o.attempts && r.availableAt.Equal(o.availableAt)
}

// tryRow reads a row without failing the test, for polling conditions.
func (s *stack) tryRow(id uuid.UUID) (outboxRow, error) {
	var r outboxRow
	err := s.pool.QueryRow(context.Background(),
		`SELECT published_at, attempts, last_error, available_at FROM outbox_events WHERE event_id = $1`, id).
		Scan(&r.publishedAt, &r.attempts, &r.lastError, &r.availableAt)
	return r, err
}

func (s *stack) row(t *testing.T, id uuid.UUID) outboxRow {
	t.Helper()
	r, err := s.tryRow(id)
	if err != nil {
		t.Fatalf("read outbox row %s: %v", id, err)
	}
	return r
}

// published reports whether every row in ids is marked published.
func (s *stack) published(ids ...uuid.UUID) bool {
	var n int
	err := s.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_events WHERE event_id = ANY($1) AND published_at IS NOT NULL`, ids).Scan(&n)
	return err == nil && n == len(ids)
}

// dueBetween reports whether the row's available_at lies between from and to
// ahead of now.
func (s *stack) dueBetween(t *testing.T, id uuid.UUID, from, to time.Duration) bool {
	t.Helper()
	var ok bool
	err := s.pool.QueryRow(context.Background(),
		`SELECT available_at BETWEEN now() + $2::interval AND now() + $3::interval FROM outbox_events WHERE event_id = $1`,
		id, from, to).Scan(&ok)
	if err != nil {
		t.Fatalf("read available_at of %s: %v", id, err)
	}
	return ok
}

// storedPayload is the row's payload as PostgreSQL returns it.
func (s *stack) storedPayload(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var payload string
	if err := s.pool.QueryRow(context.Background(), `SELECT payload::text FROM outbox_events WHERE event_id = $1`, id).Scan(&payload); err != nil {
		t.Fatalf("read payload of %s: %v", id, err)
	}
	return payload
}

// lockCode tries to lock the row from another transaction without waiting,
// and returns the SQLSTATE that failed with, or "" if the lock was free.
func (s *stack) lockCode(t *testing.T, id uuid.UUID) string {
	t.Helper()
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock probe: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT 1 FROM outbox_events WHERE event_id = $1 FOR UPDATE NOWAIT`, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	if err != nil {
		t.Fatalf("lock probe: %v", err)
	}
	return ""
}

// waitUntil polls cond every pollTick until it holds, failing the test after
// waitLimit.
func (s *stack) waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(waitLimit)
	tick := time.NewTicker(pollTick)
	defer tick.Stop()
	for !cond() {
		select {
		case <-deadline:
			t.Fatalf("timed out after %s waiting until %s", waitLimit, what)
		case <-tick.C:
		}
	}
}

// drain takes every message waiting in the registration queue. Run only
// after the relays have stopped: a confirmed publish is already in the queue.
func (s *stack) drain(t *testing.T) []amqp.Delivery {
	t.Helper()
	ch := brokerChannel(t, s.amqpURL)
	var out []amqp.Delivery
	for {
		d, ok, err := ch.Get(registrationQueue, true)
		if err != nil {
			t.Fatalf("get from %s: %v", registrationQueue, err)
		}
		if !ok {
			return out
		}
		out = append(out, d)
	}
}

// closedAddr returns a local address that nothing listens on.
func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

// assertJSONEqual fails the test unless want and got are the same JSON value.
func assertJSONEqual(t *testing.T, want, got string) {
	t.Helper()
	var w, g any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("decode expected JSON %s: %v", want, err)
	}
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("decode JSON %s: %v", got, err)
	}
	wantText, _ := json.Marshal(w)
	gotText, _ := json.Marshal(g)
	if string(wantText) != string(gotText) {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}

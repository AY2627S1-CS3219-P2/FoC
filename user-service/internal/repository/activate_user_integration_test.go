//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: Real-PostgreSQL coverage of ActivateUser's outbox write: exactly one
//   user.registered row per activation, in the activation's transaction, and
//   none when activation is refused.
// Author review: James Teo — reviewed and verified.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRepositoryActivateUserOutbox(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t, ctx)
	users := NewPostgresRepository(pool)

	t.Run("activates a pending account and queues one user.registered event", func(t *testing.T) {
		resetActivationDatabase(t, ctx, pool)
		account := createPendingAccount(t, ctx, users, "activate")

		before := time.Now()
		// The lookup is case-insensitive, as for every other email lookup.
		if err := users.ActivateUser(ctx, strings.ToUpper(account.Email)); err != nil {
			t.Fatalf("activate account: %v", err)
		}
		after := time.Now()

		assertAccountStatus(t, ctx, users, account.UID, user.AccountStatusActive)
		events := readOutboxEvents(t, ctx, pool)
		if len(events) != 1 {
			t.Fatalf("outbox rows = %d, want exactly 1", len(events))
		}
		event := events[0]
		if event.routingKey != user.RoutingUserRegistered {
			t.Errorf("routing_key = %q, want %q", event.routingKey, user.RoutingUserRegistered)
		}
		if event.correlationID != "" || event.publishedAt != nil || event.attempts != 0 || event.lastError != nil {
			t.Errorf("new outbox row = %+v, want no correlation id, unpublished, no attempts, no error", event)
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(event.payload), &payload); err != nil {
			t.Fatalf("decode payload %s: %v", event.payload, err)
		}
		if len(payload) != 4 {
			t.Errorf("payload = %s, want exactly eventId, eventType, userId and occurredAt", event.payload)
		}
		if payload["eventId"] != event.eventID.String() {
			t.Errorf("payload eventId = %v, want the row's event_id %s", payload["eventId"], event.eventID)
		}
		if payload["eventType"] != user.RoutingUserRegistered {
			t.Errorf("payload eventType = %v, want %q", payload["eventType"], user.RoutingUserRegistered)
		}
		if payload["userId"] != account.UID.String() {
			t.Errorf("payload userId = %v, want the activated account's uid %s", payload["userId"], account.UID)
		}
		occurredAtText, _ := payload["occurredAt"].(string)
		occurredAt, err := time.Parse(time.RFC3339Nano, occurredAtText)
		if err != nil || !strings.HasSuffix(occurredAtText, "Z") {
			t.Fatalf("payload occurredAt = %q, want an RFC 3339 UTC timestamp", occurredAtText)
		}
		if occurredAt.Before(before) || occurredAt.After(after) {
			t.Errorf("payload occurredAt = %s, want the activation time, between %s and %s", occurredAt, before, after)
		}
	})

	t.Run("queues one event when the same activation races itself", func(t *testing.T) {
		resetActivationDatabase(t, ctx, pool)
		account := createPendingAccount(t, ctx, users, "race")

		// A replayed OTP submitted while the first is in flight: the row lock
		// lets one activation through and shows the rest an ACTIVE account.
		const attempts = 5
		errs := make([]error, attempts)
		var wg sync.WaitGroup
		for i := range attempts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = users.ActivateUser(ctx, account.Email)
			}()
		}
		wg.Wait()

		succeeded := 0
		for _, err := range errs {
			switch {
			case err == nil:
				succeeded++
			case !errors.Is(err, user.ErrAccountAlreadyVerified):
				t.Errorf("concurrent activation error = %v, want nil or ErrAccountAlreadyVerified", err)
			}
		}
		if succeeded != 1 {
			t.Errorf("successful activations = %d, want exactly 1", succeeded)
		}
		if n := len(readOutboxEvents(t, ctx, pool)); n != 1 {
			t.Errorf("outbox rows = %d, want exactly 1", n)
		}
	})

	t.Run("refuses activation without queueing an event", func(t *testing.T) {
		resetActivationDatabase(t, ctx, pool)
		active := createIntegrationAccount(t, ctx, users, "already-active", user.AccountRoleStudent)
		if err := users.ActivateUser(ctx, active.Email); !errors.Is(err, user.ErrAccountAlreadyVerified) {
			t.Errorf("activating an ACTIVE account: error = %v, want ErrAccountAlreadyVerified", err)
		}
		if err := users.ActivateUser(ctx, "missing@u.nus.edu"); !errors.Is(err, user.ErrNotFound) {
			t.Errorf("activating an unknown email: error = %v, want ErrNotFound", err)
		}

		suspended := integrationAccount("suspended", user.AccountRoleStudent)
		suspended.AccountStatus = user.AccountStatusSuspended
		if err := users.Create(ctx, suspended); err != nil {
			t.Fatalf("create suspended account: %v", err)
		}
		// Only the outbox is checked here: what ActivateUser returns for a
		// SUSPENDED account is the repository's existing behaviour, unchanged.
		_ = users.ActivateUser(ctx, suspended.Email)
		assertAccountStatus(t, ctx, users, suspended.UID, user.AccountStatusSuspended)

		if n := len(readOutboxEvents(t, ctx, pool)); n != 0 {
			t.Errorf("outbox rows = %d, want none for a refused activation", n)
		}
	})

	t.Run("rolls the activation back when its event cannot be queued", func(t *testing.T) {
		resetActivationDatabase(t, ctx, pool)
		account := createPendingAccount(t, ctx, users, "rollback")

		// NOT VALID leaves existing rows alone but rejects every new one.
		const constraint = "reject_every_event"
		if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT `+constraint+` CHECK (false) NOT VALID`); err != nil {
			t.Fatalf("make outbox inserts fail: %v", err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(context.Background(), `ALTER TABLE outbox_events DROP CONSTRAINT IF EXISTS `+constraint); err != nil {
				t.Errorf("restore outbox inserts: %v", err)
			}
		})

		if err := users.ActivateUser(ctx, account.Email); err == nil {
			t.Fatal("activation succeeded although its event could not be queued")
		}
		assertAccountStatus(t, ctx, users, account.UID, user.AccountStatusUnverified)
		if n := len(readOutboxEvents(t, ctx, pool)); n != 0 {
			t.Errorf("outbox rows = %d, want none", n)
		}
	})
}

// resetActivationDatabase empties every table ActivateUser reads or writes.
func resetActivationDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, "TRUNCATE TABLE outbox_events, sessions, users"); err != nil {
		t.Fatalf("reset integration database: %v", err)
	}
}

// createPendingAccount stores an UNVERIFIED account, as registration does.
func createPendingAccount(t *testing.T, ctx context.Context, users *PostgresRepository, suffix string) *user.User {
	t.Helper()
	account := integrationAccount(suffix, user.AccountRoleStudent)
	account.AccountStatus = user.AccountStatusUnverified
	if err := users.Create(ctx, account); err != nil {
		t.Fatalf("create pending account %q: %v", suffix, err)
	}
	return account
}

func assertAccountStatus(t *testing.T, ctx context.Context, users *PostgresRepository, uid uuid.UUID, want user.AccountStatus) {
	t.Helper()
	got, err := users.GetByID(ctx, uid)
	if err != nil {
		t.Fatalf("get account %s: %v", uid, err)
	}
	if got.AccountStatus != want {
		t.Fatalf("account status = %q, want %q", got.AccountStatus, want)
	}
}

// outboxEvent is one outbox_events row as stored.
type outboxEvent struct {
	eventID       uuid.UUID
	routingKey    string
	payload       string
	correlationID string
	publishedAt   *time.Time
	attempts      int
	lastError     *string
}

func readOutboxEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []outboxEvent {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT event_id, routing_key, payload::text, correlation_id, published_at, attempts, last_error
		FROM outbox_events
		ORDER BY created_at`)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()
	var events []outboxEvent
	for rows.Next() {
		var e outboxEvent
		if err := rows.Scan(&e.eventID, &e.routingKey, &e.payload, &e.correlationID, &e.publishedAt, &e.attempts, &e.lastError); err != nil {
			t.Fatalf("scan outbox row: %v", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	return events
}

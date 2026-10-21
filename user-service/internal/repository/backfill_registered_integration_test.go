//go:build integration

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Real-PostgreSQL coverage of migration 000006, the user.registered
//   backfill: one event per ACTIVE STUDENT that had none, none for other
//   accounts, and still one per account after a re-run or a racing activation.
// Author review: James Teo — reviewed and verified.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"foc/user-service/internal/user"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// Schema versions around the backfill: 4 has no outbox, 5 adds it and
	// ActivateUser's event, 6 is the backfill.
	versionBeforeOutbox = 4
	versionOutbox       = 5
	versionBackfill     = 6
	backfillUpFile      = "000006_backfill_user_registered_events.up.sql"
)

func TestUserRegisteredBackfillMigration(t *testing.T) {
	ctx := context.Background()
	databaseURL := startPostgres(t, ctx)
	migrator := newVersionedMigrator(t, databaseURL)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	users := NewPostgresRepository(pool)

	// Accounts that existed before user-service queued user.registered.
	migrateTo(t, migrator, versionBeforeOutbox)
	sgt := time.FixedZone("SGT", 8*60*60)
	earlyStudent := createBackfillAccount(t, ctx, users, "early-student", user.AccountRoleStudent, user.AccountStatusActive,
		time.Date(2026, 9, 20, 9, 15, 30, 123456000, sgt))
	otherStudent := createBackfillAccount(t, ctx, users, "other-student", user.AccountRoleStudent, user.AccountStatusActive,
		time.Date(2026, 9, 21, 8, 0, 0, 0, sgt))
	unverified := createBackfillAccount(t, ctx, users, "unverified", user.AccountRoleStudent, user.AccountStatusUnverified, time.Time{})
	admin := createBackfillAccount(t, ctx, users, "admin", user.AccountRoleAdmin, user.AccountStatusActive, time.Time{})
	suspended := createBackfillAccount(t, ctx, users, "suspended", user.AccountRoleStudent, user.AccountStatusSuspended, time.Time{})

	// An account activated once the outbox existed already has its event.
	migrateTo(t, migrator, versionOutbox)
	activatedWithEvent := createPendingAccount(t, ctx, users, "activated-with-event")
	if err := users.ActivateUser(ctx, activatedWithEvent.Email); err != nil {
		t.Fatalf("activate account before the backfill: %v", err)
	}
	queuedByActivation := registeredEventsByUser(t, ctx, pool)[activatedWithEvent.UID]
	if len(queuedByActivation) != 1 {
		t.Fatalf("events queued by the activation = %d, want 1", len(queuedByActivation))
	}

	migrateTo(t, migrator, versionBackfill)

	backfilled := registeredEventsByUser(t, ctx, pool)
	assertEventCounts(t, backfilled, map[uuid.UUID]int{
		earlyStudent.UID:       1,
		otherStudent.UID:       1,
		activatedWithEvent.UID: 1,
		unverified.UID:         0,
		admin.UID:              0,
		suspended.UID:          0,
	})
	if events := backfilled[activatedWithEvent.UID]; len(events) == 1 && events[0].eventID != queuedByActivation[0].eventID {
		t.Errorf("activated account's event = %s, want ActivateUser's %s left as it was", events[0].eventID, queuedByActivation[0].eventID)
	}
	for _, account := range []*user.User{earlyStudent, otherStudent} {
		if events := backfilled[account.UID]; len(events) == 1 {
			assertBackfilledEvent(t, events[0], account)
		}
	}

	t.Run("re-running the backfill queues nothing more", func(t *testing.T) {
		if err := migrator.Steps(-1); err != nil {
			t.Fatalf("roll back the backfill: %v", err)
		}
		migrateTo(t, migrator, versionBackfill)
		if _, err := pool.Exec(ctx, readBackfillMigration(t)); err != nil {
			t.Fatalf("run the backfill again: %v", err)
		}
		assertSameEvents(t, registeredEventsByUser(t, ctx, pool), backfilled)
	})

	t.Run("an account activated after the backfill gets exactly one event", func(t *testing.T) {
		if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			t.Fatalf("apply remaining migrations: %v", err)
		}
		if err := users.ActivateUser(ctx, unverified.Email); err != nil {
			t.Fatalf("activate account after the backfill: %v", err)
		}
		assertEventCounts(t, registeredEventsByUser(t, ctx, pool), map[uuid.UUID]int{
			earlyStudent.UID:       1,
			otherStudent.UID:       1,
			activatedWithEvent.UID: 1,
			unverified.UID:         1,
			admin.UID:              0,
			suspended.UID:          0,
		})
	})
}

func TestUserRegisteredBackfillRacingActivation(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t, ctx)
	users := NewPostgresRepository(pool)
	account := createPendingAccount(t, ctx, users, "racing")

	// Holding SHARE on outbox_events stops ActivateUser at its event insert,
	// after it has marked the account ACTIVE but before it commits: the
	// window in which the backfill runs below.
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
	if _, err := holder.Exec(ctx, "LOCK TABLE outbox_events IN SHARE MODE"); err != nil {
		t.Fatalf("lock outbox_events: %v", err)
	}

	activated := make(chan error, 1)
	go func() { activated <- users.ActivateUser(ctx, account.Email) }()
	waitForOutboxLockWaiter(t, ctx, pool)

	if _, err := holder.Exec(ctx, readBackfillMigration(t)); err != nil {
		t.Fatalf("run the backfill during the activation: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit the backfill: %v", err)
	}
	if err := <-activated; err != nil {
		t.Fatalf("activate account: %v", err)
	}

	assertEventCounts(t, registeredEventsByUser(t, ctx, pool), map[uuid.UUID]int{account.UID: 1})
}

// newVersionedMigrator returns a migrator over this service's migrations, so
// a test can stop the schema at a chosen version.
func newVersionedMigrator(t *testing.T, databaseURL string) *migrate.Migrate {
	t.Helper()
	migrationsPath, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	migrator, err := migrate.New((&url.URL{Scheme: "file", Path: migrationsPath}).String(), databaseURL)
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	t.Cleanup(func() { _, _ = migrator.Close() })
	return migrator
}

func migrateTo(t *testing.T, migrator *migrate.Migrate, version uint) {
	t.Helper()
	if err := migrator.Migrate(version); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate to version %d: %v", version, err)
	}
}

func readBackfillMigration(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", backfillUpFile))
	if err != nil {
		t.Fatalf("read backfill migration: %v", err)
	}
	return string(body)
}

// createBackfillAccount stores an account with the given role and status. A
// zero created time lets the repository stamp the current one.
func createBackfillAccount(t *testing.T, ctx context.Context, users *PostgresRepository, suffix string, role user.AccountRole, status user.AccountStatus, created time.Time) *user.User {
	t.Helper()
	account := integrationAccount(suffix, role)
	account.AccountStatus = status
	account.DateCreated = created
	if err := users.Create(ctx, account); err != nil {
		t.Fatalf("create account %q: %v", suffix, err)
	}
	return account
}

// registeredEventsByUser groups the user.registered outbox rows by the
// payload's userId.
func registeredEventsByUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[uuid.UUID][]outboxEvent {
	t.Helper()
	byUser := make(map[uuid.UUID][]outboxEvent)
	for _, event := range readOutboxEvents(t, ctx, pool) {
		if event.routingKey != user.RoutingUserRegistered {
			t.Fatalf("outbox row %s has routing key %q, want only %q", event.eventID, event.routingKey, user.RoutingUserRegistered)
		}
		var payload user.UserRegisteredEvent
		if err := json.Unmarshal([]byte(event.payload), &payload); err != nil {
			t.Fatalf("decode payload %s: %v", event.payload, err)
		}
		byUser[payload.UserID] = append(byUser[payload.UserID], event)
	}
	return byUser
}

func assertEventCounts(t *testing.T, byUser map[uuid.UUID][]outboxEvent, want map[uuid.UUID]int) {
	t.Helper()
	total := 0
	for uid, n := range want {
		total += n
		if got := len(byUser[uid]); got != n {
			t.Errorf("user.registered events for %s = %d, want %d", uid, got, n)
		}
	}
	got := 0
	for _, events := range byUser {
		got += len(events)
	}
	if got != total {
		t.Errorf("user.registered events in total = %d, want %d", got, total)
	}
}

func assertSameEvents(t *testing.T, got, want map[uuid.UUID][]outboxEvent) {
	t.Helper()
	ids := func(byUser map[uuid.UUID][]outboxEvent) map[uuid.UUID]bool {
		set := make(map[uuid.UUID]bool)
		for _, events := range byUser {
			for _, event := range events {
				set[event.eventID] = true
			}
		}
		return set
	}
	gotIDs, wantIDs := ids(got), ids(want)
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("user.registered events = %d, want the %d already queued", len(gotIDs), len(wantIDs))
	}
	for id := range wantIDs {
		if !gotIDs[id] {
			t.Errorf("event %s is gone, want it kept", id)
		}
	}
}

// assertBackfilledEvent checks a backfilled row against the event
// ActivateUser would have queued for account, with date_created standing in
// for the unrecorded activation time.
func assertBackfilledEvent(t *testing.T, event outboxEvent, account *user.User) {
	t.Helper()
	if event.correlationID != "" || event.publishedAt != nil || event.attempts != 0 || event.lastError != nil {
		t.Errorf("backfilled row = %+v, want no correlation id, unpublished, no attempts, no error", event)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(event.payload), &fields); err != nil {
		t.Fatalf("decode payload %s: %v", event.payload, err)
	}
	if len(fields) != 4 {
		t.Errorf("payload = %s, want exactly eventId, eventType, userId and occurredAt", event.payload)
	}
	if occurredAt, _ := fields["occurredAt"].(string); !strings.HasSuffix(occurredAt, "Z") {
		t.Errorf("payload occurredAt = %q, want an RFC 3339 UTC timestamp", occurredAt)
	}

	var got user.UserRegisteredEvent
	if err := json.Unmarshal([]byte(event.payload), &got); err != nil {
		t.Fatalf("decode payload %s: %v", event.payload, err)
	}
	want := user.NewUserRegisteredEvent(event.eventID, account.UID, account.DateCreated)
	if got.EventID != want.EventID || got.EventType != want.EventType || got.UserID != want.UserID {
		t.Errorf("payload = %+v, want %+v", got, want)
	}
	if !equalPostgresTime(got.OccurredAt, want.OccurredAt) || got.OccurredAt.Location() != time.UTC {
		t.Errorf("payload occurredAt = %s, want date_created %s in UTC", got.OccurredAt, want.OccurredAt)
	}
}

// waitForOutboxLockWaiter returns once a transaction is queued for a lock on
// outbox_events.
func waitForOutboxLockWaiter(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_locks
				WHERE relation = 'outbox_events'::regclass AND NOT granted
			)`).Scan(&waiting); err != nil {
			t.Fatalf("read lock waiters: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the activation to queue for the outbox lock")
		case <-tick.C:
		}
	}
}

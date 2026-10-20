//go:build integration

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-27
// Scope: Added PostgreSQL integration coverage for account-status transitions.
// Author review: ZI YANG - validated correctness of tests

package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPostgresRepositoryUpdateAccountStatusByID(t *testing.T) {
	ctx := context.Background()
	databaseURL := startPostgres(t, ctx)
	migrationsPath, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	migrationURL := (&url.URL{Scheme: "file", Path: migrationsPath}).String()
	if err := Apply(databaseURL, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repository := NewPostgresRepository(pool)
	// AI Assistance Disclosure: Codex (GPT-5), 2026-10-09 — verifies the
	// forward migration applies the owner-recorded database default. Author review: Verified correctness.
	var accountStatusDefault string
	if err := pool.QueryRow(ctx, `
		SELECT column_default
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'account_status'
	`).Scan(&accountStatusDefault); err != nil {
		t.Fatalf("read account_status default: %v", err)
	}
	if accountStatusDefault != "'UNVERIFIED'::account_status" {
		t.Fatalf("account_status default = %q, want UNVERIFIED enum default", accountStatusDefault)
	}

	account := &user.User{
		Username:     "status-integration",
		Email:        "status-integration@example.com",
		PasswordHash: "hashed-password",
		PhoneNum:     "+6512345678",
	}
	if err := repository.Create(ctx, account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	suspendedAt := time.Date(2026, 9, 27, 10, 30, 0, 123456000, time.UTC)
	if err := repository.UpdateAccountStatusByID(ctx, account.UID, user.AccountStatusSuspended, suspendedAt); err != nil {
		t.Fatalf("suspend account: %v", err)
	}
	suspended, err := repository.GetByID(ctx, account.UID)
	if err != nil {
		t.Fatalf("get suspended account: %v", err)
	}
	if suspended.AccountStatus != user.AccountStatusSuspended {
		t.Fatalf("account status = %q, want %q", suspended.AccountStatus, user.AccountStatusSuspended)
	}
	if !suspended.TokensValidAfter.Equal(suspendedAt) {
		t.Fatalf("tokens_valid_after = %s, want %s", suspended.TokensValidAfter, suspendedAt)
	}

	if err := repository.UpdateAccountStatusByID(ctx, account.UID, user.AccountStatusActive, suspendedAt.Add(time.Hour)); err != nil {
		t.Fatalf("reactivate account: %v", err)
	}
	active, err := repository.GetByID(ctx, account.UID)
	if err != nil {
		t.Fatalf("get reactivated account: %v", err)
	}
	if active.AccountStatus != user.AccountStatusActive {
		t.Fatalf("account status = %q, want %q", active.AccountStatus, user.AccountStatusActive)
	}
	if !active.TokensValidAfter.Equal(suspendedAt) {
		t.Fatalf("tokens_valid_after after reactivation = %s, want preserved %s", active.TokensValidAfter, suspendedAt)
	}
	var registrationEvents int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE routing_key = 'user.registered' AND payload->>'userId' = $1`, account.UID.String()).Scan(&registrationEvents); err != nil {
		t.Fatalf("count registration events: %v", err)
	}
	if registrationEvents != 1 {
		t.Fatalf("registration events after reactivation = %d, want 1", registrationEvents)
	}
	if err := repository.UpdateAccountStatusByID(ctx, account.UID, user.AccountStatusActive, suspendedAt.Add(2*time.Hour)); err != nil {
		t.Fatalf("repeat reactivation: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE routing_key = 'user.registered' AND payload->>'userId' = $1`, account.UID.String()).Scan(&registrationEvents); err != nil {
		t.Fatalf("count repeated registration events: %v", err)
	}
	if registrationEvents != 1 {
		t.Fatalf("registration events after repeated reactivation = %d, want 1", registrationEvents)
	}

	if err := repository.UpdateAccountStatusByID(ctx, uuid.New(), user.AccountStatusSuspended, suspendedAt); err != user.ErrNotFound {
		t.Fatalf("unknown UID error = %v, want %v", err, user.ErrNotFound)
	}
}

func TestAccountServiceRejectsUnverifiedStatusTransitionsWithPostgres(t *testing.T) {
	// AI Assistance Disclosure: Codex (GPT-5), 2026-10-19 — covers D-078's
	// full AccountService refusal path against real PostgreSQL and recording fakes.
	ctx := context.Background()
	pool := newIntegrationPool(t, ctx)
	users := NewPostgresRepository(pool)

	for _, status := range []user.AccountStatus{user.AccountStatusActive, user.AccountStatusSuspended} {
		t.Run(string(status), func(t *testing.T) {
			resetIntegrationDatabase(t, ctx, pool)
			account := integrationAccount("unverified-"+string(status), user.AccountRoleStudent)
			account.AccountStatus = user.AccountStatusUnverified
			if err := users.Create(ctx, account); err != nil {
				t.Fatalf("create account: %v", err)
			}
			before, err := users.GetByID(ctx, account.UID)
			if err != nil {
				t.Fatalf("get account before the refused change: %v", err)
			}

			writer := &recordingSuspensionWriter{}
			sessions := &recordingSessionRepository{}
			service := user.NewAccountService(users, writer, sessions, time.Minute)
			if err := service.UpdateAccountStatus(ctx, account.UID, status); !errors.Is(err, user.ErrAccountUnverified) {
				t.Fatalf("status update error = %v, want ErrAccountUnverified", err)
			}

			got, err := users.GetByID(ctx, account.UID)
			if err != nil {
				t.Fatalf("get unchanged account: %v", err)
			}
			if got.AccountStatus != user.AccountStatusUnverified {
				t.Fatalf("account status = %q, want UNVERIFIED", got.AccountStatus)
			}
			var events int
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE payload->>'userId' = $1`, account.UID.String()).Scan(&events); err != nil {
				t.Fatalf("count outbox events: %v", err)
			}
			if events != 0 || writer.calls != 0 || sessions.calls != 0 {
				t.Fatalf("side effects = outbox %d, suspension %d, sessions %d; want none", events, writer.calls, sessions.calls)
			}

			// The refusal leaves the account verifiable: its emailed code still
			// activates it, with exactly one user.registered and the token
			// cutoff untouched.
			if err := users.ActivateUser(ctx, account.Email); err != nil {
				t.Fatalf("activate after the refused change: %v", err)
			}
			activated, err := users.GetByID(ctx, account.UID)
			if err != nil {
				t.Fatalf("get activated account: %v", err)
			}
			if activated.AccountStatus != user.AccountStatusActive {
				t.Fatalf("account status after activation = %q, want ACTIVE", activated.AccountStatus)
			}
			if !activated.TokensValidAfter.Equal(before.TokensValidAfter) {
				t.Fatalf("tokens_valid_after = %s, want unchanged %s", activated.TokensValidAfter, before.TokensValidAfter)
			}
			if got := countRegistrationEvents(t, ctx, pool, account.UID); got != 1 {
				t.Fatalf("user.registered events after activation = %d, want 1", got)
			}
		})
	}
}

// TestReactivationQueuesUserRegisteredExactlyOnce exercises the reactivation
// guard where it can bite: from SUSPENDED, however many times, and for a
// student whose email verification already queued the event.
func TestReactivationQueuesUserRegisteredExactlyOnce(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t, ctx)
	users := NewPostgresRepository(pool)
	at := time.Now().UTC().Truncate(time.Second)

	cycle := func(t *testing.T, uid uuid.UUID, times int) {
		t.Helper()
		for i := range times {
			suspendedAt := at.Add(time.Duration(i) * time.Minute)
			if err := users.UpdateAccountStatusByID(ctx, uid, user.AccountStatusSuspended, suspendedAt); err != nil {
				t.Fatalf("suspend %d: %v", i+1, err)
			}
			if err := users.UpdateAccountStatusByID(ctx, uid, user.AccountStatusActive, suspendedAt); err != nil {
				t.Fatalf("reactivate %d: %v", i+1, err)
			}
		}
	}

	t.Run("suspended and reactivated twice", func(t *testing.T) {
		account := createIntegrationAccount(t, ctx, users, "reactivated-twice", user.AccountRoleStudent)
		cycle(t, account.UID, 2)
		if got := countRegistrationEvents(t, ctx, pool, account.UID); got != 1 {
			t.Fatalf("user.registered events = %d, want 1", got)
		}
	})

	t.Run("verified by email, then suspended and reactivated", func(t *testing.T) {
		account := integrationAccount("verified-then-reactivated", user.AccountRoleStudent)
		account.AccountStatus = user.AccountStatusUnverified
		if err := users.Create(ctx, account); err != nil {
			t.Fatalf("create account: %v", err)
		}
		if err := users.ActivateUser(ctx, account.Email); err != nil {
			t.Fatalf("activate: %v", err)
		}
		cycle(t, account.UID, 1)
		if got := countRegistrationEvents(t, ctx, pool, account.UID); got != 1 {
			t.Fatalf("user.registered events = %d, want 1", got)
		}
	})
}

func countRegistrationEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, uid uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE routing_key = 'user.registered' AND payload->>'userId' = $1`, uid.String()).Scan(&n); err != nil {
		t.Fatalf("count user.registered events: %v", err)
	}
	return n
}

type recordingSuspensionWriter struct{ calls int }

func (w *recordingSuspensionWriter) WriteSuspension(context.Context, uuid.UUID, time.Time, time.Duration) error {
	w.calls++
	return nil
}

type recordingSessionRepository struct{ calls int }

func (*recordingSessionRepository) CreateSession(context.Context, *user.Session) error { return nil }
func (*recordingSessionRepository) GetSessionByHash(context.Context, string) (*user.Session, error) {
	return nil, nil
}
func (*recordingSessionRepository) RotateSession(context.Context, string, *user.Session) error {
	return nil
}
func (*recordingSessionRepository) RevokeSessionByHash(context.Context, string) error { return nil }
func (s *recordingSessionRepository) RevokeAllUserSessions(context.Context, uuid.UUID) error {
	s.calls++
	return nil
}

func startPostgres(t *testing.T, ctx context.Context) string {
	t.Helper()
	request := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "user_service",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_USER":     "postgres",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(time.Minute),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: request,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Errorf("terminate PostgreSQL container: %v", err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get PostgreSQL host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("get PostgreSQL port: %v", err)
	}
	return fmt.Sprintf("postgres://postgres:postgres@%s:%s/user_service?sslmode=disable", host, port.Port())
}

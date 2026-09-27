//go:build integration

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-27
// Scope: Added PostgreSQL integration coverage for account-status transitions.
// Author review: PENDING — reviewer to complete

package repository

import (
	"context"
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

	if err := repository.UpdateAccountStatusByID(ctx, uuid.New(), user.AccountStatusSuspended, suspendedAt); err != user.ErrNotFound {
		t.Fatalf("unknown UID error = %v, want %v", err, user.ErrNotFound)
	}
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

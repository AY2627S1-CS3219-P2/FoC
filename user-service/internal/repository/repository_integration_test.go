//go:build integration

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-27
// Scope: Added real-PostgreSQL integration coverage for every user and session repository method.
// Author review: ZI YANG - validated correctness of tests

package repository

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRepositories(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t, ctx)
	users := NewPostgresRepository(pool)
	sessions := NewPostgresSessionRepository(pool)

	t.Run("user repository", func(t *testing.T) {
		t.Run("creates and finds accounts", func(t *testing.T) {
			resetIntegrationDatabase(t, ctx, pool)
			account := createIntegrationAccount(t, ctx, users, "lookup", user.AccountRoleStudent)

			byID, err := users.GetByID(ctx, account.UID)
			if err != nil {
				t.Fatalf("get account by ID: %v", err)
			}
			assertIntegrationAccount(t, byID, account)

			for name, identifier := range map[string]string{
				// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — verifies
				// case-insensitive retrieval for the recorded identifier policy.
				// Author review: PENDING.
				"username": "INTEGRATION-LOOKUP",
				"email":    "INTEGRATION-LOOKUP@EXAMPLE.COM",
			} {
				t.Run(name, func(t *testing.T) {
					got, err := users.GetByIdentifier(ctx, identifier)
					if err != nil {
						t.Fatalf("get account by %s: %v", name, err)
					}
					assertIntegrationAccount(t, got, account)
				})
			}

			if _, err := users.GetByID(ctx, uuid.New()); !errors.Is(err, user.ErrNotFound) {
				t.Fatalf("unknown ID error = %v, want ErrNotFound", err)
			}
			if _, err := users.GetByIdentifier(ctx, "missing@example.com"); !errors.Is(err, user.ErrNotFound) {
				t.Fatalf("unknown identifier error = %v, want ErrNotFound", err)
			}
		})

		t.Run("maps duplicate account fields", func(t *testing.T) {
			resetIntegrationDatabase(t, ctx, pool)
			createIntegrationAccount(t, ctx, users, "duplicate", user.AccountRoleStudent)

			duplicateEmail := integrationAccount("other-username", user.AccountRoleStudent)
			// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — verifies the
			// recorded case-insensitive email uniqueness. Author review: PENDING.
			duplicateEmail.Email = "INTEGRATION-DUPLICATE@EXAMPLE.COM"
			if err := users.Create(ctx, duplicateEmail); !errors.Is(err, user.ErrDuplicateEmail) {
				t.Fatalf("duplicate email error = %v, want ErrDuplicateEmail", err)
			}

			// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — verifies the
			// recorded case-insensitive username uniqueness. Author review: PENDING.
			duplicateUsername := integrationAccount("DUPLICATE", user.AccountRoleStudent)
			duplicateUsername.Email = "other-email@example.com"
			if err := users.Create(ctx, duplicateUsername); !errors.Is(err, user.ErrDuplicateUsername) {
				t.Fatalf("duplicate username error = %v, want ErrDuplicateUsername", err)
			}
		})

		t.Run("detects an administrator", func(t *testing.T) {
			resetIntegrationDatabase(t, ctx, pool)
			assertHasAdmin(t, ctx, users, false)
			createIntegrationAccount(t, ctx, users, "student", user.AccountRoleStudent)
			assertHasAdmin(t, ctx, users, false)
			createIntegrationAccount(t, ctx, users, "admin", user.AccountRoleAdmin)
			assertHasAdmin(t, ctx, users, true)
		})

		t.Run("updates mutable account fields", func(t *testing.T) {
			resetIntegrationDatabase(t, ctx, pool)
			account := createIntegrationAccount(t, ctx, users, "update", user.AccountRoleStudent)
			originalEmail := account.Email
			originalBoundary := account.TokensValidAfter
			originalLastLogin := account.LastLoginDate
			originalRole := account.AccountRole
			originalStatus := account.AccountStatus
			account.Username = "updated-username"
			account.Email = "must-not-change@example.com"
			account.PasswordHash = "updated-password-hash"
			account.PhoneNum = "+6587654321"
			changedLastLogin := time.Date(2026, 9, 27, 11, 45, 0, 123456000, time.UTC)
			account.LastLoginDate = &changedLastLogin
			account.AccountRole = user.AccountRoleAdmin
			account.AccountStatus = user.AccountStatusSuspended

			if err := users.Update(ctx, account); err != nil {
				t.Fatalf("update account: %v", err)
			}
			got, err := users.GetByID(ctx, account.UID)
			if err != nil {
				t.Fatalf("get updated account: %v", err)
			}
			if got.Username != account.Username || got.PasswordHash != account.PasswordHash || got.PhoneNum != account.PhoneNum {
				t.Fatalf("updated account fields = %#v, want values from %#v", got, account)
			}
			if got.LastLoginDate != originalLastLogin || got.AccountRole != originalRole || got.AccountStatus != originalStatus {
				t.Fatalf("protected fields = %#v, want original login/role/status", got)
			}
			if got.Email != originalEmail || !equalPostgresTime(got.TokensValidAfter, originalBoundary) {
				t.Fatalf("immutable email/boundary = %q/%v, want %q/%v", got.Email, got.TokensValidAfter, originalEmail, originalBoundary)
			}

			missing := integrationAccount("missing-update", user.AccountRoleStudent)
			missing.UID = uuid.New()
			if err := users.Update(ctx, missing); !errors.Is(err, user.ErrNotFound) {
				t.Fatalf("unknown account update error = %v, want ErrNotFound", err)
			}
		})
	})

	t.Run("session repository", func(t *testing.T) {
		t.Run("creates and finds sessions", func(t *testing.T) {
			resetIntegrationDatabase(t, ctx, pool)
			account := createIntegrationAccount(t, ctx, users, "session-create", user.AccountRoleStudent)
			session := integrationSession(account.UID, "create-hash", time.Time{})

			if err := sessions.CreateSession(ctx, session); err != nil {
				t.Fatalf("create session: %v", err)
			}
			got, err := sessions.GetSessionByHash(ctx, session.TokenHash)
			if err != nil {
				t.Fatalf("get session: %v", err)
			}
			assertIntegrationSession(t, got, session)

			if _, err := sessions.GetSessionByHash(ctx, "missing-hash"); !errors.Is(err, user.ErrSessionNotFound) {
				t.Fatalf("unknown session error = %v, want ErrSessionNotFound", err)
			}
			if err := sessions.CreateSession(ctx, session); err == nil {
				t.Fatal("duplicate session was accepted")
			}
		})

		t.Run("rotates a session atomically", func(t *testing.T) {
			resetIntegrationDatabase(t, ctx, pool)
			account := createIntegrationAccount(t, ctx, users, "session-rotate", user.AccountRoleStudent)
			oldSession := integrationSession(account.UID, "old-hash", time.Time{})
			if err := sessions.CreateSession(ctx, oldSession); err != nil {
				t.Fatalf("create old session: %v", err)
			}
			replacement := integrationSession(account.UID, "replacement-hash", oldSession.CreatedAt.Add(time.Minute))

			if err := sessions.RotateSession(ctx, oldSession.TokenHash, replacement); err != nil {
				t.Fatalf("rotate session: %v", err)
			}
			rotated, err := sessions.GetSessionByHash(ctx, oldSession.TokenHash)
			if err != nil {
				t.Fatalf("get rotated session: %v", err)
			}
			if rotated.RevokedAt == nil || rotated.ReplacedByTokenHash == nil || *rotated.ReplacedByTokenHash != replacement.TokenHash {
				t.Fatalf("rotated session = %#v, want revocation and replacement hash", rotated)
			}
			created, err := sessions.GetSessionByHash(ctx, replacement.TokenHash)
			if err != nil {
				t.Fatalf("get replacement session: %v", err)
			}
			assertIntegrationSession(t, created, replacement)

			replayReplacement := integrationSession(account.UID, "replay-hash", replacement.CreatedAt.Add(time.Minute))
			if err := sessions.RotateSession(ctx, oldSession.TokenHash, replayReplacement); !errors.Is(err, user.ErrSessionCompromised) {
				t.Fatalf("rotation replay error = %v, want ErrSessionCompromised", err)
			}
			if _, err := sessions.GetSessionByHash(ctx, replayReplacement.TokenHash); !errors.Is(err, user.ErrSessionNotFound) {
				t.Fatalf("rolled-back replay replacement error = %v, want ErrSessionNotFound", err)
			}
			if err := sessions.RotateSession(ctx, "missing-hash", replayReplacement); !errors.Is(err, user.ErrSessionNotFound) {
				t.Fatalf("missing rotation source error = %v, want ErrSessionNotFound", err)
			}
		})

		t.Run("revokes one session", func(t *testing.T) {
			resetIntegrationDatabase(t, ctx, pool)
			account := createIntegrationAccount(t, ctx, users, "session-revoke", user.AccountRoleStudent)
			session := integrationSession(account.UID, "revoke-hash", time.Time{})
			if err := sessions.CreateSession(ctx, session); err != nil {
				t.Fatalf("create session: %v", err)
			}

			if err := sessions.RevokeSessionByHash(ctx, session.TokenHash); err != nil {
				t.Fatalf("revoke session: %v", err)
			}
			revoked, err := sessions.GetSessionByHash(ctx, session.TokenHash)
			if err != nil {
				t.Fatalf("get revoked session: %v", err)
			}
			if revoked.RevokedAt == nil {
				t.Fatal("revoked session has no revoked_at timestamp")
			}
			if err := sessions.RevokeSessionByHash(ctx, session.TokenHash); !errors.Is(err, user.ErrSessionNotFound) {
				t.Fatalf("second revocation error = %v, want ErrSessionNotFound", err)
			}
			if err := sessions.RevokeSessionByHash(ctx, "missing-hash"); !errors.Is(err, user.ErrSessionNotFound) {
				t.Fatalf("unknown revocation error = %v, want ErrSessionNotFound", err)
			}
		})

		t.Run("revokes all active sessions for one account", func(t *testing.T) {
			resetIntegrationDatabase(t, ctx, pool)
			target := createIntegrationAccount(t, ctx, users, "revoke-all-target", user.AccountRoleStudent)
			other := createIntegrationAccount(t, ctx, users, "revoke-all-other", user.AccountRoleStudent)
			active := integrationSession(target.UID, "target-active", time.Time{})
			alreadyRevoked := integrationSession(target.UID, "target-revoked", active.CreatedAt.Add(time.Minute))
			otherActive := integrationSession(other.UID, "other-active", time.Time{})
			for _, session := range []*user.Session{active, alreadyRevoked, otherActive} {
				if err := sessions.CreateSession(ctx, session); err != nil {
					t.Fatalf("create session %q: %v", session.TokenHash, err)
				}
			}
			if err := sessions.RevokeSessionByHash(ctx, alreadyRevoked.TokenHash); err != nil {
				t.Fatalf("pre-revoke session: %v", err)
			}
			before, err := sessions.GetSessionByHash(ctx, alreadyRevoked.TokenHash)
			if err != nil {
				t.Fatalf("get pre-revoked session: %v", err)
			}

			if err := sessions.RevokeAllUserSessions(ctx, target.UID); err != nil {
				t.Fatalf("revoke all sessions: %v", err)
			}
			gotActive, err := sessions.GetSessionByHash(ctx, active.TokenHash)
			if err != nil {
				t.Fatalf("get newly revoked session: %v", err)
			}
			gotAlreadyRevoked, err := sessions.GetSessionByHash(ctx, alreadyRevoked.TokenHash)
			if err != nil {
				t.Fatalf("get already-revoked session: %v", err)
			}
			gotOther, err := sessions.GetSessionByHash(ctx, otherActive.TokenHash)
			if err != nil {
				t.Fatalf("get other account session: %v", err)
			}
			if gotActive.RevokedAt == nil {
				t.Fatal("target account active session was not revoked")
			}
			if gotAlreadyRevoked.RevokedAt == nil || before.RevokedAt == nil || !gotAlreadyRevoked.RevokedAt.Equal(*before.RevokedAt) {
				t.Fatal("existing revocation timestamp was changed")
			}
			if gotOther.RevokedAt != nil {
				t.Fatal("other account session was revoked")
			}
			if err := sessions.RevokeAllUserSessions(ctx, uuid.New()); err != nil {
				t.Fatalf("revoke sessions for unknown account: %v", err)
			}
		})
	})
}

func newIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	databaseURL := startPostgres(t, ctx)
	migrationsPath, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	if err := Apply(databaseURL, (&url.URL{Scheme: "file", Path: migrationsPath}).String()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func resetIntegrationDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, "TRUNCATE TABLE sessions, users"); err != nil {
		t.Fatalf("reset integration database: %v", err)
	}
}

func integrationAccount(suffix string, role user.AccountRole) *user.User {
	return &user.User{
		Username:      "integration-" + suffix,
		Email:         "integration-" + suffix + "@example.com",
		PasswordHash:  "hashed-password",
		PhoneNum:      "+6512345678",
		AccountRole:   role,
		AccountStatus: user.AccountStatusActive,
	}
}

func createIntegrationAccount(t *testing.T, ctx context.Context, repository *PostgresRepository, suffix string, role user.AccountRole) *user.User {
	t.Helper()
	account := integrationAccount(suffix, role)
	if err := repository.Create(ctx, account); err != nil {
		t.Fatalf("create integration account %q: %v", suffix, err)
	}
	return account
}

func assertIntegrationAccount(t *testing.T, got, want *user.User) {
	t.Helper()
	if got.UID != want.UID || got.Username != want.Username || got.Email != want.Email || got.PasswordHash != want.PasswordHash || got.PhoneNum != want.PhoneNum {
		t.Fatalf("account = %#v, want %#v", got, want)
	}
	if !equalPostgresTime(got.DateCreated, want.DateCreated) || !equalPostgresTime(got.TokensValidAfter, want.TokensValidAfter) {
		t.Fatalf("account timestamps = %v/%v, want %v/%v", got.DateCreated, got.TokensValidAfter, want.DateCreated, want.TokensValidAfter)
	}
	if got.AccountRole != want.AccountRole || got.AccountStatus != want.AccountStatus {
		t.Fatalf("account role/status = %q/%q, want %q/%q", got.AccountRole, got.AccountStatus, want.AccountRole, want.AccountStatus)
	}
}

func equalPostgresTime(got, want time.Time) bool {
	return got.Equal(want.Truncate(time.Microsecond))
}

func assertHasAdmin(t *testing.T, ctx context.Context, repository *PostgresRepository, want bool) {
	t.Helper()
	got, err := repository.HasAdmin(ctx)
	if err != nil {
		t.Fatalf("check for administrator: %v", err)
	}
	if got != want {
		t.Fatalf("HasAdmin() = %t, want %t", got, want)
	}
}

func integrationSession(uid uuid.UUID, hash string, createdAt time.Time) *user.Session {
	if createdAt.IsZero() {
		createdAt = time.Date(2026, 9, 27, 12, 0, 0, 123456000, time.UTC)
	}
	return &user.Session{
		JTI:       uuid.New(),
		CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(7 * 24 * time.Hour),
		TokenHash: hash,
		UID:       uid,
	}
}

func assertIntegrationSession(t *testing.T, got, want *user.Session) {
	t.Helper()
	if got.JTI != want.JTI || got.UID != want.UID || got.TokenHash != want.TokenHash {
		t.Fatalf("session identifiers = %#v, want %#v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("session timestamps = %v/%v, want %v/%v", got.CreatedAt, got.ExpiresAt, want.CreatedAt, want.ExpiresAt)
	}
	if got.RevokedAt != nil || got.ReplacedByTokenHash != nil {
		t.Fatalf("new session revocation fields = %#v, want nil", got)
	}
}

//go:build integration

// AI Assistance Disclosure:
// Tool: Codex (model: GPT-6), date: 2026-11-09
// Scope: PostgreSQL/Redis regression coverage for password-change invalidation.
// Author review: ZI YANG — validated correctness

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"foc/user-service/internal/hash"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPasswordChangeInvalidatesAllSessions(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t, ctx)
	client := passwordInvalidationRedis(t, ctx)
	users := NewPostgresRepository(pool)
	sessions := NewPostgresSessionRepository(pool)
	reader := NewRedisBlocklistReader(client)
	writer := NewRedisBlocklistWriter(client)
	for _, mode := range []string{"self", "admin"} {
		t.Run(mode, func(t *testing.T) {
			account := createIntegrationAccount(t, ctx, users, "password-"+mode, user.AccountRoleStudent)
			other := createIntegrationAccount(t, ctx, users, "unaffected-"+mode, user.AccountRoleStudent)
			var err error
			account.PasswordHash, err = hash.HashPassword("OldPass1")
			if err != nil {
				t.Fatal(err)
			}
			if err := users.Update(ctx, account); err != nil {
				t.Fatal(err)
			}
			at := time.Now().UTC().Truncate(time.Second)
			first := integrationSession(account.UID, "first-"+mode, at.Add(-time.Minute))
			second := integrationSession(account.UID, "second-"+mode, at)
			unaffected := integrationSession(other.UID, "unaffected-"+mode, at)
			for _, s := range []*user.Session{first, second, unaffected} {
				if err := sessions.CreateSession(ctx, s); err != nil {
					t.Fatal(err)
				}
			}
			ttl := 15*time.Minute + 5*time.Second
			svc := user.NewAccountServiceWithClock(users, writer, sessions, ttl, func() time.Time { return at })
			if mode == "self" {
				_, err = svc.UpdateProfile(ctx, account.UID, "", "", "OldPass1", "NewPass1")
			} else {
				_, err = svc.AdminUpdateProfile(ctx, integrationAdmin, account.UID, "", "", "NewPass1")
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				session     *user.Session
				wantRevoked bool
			}{
				{first, true}, {second, true}, {unaffected, false},
			} {
				stored, err := sessions.GetSessionByHash(ctx, tc.session.TokenHash)
				if err != nil {
					t.Fatal(err)
				}
				if (stored.RevokedAt != nil) != tc.wantRevoked {
					t.Fatalf("refresh session %s: revoked = %t, want %t", stored.TokenHash, stored.RevokedAt != nil, tc.wantRevoked)
				}
				// Each access token has its own JTI; readers invalidate all of
				// them using the verified subject and issue time.
				revoked, err := reader.IsRevoked(ctx, uuid.New(), stored.UID, stored.CreatedAt)
				if err != nil || revoked != tc.wantRevoked {
					t.Fatalf("access revocation = %t, %v; want %t", revoked, err, tc.wantRevoked)
				}
			}
			stored, err := users.GetByID(ctx, account.UID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.AccountStatus != user.AccountStatusActive || !stored.TokensValidAfter.Equal(at) || !hash.CheckPassword(stored.PasswordHash, "NewPass1") {
				t.Fatal("password change did not persist password/cutoff with the account still active")
			}
			if revoked, err := reader.IsRevoked(ctx, uuid.New(), account.UID, at.Add(time.Second)); err != nil || revoked {
				t.Fatalf("token issued after the cutoff: revoked = %t, %v", revoked, err)
			}
			if remaining := client.TTL(ctx, suspensionKeyPrefix+account.UID.String()).Val(); remaining <= ttl-time.Minute || remaining > ttl {
				t.Fatalf("cutoff key TTL = %s, want access lifetime plus skew", remaining)
			}
		})
	}
}

func passwordInvalidationRedis(t *testing.T, ctx context.Context) *redis.Client {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Errorf("terminate Redis: %v", err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("%s:%s", host, port.Port())})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

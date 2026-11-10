//go:build integration

// AI Assistance Disclosure:
// Tool: Codex (GPT-6), date: 2026-11-09
// Scope: Role HTTP workflow over real PostgreSQL/Redis, token/session revocation
// and monotonic Redis cutoff regression tests.
// Author review: ZI YANG - reviewed and verified

package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/router"
	"foc/user-service/internal/httpapi/routes"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
)

type roleHTTPVerifier struct{ principal handlers.Principal }

func (v roleHTTPVerifier) Verify(context.Context, string) (handlers.Principal, error) {
	return v.principal, nil
}

type roleHTTPRevocations struct{ reader *RedisBlocklistReader }

func (r roleHTTPRevocations) Check(ctx context.Context, p handlers.Principal) (bool, error) {
	return r.reader.IsRevoked(ctx, p.JTI, p.UserID, p.IssuedAt)
}

func TestRoleHTTPWorkflowAndRedisCutoffs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := newIntegrationPool(t, ctx)
	client := passwordInvalidationRedis(t, ctx)
	users := NewPostgresRepository(pool)
	sessions := NewPostgresSessionRepository(pool)
	writer := NewRedisBlocklistWriter(client)
	reader := NewRedisBlocklistReader(client)
	actor := createIntegrationAccount(t, ctx, users, "http-owner", user.AccountRoleAdmin)
	target := createIntegrationAccount(t, ctx, users, "http-student", user.AccountRoleStudent)
	suspended := createIntegrationAccount(t, ctx, users, "http-suspended", user.AccountRoleStudent)
	if err := users.UpdateAccountStatusByID(ctx, suspended.UID, user.AccountStatusSuspended, time.Now()); err != nil {
		t.Fatal(err)
	}
	issuedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	svc := user.NewAccountService(users, writer, sessions, 15*time.Minute+5*time.Second)
	makeRouter := func(id uuid.UUID, role user.AccountRole) http.Handler {
		return router.Setup(routes.Dependencies{Admin: handlers.AdminDependencies{RoleUpdater: svc}, TokenVerifier: roleHTTPVerifier{handlers.Principal{UserID: id, Role: role, JTI: uuid.New(), IssuedAt: issuedAt}}, RevocationChecker: roleHTTPRevocations{reader}})
	}
	change := func(h http.Handler, id uuid.UUID, role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+id.String()+"/role", strings.NewReader(`{"role":"`+role+`"}`))
		req.Header.Set("Authorization", "Bearer verified-test-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	ownerRouter := makeRouter(actor.UID, user.AccountRoleAdmin)
	if rec := change(ownerRouter, suspended.UID, "ADMIN"); rec.Code != 409 {
		t.Fatalf("suspended promotion = %d %s", rec.Code, rec.Body.String())
	}
	if roleOf(t, ctx, users, suspended.UID) != user.AccountRoleStudent {
		t.Fatal("suspended student promoted")
	}
	for _, role := range []string{"ADMIN", "STUDENT"} {
		pending := integrationSession(target.UID, "http-session-"+role, issuedAt)
		if err := sessions.CreateSession(ctx, pending); err != nil {
			t.Fatal(err)
		}
		if rec := change(ownerRouter, target.UID, role); rec.Code != 204 {
			t.Fatalf("role %s = %d %s", role, rec.Code, rec.Body.String())
		}
		stored, err := sessions.GetSessionByHash(ctx, pending.TokenHash)
		if err != nil || stored.RevokedAt == nil {
			t.Fatalf("refresh not revoked: %+v %v", stored, err)
		}
		revoked, err := reader.IsRevoked(ctx, uuid.New(), target.UID, issuedAt)
		if err != nil || !revoked {
			t.Fatalf("old access token accepted: %t %v", revoked, err)
		}
	}
	// A JWT that still claims ADMIN is rejected by the existing middleware.
	if rec := change(makeRouter(target.UID, user.AccountRoleAdmin), actor.UID, "STUDENT"); rec.Code != 401 {
		t.Fatalf("demoted access token accepted: %d %s", rec.Code, rec.Body.String())
	}
	if rec := change(ownerRouter, actor.UID, "STUDENT"); rec.Code != 403 {
		t.Fatalf("self demotion = %d %s", rec.Code, rec.Body.String())
	}
	audit, err := svc.AuditLog(ctx, user.AuditFilter{Action: user.ActionChangeRole, TargetID: target.UID.String(), Limit: 10})
	if err != nil || audit.Total != 2 {
		t.Fatalf("role audit = %+v %v", audit, err)
	}
	if count, err := users.CountActiveAdmins(ctx); err != nil || count != 1 {
		t.Fatalf("active admins=%d %v", count, err)
	}

	t.Run("older invalidation cannot lower cutoff or TTL", func(t *testing.T) {
		uid := uuid.New()
		key := suspensionKeyPrefix + uid.String()
		newer := time.Now().UTC().Truncate(time.Second).Add(10 * time.Second)
		older := newer.Add(-time.Second)
		if err := writer.WriteSuspension(ctx, uid, newer, 2*time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteSuspension(ctx, uid, older, time.Minute); err != nil {
			t.Fatal(err)
		}
		got, err := client.Get(ctx, key).Result()
		if err != nil || got != newer.Format(time.RFC3339Nano) {
			t.Fatalf("cutoff regressed: %s %v", got, err)
		}
		if remaining := client.PTTL(ctx, key).Val(); remaining < 110*time.Second {
			t.Fatalf("TTL shortened: %s", remaining)
		}
		// Optional fractional seconds must compare numerically, including .1 vs .09.
		fractional := newer.Add(100 * time.Millisecond)
		if err := writer.WriteSuspension(ctx, uid, fractional, time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteSuspension(ctx, uid, newer.Add(90*time.Millisecond), time.Minute); err != nil {
			t.Fatal(err)
		}
		got, err = client.Get(ctx, key).Result()
		if err != nil || got != fractional.Format(time.RFC3339Nano) {
			t.Fatalf("fractional cutoff regressed: %s %v", got, err)
		}
	})
	t.Run("malformed existing cutoff stays fail closed", func(t *testing.T) {
		uid := uuid.New()
		key := suspensionKeyPrefix + uid.String()
		if err := client.Set(ctx, key, "malformed", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteSuspension(ctx, uid, time.Now().UTC(), time.Minute); err == nil {
			t.Fatal("malformed cutoff overwritten")
		}
		revoked, err := reader.IsRevoked(ctx, uuid.New(), uid, time.Now())
		if err != nil || !revoked {
			t.Fatalf("malformed key accepted: %t %v", revoked, err)
		}
	})
}

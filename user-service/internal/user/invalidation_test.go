// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added focused tests for Redis-first logout ordering and TTL behavior.
// Author review: PENDING — reviewer to complete

package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeBlocklistWriter struct {
	err       error
	jti       uuid.UUID
	ttl       time.Duration
	called    bool
	callOrder *[]string
}

func (f *fakeBlocklistWriter) BlockAccessToken(_ context.Context, jti uuid.UUID, ttl time.Duration) error {
	f.called = true
	f.jti = jti
	f.ttl = ttl
	if f.callOrder != nil {
		*f.callOrder = append(*f.callOrder, "redis")
	}
	return f.err
}

type orderedSessionRepository struct {
	fakeSessionRepository
	callOrder *[]string
	err       error
}

func (f *orderedSessionRepository) RevokeSessionByHash(ctx context.Context, hash string) error {
	if f.callOrder != nil {
		*f.callOrder = append(*f.callOrder, "postgres")
	}
	if f.err != nil {
		return f.err
	}
	return f.fakeSessionRepository.RevokeSessionByHash(ctx, hash)
}

func TestLogoutServiceBlocksAccessBeforeRevokingRefreshSession(t *testing.T) {
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	order := []string{}
	sessions := &orderedSessionRepository{callOrder: &order}
	blocklist := &fakeBlocklistWriter{callOrder: &order}
	service := NewLogoutService(sessions, blocklist, func() time.Time { return now })
	jti := uuid.New()

	if err := service.Logout(context.Background(), AccessTokenClaims{JTI: jti, ExpiresAt: now.Add(15 * time.Minute)}, "refresh"); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "redis" || order[1] != "postgres" {
		t.Fatalf("call order = %#v, want redis then postgres", order)
	}
	if blocklist.jti != jti || blocklist.ttl != 15*time.Minute {
		t.Fatalf("blocklist request = %#v, want exact remaining TTL", blocklist)
	}
}

func TestLogoutServiceDoesNotRevokeSessionWhenRedisFails(t *testing.T) {
	redisFailure := errors.New("redis unavailable")
	order := []string{}
	sessions := &orderedSessionRepository{callOrder: &order}
	blocklist := &fakeBlocklistWriter{err: redisFailure, callOrder: &order}
	service := NewLogoutService(sessions, blocklist, time.Now)

	err := service.Logout(context.Background(), AccessTokenClaims{JTI: uuid.New(), ExpiresAt: time.Now().Add(time.Minute)}, "refresh")
	if !errors.Is(err, redisFailure) {
		t.Fatalf("error = %v, want Redis failure", err)
	}
	if len(order) != 1 || order[0] != "redis" {
		t.Fatalf("call order = %#v, want Redis only", order)
	}
}

func TestLogoutServiceRejectsExpiredAccessTokenWithoutSideEffects(t *testing.T) {
	order := []string{}
	sessions := &orderedSessionRepository{callOrder: &order}
	blocklist := &fakeBlocklistWriter{callOrder: &order}
	service := NewLogoutService(sessions, blocklist, func() time.Time { return time.Unix(100, 0) })

	if err := service.Logout(context.Background(), AccessTokenClaims{JTI: uuid.New(), ExpiresAt: time.Unix(99, 0)}, "refresh"); err == nil {
		t.Fatal("expired access token was accepted")
	}
	if len(order) != 0 {
		t.Fatalf("call order = %#v, want no side effects", order)
	}
}

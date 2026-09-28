// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added focused tests for Redis-first logout ordering and TTL behavior.
// Author review: Repackaed and validated correctness

package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"foc/user-service/internal/user"

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

func (f *orderedSessionRepository) GetSessionByHash(ctx context.Context, hash string) (*user.Session, error) {
	if f.callOrder != nil {
		*f.callOrder = append(*f.callOrder, "postgres-read")
	}
	return f.fakeSessionRepository.GetSessionByHash(ctx, hash)
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
	userID := uuid.New()
	refreshToken := "refresh"
	sessions := &orderedSessionRepository{fakeSessionRepository: fakeSessionRepository{sessions: map[string]*user.Session{HashRefreshToken(refreshToken): {UID: userID}}}, callOrder: &order}
	blocklist := &fakeBlocklistWriter{callOrder: &order}
	service := NewLogoutService(sessions, blocklist, func() time.Time { return now })
	jti := uuid.New()

	if err := service.Logout(context.Background(), AccessTokenClaims{UserID: userID, JTI: jti, ExpiresAt: now.Add(15 * time.Minute)}, refreshToken); err != nil {
		t.Fatal(err)
	}
	if got, want := order, []string{"postgres-read", "redis", "postgres"}; !equalStrings(got, want) {
		t.Fatalf("call order = %#v, want %#v", got, want)
	}
	if blocklist.jti != jti || blocklist.ttl != 15*time.Minute {
		t.Fatalf("blocklist request = %#v, want exact remaining TTL", blocklist)
	}
}

// AI-generated (edited by ZI YANG): a refresh session must belong to the verified access-token subject.
func TestLogoutServiceRejectsRefreshSessionOwnedByAnotherAccount(t *testing.T) {
	now := time.Date(2026, 9, 27, 21, 30, 0, 0, time.UTC)
	order := []string{}
	ownerID := uuid.New()
	sessions := &orderedSessionRepository{fakeSessionRepository: fakeSessionRepository{sessions: map[string]*user.Session{HashRefreshToken("other-user-refresh"): {UID: ownerID}}}, callOrder: &order}
	blocklist := &fakeBlocklistWriter{callOrder: &order}
	service := NewLogoutService(sessions, blocklist, func() time.Time { return now })

	err := service.Logout(context.Background(), AccessTokenClaims{UserID: uuid.New(), JTI: uuid.New(), ExpiresAt: now.Add(time.Minute)}, "other-user-refresh")
	if !errors.Is(err, ErrSessionOwnershipMismatch) {
		t.Fatalf("error = %v, want ErrSessionOwnershipMismatch", err)
	}
	if got, want := order, []string{"postgres-read"}; !equalStrings(got, want) {
		t.Fatalf("call order = %#v, want %#v", got, want)
	}
	if blocklist.called {
		t.Fatal("mismatched session must not block the access token")
	}
}

// AI-generated (edited by PENDING): missing and already-revoked refresh sessions are recorded idempotent logout successes.
func TestLogoutServiceTreatsMissingAndRevokedSessionsAsIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 15, 0, 0, time.UTC)
	userID := uuid.New()
	for name, sessions := range map[string]*orderedSessionRepository{
		"missing session error":   {fakeSessionRepository: fakeSessionRepository{getErr: user.ErrSessionNotFound}},
		"already revoked session": {fakeSessionRepository: fakeSessionRepository{sessions: map[string]*user.Session{HashRefreshToken("refresh"): {UID: userID, RevokedAt: &now}}}},
	} {
		t.Run(name, func(t *testing.T) {
			order := []string{}
			sessions.callOrder = &order
			blocklist := &fakeBlocklistWriter{callOrder: &order}
			err := NewLogoutService(sessions, blocklist, func() time.Time { return now }).Logout(context.Background(), AccessTokenClaims{UserID: userID, JTI: uuid.New(), ExpiresAt: now.Add(time.Minute)}, "refresh")
			if err != nil {
				t.Fatalf("Logout() error = %v, want nil", err)
			}
			if got, want := order, []string{"postgres-read"}; !equalStrings(got, want) {
				t.Fatalf("call order = %#v, want %#v", got, want)
			}
		})
	}
}

func TestLogoutServiceDoesNotRevokeSessionWhenRedisFails(t *testing.T) {
	redisFailure := errors.New("redis unavailable")
	order := []string{}
	userID := uuid.New()
	refreshToken := "refresh"
	sessions := &orderedSessionRepository{fakeSessionRepository: fakeSessionRepository{sessions: map[string]*user.Session{HashRefreshToken(refreshToken): {UID: userID}}}, callOrder: &order}
	blocklist := &fakeBlocklistWriter{err: redisFailure, callOrder: &order}
	service := NewLogoutService(sessions, blocklist, time.Now)

	err := service.Logout(context.Background(), AccessTokenClaims{UserID: userID, JTI: uuid.New(), ExpiresAt: time.Now().Add(time.Minute)}, refreshToken)
	if !errors.Is(err, redisFailure) {
		t.Fatalf("error = %v, want Redis failure", err)
	}
	if got, want := order, []string{"postgres-read", "redis"}; !equalStrings(got, want) {
		t.Fatalf("call order = %#v, want %#v", got, want)
	}
}

func TestLogoutServiceRejectsExpiredAccessTokenWithoutSideEffects(t *testing.T) {
	order := []string{}
	sessions := &orderedSessionRepository{callOrder: &order}
	blocklist := &fakeBlocklistWriter{callOrder: &order}
	service := NewLogoutService(sessions, blocklist, func() time.Time { return time.Unix(100, 0) })

	if err := service.Logout(context.Background(), AccessTokenClaims{UserID: uuid.New(), JTI: uuid.New(), ExpiresAt: time.Unix(99, 0)}, "refresh"); err == nil {
		t.Fatal("expired access token was accepted")
	}
	if len(order) != 0 {
		t.Fatalf("call order = %#v, want no side effects", order)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Implemented the recorded Redis-first logout invalidation boundary.
// Author review: Repackaged and validated correctness

package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"foc/user-service/internal/user"

	"github.com/google/uuid"
)

// AI-generated (edited by ZI YANG): removes implementation-boundary rationale
// and describes the claims required by the logout operation.
// AccessTokenClaims holds the access-token JTI and expiry that logout needs.
type AccessTokenClaims struct {
	UserID    uuid.UUID
	JTI       uuid.UUID
	ExpiresAt time.Time
}

// ErrSessionOwnershipMismatch indicates that a refresh session belongs to a
// different account than the verified access token.
var ErrSessionOwnershipMismatch = errors.New("mismatched session")

const minimumLogoutTTL = 5 * time.Second

// AccessTokenVerifier verifies a bearer access token and returns only the
// claims required for logout invalidation.
type AccessTokenVerifier interface {
	VerifyAccess(ctx context.Context, rawToken string) (AccessTokenClaims, error)
}

// BlocklistWriter blocks an access token's JTI for a given TTL.
type BlocklistWriter interface {
	BlockAccessToken(ctx context.Context, jti uuid.UUID, ttl time.Duration) error
}

// LogoutService ends a session: it blocks the access token and revokes the
// refresh token.
type LogoutService struct {
	sessions  user.SessionRepository
	blocklist BlocklistWriter
	now       func() time.Time
}

// NewLogoutService constructs a logout service with its persistence and
// invalidation boundaries ready for use.
func NewLogoutService(sessions user.SessionRepository, blocklist BlocklistWriter, now func() time.Time) *LogoutService {
	if now == nil {
		now = time.Now
	}
	return &LogoutService{sessions: sessions, blocklist: blocklist, now: now}
}

// Logout blocks the access-token JTI with its remaining lifetime, or the
// recorded clock-skew minimum, then revokes the hashed refresh token.
// PostgreSQL is not touched if Redis fails.
func (s *LogoutService) Logout(ctx context.Context, access AccessTokenClaims, refreshToken string) error {
	if s.sessions == nil || s.blocklist == nil {
		return errors.New("logout dependencies are required")
	}
	if access.UserID == uuid.Nil || access.JTI == uuid.Nil || access.ExpiresAt.IsZero() {
		return errors.New("access token claims are required")
	}
	ttl := access.ExpiresAt.Sub(s.now())
	// AI-generated (edited by ZI YANG): retain blocklisting through the recorded JWT clock-skew window.
	if ttl <= 0 {
		ttl = minimumLogoutTTL
	}
	// AI-generated (edited by ZI YANG): verify session ownership before Redis invalidation or revocation.
	refreshSession, err := s.sessions.GetSessionByHash(ctx, HashRefreshToken(refreshToken))
	if errors.Is(err, user.ErrSessionNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get refresh session for logout: %w", err)
	}
	if refreshSession == nil || refreshSession.RevokedAt != nil {
		return nil
	}
	if refreshSession.UID != access.UserID {
		return ErrSessionOwnershipMismatch
	}
	if err := s.blocklist.BlockAccessToken(ctx, access.JTI, ttl); err != nil {
		return fmt.Errorf("block access token: %w", err)
	}
	if err := s.sessions.RevokeSessionByHash(ctx, HashRefreshToken(refreshToken)); err != nil {
		return fmt.Errorf("revoke refresh session: %w", err)
	}
	return nil
}

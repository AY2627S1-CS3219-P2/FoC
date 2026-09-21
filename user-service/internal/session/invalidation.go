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

// AccessTokenClaims contains the access-token data required for logout
// invalidation without exposing JWT library types to the domain service.
type AccessTokenClaims struct {
	JTI       uuid.UUID
	ExpiresAt time.Time
}

// AccessTokenVerifier verifies a bearer access token and returns only the
// claims required for logout invalidation.
type AccessTokenVerifier interface {
	VerifyAccess(ctx context.Context, rawToken string) (AccessTokenClaims, error)
}

// BlocklistWriter is the sole write boundary for access-token invalidation.
type BlocklistWriter interface {
	BlockAccessToken(ctx context.Context, jti uuid.UUID, ttl time.Duration) error
}

// LogoutService invalidates an access token in Redis before revoking its
// refresh session in PostgreSQL, as recorded by the service requirements.
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

// Logout blocks the access-token JTI with its exact remaining lifetime, then
// revokes the hashed refresh token. PostgreSQL is not touched if Redis fails.
func (s *LogoutService) Logout(ctx context.Context, access AccessTokenClaims, refreshToken string) error {
	if s.sessions == nil || s.blocklist == nil {
		return errors.New("logout dependencies are required")
	}
	if access.JTI == uuid.Nil || access.ExpiresAt.IsZero() {
		return errors.New("access token claims are required")
	}
	ttl := access.ExpiresAt.Sub(s.now())
	if ttl <= 0 {
		return errors.New("access token is expired")
	}
	if err := s.blocklist.BlockAccessToken(ctx, access.JTI, ttl); err != nil {
		return fmt.Errorf("block access token: %w", err)
	}
	if err := s.sessions.RevokeSessionByHash(ctx, HashRefreshToken(refreshToken)); err != nil {
		return fmt.Errorf("revoke refresh session: %w", err)
	}
	return nil
}

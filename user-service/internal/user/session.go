// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Implemented the recorded login and refresh session application services.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RefreshClaims contains the verified identity and session claims required by
// the refresh flow.
type RefreshClaims struct {
	UserID    uuid.UUID
	JTI       uuid.UUID
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// RefreshTokenVerifier verifies a refresh token without exposing JWT vendor
// types to the domain service.
type RefreshTokenVerifier interface {
	VerifyRefresh(ctx context.Context, rawToken string) (RefreshClaims, error)
}

// LoginService authenticates an account and records its refresh session.
type LoginService struct {
	authenticator *Authenticator
	sessions      SessionRepository
	now           func() time.Time
}

// NewLoginService constructs a login service with its persistence and token
// dependencies ready for use.
func NewLoginService(repository UserRepository, sessions SessionRepository, issuer TokenIssuer, now func() time.Time) *LoginService {
	if now == nil {
		now = time.Now
	}
	return &LoginService{
		authenticator: NewAuthenticator(repository, issuer),
		sessions:      sessions,
		now:           now,
	}
}

// Login verifies credentials, issues tokens, and stores the hashed refresh
// token session.
func (s *LoginService) Login(ctx context.Context, identifier, password string) (TokenPair, error) {
	if s.sessions == nil {
		return TokenPair{}, errors.New("session repository is required")
	}

	account, err := s.authenticator.repository.GetByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return TokenPair{}, ErrInvalidCredentials
		}
		return TokenPair{}, fmt.Errorf("look up credentials: %w", err)
	}
	if account == nil || !CheckPassword(account.PasswordHash, password) {
		return TokenPair{}, ErrInvalidCredentials
	}
	if account.AccountStatus == AccountStatusSuspended {
		return TokenPair{}, ErrAccountSuspended
	}

	pair, err := s.authenticator.issuer.Issue(ctx, account)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue JWT session: %w", err)
	}
	if pair.RefreshJTI == uuid.Nil || pair.RefreshExpiresAt.IsZero() {
		return TokenPair{}, errors.New("issuer returned incomplete refresh metadata")
	}

	now := s.now().UTC()
	session := &Session{
		JTI:       pair.RefreshJTI,
		CreatedAt: now,
		ExpiresAt: pair.RefreshExpiresAt,
		TokenHash: HashRefreshToken(pair.RefreshToken),
		UID:       account.UID,
	}
	if err := s.sessions.CreateSession(ctx, session); err != nil {
		return TokenPair{}, fmt.Errorf("create refresh session: %w", err)
	}
	return pair, nil
}

// RefreshService rotates refresh sessions and detects replayed credentials.
type RefreshService struct {
	repository UserRepository
	sessions   SessionRepository
	verifier   RefreshTokenVerifier
	issuer     TokenIssuer
	now        func() time.Time
}

// NewRefreshService constructs a refresh service with injected token and
// persistence boundaries.
func NewRefreshService(repository UserRepository, sessions SessionRepository, verifier RefreshTokenVerifier, issuer TokenIssuer, now func() time.Time) *RefreshService {
	if now == nil {
		now = time.Now
	}
	return &RefreshService{repository: repository, sessions: sessions, verifier: verifier, issuer: issuer, now: now}
}

// Refresh verifies and rotates a refresh session. A previously revoked or
// replaced token revokes every session belonging to the account.
func (s *RefreshService) Refresh(ctx context.Context, rawToken string) (TokenPair, error) {
	if s.repository == nil || s.sessions == nil || s.verifier == nil || s.issuer == nil {
		return TokenPair{}, errors.New("refresh dependencies are required")
	}
	claims, err := s.verifier.VerifyRefresh(ctx, rawToken)
	if err != nil {
		return TokenPair{}, fmt.Errorf("verify refresh token: %w", err)
	}
	if claims.UserID == uuid.Nil || claims.JTI == uuid.Nil {
		return TokenPair{}, ErrSessionNotFound
	}

	oldHash := HashRefreshToken(rawToken)
	session, err := s.sessions.GetSessionByHash(ctx, oldHash)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return TokenPair{}, ErrSessionNotFound
		}
		return TokenPair{}, fmt.Errorf("get refresh session: %w", err)
	}
	if session == nil || session.UID != claims.UserID || session.JTI != claims.JTI {
		return TokenPair{}, ErrSessionNotFound
	}
	if session.RevokedAt != nil || session.ReplacedByTokenHash != nil {
		if err := s.sessions.RevokeAllUserSessions(ctx, session.UID); err != nil {
			return TokenPair{}, fmt.Errorf("revoke compromised user sessions: %w", err)
		}
		return TokenPair{}, ErrSessionCompromised
	}
	if !session.ExpiresAt.After(s.now()) {
		return TokenPair{}, ErrSessionNotFound
	}

	account, err := s.repository.GetByID(ctx, claims.UserID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("get refresh account: %w", err)
	}
	if account == nil || account.AccountStatus == AccountStatusSuspended || (!account.TokensValidAfter.IsZero() && claims.IssuedAt.Before(account.TokensValidAfter)) {
		return TokenPair{}, ErrAccountSuspended
	}

	pair, err := s.issuer.Issue(ctx, account)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue rotated JWT session: %w", err)
	}
	if pair.RefreshJTI == uuid.Nil || pair.RefreshExpiresAt.IsZero() {
		return TokenPair{}, errors.New("issuer returned incomplete refresh metadata")
	}
	newSession := &Session{
		JTI:       pair.RefreshJTI,
		CreatedAt: s.now().UTC(),
		ExpiresAt: pair.RefreshExpiresAt,
		TokenHash: HashRefreshToken(pair.RefreshToken),
		UID:       account.UID,
	}
	if err := s.sessions.RotateSession(ctx, oldHash, newSession); err != nil {
		if errors.Is(err, ErrSessionCompromised) {
			if revokeErr := s.sessions.RevokeAllUserSessions(ctx, session.UID); revokeErr != nil {
				return TokenPair{}, fmt.Errorf("revoke compromised user sessions: %w", revokeErr)
			}
			return TokenPair{}, ErrSessionCompromised
		}
		return TokenPair{}, fmt.Errorf("rotate refresh session: %w", err)
	}
	return pair, nil
}

// HashRefreshToken returns the stable database lookup value for a refresh
// token. The raw credential is never persisted.
func HashRefreshToken(rawToken string) string {
	digest := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(digest[:])
}

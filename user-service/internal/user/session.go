// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Implemented the recorded login and refresh session application services.
// Author review: COMPLETED BY ZI YANG

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Refactored the refresh-session workflow into focused private helpers.
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
func NewLoginService(a *Authenticator, sessions SessionRepository, now func() time.Time) *LoginService {
	if now == nil {
		now = time.Now
	}
	return &LoginService{
		authenticator: a,
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

	account, err := s.authenticator.authenticate(ctx, identifier, password)
	if err != nil {
		return TokenPair{}, err
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
	if err := s.validateDependencies(); err != nil {
		return TokenPair{}, err
	}
	claims, err := s.verifyClaims(ctx, rawToken)
	if err != nil {
		return TokenPair{}, err
	}
	session, oldHash, err := s.activeSession(ctx, rawToken, claims)
	if err != nil {
		return TokenPair{}, err
	}
	account, err := s.activeAccount(ctx, claims)
	if err != nil {
		return TokenPair{}, err
	}
	pair, newSession, err := s.issueRefreshSession(ctx, account)
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.rotateSession(ctx, session.UID, oldHash, newSession); err != nil {
		return TokenPair{}, err
	}
	return pair, nil
}

func (s *RefreshService) validateDependencies() error {
	if s.repository == nil || s.sessions == nil || s.verifier == nil || s.issuer == nil {
		return errors.New("refresh dependencies are required")
	}
	return nil
}

func (s *RefreshService) verifyClaims(ctx context.Context, rawToken string) (RefreshClaims, error) {
	claims, err := s.verifier.VerifyRefresh(ctx, rawToken)
	if err != nil {
		return RefreshClaims{}, fmt.Errorf("verify refresh token: %w", err)
	}
	if claims.UserID == uuid.Nil || claims.JTI == uuid.Nil {
		return RefreshClaims{}, ErrSessionNotFound
	}
	return claims, nil
}

func (s *RefreshService) activeSession(ctx context.Context, rawToken string, claims RefreshClaims) (*Session, string, error) {
	oldHash := HashRefreshToken(rawToken)
	session, err := s.sessions.GetSessionByHash(ctx, oldHash)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, "", ErrSessionNotFound
		}
		return nil, "", fmt.Errorf("get refresh session: %w", err)
	}
	if session == nil || session.UID != claims.UserID || session.JTI != claims.JTI {
		return nil, "", ErrSessionNotFound
	}
	if session.RevokedAt != nil || session.ReplacedByTokenHash != nil {
		if err := s.revokeCompromisedSessions(ctx, session.UID); err != nil {
			return nil, "", err
		}
		return nil, "", ErrSessionCompromised
	}
	if !session.ExpiresAt.After(s.now()) {
		return nil, "", ErrSessionNotFound
	}
	return session, oldHash, nil
}

func (s *RefreshService) activeAccount(ctx context.Context, claims RefreshClaims) (*User, error) {
	account, err := s.repository.GetByID(ctx, claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("get refresh account: %w", err)
	}
	if account == nil || account.AccountStatus == AccountStatusSuspended || (!account.TokensValidAfter.IsZero() && claims.IssuedAt.Before(account.TokensValidAfter)) {
		return nil, ErrAccountSuspended
	}
	return account, nil
}

func (s *RefreshService) issueRefreshSession(ctx context.Context, account *User) (TokenPair, *Session, error) {
	pair, err := s.issuer.Issue(ctx, account)
	if err != nil {
		return TokenPair{}, nil, fmt.Errorf("issue rotated JWT session: %w", err)
	}
	if pair.RefreshJTI == uuid.Nil || pair.RefreshExpiresAt.IsZero() {
		return TokenPair{}, nil, errors.New("issuer returned incomplete refresh metadata")
	}
	return pair, &Session{
		JTI:       pair.RefreshJTI,
		CreatedAt: s.now().UTC(),
		ExpiresAt: pair.RefreshExpiresAt,
		TokenHash: HashRefreshToken(pair.RefreshToken),
		UID:       account.UID,
	}, nil
}

func (s *RefreshService) rotateSession(ctx context.Context, uid uuid.UUID, oldHash string, newSession *Session) error {
	if err := s.sessions.RotateSession(ctx, oldHash, newSession); err != nil {
		if errors.Is(err, ErrSessionCompromised) {
			if revokeErr := s.revokeCompromisedSessions(ctx, uid); revokeErr != nil {
				return revokeErr
			}
			return ErrSessionCompromised
		}
		return fmt.Errorf("rotate refresh session: %w", err)
	}
	return nil
}

func (s *RefreshService) revokeCompromisedSessions(ctx context.Context, uid uuid.UUID) error {
	if err := s.sessions.RevokeAllUserSessions(ctx, uid); err != nil {
		return fmt.Errorf("revoke compromised user sessions: %w", err)
	}
	return nil
}

// HashRefreshToken returns the stable database lookup value for a refresh
// token. The raw credential is never persisted.
func HashRefreshToken(rawToken string) string {
	digest := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(digest[:])
}

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Implemented the recorded login and refresh session application services.
// Author review: COMPLETED BY ZI YANG

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Refactored the refresh-session workflow into focused private helpers.
// Author review: Repackaged and validated correctness

package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"foc/user-service/internal/user"

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

// AI-generated (edited by ZI YANG): simplifies the interface documentation to
// its input and returned claims without describing its implementation boundary.
// RefreshTokenVerifier verifies a refresh token and returns its claims.
type RefreshTokenVerifier interface {
	VerifyRefresh(ctx context.Context, rawToken string) (RefreshClaims, error)
}

// AI-generated (edited by ZI YANG): keeps the Redis vendor dependency at the application boundary while enforcing the recorded refresh debounce.
// RefreshLock serializes refresh attempts for the same hashed credential.
type RefreshLock interface {
	Acquire(ctx context.Context, tokenHash string, ttl time.Duration) (bool, error)
}

const refreshLockTTL = 5 * time.Second

// LoginService authenticates an account and records its refresh session.
// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — implements the
// approved role-change concurrency and credential safeguards. Author review: ZI YANG - reviewed and verified.
type LoginService struct {
	authenticator *Authenticator
	sessions      CredentialSessionRepository
	now           func() time.Time
	logger        *slog.Logger
}

// NewLoginService constructs a login service with its persistence and token
// dependencies ready for use. A nil logger discards the service's logs.
// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — implements the
// approved role-change concurrency and credential safeguards. Author review: ZI YANG - reviewed and verified.
func NewLoginService(a *Authenticator, sessions CredentialSessionRepository, now func() time.Time, logger *slog.Logger) *LoginService {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &LoginService{
		authenticator: a,
		sessions:      sessions,
		now:           now,
		logger:        logger,
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
	if err := s.authenticator.repository.UpdateLastLoginByID(ctx, account.UID, s.now().UTC()); err != nil {
		// AI-generated (edited by ZI YANG): login analytics must not make authentication unavailable.
		s.logger.WarnContext(ctx, "record last login failed", "user_id", account.UID.String(), "error", err.Error())
	}
	// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — reread the
	// role under a shared account lock and return tokens only after commit.
	// Author review: ZI YANG - reviewed and verified.
	var pair TokenPair
	err = s.sessions.WithAccountCredentials(ctx, account.UID, func(current *user.User, sessions user.SessionRepository) error {
		if current.PasswordHash != account.PasswordHash {
			return user.ErrInvalidCredentials
		}
		if current.AccountStatus == user.AccountStatusSuspended {
			return user.ErrAccountSuspended
		}
		if current.AccountStatus != user.AccountStatusActive {
			return user.ErrAccountUnverified
		}
		var err error
		pair, err = s.authenticator.issuer.Issue(ctx, current)
		if err != nil {
			return fmt.Errorf("issue JWT session: %w", err)
		}
		if pair.RefreshJTI == uuid.Nil || pair.RefreshExpiresAt.IsZero() {
			return errors.New("issuer returned incomplete refresh metadata")
		}
		return sessions.CreateSession(ctx, &user.Session{
			JTI: pair.RefreshJTI, CreatedAt: s.now().UTC(), ExpiresAt: pair.RefreshExpiresAt,
			TokenHash: HashRefreshToken(pair.RefreshToken), UID: current.UID,
		})
	})
	if err != nil {
		return TokenPair{}, fmt.Errorf("persist login credentials: %w", err)
	}
	return pair, nil
}

// RefreshService rotates refresh sessions and detects replayed credentials.
// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — implements the
// approved role-change concurrency and credential safeguards. Author review: ZI YANG - reviewed and verified.
type RefreshService struct {
	repository user.UserRepository
	sessions   CredentialSessionRepository
	verifier   RefreshTokenVerifier
	issuer     TokenIssuer
	lock       RefreshLock
	now        func() time.Time
}

// NewRefreshService constructs a refresh service with injected token and
// persistence boundaries.
// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — implements the
// approved role-change concurrency and credential safeguards. Author review: ZI YANG - reviewed and verified.
func NewRefreshService(repository user.UserRepository, sessions CredentialSessionRepository, verifier RefreshTokenVerifier, issuer TokenIssuer, lock RefreshLock, now func() time.Time) *RefreshService {
	if now == nil {
		now = time.Now
	}
	return &RefreshService{repository: repository, sessions: sessions, verifier: verifier, issuer: issuer, lock: lock, now: now}
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
	// AI-generated (edited by ZI YANG): hash before locking so Redis never receives the raw refresh credential.
	oldHash := HashRefreshToken(rawToken)
	if err := s.acquireRefreshLock(ctx, oldHash); err != nil {
		return TokenPair{}, err
	}
	session, err := s.activeSession(ctx, oldHash, claims)
	if err != nil {
		return TokenPair{}, err
	}
	// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — share the
	// account lock with role changes through rotation. Author review: ZI YANG - reviewed and verified.
	var pair TokenPair
	err = s.sessions.WithAccountCredentials(ctx, claims.UserID, func(account *user.User, sessions user.SessionRepository) error {
		if err := validateRefreshAccount(account, claims); err != nil {
			return err
		}
		var next *user.Session
		var err error
		pair, next, err = s.issueRefreshSession(ctx, account)
		if err != nil {
			return err
		}
		return sessions.RotateSession(ctx, oldHash, next)
	})
	if err != nil {
		// Release the account/session transaction before replay revocation.
		if errors.Is(err, user.ErrSessionCompromised) {
			if revokeErr := s.revokeCompromisedSessions(ctx, session.UID); revokeErr != nil {
				return TokenPair{}, revokeErr
			}
		}
		return TokenPair{}, fmt.Errorf("refresh credentials: %w", err)
	}
	return pair, nil
}

func (s *RefreshService) validateDependencies() error {
	if s.repository == nil || s.sessions == nil || s.verifier == nil || s.issuer == nil || s.lock == nil {
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
		return RefreshClaims{}, user.ErrSessionNotFound
	}
	return claims, nil
}

func (s *RefreshService) acquireRefreshLock(ctx context.Context, oldHash string) error {
	// AI-generated (edited by ZI YANG): reject concurrent refreshes before any session query so one client cannot trigger its own replay defense.
	locked, err := s.lock.Acquire(ctx, oldHash, refreshLockTTL)
	if err != nil {
		return fmt.Errorf("acquire refresh lock: %w", err)
	}
	if !locked {
		return user.ErrRefreshInProgress
	}
	return nil
}

func (s *RefreshService) activeSession(ctx context.Context, oldHash string, claims RefreshClaims) (*user.Session, error) {
	session, err := s.sessions.GetSessionByHash(ctx, oldHash)
	if err != nil {
		if errors.Is(err, user.ErrSessionNotFound) {
			return nil, user.ErrSessionNotFound
		}
		return nil, fmt.Errorf("get refresh session: %w", err)
	}
	if session == nil || session.UID != claims.UserID || session.JTI != claims.JTI {
		return nil, user.ErrSessionNotFound
	}
	if session.ReplacedByTokenHash != nil {
		if err := s.revokeCompromisedSessions(ctx, session.UID); err != nil {
			return nil, err
		}
		return nil, user.ErrSessionCompromised
	}
	if session.RevokedAt != nil {
		// AI Assistance Disclosure: Codex (GPT-5), 2026-09-28 — preserves the
		// recorded distinction between normal logout and rotated-token replay.
		// Author review: ZI YANG - validated correctness.
		return nil, user.ErrSessionNotFound
	}
	if !session.ExpiresAt.After(s.now()) {
		return nil, user.ErrSessionNotFound
	}
	return session, nil
}

// validateRefreshAccount uses the state read after acquiring the shared lock.
// AI Assistance Disclosure: Codex (GPT-6), 2026-11-09 — implements the
// approved role-change concurrency and credential safeguards. Author review: ZI YANG - reviewed and verified.
func validateRefreshAccount(account *user.User, claims RefreshClaims) error {
	if account == nil || account.AccountStatus != user.AccountStatusActive || (!account.TokensValidAfter.IsZero() && claims.IssuedAt.Before(account.TokensValidAfter)) {
		return user.ErrAccountSuspended
	}
	return nil
}

func (s *RefreshService) issueRefreshSession(ctx context.Context, account *user.User) (TokenPair, *user.Session, error) {
	pair, err := s.issuer.Issue(ctx, account)
	if err != nil {
		return TokenPair{}, nil, fmt.Errorf("issue rotated JWT session: %w", err)
	}
	if pair.RefreshJTI == uuid.Nil || pair.RefreshExpiresAt.IsZero() {
		return TokenPair{}, nil, errors.New("issuer returned incomplete refresh metadata")
	}
	return pair, &user.Session{
		JTI:       pair.RefreshJTI,
		CreatedAt: s.now().UTC(),
		ExpiresAt: pair.RefreshExpiresAt,
		TokenHash: HashRefreshToken(pair.RefreshToken),
		UID:       account.UID,
	}, nil
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

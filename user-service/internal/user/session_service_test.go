// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added focused tests for login session creation, refresh rotation, and replay handling.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeSessionRepository struct {
	sessions       map[string]*Session
	created        *Session
	rotatedOldHash string
	revokedAllFor  uuid.UUID
	getErr         error
	rotateErr      error
}

func (f *fakeSessionRepository) CreateSession(_ context.Context, session *Session) error {
	if f.sessions == nil {
		f.sessions = make(map[string]*Session)
	}
	f.created = session
	f.sessions[session.TokenHash] = session
	return nil
}

func (f *fakeSessionRepository) GetSessionByHash(_ context.Context, hash string) (*Session, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.sessions[hash], nil
}

func (f *fakeSessionRepository) RotateSession(_ context.Context, oldHash string, newSession *Session) error {
	if f.rotateErr != nil {
		return f.rotateErr
	}
	f.rotatedOldHash = oldHash
	old := f.sessions[oldHash]
	if old != nil {
		replaced := newSession.TokenHash
		now := time.Now()
		old.RevokedAt = &now
		old.ReplacedByTokenHash = &replaced
	}
	f.sessions[newSession.TokenHash] = newSession
	return nil
}

func (f *fakeSessionRepository) RevokeSessionByHash(context.Context, string) error { return nil }

func (f *fakeSessionRepository) RevokeAllUserSessions(_ context.Context, uid uuid.UUID) error {
	f.revokedAllFor = uid
	return nil
}

type fakeRefreshVerifier struct {
	claims RefreshClaims
	err    error
}

func (f *fakeRefreshVerifier) VerifyRefresh(context.Context, string) (RefreshClaims, error) {
	return f.claims, f.err
}

func newSessionServiceFixtures(t *testing.T) (*User, *fakeAuthRepository, *fakeSessionRepository, *fakeTokenIssuer, time.Time) {
	t.Helper()
	passwordHash, err := HashPassword("ValidPass1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	account := &User{
		UID:              uuid.New(),
		PasswordHash:     passwordHash,
		AccountStatus:    AccountStatusActive,
		AccountRole:      AccountRoleStudent,
		TokensValidAfter: now.Add(-time.Hour),
	}
	repo := &fakeAuthRepository{user: account}
	sessions := &fakeSessionRepository{sessions: make(map[string]*Session)}
	issuer := &fakeTokenIssuer{pair: TokenPair{
		AccessToken:      "access",
		RefreshToken:     "refresh",
		RefreshJTI:       uuid.New(),
		RefreshExpiresAt: now.Add(7 * 24 * time.Hour),
	}}
	return account, repo, sessions, issuer, now
}

func TestLoginServiceStoresHashedRefreshSession(t *testing.T) {
	account, repo, sessions, issuer, now := newSessionServiceFixtures(t)
	authenticator := NewAuthenticator(repo, issuer)
	service := NewLoginService(authenticator, sessions, func() time.Time { return now })

	pair, err := service.Login(context.Background(), "user@example.com", "ValidPass1")
	if err != nil {
		t.Fatal(err)
	}
	if pair.AccessToken != "access" || sessions.created == nil {
		t.Fatalf("login result/session = %#v/%#v", pair, sessions.created)
	}
	if sessions.created.TokenHash != HashRefreshToken(pair.RefreshToken) || sessions.created.TokenHash == pair.RefreshToken {
		t.Fatalf("stored refresh token = %q, want hash", sessions.created.TokenHash)
	}
	if sessions.created.UID != account.UID || sessions.created.JTI != issuer.pair.RefreshJTI {
		t.Fatalf("stored session = %#v", sessions.created)
	}
}

func TestRefreshServiceRotatesValidSession(t *testing.T) {
	account, repo, sessions, issuer, now := newSessionServiceFixtures(t)
	oldToken := "old-refresh"
	oldHash := HashRefreshToken(oldToken)
	oldJTI := uuid.New()
	sessions.sessions[oldHash] = &Session{UID: account.UID, JTI: oldJTI, TokenHash: oldHash, ExpiresAt: now.Add(time.Hour)}
	verifier := &fakeRefreshVerifier{claims: RefreshClaims{UserID: account.UID, JTI: oldJTI, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}}
	service := NewRefreshService(repo, sessions, verifier, issuer, func() time.Time { return now })

	if _, err := service.Refresh(context.Background(), oldToken); err != nil {
		t.Fatal(err)
	}
	if sessions.rotatedOldHash != oldHash {
		t.Fatalf("rotated hash = %q, want %q", sessions.rotatedOldHash, oldHash)
	}
	if sessions.created != nil {
		t.Fatalf("login session unexpectedly created during refresh: %#v", sessions.created)
	}
}

func TestRefreshServiceRevokesAllSessionsOnReplay(t *testing.T) {
	account, repo, sessions, issuer, now := newSessionServiceFixtures(t)
	oldToken := "replayed-refresh"
	oldHash := HashRefreshToken(oldToken)
	oldJTI := uuid.New()
	revokedAt := now.Add(-time.Minute)
	sessions.sessions[oldHash] = &Session{UID: account.UID, JTI: oldJTI, TokenHash: oldHash, RevokedAt: &revokedAt, ExpiresAt: now.Add(time.Hour)}
	verifier := &fakeRefreshVerifier{claims: RefreshClaims{UserID: account.UID, JTI: oldJTI, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}}
	service := NewRefreshService(repo, sessions, verifier, issuer, func() time.Time { return now })

	_, err := service.Refresh(context.Background(), oldToken)
	if !errors.Is(err, ErrSessionCompromised) {
		t.Fatalf("error = %v, want compromised session", err)
	}
	if sessions.revokedAllFor != account.UID {
		t.Fatalf("revoked user = %v, want %v", sessions.revokedAllFor, account.UID)
	}
}

func TestRefreshServiceRejectsUnknownSession(t *testing.T) {
	account, repo, sessions, issuer, now := newSessionServiceFixtures(t)
	verifier := &fakeRefreshVerifier{claims: RefreshClaims{UserID: account.UID, JTI: uuid.New()}}
	service := NewRefreshService(repo, sessions, verifier, issuer, func() time.Time { return now })

	_, err := service.Refresh(context.Background(), "missing-refresh")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("error = %v, want missing session", err)
	}
}

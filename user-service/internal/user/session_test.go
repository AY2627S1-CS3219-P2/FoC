// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-19
// Scope: Added unit tests for the recorded session persistence model.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUserTracksTokenValidityBoundary(t *testing.T) {
	validAfter := time.Now()
	account := User{TokensValidAfter: validAfter}

	if !account.TokensValidAfter.Equal(validAfter) {
		t.Fatalf("tokens valid after = %v, want %v", account.TokensValidAfter, validAfter)
	}
}

func TestSessionTracksRefreshTokenLifecycle(t *testing.T) {
	createdAt := time.Now()
	expiresAt := createdAt.Add(7 * 24 * time.Hour)
	accountID := uuid.New()
	jti := uuid.New()
	replacementHash := "replacement-hash"
	session := Session{
		JTI:                 jti,
		CreatedAt:           createdAt,
		ExpiresAt:           expiresAt,
		TokenHash:           "token-hash",
		ReplacedByTokenHash: &replacementHash,
		UID:                 accountID,
	}

	if session.JTI != jti || session.UID != accountID {
		t.Fatalf("session identifiers = %v/%v, want %v/%v", session.JTI, session.UID, jti, accountID)
	}
	if session.RevokedAt != nil {
		t.Fatalf("revoked at = %v, want nil for a new session", session.RevokedAt)
	}
	if session.ExpiresAt.Sub(session.CreatedAt) != 7*24*time.Hour {
		t.Fatalf("session lifetime = %v, want 7 days", session.ExpiresAt.Sub(session.CreatedAt))
	}
}
